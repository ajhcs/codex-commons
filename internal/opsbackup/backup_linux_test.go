//go:build linux

package opsbackup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/opsfs"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

func TestMain(m *testing.M) {
	unix.Umask(0o077)
	os.Exit(m.Run())
}

func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeSource(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE schema_migrations(version INTEGER);
INSERT INTO schema_migrations VALUES(17);
CREATE TABLE projects(id INTEGER);
INSERT INTO projects VALUES(1);
CREATE TABLE tasks(id INTEGER);
INSERT INTO tasks VALUES(1),(2);
CREATE TABLE archaeology_native_batches(id INTEGER);
INSERT INTO archaeology_native_batches VALUES(1);
CREATE TABLE installation_status(
  id INTEGER PRIMARY KEY CHECK (id=1),
  backup_status TEXT NOT NULL DEFAULT 'unknown',
  backup_verified_at TEXT,
  updated_at TEXT
);
INSERT INTO installation_status(id) VALUES(1);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func setupBackupTree(t *testing.T) (dbPath, backupDir string) {
	t.Helper()
	root := privateDir(t)
	srcDir := filepath.Join(root, "src")
	backupDir = filepath.Join(root, "backups")
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath = filepath.Join(srcDir, "commons.sqlite3")
	writeSource(t, dbPath)
	return dbPath, backupDir
}

func TestBackupPublishesSanitizedSidecars(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })

	path, err := Backup(context.Background(), dbPath, backupDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(backupDir, "daily", "commons-20260820T120000Z.sqlite3")
	if path != want {
		t.Fatalf("published %q want %q", path, want)
	}
	for _, name := range []string{path, path + ".sha256", path + ".receipt.json"} {
		var st unix.Stat_t
		if err := unix.Lstat(name, &st); err != nil {
			t.Fatal(err)
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Mode&0o7777 != 0o600 {
			t.Fatalf("%s mode/nlink %o %d", name, st.Mode, st.Nlink)
		}
	}
	if _, err := os.Lstat(filepath.Join(backupDir, ".backup.lock")); !os.IsNotExist(err) {
		t.Fatal("created pathname .backup.lock")
	}
	body, err := os.ReadFile(path + ".receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"file", "sha256", "verified_at", "schema", "schema_digest", "counts", "selected_digest", "integrity", "foreign_keys"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("missing receipt field %s in %s", key, body)
		}
	}
	if len(doc) != 9 {
		t.Fatalf("receipt has unexpected fields: %s", body)
	}
	raw := string(body)
	for _, banned := range []string{"result_json", "prompt", "secret", "transcript", "review_secret", "password", "token"} {
		if strings.Contains(raw, banned) {
			t.Fatalf("receipt leaked %s: %s", banned, raw)
		}
	}
	if !strings.Contains(raw, `"schema":17`) || !strings.Contains(raw, `"counts":"1,2,1,0"`) {
		t.Fatalf("receipt metadata: %s", raw)
	}
	monthly := filepath.Join(backupDir, "monthly", "commons-2026-08.sqlite3")
	if _, err := os.Lstat(monthly); err != nil {
		t.Fatal(err)
	}
	var status string
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT backup_status FROM installation_status WHERE id=1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "verified" {
		t.Fatalf("backup_status=%q", status)
	}

	second, err := Backup(context.Background(), dbPath, backupDir)
	if err == nil {
		t.Fatalf("second same-stamp backup succeeded: %s", second)
	}
}

func TestBackupRejectsSymlinkFIFOModeAndExisting(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 12, 1, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })

	alias := backupDir + "-alias"
	if err := os.Symlink(backupDir, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), dbPath, alias); err == nil {
		t.Fatal("accepted symlinked backup root")
	}

	if err := os.Mkdir(filepath.Join(backupDir, "daily"), 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(backupDir, "daily", "commons-20260820T120100Z.sqlite3")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
		t.Fatal("accepted FIFO destination")
	}
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
		t.Fatal("accepted mode 0755 backup root")
	}
	if err := os.Chmod(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}

	keep := filepath.Join(backupDir, "daily", "commons-20260820T120100Z.sqlite3")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
		t.Fatal("replaced existing target")
	}
	got, err := os.ReadFile(keep)
	if err != nil || string(got) != "keep" {
		t.Fatalf("existing target mutated: %q %v", got, err)
	}
}

