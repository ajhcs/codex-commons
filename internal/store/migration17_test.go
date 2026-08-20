package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"codex-commons/internal/domain"

	_ "modernc.org/sqlite"
)

const migration17At = "2026-08-20T00:00:00Z"

var installationIdentityHex = regexp.MustCompile(`^[0-9a-f]{32}$`)

func mustNonZeroInstallationIdentityHex(t *testing.T, id string) {
	t.Helper()
	if !installationIdentityHex.MatchString(id) || id == strings.Repeat("0", 32) {
		t.Fatalf("identity hex=%q", id)
	}
}

func newMigration17Database(t *testing.T, through int) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migration17.sqlite3")
	db, err := sql.Open("sqlite", path)
	must(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL) STRICT`)
	must(t, err)
	applyMigration16Through(t, db, through)
	return db, path
}

func readInstallationIdentity(t *testing.T, db *sql.DB) []byte {
	t.Helper()
	var id []byte
	must(t, db.QueryRow(`SELECT installation_id FROM installation_status WHERE id=1`).Scan(&id))
	if len(id) != 16 {
		t.Fatalf("installation_id length=%d", len(id))
	}
	return append([]byte(nil), id...)
}

func mustMigration17Reject(t *testing.T, err error, description string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted invalid restore evidence: %s", description)
	}
}

func insertRestoreEvidence(db *sql.DB, drillID string, installationID []byte, recordedAt, receipt, backup string, schemaVersion int, releaseID string) error {
	_, err := db.Exec(`INSERT INTO installation_restore_evidence(drill_id,installation_id,recorded_at,restore_receipt_digest,restored_backup_digest,schema_version,release_id) VALUES(?,?,?,?,?,?,?)`,
		drillID, installationID, recordedAt, receipt, backup, schemaVersion, releaseID)
	return err
}

func TestMigration17FreshUpgradeAndStableReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fresh.sqlite3")
	store, err := Open(ctx, path)
	must(t, err)

	var version, evidence int
	must(t, store.DB().QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version))
	if version != 17 {
		t.Fatalf("schema version=%d", version)
	}
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM installation_restore_evidence`).Scan(&evidence))
	if evidence != 0 {
		t.Fatalf("restore evidence count=%d", evidence)
	}
	var restoreStatus, backupStatus string
	must(t, store.DB().QueryRowContext(ctx, `SELECT restore_status,backup_status FROM installation_status WHERE id=1`).Scan(&restoreStatus, &backupStatus))
	if restoreStatus != "unknown" || backupStatus != "unknown" {
		t.Fatalf("status restore=%q backup=%q", restoreStatus, backupStatus)
	}
	first, err := store.InstallationIdentity(ctx)
	must(t, err)
	firstHex, err := store.InstallationIdentityHex(ctx)
	must(t, err)
	mustNonZeroInstallationIdentityHex(t, firstHex)
	if firstHex != hex.EncodeToString(first) {
		t.Fatalf("identity hex=%q", firstHex)
	}
	var secret []byte
	must(t, store.DB().QueryRowContext(ctx, `SELECT review_secret FROM installation_status WHERE id=1`).Scan(&secret))
	if len(secret) != 32 {
		t.Fatalf("review_secret length=%d", len(secret))
	}
	if bytes.Equal(first, secret[:16]) {
		t.Fatal("installation identity was derived from review_secret")
	}
	must(t, store.Close())

	reopened, err := Open(ctx, path)
	must(t, err)
	defer reopened.Close()
	must(t, reopened.DB().QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version))
	if version != 17 {
		t.Fatalf("reopen schema version=%d", version)
	}
	again, err := reopened.InstallationIdentity(ctx)
	must(t, err)
	if !bytes.Equal(first, again) {
		t.Fatalf("identity changed on reopen first=%x again=%x", first, again)
	}
	must(t, reopened.DB().QueryRowContext(ctx, `SELECT count(*) FROM installation_restore_evidence`).Scan(&evidence))
	if evidence != 0 {
		t.Fatalf("reopen restore evidence count=%d", evidence)
	}
}

