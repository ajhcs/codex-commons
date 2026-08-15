package store

import (
	"context"
	"time"

	"codex-commons/internal/domain"
)

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
