package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"codex-commons/internal/domain"
)

const (
	archaeologyPersistenceMaxAttempts = 8
	archaeologyPersistenceMaxRows     = 32
	archaeologyPersistenceLease       = 30 * time.Second
	archaeologyPersistenceRetryBase   = 100 * time.Millisecond
	archaeologyPersistenceRetryMax    = 30 * time.Second
)

// SQLite compares the scheduling columns as TEXT.  Fixed-width UTC output is
// therefore required: RFC3339Nano's variable fractional precision would sort
// 100ms after 1s before the 1s value lexicographically.
func archaeologyPersistenceStamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

// These payloads are intentionally closed structs.  In particular, maps and
// arbitrary JSON are not accepted: encoding/json gives these structs stable
// key order, so the stored bytes and digest are deterministic across retries.
type archaeologyNativeFailStartPayload struct {
	Launch    domain.ArchaeologyLaunchResult `json:"launch"`
	Uncertain bool                           `json:"uncertain"`
}

type archaeologyNativeIdentityPayload struct {
	ThreadID       string `json:"thread_id"`
	CodexSessionID string `json:"codex_session_id"`
	TurnID         string `json:"turn_id"`
}

type archaeologyNativeTurnPayload struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
}

type archaeologyNativeCompletePayload struct {
	ThreadID   string `json:"thread_id"`
	TurnID     string `json:"turn_id"`
	Status     string `json:"status"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
}

type persistenceIntentRow struct {
	domain.ArchaeologyNativePersistenceIntentRecord
	payloadJSON string
}

type persistenceScanner interface {
	Scan(...any) error
}

func validatePersistenceLaunch(launch domain.ArchaeologyLaunchResult) bool {
	return boundedCoreText(launch.LaunchID, 200, false) &&
		boundedCoreText(launch.ProjectID, 200, false) &&
		boundedCoreText(launch.State, 120, false) &&
		boundedCoreText(launch.ThreadID, 120, false) &&
		boundedCoreText(launch.CodexSessionID, 120, false) &&
		boundedCoreText(launch.TurnID, 120, false) &&
		boundedCoreText(launch.Error, 200, false)
}

func emptyPersistenceLaunch(launch domain.ArchaeologyLaunchResult) bool {
	return launch == (domain.ArchaeologyLaunchResult{})
}

func validPersistenceDuration(duration *int64) bool {
	return duration == nil || *duration >= 0 && *duration <= 604800000
}

func canonicalArchaeologyNativePersistence(intent domain.ArchaeologyNativePersistenceIntent) (domain.ArchaeologyNativePersistenceOperation, []byte, [32]byte, error) {
	var zero [32]byte
	if !boundedCoreText(intent.JobID, 120, true) {
		return "", nil, zero, domain.ErrInvalid
	}

	var payload any
	switch intent.Operation {
	case domain.ArchaeologyNativePersistenceFailStart:
		if !validatePersistenceLaunch(intent.Launch) || !boundedCoreText(intent.ThreadID, 120, false) ||
			!boundedCoreText(intent.CodexSessionID, 120, false) || !boundedCoreText(intent.TurnID, 120, false) ||
			!boundedCoreText(intent.Status, 120, false) || !validPersistenceDuration(intent.DurationMS) {
			return "", nil, zero, domain.ErrInvalid
		}
		if intent.ThreadID != "" || intent.CodexSessionID != "" || intent.TurnID != "" || intent.Status != "" || intent.DurationMS != nil {
			return "", nil, zero, domain.ErrInvalid
		}
		payload = archaeologyNativeFailStartPayload{Launch: intent.Launch, Uncertain: intent.Uncertain}
	case domain.ArchaeologyNativePersistenceBindIdentity:
		if !boundedCoreText(intent.ThreadID, 120, true) || !boundedCoreText(intent.CodexSessionID, 120, true) || !boundedCoreText(intent.TurnID, 120, true) ||
			!emptyPersistenceLaunch(intent.Launch) || intent.Uncertain || intent.Status != "" || intent.DurationMS != nil {
			return "", nil, zero, domain.ErrInvalid
		}
		payload = archaeologyNativeIdentityPayload{ThreadID: intent.ThreadID, CodexSessionID: intent.CodexSessionID, TurnID: intent.TurnID}
	case domain.ArchaeologyNativePersistenceActivate:
		if !boundedCoreText(intent.ThreadID, 120, true) || !boundedCoreText(intent.TurnID, 120, true) ||
			intent.CodexSessionID != "" || !emptyPersistenceLaunch(intent.Launch) || intent.Uncertain || intent.Status != "" || intent.DurationMS != nil {
			return "", nil, zero, domain.ErrInvalid
		}
		payload = archaeologyNativeTurnPayload{ThreadID: intent.ThreadID, TurnID: intent.TurnID}
	case domain.ArchaeologyNativePersistenceLoseTurn:
		if !boundedCoreText(intent.ThreadID, 120, true) || !boundedCoreText(intent.TurnID, 120, true) ||
			intent.CodexSessionID != "" || !emptyPersistenceLaunch(intent.Launch) || intent.Uncertain || intent.Status != "" || intent.DurationMS != nil {
			return "", nil, zero, domain.ErrInvalid
		}
		payload = archaeologyNativeTurnPayload{ThreadID: intent.ThreadID, TurnID: intent.TurnID}
	case domain.ArchaeologyNativePersistenceCompleteTurn:
		if !boundedCoreText(intent.ThreadID, 120, true) || !boundedCoreText(intent.TurnID, 120, true) ||
			!boundedCoreText(intent.Status, 32, true) || (intent.Status != "completed" && intent.Status != "interrupted" && intent.Status != "failed") ||
			!validPersistenceDuration(intent.DurationMS) || intent.CodexSessionID != "" || !emptyPersistenceLaunch(intent.Launch) || intent.Uncertain {
			return "", nil, zero, domain.ErrInvalid
		}
		payload = archaeologyNativeCompletePayload{ThreadID: intent.ThreadID, TurnID: intent.TurnID, Status: intent.Status, DurationMS: intent.DurationMS}
	default:
		return "", nil, zero, domain.ErrInvalid
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", nil, zero, err
	}
	digest := sha256.Sum256(payloadJSON)
	return intent.Operation, payloadJSON, digest, nil
}

func parseArchaeologyPersistenceStamp(value string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || archaeologyPersistenceStamp(t) != value {
		return time.Time{}, false
	}
	return t, true
}

func optionalPersistenceStamp(value sql.NullString) (*time.Time, bool) {
	if !value.Valid || value.String == "" {
		return nil, true
	}
	t, ok := parseArchaeologyPersistenceStamp(value.String)
	if !ok {
		return nil, false
	}
	return &t, true
}

func scanPersistenceIntent(scanner persistenceScanner) (persistenceIntentRow, error) {
	var (
		row                         persistenceIntentRow
		digest                      []byte
		next, leaseExpires, applied sql.NullString
		created, updated            string
	)
	err := scanner.Scan(
		&row.ID, &row.JobID, &row.Operation, &digest, &row.payloadJSON, &row.State,
		&row.Attempts, &next, &row.LeaseOwner, &leaseExpires, &row.LastErrorCode,
		&applied, &created, &updated,
	)
	if err != nil {
		return persistenceIntentRow{}, err
	}
	if len(digest) != sha256.Size {
		return persistenceIntentRow{}, fmt.Errorf("%w: persistence digest length", domain.ErrInvalid)
	}
	copy(row.PayloadDigest[:], digest)
	var ok bool
	if row.NextAttemptAt, ok = optionalPersistenceStamp(next); !ok {
		return persistenceIntentRow{}, fmt.Errorf("%w: invalid next_attempt_at", domain.ErrInvalid)
	}
	if row.LeaseExpiresAt, ok = optionalPersistenceStamp(leaseExpires); !ok {
		return persistenceIntentRow{}, fmt.Errorf("%w: invalid lease_expires_at", domain.ErrInvalid)
	}
	if row.AppliedAt, ok = optionalPersistenceStamp(applied); !ok {
		return persistenceIntentRow{}, fmt.Errorf("%w: invalid applied_at", domain.ErrInvalid)
	}
	if row.CreatedAt, ok = parseArchaeologyPersistenceStamp(created); !ok {
		return persistenceIntentRow{}, fmt.Errorf("%w: invalid created_at", domain.ErrInvalid)
	}
	if row.UpdatedAt, ok = parseArchaeologyPersistenceStamp(updated); !ok {
		return persistenceIntentRow{}, fmt.Errorf("%w: invalid updated_at", domain.ErrInvalid)
	}
	return row, nil
}

func persistenceRecordFromRow(row persistenceIntentRow) domain.ArchaeologyNativePersistenceIntentRecord {
	return row.ArchaeologyNativePersistenceIntentRecord
}

// EnsureArchaeologyNativePersistenceIntent commits the pending ledger row
// before the caller is allowed to perform the repository mutation.  The
// unique job/operation key gives retries one stable durable identity.
func (s *Store) EnsureArchaeologyNativePersistenceIntent(ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent) (domain.ArchaeologyNativePersistenceIntentRecord, error) {
	operation, payload, digest, err := canonicalArchaeologyNativePersistence(intent)
	if err != nil {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, err
	}
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, err
	}
	defer tx.Rollback()
	row, scanErr := scanPersistenceIntent(tx.QueryRowContext(ctx, `
SELECT id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,
       lease_owner,lease_expires_at,last_error_code,applied_at,created_at,updated_at
FROM archaeology_native_persistence_intents WHERE job_id=? AND operation=?`, intent.JobID, string(operation)))
	if scanErr == nil {
		if !bytes.Equal(row.PayloadDigest[:], digest[:]) || row.payloadJSON != string(payload) {
			return domain.ArchaeologyNativePersistenceIntentRecord{}, domain.ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return domain.ArchaeologyNativePersistenceIntentRecord{}, err
		}
		return persistenceRecordFromRow(row), nil
	}
	if !errors.Is(scanErr, sql.ErrNoRows) {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, mapErr(scanErr)
	}
	created := archaeologyPersistenceStamp(now)
	id := deterministicHistoricalID("ARPI-", intent.JobID, string(operation))
	if _, err = tx.ExecContext(ctx, `
INSERT INTO archaeology_native_persistence_intents
 (id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,
  lease_owner,lease_expires_at,last_error_code,applied_at,created_at,updated_at)
VALUES(?,?,?,?,?,'pending',0,?,'',NULL,'',NULL,?,?)`,
		id, intent.JobID, string(operation), digest[:], string(payload), created, created, created); err != nil {
		mapped := mapErr(err)
		// Another process may have committed the same deterministic row after
		// our read but before this insert.  Treat that unique race as the exact
		// replay it is; a different payload still returns ErrConflict.
		if errors.Is(mapped, domain.ErrConflict) {
			_ = tx.Rollback()
			prior, readErr := scanPersistenceIntent(s.db.QueryRowContext(ctx, `
SELECT id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,
       lease_owner,lease_expires_at,last_error_code,applied_at,created_at,updated_at
FROM archaeology_native_persistence_intents WHERE id=?`, id))
			if readErr == nil && bytes.Equal(prior.PayloadDigest[:], digest[:]) && prior.payloadJSON == string(payload) {
				return persistenceRecordFromRow(prior), nil
			}
		}
		return domain.ArchaeologyNativePersistenceIntentRecord{}, mapped
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, err
	}
	return domain.ArchaeologyNativePersistenceIntentRecord{
		ID: id, JobID: intent.JobID, Operation: operation, PayloadDigest: digest,
		State: "pending", NextAttemptAt: &now, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// EnsureArchaeologyNativePersistence is the concise capability name used by
// scheduler callers; the Intent-suffixed method remains the explicit form.
func (s *Store) EnsureArchaeologyNativePersistence(ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent) (domain.ArchaeologyNativePersistenceIntentRecord, error) {
	return s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
}

// EnsureAndApplyArchaeologyNativePersistence is the normal synchronous path
// for a repository callback: the ledger commit happens first, then the
// replay-safe repository write and its acknowledgement are attempted.
func (s *Store) EnsureAndApplyArchaeologyNativePersistence(ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent) (domain.ArchaeologyNativePersistenceIntentRecord, error) {
	record, err := s.EnsureArchaeologyNativePersistenceIntent(ctx, intent)
	if err != nil {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, err
	}
	if err = s.ApplyArchaeologyNativePersistenceIntent(ctx, record.ID); err != nil {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, err
	}
	return s.ArchaeologyNativePersistenceIntent(ctx, record.ID)
}

func (s *Store) ArchaeologyNativePersistenceIntent(ctx context.Context, id string) (domain.ArchaeologyNativePersistenceIntentRecord, error) {
	if !boundedCoreText(id, 200, true) {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, domain.ErrInvalid
	}
	row, err := scanPersistenceIntent(s.db.QueryRowContext(ctx, `
SELECT id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,
       lease_owner,lease_expires_at,last_error_code,applied_at,created_at,updated_at
FROM archaeology_native_persistence_intents WHERE id=?`, id))
	if err != nil {
		return domain.ArchaeologyNativePersistenceIntentRecord{}, mapErr(err)
	}
	return persistenceRecordFromRow(row), nil
}

func (s *Store) ArchaeologyNativePersistenceStatus(ctx context.Context) (domain.ArchaeologyNativePersistenceStatus, error) {
	var status domain.ArchaeologyNativePersistenceStatus
	rows, err := s.db.QueryContext(ctx, `SELECT state,count(*) FROM archaeology_native_persistence_intents GROUP BY state`)
	if err != nil {
		return status, mapErr(err)
	}
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			rows.Close()
			return status, err
		}
		switch state {
		case "pending":
			status.Pending = count
		case "leased":
			status.Leased = count
		case "blocked":
			status.Blocked = count
		case "applied":
			status.Applied = count
		case "superseded":
			status.Superseded = count
		}
	}
	if err = rows.Close(); err != nil {
		return status, err
	}
	var next sql.NullString
	if err = s.db.QueryRowContext(ctx, `SELECT next_attempt_at FROM archaeology_native_persistence_intents WHERE state='pending' ORDER BY next_attempt_at,created_at,id LIMIT 1`).Scan(&next); err == nil {
		var valid bool
		if status.NextAttemptAt, valid = optionalPersistenceStamp(next); !valid {
			return status, fmt.Errorf("%w: invalid pending next_attempt_at", domain.ErrInvalid)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return status, mapErr(err)
	}
	return status, nil
}

// ArchaeologyNativePersistenceCounts is a short alias for callers that only
// need the bounded health counters.
func (s *Store) ArchaeologyNativePersistenceCounts(ctx context.Context) (domain.ArchaeologyNativePersistenceStatus, error) {
	return s.ArchaeologyNativePersistenceStatus(ctx)
}

func recoverExpiredArchaeologyPersistenceLeases(ctx context.Context, db *sql.DB, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT id,attempts FROM archaeology_native_persistence_intents
WHERE state='leased' AND lease_expires_at IS NOT NULL AND lease_expires_at<=?
ORDER BY lease_expires_at,created_at,id`, archaeologyPersistenceStamp(now))
	if err != nil {
		return mapErr(err)
	}
	type expiredLease struct {
		id       string
		attempts int
	}
	var expired []expiredLease
	for rows.Next() {
		var lease expiredLease
		if err = rows.Scan(&lease.id, &lease.attempts); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, lease)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, lease := range expired {
		attempts := lease.attempts + 1
		if attempts >= archaeologyPersistenceMaxAttempts {
			_, err = tx.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state='blocked',attempts=?,next_attempt_at=NULL,lease_owner='',lease_expires_at=NULL,
    last_error_code='lease_expired',updated_at=?
WHERE id=? AND state='leased' AND lease_expires_at IS NOT NULL AND lease_expires_at<=?`, archaeologyPersistenceMaxAttempts, archaeologyPersistenceStamp(now), lease.id, archaeologyPersistenceStamp(now))
		} else {
			next := now.Add(archaeologyPersistenceRetryDelay(attempts))
			_, err = tx.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state='pending',attempts=?,next_attempt_at=?,lease_owner='',lease_expires_at=NULL,
    last_error_code='lease_expired',updated_at=?
WHERE id=? AND state='leased' AND lease_expires_at IS NOT NULL AND lease_expires_at<=?`, attempts, archaeologyPersistenceStamp(next), archaeologyPersistenceStamp(now), lease.id, archaeologyPersistenceStamp(now))
		}
		if err != nil {
			return mapErr(err)
		}
	}
	return tx.Commit()
}