func TestMigration17UpgradeFromFifteenAndSixteenPreservesReviewSecret(t *testing.T) {
	ctx := context.Background()
	knownSecret := bytes.Repeat([]byte{0x11}, 32)
	for _, from := range []int{15, 16} {
		t.Run(fmt.Sprintf("from_%d", from), func(t *testing.T) {
			db, path := newMigration17Database(t, from)
			_, err := db.Exec(`UPDATE installation_status SET review_secret=? WHERE id=1`, knownSecret)
			must(t, err)
			must(t, db.Close())

			store, err := Open(ctx, path)
			must(t, err)
			defer store.Close()
			var version int
			must(t, store.DB().QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version))
			if version != 17 {
				t.Fatalf("schema version=%d from=%d", version, from)
			}
			var secret []byte
			must(t, store.DB().QueryRowContext(ctx, `SELECT review_secret FROM installation_status WHERE id=1`).Scan(&secret))
			if !bytes.Equal(secret, knownSecret) {
				t.Fatalf("review_secret changed during upgrade from %d", from)
			}
			id, err := store.InstallationIdentity(ctx)
			must(t, err)
			idHex, err := store.InstallationIdentityHex(ctx)
			must(t, err)
			mustNonZeroInstallationIdentityHex(t, idHex)
			if bytes.Equal(id, knownSecret[:16]) {
				t.Fatal("installation identity was copied from review_secret")
			}
			var evidence int
			must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM installation_restore_evidence`).Scan(&evidence))
			if evidence != 0 {
				t.Fatalf("upgrade restore evidence count=%d", evidence)
			}
		})
	}
}

func TestMigration17ConcurrentOpensShareOneIdentity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "concurrent.sqlite3")
	const n = 8
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			store, err := Open(ctx, path)
			if err != nil {
				errs[i] = err
				return
			}
			ids[i], errs[i] = store.InstallationIdentityHex(ctx)
			_ = store.Close()
		}()
	}
	wg.Wait()
	var first string
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("open %d: %v", i, errs[i])
		}
		mustNonZeroInstallationIdentityHex(t, ids[i])
		if first == "" {
			first = ids[i]
			continue
		}
		if ids[i] != first {
			t.Fatalf("concurrent identity mismatch %q vs %q", first, ids[i])
		}
	}
}

func TestMigration17DistinctDatabaseFilesHaveDistinctIdentities(t *testing.T) {
	ctx := context.Background()
	left, err := Open(ctx, filepath.Join(t.TempDir(), "left.sqlite3"))
	must(t, err)
	defer left.Close()
	right, err := Open(ctx, filepath.Join(t.TempDir(), "right.sqlite3"))
	must(t, err)
	defer right.Close()
	leftID, err := left.InstallationIdentityHex(ctx)
	must(t, err)
	rightID, err := right.InstallationIdentityHex(ctx)
	must(t, err)
	mustNonZeroInstallationIdentityHex(t, leftID)
	mustNonZeroInstallationIdentityHex(t, rightID)
	if leftID == rightID {
		t.Fatalf("distinct files shared identity %q", leftID)
	}
}

func TestMigration17RejectsIdentityMutationAndRestoreEvidenceTampering(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "constraints.sqlite3"))
	must(t, err)
	defer store.Close()
	id := readInstallationIdentity(t, store.DB())
	receipt := strings.Repeat("a", 64)
	backup := strings.Repeat("b", 64)
	must(t, insertRestoreEvidence(store.DB(), "drill-1", id, migration17At, receipt, backup, 17, "continuous-dogfood-test"))

	if _, err := store.DB().ExecContext(ctx, `UPDATE installation_status SET installation_id=randomblob(16) WHERE id=1`); err == nil {
		t.Fatal("accepted installation identity update")
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM installation_status WHERE id=1`); err == nil {
		t.Fatal("accepted installation status delete")
	}
	unchanged := readInstallationIdentity(t, store.DB())
	if !bytes.Equal(id, unchanged) {
		t.Fatal("installation identity changed after rejected mutation")
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE installation_status SET reconciliation_status='healthy',updated_at=? WHERE id=1`, migration17At); err != nil {
		t.Fatalf("ordinary status update: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `UPDATE installation_restore_evidence SET release_id='mutated' WHERE drill_id='drill-1'`); err == nil {
		t.Fatal("accepted restore evidence update")
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM installation_restore_evidence WHERE drill_id='drill-1'`); err == nil {
		t.Fatal("accepted restore evidence delete")
	}

	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-zero-identity", bytes.Repeat([]byte{0x00}, 16), migration17At, strings.Repeat("c", 64), strings.Repeat("d", 64), 17, "continuous-dogfood-test"), "all-zero installation identity")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-foreign", bytes.Repeat([]byte{0x22}, 16), migration17At, strings.Repeat("c", 64), strings.Repeat("d", 64), 17, "continuous-dogfood-test"), "foreign installation identity")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-short-receipt", id, migration17At, strings.Repeat("a", 63), backup, 17, "continuous-dogfood-test"), "short receipt digest")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-long-receipt", id, migration17At, strings.Repeat("a", 65), backup, 17, "continuous-dogfood-test"), "long receipt digest")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-upper-receipt", id, migration17At, strings.Repeat("A", 64), backup, 17, "continuous-dogfood-test"), "uppercase receipt digest")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-nonhex-receipt", id, migration17At, strings.Repeat("g", 64), backup, 17, "continuous-dogfood-test"), "non-hex receipt digest")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-short-backup", id, migration17At, strings.Repeat("e", 64), strings.Repeat("b", 63), 17, "continuous-dogfood-test"), "short backup digest")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-schema-zero", id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 0, "continuous-dogfood-test"), "schema version 0")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-schema-high", id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 10001, "continuous-dogfood-test"), "schema version overflow")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-empty-release", id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 17, ""), "empty release id")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-blank-release", id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 17, "   "), "blank release id")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-long-release", id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 17, strings.Repeat("r", 201)), "oversized release id")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "", id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 17, "continuous-dogfood-test"), "empty drill id")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "   ", id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 17, "continuous-dogfood-test"), "blank drill id")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), strings.Repeat("d", 201), id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 17, "continuous-dogfood-test"), "oversized drill id")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-empty-time", id, "", strings.Repeat("e", 64), strings.Repeat("f", 64), 17, "continuous-dogfood-test"), "empty timestamp")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-long-time", id, strings.Repeat("t", 101), strings.Repeat("e", 64), strings.Repeat("f", 64), 17, "continuous-dogfood-test"), "oversized timestamp")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-1", id, migration17At, strings.Repeat("e", 64), strings.Repeat("f", 64), 17, "continuous-dogfood-test"), "duplicate drill id")
	mustMigration17Reject(t, insertRestoreEvidence(store.DB(), "drill-dup-receipt", id, migration17At, receipt, strings.Repeat("f", 64), 17, "continuous-dogfood-test"), "duplicate receipt digest")
}

