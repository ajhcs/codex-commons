//go:build linux

package opsbackup

import (
	"context"
	"database/sql"
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
