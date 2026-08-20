package store

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"codex-commons/internal/domain"
)

func (s *Store) InstallationIdentity(ctx context.Context) ([]byte, error) {
	var id []byte
	if err := s.db.QueryRowContext(ctx, `SELECT installation_id FROM installation_status WHERE id=1`).Scan(&id); err != nil {
		return nil, mapErr(err)
	}
	if len(id) != 16 {
		return nil, fmt.Errorf("%w: installation identity length %d", domain.ErrInvalid, len(id))
	}
	return append([]byte(nil), id...), nil
}

func (s *Store) InstallationIdentityHex(ctx context.Context) (string, error) {
	id, err := s.InstallationIdentity(ctx)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(id), nil
}

func (s *Store) RecordReconciliationStatus(ctx context.Context, status string, at time.Time) error {
	if status != "healthy" && status != "attention" && status != "failed" {
		return domain.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `UPDATE installation_status SET reconciliation_status=?,reconciliation_checked_at=?,updated_at=? WHERE id=1`, status, stamp(at.UTC()), stamp(at.UTC()))
	return err
}

func (s *Store) RecordCompatibilityStatus(ctx context.Context, status string, at time.Time) error {
	if status != "compatible" && status != "incompatible" && status != "unavailable" {
		return domain.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `UPDATE installation_status SET compatibility_status=?,compatibility_checked_at=?,updated_at=? WHERE id=1`, status, stamp(at.UTC()), stamp(at.UTC()))
	return err
}

func (s *Store) ArchaeologyUncertaintyCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_jobs WHERE state='uncertain'`).Scan(&count)
	return count, err
}
