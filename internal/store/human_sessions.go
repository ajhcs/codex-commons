package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codex-commons/internal/domain"
)

func (s *Store) SaveHumanBrowserSession(ctx context.Context, v domain.HumanBrowserSession) error {
	if len(v.TokenDigest) != 32 || len(v.CSRFDigest) != 32 || v.Principal == "" || (v.AuthMethod != "codex" && v.AuthMethod != "recovery") || v.BindingRevision < 0 || !v.ExpiresAt.After(v.CreatedAt) {
		return domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM human_browser_sessions WHERE revoked_at IS NOT NULL OR expires_at<=?`, stamp(v.CreatedAt.UTC())); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO human_browser_sessions(token_digest,csrf_digest,principal,auth_method,binding_revision,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`, v.TokenDigest, v.CSRFDigest, v.Principal, v.AuthMethod, v.BindingRevision, stamp(v.CreatedAt), stamp(v.ExpiresAt)); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM human_browser_sessions WHERE token_digest IN (SELECT token_digest FROM human_browser_sessions WHERE principal=? ORDER BY created_at DESC,hex(token_digest) DESC LIMIT -1 OFFSET 8)`, v.Principal); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) HumanBrowserSession(ctx context.Context, tokenDigest []byte, now time.Time) (domain.HumanBrowserSession, error) {
	var v domain.HumanBrowserSession
	var created, expires string
	err := s.db.QueryRowContext(ctx, `SELECT token_digest,csrf_digest,principal,auth_method,binding_revision,created_at,expires_at FROM human_browser_sessions WHERE token_digest=? AND revoked_at IS NULL AND expires_at>?`, tokenDigest, stamp(now.UTC())).Scan(&v.TokenDigest, &v.CSRFDigest, &v.Principal, &v.AuthMethod, &v.BindingRevision, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return v, domain.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.CreatedAt = parseStamp(created)
	v.ExpiresAt = parseStamp(expires)
	if v.CreatedAt.IsZero() || v.ExpiresAt.IsZero() {
		return v, domain.ErrConflict
	}
	return v, nil
}

func (s *Store) RevokeHumanBrowserSession(ctx context.Context, tokenDigest []byte, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE human_browser_sessions SET revoked_at=? WHERE token_digest=? AND revoked_at IS NULL`, stamp(now.UTC()), tokenDigest)
	return err
}

func (s *Store) RevokeHumanBrowserSessionsByMethod(ctx context.Context, method string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE human_browser_sessions SET revoked_at=? WHERE auth_method=? AND revoked_at IS NULL`, stamp(now.UTC()), method)
	return err
}

func (s *Store) UpdateHumanBrowserSessionCSRF(ctx context.Context, tokenDigest, csrfDigest []byte) error {
	result, err := s.db.ExecContext(ctx, `UPDATE human_browser_sessions SET csrf_digest=? WHERE token_digest=? AND revoked_at IS NULL`, csrfDigest, tokenDigest)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) UpdateHumanBrowserSessionRevisions(ctx context.Context, principal string, revision int64) error {
	if principal == "" || revision < 0 {
		return domain.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `UPDATE human_browser_sessions SET binding_revision=? WHERE principal=? AND revoked_at IS NULL`, revision, principal)
	return err
}

func (s *Store) SetCodexSessionRevocationPending(ctx context.Context, pending bool) error {
	value := 0
	if pending {
		value = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE installation_status SET codex_session_revocation_pending=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1`, value)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) CodexSessionRevocationPending(ctx context.Context) (bool, error) {
	var pending int
	err := s.db.QueryRowContext(ctx, `SELECT codex_session_revocation_pending FROM installation_status WHERE id=1`).Scan(&pending)
	return pending == 1, err
}
