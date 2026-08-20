package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"codex-commons/internal/domain"
	"codex-commons/internal/restore"
)

func restoreReceiptJSON(t *testing.T, store *Store, drill, release, recorded, backupHex string, schema int) []byte {
	t.Helper()
	id, err := store.InstallationIdentityHex(context.Background())
	must(t, err)
	return []byte(`{"schema_version":` + strconv.Itoa(schema) + `,"release_id":"` + release + `","drill_id":"` + drill + `","recorded_at":"` + recorded + `","installation_id":"` + id + `","restored_backup_digest":"` + backupHex + `"}`)
}

func testBackupHex(seed byte) string {
	return hex.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

func countRestoreEvidence(t *testing.T, store *Store) int {
	t.Helper()
	var count int
	must(t, store.DB().QueryRow(`SELECT count(*) FROM installation_restore_evidence`).Scan(&count))
	return count
}

func TestRecordRestoreEvidenceValidCanonicalReceipt(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "restore.sqlite3"))
	must(t, err)
	defer store.Close()
	input := restoreReceiptJSON(t, store, "drill-1", "continuous-dogfood-test", "2026-08-20T00:00:00Z", testBackupHex(0xbb), 17)
	got, err := store.RecordRestoreEvidence(ctx, input)
	must(t, err)
	id, err := store.InstallationIdentity(ctx)
	must(t, err)
	if got.DrillID != "drill-1" || got.ReleaseID != "continuous-dogfood-test" || got.SchemaVersion != 17 || got.RestoredBackupDigest != testBackupHex(0xbb) {
		t.Fatalf("evidence=%+v", got)
	}
	if !bytes.Equal(got.InstallationID, id) || len(got.RestoreReceiptDigest) != 64 || got.RestoreReceiptDigest != strings.ToLower(got.RestoreReceiptDigest) {
		t.Fatalf("bound identity/digest=%x %q", got.InstallationID, got.RestoreReceiptDigest)
	}
	receipt, err := restore.Parse(input)
	must(t, err)
	if got.RestoreReceiptDigest != receipt.Fingerprint() {
		t.Fatalf("stored digest=%q fingerprint=%q", got.RestoreReceiptDigest, receipt.Fingerprint())
	}
	if countRestoreEvidence(t, store) != 1 {
		t.Fatalf("evidence count=%d", countRestoreEvidence(t, store))
	}
	var restoreStatus, backupStatus string
	must(t, store.DB().QueryRowContext(ctx, `SELECT restore_status,backup_status FROM installation_status WHERE id=1`).Scan(&restoreStatus, &backupStatus))
	if restoreStatus != "unknown" || backupStatus != "unknown" {
		t.Fatalf("status restore=%q backup=%q", restoreStatus, backupStatus)
	}
}

func TestRecordRestoreEvidenceRejectsIdentityMismatchAndInvalidInput(t *testing.T) {
	ctx := context.Background()
	left, err := Open(ctx, filepath.Join(t.TempDir(), "left.sqlite3"))
	must(t, err)
	defer left.Close()
	right, err := Open(ctx, filepath.Join(t.TempDir(), "right.sqlite3"))
	must(t, err)
	defer right.Close()
	foreign := restoreReceiptJSON(t, left, "drill-1", "continuous-dogfood-test", "2026-08-20T00:00:00Z", testBackupHex(0xbb), 17)
	if _, err = right.RecordRestoreEvidence(ctx, foreign); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("cross-installation err=%v", err)
	}
	if countRestoreEvidence(t, right) != 0 {
		t.Fatal("foreign receipt was recorded")
	}
	if _, err = right.RecordRestoreEvidence(ctx, []byte(`{"not":"a receipt"}`)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown fields err=%v", err)
	}
	if _, err = right.RecordRestoreEvidence(ctx, bytes.Repeat([]byte("a"), restore.MaxBytes+1)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversized err=%v", err)
	}
}

func TestRecordRestoreEvidenceIdempotentReplayAndCollision(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "idempotent.sqlite3"))
	must(t, err)
	defer store.Close()
	first := restoreReceiptJSON(t, store, "drill-1", "continuous-dogfood-test", "2026-08-20T00:00:00Z", testBackupHex(0xbb), 17)
	got, err := store.RecordRestoreEvidence(ctx, first)
	must(t, err)
	replay, err := store.RecordRestoreEvidence(ctx, first)
	must(t, err)
	if replay.RestoreReceiptDigest != got.RestoreReceiptDigest || countRestoreEvidence(t, store) != 1 {
		t.Fatalf("replay digest=%q count=%d", replay.RestoreReceiptDigest, countRestoreEvidence(t, store))
	}
	changedBackup := restoreReceiptJSON(t, store, "drill-1", "continuous-dogfood-test", "2026-08-20T00:00:00Z", testBackupHex(0xcc), 17)
	if _, err = store.RecordRestoreEvidence(ctx, changedBackup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("drill collision err=%v", err)
	}
	changedTime := restoreReceiptJSON(t, store, "drill-1", "continuous-dogfood-test", "2026-08-20T00:00:01Z", testBackupHex(0xbb), 17)
	if _, err = store.RecordRestoreEvidence(ctx, changedTime); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("timestamp collision err=%v", err)
	}
	if countRestoreEvidence(t, store) != 1 {
		t.Fatalf("collision mutated evidence count=%d", countRestoreEvidence(t, store))
	}

	other := restoreReceiptJSON(t, store, "drill-2", "continuous-dogfood-test", "2026-08-20T00:00:00Z", testBackupHex(0xdd), 17)
	parsed, err := restore.Parse(other)
	must(t, err)
	id, err := store.InstallationIdentity(ctx)
	must(t, err)
	_, err = store.DB().ExecContext(ctx, `INSERT INTO installation_restore_evidence(drill_id,installation_id,recorded_at,restore_receipt_digest,restored_backup_digest,schema_version,release_id) VALUES(?,?,?,?,?,?,?)`,
		"drill-stolen-digest", id, "2026-08-20T00:00:00Z", parsed.Fingerprint(), testBackupHex(0xee), 17, "continuous-dogfood-test")
	must(t, err)
	if _, err = store.RecordRestoreEvidence(ctx, other); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("digest collision err=%v", err)
	}
	if countRestoreEvidence(t, store) != 2 {
		t.Fatalf("digest collision count=%d", countRestoreEvidence(t, store))
	}
}

func TestRecordRestoreEvidenceConcurrentExactReplay(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "concurrent.sqlite3"))
	must(t, err)
	defer store.Close()
	input := restoreReceiptJSON(t, store, "drill-1", "continuous-dogfood-test", "2026-08-20T00:00:00Z", testBackupHex(0xbb), 17)
	const n = 8
	digests := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			got, err := store.RecordRestoreEvidence(ctx, input)
			errs[i] = err
			digests[i] = got.RestoreReceiptDigest
		}()
	}
	wg.Wait()
	var first string
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("record %d: %v", i, errs[i])
		}
		if first == "" {
			first = digests[i]
			continue
		}
		if digests[i] != first {
			t.Fatalf("concurrent digest mismatch %q vs %q", first, digests[i])
		}
	}
	if countRestoreEvidence(t, store) != 1 {
		t.Fatalf("concurrent evidence count=%d", countRestoreEvidence(t, store))
	}
}