func TestBackupConcurrentLock(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	hold := filepath.Join(filepath.Dir(backupDir), "hold")
	status := filepath.Join(filepath.Dir(backupDir), "status")
	if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMONS_OPS_HOLD", hold)
	t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldPreOpen)
	t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := Backup(context.Background(), dbPath, backupDir)
		done <- err
	}()
	<-started
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(status); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hold status missing")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err := Backup(context.Background(), dbPath, backupDir)
	if err != ErrBusy {
		t.Fatalf("concurrent backup err=%v want ErrBusy", err)
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRetentionSkipsSymlinkDirHardlinkAndMode(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	daily := filepath.Join(backupDir, "daily")
	if err := os.Mkdir(daily, 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 32; i++ {
		stamp := base.Add(time.Duration(i) * time.Hour).Format("20060102T150405Z")
		name := filepath.Join(daily, "commons-"+stamp+".sqlite3")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, base.Add(time.Duration(i)*time.Hour), base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("commons-20260101T000000Z.sqlite3", filepath.Join(daily, "commons-symlink.sqlite3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(daily, "commons-dir.sqlite3"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(daily, "commons-20260101T000000Z.sqlite3"), filepath.Join(daily, "commons-hard.sqlite3")); err != nil {
		t.Fatal(err)
	}
	wide := filepath.Join(daily, "commons-wide.sqlite3")
	if err := os.WriteFile(wide, []byte("wide"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wide, 0o644); err != nil {
		t.Fatal(err)
	}

	nowUTC = func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })
	if _, err := Backup(context.Background(), dbPath, backupDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(daily, "commons-symlink.sqlite3")); err != nil {
		t.Fatal("retention deleted symlink")
	}
	if _, err := os.Stat(filepath.Join(daily, "commons-dir.sqlite3")); err != nil {
		t.Fatal("retention deleted directory")
	}
	if _, err := os.Lstat(filepath.Join(daily, "commons-hard.sqlite3")); err != nil {
		t.Fatal("retention deleted hard link")
	}
	if _, err := os.Lstat(wide); err != nil {
		t.Fatal("retention deleted foreign mode file")
	}
	var validated int
	entries, err := os.ReadDir(daily)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "commons-") || !strings.HasSuffix(entry.Name(), ".sqlite3") {
			continue
		}
		info, err := os.Lstat(filepath.Join(daily, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			continue
		}
		var st unix.Stat_t
		if err := unix.Lstat(filepath.Join(daily, entry.Name()), &st); err != nil || st.Nlink != 1 {
			continue
		}
		validated++
	}
	if validated != dailyKeep {
		t.Fatalf("validated regular backups = %d, want %d", validated, dailyKeep)
	}
}

func TestBackupMonthlyNoReplaceSymlink(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	monthly := filepath.Join(backupDir, "monthly")
	if err := os.Mkdir(monthly, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing.sqlite3", filepath.Join(monthly, "commons-2026-08.sqlite3")); err != nil {
		t.Fatal(err)
	}
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })
	if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
		t.Fatal("replaced monthly symlink")
	}
	var st unix.Stat_t
	if err := unix.Lstat(filepath.Join(monthly, "commons-2026-08.sqlite3"), &st); err != nil {
		t.Fatal(err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFLNK {
		t.Fatal("monthly symlink was replaced")
	}
}

func TestBackupRootSwapDetected(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	hold := filepath.Join(filepath.Dir(backupDir), "hold")
	status := filepath.Join(filepath.Dir(backupDir), "status")
	if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMONS_OPS_HOLD", hold)
	t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldPrePublish)
	t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := Backup(context.Background(), dbPath, backupDir)
		errCh <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(status); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hold status missing")
		}
		time.Sleep(20 * time.Millisecond)
	}
	swapped := backupDir + ".swapped"
	if err := os.Rename(backupDir, swapped); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if err := <-errCh; err == nil {
		t.Fatal("backup succeeded after root swap")
	}
}