func TestEncodeInstallationIdentityRejectsAllZeroAndWrongLength(t *testing.T) {
	if _, err := EncodeInstallationIdentity(nil); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("nil identity err=%v", err)
	}
	if _, err := EncodeInstallationIdentity(make([]byte, 15)); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("short identity err=%v", err)
	}
	if _, err := EncodeInstallationIdentity(make([]byte, 16)); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("all-zero identity err=%v", err)
	}
	id := bytes.Repeat([]byte{0xab}, 16)
	got, err := EncodeInstallationIdentity(id)
	if err != nil || got != strings.Repeat("ab", 16) {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestInstallationIdentityRejectsAllZero(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "zero-identity.sqlite3"))
	must(t, err)
	defer store.Close()
	_, err = store.DB().ExecContext(ctx, `DROP TRIGGER installation_status_identity_no_update`)
	must(t, err)
	_, err = store.DB().ExecContext(ctx, `UPDATE installation_status SET installation_id=x'00000000000000000000000000000000' WHERE id=1`)
	must(t, err)
	if _, err := store.InstallationIdentity(ctx); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("zero identity err=%v", err)
	}
	if _, err := store.InstallationIdentityHex(ctx); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("zero identity hex err=%v", err)
	}
}

func TestMigration17FailureRollsBackToSixteenWithoutPartialObjects(t *testing.T) {
	ctx := context.Background()
	db, path := newMigration17Database(t, 16)
	_, err := db.Exec(`CREATE TABLE installation_restore_evidence(id INTEGER PRIMARY KEY) STRICT`)
	must(t, err)
	must(t, db.Close())

	if _, err = Open(ctx, path); err == nil {
		t.Fatal("expected migration 017 failure")
	}

	raw, err := sql.Open("sqlite", path)
	must(t, err)
	defer raw.Close()
	var version int
	must(t, raw.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version))
	if version != 16 {
		t.Fatalf("rolled-back schema version=%d", version)
	}
	var identityColumn int
	must(t, raw.QueryRow(`SELECT count(*) FROM pragma_table_info('installation_status') WHERE name='installation_id'`).Scan(&identityColumn))
	if identityColumn != 0 {
		t.Fatal("partial installation_id column survived rollback")
	}
	var dummyCols, triggers, uniqueIndex int
	must(t, raw.QueryRow(`SELECT count(*) FROM pragma_table_info('installation_restore_evidence')`).Scan(&dummyCols))
	if dummyCols != 1 {
		t.Fatalf("restore-evidence table was replaced cols=%d", dummyCols)
	}
	must(t, raw.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name IN ('installation_status_identity_no_update','installation_status_identity_no_delete','installation_restore_evidence_no_update','installation_restore_evidence_no_delete')`).Scan(&triggers))
	if triggers != 0 {
		t.Fatalf("partial 017 triggers=%d", triggers)
	}
	must(t, raw.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='installation_status_installation_id'`).Scan(&uniqueIndex))
	if uniqueIndex != 0 {
		t.Fatalf("partial identity index survived rollback")
	}
	var recorded int
	must(t, raw.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=17`).Scan(&recorded))
	if recorded != 0 {
		t.Fatal("schema_migrations recorded failed 017")
	}
}
