package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"errors"
	"regexp"
	"strings"

	"codex-commons/internal/domain"
)

var humanHandlePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,62}[a-z0-9])?$`)

func humanProfileRequestDigest(request domain.UpdateHumanProfileRequest) [sha256.Size]byte {
	hash := sha256.New()
	var encoded [8]byte
	writeString := func(value string) {
		binary.BigEndian.PutUint64(encoded[:], uint64(len(value)))
		_, _ = hash.Write(encoded[:])
		_, _ = hash.Write([]byte(value))
	}
	writeString(request.Principal)
	binary.BigEndian.PutUint64(encoded[:], uint64(request.BaseRevision))
	_, _ = hash.Write(encoded[:])
	writeString(request.DisplayName)
	writeString(request.Handle)
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}

func validHumanProfile(displayName, handle string) bool {
	return len(displayName) >= 1 && len(displayName) <= 200 && displayName == strings.TrimSpace(displayName) &&
		len(handle) >= 3 && len(handle) <= 64 && handle == strings.ToLower(handle) && humanHandlePattern.MatchString(handle)
}

func scanHumanBinding(scanner interface{ Scan(...any) error }) (domain.HumanAccountBinding, error) {
	var binding domain.HumanAccountBinding
	var digest []byte
	var created, updated string
	if err := scanner.Scan(&binding.Principal, &binding.Provider, &digest, &binding.DisplayName, &binding.Handle, &binding.Revision, &created, &updated); err != nil {
		return binding, mapErr(err)
	}
	if len(digest) != 32 {
		return domain.HumanAccountBinding{}, errors.New("stored human account digest has invalid length")
	}
	binding.ProviderSubjectDigest = append([]byte(nil), digest...)
	binding.CreatedAt = parseStamp(created)
	binding.UpdatedAt = parseStamp(updated)
	return binding, nil
}

func getHumanBindingTx(ctx context.Context, tx *sql.Tx) (domain.HumanAccountBinding, error) {
	return scanHumanBinding(tx.QueryRowContext(ctx, `SELECT principal,provider,provider_subject_digest,display_name,handle,revision,created_at,updated_at FROM human_account_bindings WHERE principal=?`, domain.HumanLocalPrincipal))
}

// GetHumanAccountBinding returns the single installation binding without ever
// exposing a provider email or other provider credential material.
func (s *Store) GetHumanAccountBinding(ctx context.Context) (domain.HumanAccountBinding, error) {
	if s == nil || s.db == nil {
		return domain.HumanAccountBinding{}, domain.ErrUnavailable
	}
	return scanHumanBinding(s.db.QueryRowContext(ctx, `SELECT principal,provider,provider_subject_digest,display_name,handle,revision,created_at,updated_at FROM human_account_bindings WHERE principal=?`, domain.HumanLocalPrincipal))
}

func (s *Store) BindHumanAccount(ctx context.Context, request domain.BindHumanAccountRequest) (domain.HumanAccountBinding, error) {
	if s == nil || s.db == nil || len(request.ProviderSubjectDigest) != 32 || !validHumanProfile(request.DisplayName, request.Handle) {
		return domain.HumanAccountBinding{}, domain.ErrInvalid
	}
	s.humanAuthMu.Lock()
	defer s.humanAuthMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.HumanAccountBinding{}, mapErr(err)
	}
	defer tx.Rollback()

	if current, lookupErr := getHumanBindingTx(ctx, tx); lookupErr == nil {
		if len(current.ProviderSubjectDigest) == len(request.ProviderSubjectDigest) &&
			subtle.ConstantTimeCompare(current.ProviderSubjectDigest, request.ProviderSubjectDigest) == 1 {
			return current, nil
		}
		return domain.HumanAccountBinding{}, domain.ErrConflict
	} else if !errors.Is(lookupErr, domain.ErrNotFound) {
		return domain.HumanAccountBinding{}, lookupErr
	}

	now := stamp(s.now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO human_account_bindings(principal,provider,provider_subject_digest,display_name,handle,revision,created_at,updated_at) VALUES(?,?,?,?,?,1,?,?)`, domain.HumanLocalPrincipal, "chatgpt", request.ProviderSubjectDigest, request.DisplayName, request.Handle, now, now); err != nil {
		return domain.HumanAccountBinding{}, mapErr(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO human_auth_events(id,principal,event_type,binding_revision,request_key,recorded_at) VALUES(?,?,?,1,'',?)`, newID("HAE-"), domain.HumanLocalPrincipal, "account_bound", now); err != nil {
		return domain.HumanAccountBinding{}, mapErr(err)
	}
	if err := tx.Commit(); err != nil {
		return domain.HumanAccountBinding{}, mapErr(err)
	}
	return s.GetHumanAccountBinding(ctx)
}

func (s *Store) UpdateHumanProfile(ctx context.Context, request domain.UpdateHumanProfileRequest) (domain.HumanAccountBinding, error) {
	if s == nil || s.db == nil || request.Principal != domain.HumanLocalPrincipal || request.BaseRevision < 1 ||
		!validHumanProfile(request.DisplayName, request.Handle) || request.IdempotencyKey == "" || len(request.IdempotencyKey) > 200 || strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey {
		return domain.HumanAccountBinding{}, domain.ErrInvalid
	}
	s.humanAuthMu.Lock()
	defer s.humanAuthMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.HumanAccountBinding{}, mapErr(err)
	}
	defer tx.Rollback()

	requestDigest := humanProfileRequestDigest(request)
	var eventRevision int64
	var eventDigest []byte
	if err := tx.QueryRowContext(ctx, `SELECT binding_revision,request_digest FROM human_auth_events WHERE principal=? AND event_type='profile_updated' AND request_key=?`, domain.HumanLocalPrincipal, request.IdempotencyKey).Scan(&eventRevision, &eventDigest); err == nil {
		if len(eventDigest) != sha256.Size || subtle.ConstantTimeCompare(eventDigest, requestDigest[:]) != 1 {
			return domain.HumanAccountBinding{}, domain.ErrConflict
		}
		binding, bindingErr := getHumanBindingTx(ctx, tx)
		if bindingErr != nil {
			return domain.HumanAccountBinding{}, bindingErr
		}
		return binding, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.HumanAccountBinding{}, mapErr(err)
	}

	current, err := getHumanBindingTx(ctx, tx)
	if err != nil {
		return domain.HumanAccountBinding{}, err
	}
	if current.Revision != request.BaseRevision {
		return domain.HumanAccountBinding{}, domain.ErrConflict
	}
	now := stamp(s.now())
	result, err := tx.ExecContext(ctx, `UPDATE human_account_bindings SET display_name=?,handle=?,revision=revision+1,updated_at=? WHERE principal=? AND revision=?`, request.DisplayName, request.Handle, now, domain.HumanLocalPrincipal, request.BaseRevision)
	if err != nil {
		return domain.HumanAccountBinding{}, mapErr(err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.HumanAccountBinding{}, domain.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO human_auth_events(id,principal,event_type,binding_revision,request_key,request_digest,recorded_at) VALUES(?,?,?,?,?,?,?)`, newID("HAE-"), domain.HumanLocalPrincipal, "profile_updated", request.BaseRevision+1, request.IdempotencyKey, requestDigest[:], now); err != nil {
		return domain.HumanAccountBinding{}, mapErr(err)
	}
	if err := tx.Commit(); err != nil {
		return domain.HumanAccountBinding{}, mapErr(err)
	}
	return s.GetHumanAccountBinding(ctx)
}

// RecordHumanAuthEvent records a durable outcome and is idempotent when the
// caller supplies a request key. It is used for recovery-login audit events;
// account_bound and profile_updated are committed by their store operations.
func (s *Store) RecordHumanAuthEvent(ctx context.Context, request domain.HumanAuthEventRequest) error {
	if s == nil || s.db == nil || request.Principal != domain.HumanLocalPrincipal || request.EventType != "recovery_login" || request.BindingRevision < 0 || request.IdempotencyKey == "" || len(request.IdempotencyKey) > 200 || strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey {
		return domain.ErrInvalid
	}
	s.humanAuthMu.Lock()
	defer s.humanAuthMu.Unlock()
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM human_auth_events WHERE principal=? AND event_type=? AND request_key=?`, request.Principal, request.EventType, request.IdempotencyKey).Scan(&exists); err != nil {
		return mapErr(err)
	}
	if exists != 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO human_auth_events(id,principal,event_type,binding_revision,request_key,recorded_at) VALUES(?,?,?,?,?,?)`, newID("HAE-"), request.Principal, request.EventType, request.BindingRevision, request.IdempotencyKey, stamp(s.now()))
	return mapErr(err)
}