func waitHoldReady(t *testing.T, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(status); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("hold status missing")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func plantPublishedSet(t *testing.T, dirPath, leaf, body, stamp string) {
	t.Helper()
	if err := os.MkdirAll(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dirPath, leaf)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	digest := hex.EncodeToString(sum[:])
	checksum, err := opsfs.FormatSHA256Sum(digest, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".sha256", checksum, 0o600); err != nil {
		t.Fatal(err)
	}
	receiptBody, err := marshalReceipt(receipt{
		File:           leaf,
		SHA256:         digest,
		VerifiedAt:     stamp,
		Schema:         17,
		SchemaDigest:   strings.Repeat("ab", 32),
		Counts:         "1,2,1,0",
		SelectedDigest: strings.Repeat("cd", 32),
		Integrity:      "ok",
		ForeignKeys:    0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".receipt.json", receiptBody, 0o600); err != nil {
		t.Fatal(err)
	}
}

func countValidatedBackups(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "commons-") || !strings.HasSuffix(entry.Name(), ".sqlite3") {
			continue
		}
		var st unix.Stat_t
		if err := unix.Lstat(filepath.Join(dir, entry.Name()), &st); err != nil {
			continue
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Mode&0o7777 != 0o600 {
			continue
		}
		n++
	}
	return n
}

func TestBackupRejectsMissingRoot(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	missing := filepath.Join(filepath.Dir(backupDir), "missing")
	if _, err := Backup(context.Background(), dbPath, missing); err == nil {
		t.Fatal("created or accepted a missing backup root")
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing backup root was created: %v", err)
	}
}

func TestBackupMonthlyCoherentSet(t *testing.T) {
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 16, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })

	t.Run("valid", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		leaf := "commons-2026-08.sqlite3"
		plantPublishedSet(t, monthly, leaf, "keep-monthly", "20260801T000000Z")
		keep, err := os.ReadFile(filepath.Join(monthly, leaf))
		if err != nil {
			t.Fatal(err)
		}
		path, err := Backup(context.Background(), dbPath, backupDir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(monthly, leaf))
		if err != nil || string(got) != string(keep) {
			t.Fatalf("valid monthly set mutated: %q %v", got, err)
		}
		if _, err := os.Lstat(path); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("malformed-receipt", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		leaf := "commons-2026-08.sqlite3"
		plantPublishedSet(t, monthly, leaf, "keep-monthly", "20260801T000000Z")
		if err := os.WriteFile(filepath.Join(monthly, leaf+".receipt.json"), []byte("{not json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("accepted malformed monthly receipt")
		}
		got, err := os.ReadFile(filepath.Join(monthly, leaf))
		if err != nil || string(got) != "keep-monthly" {
			t.Fatalf("malformed monthly mutated backup: %q %v", got, err)
		}
	})

	t.Run("malformed-checksum", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		leaf := "commons-2026-08.sqlite3"
		plantPublishedSet(t, monthly, leaf, "keep-monthly", "20260801T000000Z")
		if err := os.WriteFile(filepath.Join(monthly, leaf+".sha256"), []byte("not a checksum\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("accepted malformed monthly checksum")
		}
	})

	t.Run("mismatched-checksum", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		leaf := "commons-2026-08.sqlite3"
		plantPublishedSet(t, monthly, leaf, "keep-monthly", "20260801T000000Z")
		zeros := strings.Repeat("0", 64)
		body, err := opsfs.FormatSHA256Sum(zeros, filepath.Join(monthly, leaf))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(monthly, leaf+".sha256"), body, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("accepted mismatched monthly checksum")
		}
		got, err := os.ReadFile(filepath.Join(monthly, leaf))
		if err != nil || string(got) != "keep-monthly" {
			t.Fatalf("mismatched monthly mutated backup: %q %v", got, err)
		}
	})

	t.Run("missing-receipt", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		leaf := "commons-2026-08.sqlite3"
		plantPublishedSet(t, monthly, leaf, "keep-monthly", "20260801T000000Z")
		if err := os.Remove(filepath.Join(monthly, leaf+".receipt.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("accepted monthly backup without receipt")
		}
		got, err := os.ReadFile(filepath.Join(monthly, leaf))
		if err != nil || string(got) != "keep-monthly" {
			t.Fatalf("incomplete monthly mutated backup: %q %v", got, err)
		}
	})

	t.Run("mismatched-receipt", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		leaf := "commons-2026-08.sqlite3"
		plantPublishedSet(t, monthly, leaf, "keep-monthly", "20260801T000000Z")
		body, err := marshalReceipt(receipt{
			File:           leaf,
			SHA256:         strings.Repeat("e", 64),
			VerifiedAt:     "20260801T000000Z",
			Schema:         17,
			SchemaDigest:   strings.Repeat("ab", 32),
			Counts:         "1,2,1,0",
			SelectedDigest: strings.Repeat("cd", 32),
			Integrity:      "ok",
			ForeignKeys:    0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(monthly, leaf+".receipt.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("accepted mismatched monthly receipt")
		}
	})
}