func leaseDueArchaeologyPersistence(ctx context.Context, db *sql.DB, now time.Time, limit int) ([]persistenceIntentRow, error) {
	if limit < 1 {
		return nil, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state='blocked',next_attempt_at=NULL,lease_owner='',lease_expires_at=NULL,
    last_error_code='max_attempts',updated_at=?
WHERE state='pending' AND attempts>=? AND next_attempt_at<=?`, archaeologyPersistenceStamp(now), archaeologyPersistenceMaxAttempts, archaeologyPersistenceStamp(now)); err != nil {
		return nil, mapErr(err)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,
       lease_owner,lease_expires_at,last_error_code,applied_at,created_at,updated_at
FROM archaeology_native_persistence_intents
WHERE state='pending' AND attempts<? AND next_attempt_at<=?
ORDER BY next_attempt_at,created_at,id LIMIT ?`, archaeologyPersistenceMaxAttempts, archaeologyPersistenceStamp(now), limit)
	if err != nil {
		return nil, mapErr(err)
	}
	var candidates []persistenceIntentRow
	for rows.Next() {
		row, scanErr := scanPersistenceIntent(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		candidates = append(candidates, row)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	owner := newID("ARPI-")
	leaseUntil := archaeologyPersistenceStamp(now.Add(archaeologyPersistenceLease))
	for i := range candidates {
		result, updateErr := tx.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state='leased',lease_owner=?,lease_expires_at=?,updated_at=?
WHERE id=? AND state='pending' AND attempts<? AND next_attempt_at<=?`, owner, leaseUntil, archaeologyPersistenceStamp(now), candidates[i].ID, archaeologyPersistenceMaxAttempts, archaeologyPersistenceStamp(now))
		if updateErr != nil {
			return nil, mapErr(updateErr)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			candidates[i].ID = ""
			continue
		}
		candidates[i].State = "leased"
		candidates[i].LeaseOwner = owner
		leaseTime := now.Add(archaeologyPersistenceLease)
		candidates[i].LeaseExpiresAt = &leaseTime
		candidates[i].UpdatedAt = now
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	leased := candidates[:0]
	for _, candidate := range candidates {
		if candidate.ID != "" {
			leased = append(leased, candidate)
		}
	}
	return leased, nil
}

func archaeologyPersistenceRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := archaeologyPersistenceRetryBase
	for i := 1; i < attempt && delay < archaeologyPersistenceRetryMax; i++ {
		delay *= 2
	}
	if delay > archaeologyPersistenceRetryMax {
		return archaeologyPersistenceRetryMax
	}
	return delay
}

func decodeArchaeologyPersistenceIntent(row persistenceIntentRow) (domain.ArchaeologyNativePersistenceIntent, error) {
	intent := domain.ArchaeologyNativePersistenceIntent{JobID: row.JobID, Operation: row.Operation}
	switch row.Operation {
	case domain.ArchaeologyNativePersistenceFailStart:
		var payload archaeologyNativeFailStartPayload
		if err := json.Unmarshal([]byte(row.payloadJSON), &payload); err != nil {
			return intent, fmt.Errorf("%w: fail_start payload", domain.ErrInvalid)
		}
		intent.Launch, intent.Uncertain = payload.Launch, payload.Uncertain
	case domain.ArchaeologyNativePersistenceBindIdentity:
		var payload archaeologyNativeIdentityPayload
		if err := json.Unmarshal([]byte(row.payloadJSON), &payload); err != nil {
			return intent, fmt.Errorf("%w: bind_identity payload", domain.ErrInvalid)
		}
		intent.ThreadID, intent.CodexSessionID, intent.TurnID = payload.ThreadID, payload.CodexSessionID, payload.TurnID
	case domain.ArchaeologyNativePersistenceActivate, domain.ArchaeologyNativePersistenceLoseTurn:
		var payload archaeologyNativeTurnPayload
		if err := json.Unmarshal([]byte(row.payloadJSON), &payload); err != nil {
			return intent, fmt.Errorf("%w: turn payload", domain.ErrInvalid)
		}
		intent.ThreadID, intent.TurnID = payload.ThreadID, payload.TurnID
	case domain.ArchaeologyNativePersistenceCompleteTurn:
		var payload archaeologyNativeCompletePayload
		if err := json.Unmarshal([]byte(row.payloadJSON), &payload); err != nil {
			return intent, fmt.Errorf("%w: complete_turn payload", domain.ErrInvalid)
		}
		intent.ThreadID, intent.TurnID, intent.Status, intent.DurationMS = payload.ThreadID, payload.TurnID, payload.Status, payload.DurationMS
	default:
		return intent, domain.ErrInvalid
	}
	return intent, nil
}

func applyArchaeologyPersistenceMutation(ctx context.Context, s *Store, intent domain.ArchaeologyNativePersistenceIntent) error {
	switch intent.Operation {
	case domain.ArchaeologyNativePersistenceFailStart:
		return s.FailArchaeologyNativeStart(ctx, intent.JobID, intent.Launch, intent.Uncertain)
	case domain.ArchaeologyNativePersistenceBindIdentity:
		return s.BindArchaeologyNativeIdentity(ctx, intent.JobID, intent.ThreadID, intent.CodexSessionID, intent.TurnID)
	case domain.ArchaeologyNativePersistenceActivate:
		return s.ActivateArchaeologyNativeJob(ctx, intent.JobID, intent.ThreadID, intent.TurnID)
	case domain.ArchaeologyNativePersistenceLoseTurn:
		return s.LoseArchaeologyNativeTurn(ctx, intent.JobID, intent.ThreadID, intent.TurnID)
	case domain.ArchaeologyNativePersistenceCompleteTurn:
		return s.CompleteArchaeologyNativeTurn(ctx, domain.ArchaeologyNativeTerminal{JobID: intent.JobID, ThreadID: intent.ThreadID, TurnID: intent.TurnID, Status: intent.Status, DurationMS: intent.DurationMS})
	default:
		return domain.ErrInvalid
	}
}

type persistenceReadbackOutcome uint8

const (
	persistenceReadbackRetry persistenceReadbackOutcome = iota
	persistenceReadbackApplied
	persistenceReadbackSuperseded
	persistenceReadbackBlocked
)

func persistenceIdentityCompatible(current, desired string) bool {
	return current == "" || desired == "" || current == desired
}

func persistenceIdentityExact(current, desired string) bool {
	return desired != "" && current == desired
}

func (s *Store) classifyArchaeologyPersistenceReadback(ctx context.Context, intent domain.ArchaeologyNativePersistenceIntent) (persistenceReadbackOutcome, error) {
	var state, threadID, sessionID, turnID, errorCode string
	var report []byte
	var storedDuration sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT state,thread_id,codex_session_id,turn_id,error_code,report_digest,duration_ms FROM archaeology_native_jobs WHERE id=?`, intent.JobID).Scan(&state, &threadID, &sessionID, &turnID, &errorCode, &report, &storedDuration)
	if errors.Is(err, sql.ErrNoRows) {
		return persistenceReadbackBlocked, nil
	}
	if err != nil {
		return persistenceReadbackRetry, mapErr(err)
	}
	if !persistenceIdentityCompatible(threadID, intent.ThreadID) || !persistenceIdentityCompatible(turnID, intent.TurnID) ||
		(intent.CodexSessionID != "" && !persistenceIdentityCompatible(sessionID, intent.CodexSessionID)) {
		return persistenceReadbackBlocked, nil
	}
	switch intent.Operation {
	case domain.ArchaeologyNativePersistenceBindIdentity:
		if persistenceIdentityExact(threadID, intent.ThreadID) && persistenceIdentityExact(sessionID, intent.CodexSessionID) && persistenceIdentityExact(turnID, intent.TurnID) {
			return persistenceReadbackApplied, nil
		}
		if state == "starting" || state == "uncertain" {
			return persistenceReadbackRetry, nil
		}
		return persistenceReadbackSuperseded, nil
	case domain.ArchaeologyNativePersistenceActivate:
		if state == "active" && persistenceIdentityExact(threadID, intent.ThreadID) && persistenceIdentityExact(turnID, intent.TurnID) {
			return persistenceReadbackApplied, nil
		}
		if state == "starting" {
			return persistenceReadbackRetry, nil
		}
		return persistenceReadbackSuperseded, nil
	case domain.ArchaeologyNativePersistenceFailStart:
		wantUncertain := intent.Uncertain || intent.Launch.ThreadID != "" || intent.Launch.CodexSessionID != "" || intent.Launch.TurnID != ""
		if state == "uncertain" && wantUncertain {
			identityExact := persistenceIdentityCompatible(threadID, intent.Launch.ThreadID) &&
				persistenceIdentityCompatible(sessionID, intent.Launch.CodexSessionID) &&
				persistenceIdentityCompatible(turnID, intent.Launch.TurnID) &&
				(intent.Launch.ThreadID == "" || persistenceIdentityExact(threadID, intent.Launch.ThreadID)) &&
				(intent.Launch.CodexSessionID == "" || persistenceIdentityExact(sessionID, intent.Launch.CodexSessionID)) &&
				(intent.Launch.TurnID == "" || persistenceIdentityExact(turnID, intent.Launch.TurnID))
			if identityExact {
				return persistenceReadbackApplied, nil
			}
			// FailStart fills compatible missing identity fields atomically;
			// leave the intent retryable until that evidence is present.
			return persistenceReadbackRetry, nil
		}
		if state == "failed" && !wantUncertain && errorCode == "codex_start_failed" {
			return persistenceReadbackApplied, nil
		}
		if state == "starting" {
			return persistenceReadbackRetry, nil
		}
		return persistenceReadbackSuperseded, nil
	case domain.ArchaeologyNativePersistenceLoseTurn:
		if state == "uncertain" && errorCode == "codex_process_unavailable" && persistenceIdentityExact(threadID, intent.ThreadID) && persistenceIdentityExact(turnID, intent.TurnID) {
			return persistenceReadbackApplied, nil
		}
		if state == "uncertain" && errorCode == "server_restarted_during_active_task" && persistenceIdentityExact(threadID, intent.ThreadID) && persistenceIdentityExact(turnID, intent.TurnID) {
			// Generic startup reconciliation is weaker than an exact durable
			// lose intent. Keep the row retryable until LoseTurn can normalize
			// the reason to codex_process_unavailable.
			return persistenceReadbackRetry, nil
		}
		if state == "active" || state == "report_ready" || state == "cancel_requested" {
			return persistenceReadbackRetry, nil
		}
		return persistenceReadbackSuperseded, nil
	case domain.ArchaeologyNativePersistenceCompleteTurn:
		if state == "uncertain" {
			// An exact completion callback is evidence that may resolve an
			// uncertain job.  The callback itself must be retried, not discarded.
			return persistenceReadbackRetry, nil
		}
		wantState := intent.Status
		if intent.Status == "completed" && len(report) == 0 {
			wantState = "attention"
		}
		durationMatches := intent.DurationMS == nil && !storedDuration.Valid || intent.DurationMS != nil && storedDuration.Valid && *intent.DurationMS == storedDuration.Int64
		if state == wantState && durationMatches {
			return persistenceReadbackApplied, nil
		}
		if state == "starting" || state == "active" || state == "report_ready" || state == "cancel_requested" {
			return persistenceReadbackRetry, nil
		}
		return persistenceReadbackSuperseded, nil
	}
	return persistenceReadbackBlocked, nil
}

func (s *Store) markArchaeologyPersistenceApplied(ctx context.Context, id, owner string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state='applied',next_attempt_at=NULL,lease_owner='',lease_expires_at=NULL,
    last_error_code='',applied_at=?,updated_at=?
WHERE id=? AND state='leased' AND lease_owner=?`, archaeologyPersistenceStamp(now), archaeologyPersistenceStamp(now), id, owner)
	if err != nil {
		return mapErr(err)
	}
	if changed, _ := result.RowsAffected(); changed == 1 {
		return nil
	}
	var state string
	if err = s.db.QueryRowContext(ctx, `SELECT state FROM archaeology_native_persistence_intents WHERE id=?`, id).Scan(&state); err != nil {
		return mapErr(err)
	}
	if state == "applied" {
		return nil
	}
	return domain.ErrConflict
}

func (s *Store) markArchaeologyPersistenceTerminal(ctx context.Context, id, owner, state, errorCode string, now time.Time) error {
	if state != "superseded" && state != "blocked" {
		return domain.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state=?,next_attempt_at=NULL,lease_owner='',lease_expires_at=NULL,
    last_error_code=?,applied_at=NULL,updated_at=?
WHERE id=? AND state='leased' AND lease_owner=?`, state, errorCode, archaeologyPersistenceStamp(now), id, owner)
	if err != nil {
		return mapErr(err)
	}
	if changed, _ := result.RowsAffected(); changed == 1 {
		return nil
	}
	var current string
	if err = s.db.QueryRowContext(ctx, `SELECT state FROM archaeology_native_persistence_intents WHERE id=?`, id).Scan(&current); err != nil {
		return mapErr(err)
	}
	if current == state {
		return nil
	}
	return domain.ErrConflict
}

func (s *Store) markArchaeologyPersistenceRetry(ctx context.Context, id, owner, errorCode string, attempts int, now time.Time) error {
	if attempts < 1 {
		attempts = 1
	}
	expectedAttempts := attempts - 1
	if attempts >= archaeologyPersistenceMaxAttempts {
		result, err := s.db.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state='blocked',attempts=attempts+1,next_attempt_at=NULL,lease_owner='',lease_expires_at=NULL,
    last_error_code='max_attempts',applied_at=NULL,updated_at=?
WHERE id=? AND state='leased' AND lease_owner=? AND attempts=? AND attempts+1>=? AND attempts<?`, archaeologyPersistenceStamp(now), id, owner, expectedAttempts, archaeologyPersistenceMaxAttempts, archaeologyPersistenceMaxAttempts)
		if err != nil {
			return mapErr(err)
		}
		if changed, _ := result.RowsAffected(); changed == 1 {
			return nil
		}
		return domain.ErrConflict
	}
	next := now.Add(archaeologyPersistenceRetryDelay(attempts))
	if len(errorCode) > 200 {
		errorCode = errorCode[:200]
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state='pending',attempts=attempts+1,next_attempt_at=?,lease_owner='',lease_expires_at=NULL,
    last_error_code=?,applied_at=NULL,updated_at=?
WHERE id=? AND state='leased' AND lease_owner=? AND attempts=? AND attempts+1<?`, archaeologyPersistenceStamp(next), errorCode, archaeologyPersistenceStamp(now), id, owner, expectedAttempts, archaeologyPersistenceMaxAttempts)
	if err != nil {
		return mapErr(err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return domain.ErrConflict
	}
	return nil
}

func persistenceErrorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, domain.ErrConflict):
		return "conflict"
	case errors.Is(err, domain.ErrInvalid):
		return "invalid"
	case errors.Is(err, domain.ErrNotFound):
		return "not_found"
	default:
		return "repository_error"
	}
}

