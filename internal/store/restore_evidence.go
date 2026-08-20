package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"codex-commons/internal/domain"
	"codex-commons/internal/restore"
)

// RestoreEvidence is one append-only restore-drill row after exact validation.
type RestoreEvidence struct {
	DrillID              string
	InstallationID       []byte
	RecordedAt           string
	RestoreReceiptDigest string
	RestoredBackupDigest string
	SchemaVersion        int
	ReleaseID            string
}

// RecordRestoreEvidence parses a sanitized receipt, binds it to this database's
// installation_id, and inserts one evidence row. Exact replay is idempotent.
// A colliding drill_id or receipt digest fails closed. This does not update
// restore_status or Beta-prerequisite facts.
func (s *Store) RecordRestoreEvidence(ctx context.Context, input []byte) (RestoreEvidence, error) {
	receipt, err := restore.Parse(input)
	if err != nil {
		return RestoreEvidence{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RestoreEvidence{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE installation_status SET updated_at=updated_at WHERE id=1`); err != nil {
		return RestoreEvidence{}, err
	}
	var installationID []byte
	if err = tx.QueryRowContext(ctx, `SELECT installation_id FROM installation_status WHERE id=1`).Scan(&installationID); err != nil {
		return RestoreEvidence{}, mapErr(err)
	}
	if err = validateInstallationIdentity(installationID); err != nil {
		return RestoreEvidence{}, err
	}
	if err = receipt.Bind(installationID); err != nil {
		return RestoreEvidence{}, err
	}
	want := evidenceFromReceipt(receipt)
	existing, found, err := loadRestoreEvidence(ctx, tx, want.DrillID)
	if err != nil {
		return RestoreEvidence{}, err
	}
	if found {
		if restoreEvidenceEqual(existing, want) {
			return existing, nil
		}
		return RestoreEvidence{}, fmt.Errorf("%w: restore evidence collision", domain.ErrConflict)
	}
	if conflict, err := restoreDigestTaken(ctx, tx, want.RestoreReceiptDigest, want.DrillID); err != nil {
		return RestoreEvidence{}, err
	} else if conflict {
		return RestoreEvidence{}, fmt.Errorf("%w: restore evidence collision", domain.ErrConflict)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO installation_restore_evidence(drill_id,installation_id,recorded_at,restore_receipt_digest,restored_backup_digest,schema_version,release_id) VALUES(?,?,?,?,?,?,?)`,
		want.DrillID, want.InstallationID, want.RecordedAt, want.RestoreReceiptDigest, want.RestoredBackupDigest, want.SchemaVersion, want.ReleaseID)
	if err != nil {
		if uniqueConstraint(err) {
			existing, found, loadErr := loadRestoreEvidence(ctx, tx, want.DrillID)
			if loadErr == nil && found && restoreEvidenceEqual(existing, want) {
				return existing, nil
			}
			return RestoreEvidence{}, fmt.Errorf("%w: restore evidence collision", domain.ErrConflict)
		}
		return RestoreEvidence{}, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return RestoreEvidence{}, mapErr(err)
	}
	return want, nil
}

func evidenceFromReceipt(receipt restore.Receipt) RestoreEvidence {
	return RestoreEvidence{
		DrillID:              receipt.DrillID,
		InstallationID:       append([]byte(nil), receipt.InstallationID[:]...),
		RecordedAt:           receipt.RecordedAt,
		RestoreReceiptDigest: receipt.Fingerprint(),
		RestoredBackupDigest: receipt.BackupDigestHex(),
		SchemaVersion:        receipt.SchemaVersion,
		ReleaseID:            receipt.ReleaseID,
	}
}

func loadRestoreEvidence(ctx context.Context, tx *sql.Tx, drillID string) (RestoreEvidence, bool, error) {
	var out RestoreEvidence
	err := tx.QueryRowContext(ctx, `SELECT drill_id,installation_id,recorded_at,restore_receipt_digest,restored_backup_digest,schema_version,release_id FROM installation_restore_evidence WHERE drill_id=?`, drillID).Scan(
		&out.DrillID, &out.InstallationID, &out.RecordedAt, &out.RestoreReceiptDigest, &out.RestoredBackupDigest, &out.SchemaVersion, &out.ReleaseID)
	if errors.Is(err, sql.ErrNoRows) {
		return RestoreEvidence{}, false, nil
	}
	if err != nil {
		return RestoreEvidence{}, false, mapErr(err)
	}
	out.InstallationID = append([]byte(nil), out.InstallationID...)
	return out, true, nil
}

func restoreDigestTaken(ctx context.Context, tx *sql.Tx, digest, drillID string) (bool, error) {
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT drill_id FROM installation_restore_evidence WHERE restore_receipt_digest=?`, digest).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, mapErr(err)
	}
	return existing != drillID, nil
}

func restoreEvidenceEqual(got, want RestoreEvidence) bool {
	return got.DrillID == want.DrillID &&
		got.RecordedAt == want.RecordedAt &&
		got.RestoreReceiptDigest == want.RestoreReceiptDigest &&
		got.RestoredBackupDigest == want.RestoredBackupDigest &&
		got.SchemaVersion == want.SchemaVersion &&
		got.ReleaseID == want.ReleaseID &&
		bytes.Equal(got.InstallationID, want.InstallationID)
}

func uniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed")
}