func TestBackupDailyAndMonthlySwap(t *testing.T) {
	t.Run("daily", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		hold := filepath.Join(filepath.Dir(backupDir), "hold")
		status := filepath.Join(filepath.Dir(backupDir), "status")
		if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("COMMONS_OPS_HOLD", hold)
		t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldPrePublish)
		t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

		errCh := make(chan error, 1)
		go func() {
			_, err := Backup(context.Background(), dbPath, backupDir)
			errCh <- err
		}()
		waitHoldReady(t, status)
		swapped := backupDir + "-daily-swapped"
		if err := os.Rename(filepath.Join(backupDir, "daily"), swapped); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(backupDir, "daily"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(hold); err != nil {
			t.Fatal(err)
		}
		if err := <-errCh; err == nil {
			t.Fatal("backup succeeded after daily swap")
		}
		entries, err := os.ReadDir(filepath.Join(backupDir, "daily"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("swapped-in daily was mutated: %v", entries)
		}
	})

	t.Run("monthly", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		hold := filepath.Join(filepath.Dir(backupDir), "hold")
		status := filepath.Join(filepath.Dir(backupDir), "status")
		if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("COMMONS_OPS_HOLD", hold)
		t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldAfterPublications)
		t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

		errCh := make(chan error, 1)
		go func() {
			_, err := Backup(context.Background(), dbPath, backupDir)
			errCh <- err
		}()
		waitHoldReady(t, status)
		swapped := backupDir + "-monthly-swapped"
		if err := os.Rename(filepath.Join(backupDir, "monthly"), swapped); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(backupDir, "monthly"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(hold); err != nil {
			t.Fatal(err)
		}
		if err := <-errCh; err == nil {
			t.Fatal("backup succeeded after monthly swap")
		}
		entries, err := os.ReadDir(filepath.Join(backupDir, "monthly"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("swapped-in monthly was mutated: %v", entries)
		}
		if countValidatedBackups(t, filepath.Join(backupDir, "daily")) == 0 {
			t.Fatal("daily publication was unlinked after monthly swap")
		}
	})
}