func (s *Store) processArchaeologyPersistenceRow(ctx context.Context, row persistenceIntentRow, owner string, now time.Time) (persistenceReadbackOutcome, error) {
	intent, err := decodeArchaeologyPersistenceIntent(row)
	if err != nil {
		return persistenceReadbackBlocked, s.markArchaeologyPersistenceTerminal(ctx, row.ID, owner, "blocked", "invalid_payload", now)
	}
	err = applyArchaeologyPersistenceMutation(ctx, s, intent)
	if err == nil {
		return persistenceReadbackApplied, s.markArchaeologyPersistenceApplied(ctx, row.ID, owner, now)
	}
	if errors.Is(err, domain.ErrConflict) {
		outcome, readErr := s.classifyArchaeologyPersistenceReadback(ctx, intent)
		if readErr != nil {
			return persistenceReadbackRetry, readErr
		}
		switch outcome {
		case persistenceReadbackApplied:
			return outcome, s.markArchaeologyPersistenceApplied(ctx, row.ID, owner, now)
		case persistenceReadbackSuperseded:
			return outcome, s.markArchaeologyPersistenceTerminal(ctx, row.ID, owner, "superseded", "stale_state", now)
		case persistenceReadbackBlocked:
			return outcome, s.markArchaeologyPersistenceTerminal(ctx, row.ID, owner, "blocked", "identity_mismatch", now)
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return persistenceReadbackRetry, err
	}
	if errors.Is(err, domain.ErrInvalid) || errors.Is(err, domain.ErrNotFound) {
		return persistenceReadbackBlocked, s.markArchaeologyPersistenceTerminal(ctx, row.ID, owner, "blocked", persistenceErrorCode(err), now)
	}
	return persistenceReadbackRetry, s.markArchaeologyPersistenceRetry(ctx, row.ID, owner, persistenceErrorCode(err), row.Attempts+1, now)
}

// RetryArchaeologyNativePersistence recovers expired leases, leases a bounded
// deterministic batch, and applies each repository mutation.  No sleep is
// performed; next_attempt_at is the durable wake-up schedule for the caller.
func (s *Store) RetryArchaeologyNativePersistence(ctx context.Context, limits ...int) (domain.ArchaeologyNativePersistenceRetryReport, error) {
	limit := archaeologyPersistenceMaxRows
	if len(limits) > 0 {
		limit = limits[0]
	}
	if limit <= 0 {
		limit = archaeologyPersistenceMaxRows
	}
	if limit > archaeologyPersistenceMaxRows {
		limit = archaeologyPersistenceMaxRows
	}
	now := s.now().UTC()
	s.archaeologyLaunchMu.Lock()
	defer s.archaeologyLaunchMu.Unlock()
	if err := recoverExpiredArchaeologyPersistenceLeases(ctx, s.db, now); err != nil {
		return domain.ArchaeologyNativePersistenceRetryReport{}, err
	}
	rows, err := leaseDueArchaeologyPersistence(ctx, s.db, now, limit)
	if err != nil {
		return domain.ArchaeologyNativePersistenceRetryReport{}, err
	}
	report := domain.ArchaeologyNativePersistenceRetryReport{Leased: len(rows)}
	for _, row := range rows {
		outcome, processErr := s.processArchaeologyPersistenceRow(ctx, row, row.LeaseOwner, now)
		if processErr != nil {
			if errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded) {
				return report, processErr
			}
			return report, processErr
		}
		report.Processed++
		switch outcome {
		case persistenceReadbackApplied:
			report.Applied++
		case persistenceReadbackSuperseded:
			report.Superseded++
		case persistenceReadbackBlocked:
			report.Blocked++
		case persistenceReadbackRetry:
			report.Retried++
		}
	}
	return report, nil
}

// RetryDueArchaeologyNativePersistence is an explicit alias for scheduler
// loops whose wake-up code names the due-work operation.
func (s *Store) RetryDueArchaeologyNativePersistence(ctx context.Context, limits ...int) (domain.ArchaeologyNativePersistenceRetryReport, error) {
	return s.RetryArchaeologyNativePersistence(ctx, limits...)
}

// leaseOneArchaeologyPersistenceIntent is the CAS boundary for the
// synchronous EnsureAndApply path.  It never steals a live lease belonging to
// another worker; only the periodic worker may recover an expired lease.
func leaseOneArchaeologyPersistenceIntent(ctx context.Context, db *sql.DB, id string, now time.Time) (persistenceIntentRow, string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return persistenceIntentRow{}, "", err
	}
	defer tx.Rollback()
	row, err := scanPersistenceIntent(tx.QueryRowContext(ctx, `
SELECT id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,
       lease_owner,lease_expires_at,last_error_code,applied_at,created_at,updated_at
FROM archaeology_native_persistence_intents WHERE id=?`, id))
	if err != nil {
		return persistenceIntentRow{}, "", mapErr(err)
	}
	if row.State != "pending" {
		if row.State == "leased" {
			return persistenceIntentRow{}, "", domain.ErrUnavailable
		}
		if err = tx.Commit(); err != nil {
			return persistenceIntentRow{}, "", err
		}
		return row, "", nil
	}
	if row.Attempts >= archaeologyPersistenceMaxAttempts {
		if err = tx.Commit(); err != nil {
			return persistenceIntentRow{}, "", err
		}
		return persistenceIntentRow{}, "", domain.ErrConflict
	}
	if row.NextAttemptAt == nil || row.NextAttemptAt.After(now) {
		if err = tx.Commit(); err != nil {
			return persistenceIntentRow{}, "", err
		}
		return persistenceIntentRow{}, "", domain.ErrUnavailable
	}
	owner := newID("ARPI-")
	leaseUntil := now.Add(archaeologyPersistenceLease)
	result, err := tx.ExecContext(ctx, `
UPDATE archaeology_native_persistence_intents
SET state='leased',lease_owner=?,lease_expires_at=?,updated_at=?
WHERE id=? AND state='pending' AND attempts=? AND attempts<? AND next_attempt_at IS NOT NULL AND next_attempt_at<=?`, owner, archaeologyPersistenceStamp(leaseUntil), archaeologyPersistenceStamp(now), id, row.Attempts, archaeologyPersistenceMaxAttempts, archaeologyPersistenceStamp(now))
	if err != nil {
		return persistenceIntentRow{}, "", mapErr(err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return persistenceIntentRow{}, "", domain.ErrUnavailable
	}
	row.State = "leased"
	row.LeaseOwner = owner
	row.LeaseExpiresAt = &leaseUntil
	row.UpdatedAt = now
	if err = tx.Commit(); err != nil {
		return persistenceIntentRow{}, "", err
	}
	return row, owner, nil
}