func TestBackupRejectsMonthlyCorruptionBeforeVerified(t *testing.T) {
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })

	dbPath, backupDir := setupBackupTree(t)
	hold := filepath.Join(filepath.Dir(backupDir), "hold")
	status := filepath.Join(filepath.Dir(backupDir), "status")
	if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMONS_OPS_HOLD", hold)
	t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldAfterMonthlyPublications)
	t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

	errCh := make(chan error, 1)
	go func() {
		_, err := Backup(context.Background(), dbPath, backupDir)
		errCh <- err
	}()
	waitHoldReady(t, status)
	monthly := filepath.Join(backupDir, "monthly", "commons-2026-08.sqlite3")
	if err := os.WriteFile(monthly, []byte("corrupted-monthly"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err == nil {
		t.Fatal("backup succeeded after monthly corruption")
	}
	var backupStatus string
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT backup_status FROM installation_status WHERE id=1`).Scan(&backupStatus); err != nil {
		t.Fatal(err)
	}
	if backupStatus == "verified" {
		t.Fatalf("backup_status=%q after monthly corruption", backupStatus)
	}
}

func TestBackupRejectsDatabaseParentSwap(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	srcDir := filepath.Dir(dbPath)
	hold := filepath.Join(filepath.Dir(backupDir), "hold")
	status := filepath.Join(filepath.Dir(backupDir), "status")
	if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMONS_OPS_HOLD", hold)
	t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldPreOpen)
	t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

	errCh := make(chan error, 1)
	go func() {
		_, err := Backup(context.Background(), dbPath, backupDir)
		errCh <- err
	}()
	waitHoldReady(t, status)

	swapped := srcDir + "-swapped"
	if err := os.Rename(srcDir, swapped); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(srcDir, "commons.sqlite3")
	if err := os.WriteFile(replacement, []byte("replacement-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err == nil {
		t.Fatal("backup succeeded after database parent swap")
	}
	after, err := os.ReadFile(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("replacement path mutated: %q -> %q", before, after)
	}
	for _, sibling := range []string{replacement + "-wal", replacement + "-shm", replacement + "-journal"} {
		if _, err := os.Lstat(sibling); !os.IsNotExist(err) {
			t.Fatalf("replacement sibling created: %s (%v)", sibling, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(backupDir, "daily"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("backup mutated daily after parent swap: %v", entries)
	}
}

func TestBackupSidecarCollision(t *testing.T) {
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 17, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })

	t.Run("daily", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		daily := filepath.Join(backupDir, "daily")
		if err := os.Mkdir(daily, 0o700); err != nil {
			t.Fatal(err)
		}
		sidecar := filepath.Join(daily, "commons-20260820T170000Z.sqlite3.sha256")
		if err := os.WriteFile(sidecar, []byte("planted\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("replaced daily checksum sidecar")
		}
		got, err := os.ReadFile(sidecar)
		if err != nil || string(got) != "planted\n" {
			t.Fatalf("daily checksum mutated: %q %v", got, err)
		}
		published := filepath.Join(daily, "commons-20260820T170000Z.sqlite3")
		if _, err := os.Lstat(published); err != nil {
			t.Fatal("expected daily sqlite to remain after sidecar collision")
		}
	})

	t.Run("monthly", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		if err := os.Mkdir(monthly, 0o700); err != nil {
			t.Fatal(err)
		}
		sidecar := filepath.Join(monthly, "commons-2026-08.sqlite3.sha256")
		if err := os.WriteFile(sidecar, []byte("planted-monthly\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("replaced monthly checksum sidecar")
		}
		got, err := os.ReadFile(sidecar)
		if err != nil || string(got) != "planted-monthly\n" {
			t.Fatalf("monthly checksum mutated: %q %v", got, err)
		}
		leaf := filepath.Join(monthly, "commons-2026-08.sqlite3")
		if _, err := os.Lstat(leaf); !os.IsNotExist(err) {
			t.Fatalf("this-invocation monthly leaf should be rolled back: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(monthly, "commons-2026-08.sqlite3.receipt.json")); !os.IsNotExist(err) {
			t.Fatal("unexpected monthly receipt after rollback")
		}
		dailyLeaf := filepath.Join(backupDir, "daily", "commons-20260820T170000Z.sqlite3")
		if _, err := os.Lstat(dailyLeaf); err != nil {
			t.Fatal("daily publication must remain unchanged after monthly rollback")
		}
	})
}

func TestBackupMonthlyPartialPublishRollback(t *testing.T) {
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })

	t.Run("between-monthly-publications-cancel", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		hold := filepath.Join(filepath.Dir(backupDir), "hold")
		status := filepath.Join(filepath.Dir(backupDir), "status")
		if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("COMMONS_OPS_HOLD", hold)
		t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldBetweenMonthlyPublications)
		t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			_, err := Backup(ctx, dbPath, backupDir)
			errCh <- err
		}()
		waitHoldReady(t, status)
		monthLeaf := filepath.Join(backupDir, "monthly", "commons-2026-08.sqlite3")
		if _, err := os.Lstat(monthLeaf); err != nil {
			t.Fatal("expected this-invocation monthly leaf before cancel")
		}
		cancel()
		if err := <-errCh; err == nil {
			t.Fatal("backup succeeded after between-monthly cancel")
		}
		for _, name := range []string{
			"commons-2026-08.sqlite3",
			"commons-2026-08.sqlite3.sha256",
			"commons-2026-08.sqlite3.receipt.json",
		} {
			if _, err := os.Lstat(filepath.Join(backupDir, "monthly", name)); !os.IsNotExist(err) {
				t.Fatalf("this-invocation monthly leaf %s should be rolled back: %v", name, err)
			}
		}
		dailyLeaf := filepath.Join(backupDir, "daily", "commons-20260820T173000Z.sqlite3")
		if _, err := os.Lstat(dailyLeaf); err != nil {
			t.Fatal("daily publication must remain after monthly cancel rollback")
		}

		// Retry/liveness: clear hold seam, advance stamp, complete a full backup.
		t.Setenv("COMMONS_OPS_HOLD", "")
		t.Setenv("COMMONS_OPS_HOLD_POINT", "")
		t.Setenv("COMMONS_OPS_HOLD_STATUS", "")
		nowUTC = func() time.Time { return time.Date(2026, 8, 21, 17, 30, 0, 0, time.UTC) }
		if _, err := Backup(context.Background(), dbPath, backupDir); err != nil {
			t.Fatalf("retry after monthly rollback failed: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(backupDir, "monthly", "commons-2026-08.sqlite3")); err != nil {
			t.Fatal("retry did not publish monthly set")
		}
		if _, err := os.Lstat(filepath.Join(backupDir, "monthly", "commons-2026-08.sqlite3.sha256")); err != nil {
			t.Fatal("retry missing monthly checksum")
		}
		if _, err := os.Lstat(filepath.Join(backupDir, "monthly", "commons-2026-08.sqlite3.receipt.json")); err != nil {
			t.Fatal("retry missing monthly receipt")
		}
	})

	t.Run("planted-receipt-rolls-back-this-invocation-leaves", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		if err := os.Mkdir(monthly, 0o700); err != nil {
			t.Fatal(err)
		}
		planted := filepath.Join(monthly, "commons-2026-08.sqlite3.receipt.json")
		if err := os.WriteFile(planted, []byte("planted-receipt\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("replaced planted monthly receipt")
		}
		got, err := os.ReadFile(planted)
		if err != nil || string(got) != "planted-receipt\n" {
			t.Fatalf("planted receipt mutated: %q %v", got, err)
		}
		for _, name := range []string{"commons-2026-08.sqlite3", "commons-2026-08.sqlite3.sha256"} {
			if _, err := os.Lstat(filepath.Join(monthly, name)); !os.IsNotExist(err) {
				t.Fatalf("this-invocation monthly leaf %s should be rolled back", name)
			}
		}
	})

	t.Run("no-unsafe-unlink-after-replacement", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		if err := os.Mkdir(monthly, 0o700); err != nil {
			t.Fatal(err)
		}
		sidecar := filepath.Join(monthly, "commons-2026-08.sqlite3.sha256")
		if err := os.WriteFile(sidecar, []byte("planted-monthly\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		repl := filepath.Join(monthly, "planted-repl")
		if err := os.WriteFile(repl, []byte("replacement-monthly"), 0o600); err != nil {
			t.Fatal(err)
		}
		hold := filepath.Join(filepath.Dir(backupDir), "hold")
		status := filepath.Join(filepath.Dir(backupDir), "status")
		if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("COMMONS_OPS_HOLD", hold)
		t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldPreUnlink)
		t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

		errCh := make(chan error, 1)
		go func() {
			_, err := Backup(context.Background(), dbPath, backupDir)
			errCh <- err
		}()
		waitHoldReady(t, status)
		leaf := filepath.Join(monthly, "commons-2026-08.sqlite3")
		if err := os.Remove(leaf); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(repl, leaf); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(hold); err != nil {
			t.Fatal(err)
		}
		if err := <-errCh; err == nil {
			t.Fatal("backup succeeded despite planted monthly sidecar")
		}
		got, err := os.ReadFile(leaf)
		if err != nil || string(got) != "replacement-monthly" {
			t.Fatalf("rollback unlinked a replaced same-uid monthly leaf: %q %v", got, err)
		}
		planted, err := os.ReadFile(sidecar)
		if err != nil || string(planted) != "planted-monthly\n" {
			t.Fatalf("planted monthly checksum mutated: %q %v", planted, err)
		}
	})
}

func TestBackupRejectsHardlinks(t *testing.T) {
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })

	t.Run("live-db", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		alias := dbPath + ".alias"
		if err := os.Link(dbPath, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("accepted hardlinked live database")
		}
		daily := filepath.Join(backupDir, "daily")
		if _, err := os.Stat(daily); err == nil && countValidatedBackups(t, daily) != 0 {
			t.Fatal("published a backup from a hardlinked live database")
		}
	})

	t.Run("daily-destination", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		daily := filepath.Join(backupDir, "daily")
		if err := os.Mkdir(daily, 0o700); err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(daily, "other.sqlite3")
		if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(daily, "commons-20260820T180000Z.sqlite3")
		if err := os.Link(other, dest); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("replaced hardlinked daily destination")
		}
		got, err := os.ReadFile(dest)
		if err != nil || string(got) != "keep" {
			t.Fatalf("hardlinked daily destination mutated: %q %v", got, err)
		}
	})

	t.Run("monthly-existing", func(t *testing.T) {
		dbPath, backupDir := setupBackupTree(t)
		monthly := filepath.Join(backupDir, "monthly")
		if err := os.Mkdir(monthly, 0o700); err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(monthly, "other.sqlite3")
		if err := os.WriteFile(other, []byte("keep-month"), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(monthly, "commons-2026-08.sqlite3")
		if err := os.Link(other, dest); err != nil {
			t.Fatal(err)
		}
		if _, err := Backup(context.Background(), dbPath, backupDir); err == nil {
			t.Fatal("accepted hardlinked monthly backup")
		}
		got, err := os.ReadFile(dest)
		if err != nil || string(got) != "keep-month" {
			t.Fatalf("hardlinked monthly mutated: %q %v", got, err)
		}
	})
}

func TestMonthlyRetentionLimit(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	monthly := filepath.Join(backupDir, "monthly")
	if err := os.Mkdir(monthly, 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 13; i++ {
		stamp := base.AddDate(0, i, 0)
		leaf := "commons-" + stamp.Format("2006-01") + ".sqlite3"
		name := filepath.Join(monthly, leaf)
		if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	nowUTC = func() time.Time { return time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowUTC = func() time.Time { return time.Now().UTC() } })
	if _, err := Backup(context.Background(), dbPath, backupDir); err != nil {
		t.Fatal(err)
	}
	if got := countValidatedBackups(t, monthly); got != monthlyKeep {
		t.Fatalf("validated monthly backups = %d, want %d", got, monthlyKeep)
	}
}

func TestRetentionSameUIDNameRaceSkipped(t *testing.T) {
	// Retention revalidates the same inode after the first fd-relative check
	// and skips the unlink when a same-uid actor replaced the name with a
	// symlink. The last close-to-unlinkat(2) window remains a name-based
	// race and is not claimed closed.

	dbPath, backupDir := setupBackupTree(t)
	daily := filepath.Join(backupDir, "daily")
	if err := os.Mkdir(daily, 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	oldest := filepath.Join(daily, "commons-20260101T000000Z.sqlite3")
	for i := 0; i < 32; i++ {
		stamp := base.Add(time.Duration(i) * time.Hour).Format("20060102T150405Z")
		name := filepath.Join(daily, "commons-"+stamp+".sqlite3")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, base.Add(time.Duration(i)*time.Hour), base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	hold := filepath.Join(filepath.Dir(backupDir), "hold")
	status := filepath.Join(filepath.Dir(backupDir), "status")
	if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMONS_OPS_HOLD", hold)
	t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldPreUnlink)
	t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

	errCh := make(chan error, 1)
	go func() {
		nowUTC = func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) }
		defer func() { nowUTC = func() time.Time { return time.Now().UTC() } }()
		_, err := Backup(context.Background(), dbPath, backupDir)
		errCh <- err
	}()
	waitHoldReady(t, status)
	if err := os.Remove(oldest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("commons-20260102T070000Z.sqlite3", oldest); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	if err := unix.Lstat(oldest, &st); err != nil {
		t.Fatal("retention unlinked a name that became a symlink")
	}
	if st.Mode&unix.S_IFMT != unix.S_IFLNK {
		t.Fatal("expected planted symlink to remain")
	}
}

func TestRetentionSameUIDInodeRaceSkipped(t *testing.T) {
	dbPath, backupDir := setupBackupTree(t)
	daily := filepath.Join(backupDir, "daily")
	if err := os.Mkdir(daily, 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	oldest := filepath.Join(daily, "commons-20260101T000000Z.sqlite3")
	repl := filepath.Join(daily, "planted-repl")
	if err := os.WriteFile(repl, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		stamp := base.Add(time.Duration(i) * time.Hour).Format("20060102T150405Z")
		name := filepath.Join(daily, "commons-"+stamp+".sqlite3")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, base.Add(time.Duration(i)*time.Hour), base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	hold := filepath.Join(filepath.Dir(backupDir), "hold")
	status := filepath.Join(filepath.Dir(backupDir), "status")
	if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMONS_OPS_HOLD", hold)
	t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldPreUnlink)
	t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

	errCh := make(chan error, 1)
	go func() {
		nowUTC = func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) }
		defer func() { nowUTC = func() time.Time { return time.Now().UTC() } }()
		_, err := Backup(context.Background(), dbPath, backupDir)
		errCh <- err
	}()
	waitHoldReady(t, status)
	if err := os.Remove(oldest); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repl, oldest); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(oldest)
	if err != nil || string(got) != "replacement" {
		t.Fatalf("retention unlinked a replaced same-uid regular file: %q %v", got, err)
	}
}

func TestParseReceiptRejectsMalformed(t *testing.T) {
	good, err := marshalReceipt(receipt{
		File:           "commons-20260820T120000Z.sqlite3",
		SHA256:         strings.Repeat("ab", 32),
		VerifiedAt:     "20260820T120000Z",
		Schema:         17,
		SchemaDigest:   strings.Repeat("cd", 32),
		Counts:         "1,2,1,0",
		SelectedDigest: strings.Repeat("ef", 32),
		Integrity:      "ok",
		ForeignKeys:    0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseReceipt(good); err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{
		[]byte("{"),
		[]byte("{}\n"),
		append(append([]byte{}, good[:len(good)-1]...), []byte(`{"extra":1}`+"\n")...),
		[]byte(strings.TrimSpace(string(good))[:len(strings.TrimSpace(string(good)))-1] + `, "extra":1}` + "\n"),
	} {
		if _, err := parseReceipt(body); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}