func (s *Store) ApplyArchaeologyNativePersistenceIntent(ctx context.Context, id string) error {
	if !boundedCoreText(id, 200, true) {
		return domain.ErrInvalid
	}
	s.archaeologyLaunchMu.Lock()
	defer s.archaeologyLaunchMu.Unlock()
	row, owner, err := leaseOneArchaeologyPersistenceIntent(ctx, s.db, id, s.now().UTC())
	if err != nil {
		return err
	}
	if row.State == "applied" || row.State == "superseded" {
		return nil
	}
	if row.State == "blocked" {
		return domain.ErrConflict
	}
	outcome, err := s.processArchaeologyPersistenceRow(ctx, row, owner, s.now().UTC())
	if err != nil {
		return err
	}
	if outcome == persistenceReadbackBlocked || outcome == persistenceReadbackSuperseded {
		return nil
	}
	return nil
}

// ReconcileArchaeologyNativePersistence is healthy only after the durable
// ledger has no pending, leased, or blocked rows.  A future-due pending row or
// a fresh lease is intentionally reported as unavailable rather than silently
// declared ready.
func (s *Store) ReconcileArchaeologyNativePersistence(ctx context.Context) error {
	for i := 0; i < archaeologyPersistenceMaxAttempts+1; i++ {
		if _, err := s.RetryArchaeologyNativePersistence(ctx, archaeologyPersistenceMaxRows); err != nil {
			return err
		}
		status, err := s.ArchaeologyNativePersistenceStatus(ctx)
		if err != nil {
			return err
		}
		if status.Healthy() {
			return nil
		}
		if status.Pending == 0 && status.Leased > 0 {
			return domain.ErrUnavailable
		}
		if status.Blocked > 0 {
			return domain.ErrUnavailable
		}
		if status.Pending > 0 && status.NextAttemptAt != nil && status.NextAttemptAt.After(s.now().UTC()) {
			return domain.ErrUnavailable
		}
	}
	return domain.ErrUnavailable
}
