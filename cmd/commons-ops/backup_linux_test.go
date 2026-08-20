//go:build linux

package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/opsfs"
	_ "modernc.org/sqlite"
)

func setupOpsTree(t *testing.T) (dbPath, backupDir string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(root, "src")
	backupDir = filepath.Join(root, "backups")
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath = filepath.Join(srcDir, "commons.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE schema_migrations(version INTEGER);
INSERT INTO schema_migrations VALUES(17);
CREATE TABLE projects(id INTEGER);
INSERT INTO projects VALUES(1);
CREATE TABLE tasks(id INTEGER);
INSERT INTO tasks VALUES(1);
CREATE TABLE archaeology_native_batches(id INTEGER);
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
	if err := os.Chmod(dbPath, 0o600); err != nil {
		t.Fatal(err)
	}
	return dbPath, backupDir
}

func TestRunBackupCLISuccessAndFailures(t *testing.T) {
	dbPath, backupDir := setupOpsTree(t)
	t.Setenv("COMMONS_DB", dbPath)
	t.Setenv("COMMONS_BACKUP_DIR", backupDir)

	var stdout, stderr bytes.Buffer
	if got := run([]string{"backup"}, &stdout, &stderr); got != 0 {
		t.Fatalf("success exit %d stderr=%q", got, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("success wrote stderr %q", stderr.String())
	}
	out := stdout.String()
	if !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "/") {
		t.Fatalf("success stdout %q is not one newline-terminated absolute path", out)
	}
	published := strings.TrimSuffix(out, "\n")
	if st, err := os.Lstat(published); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("published path %s: %v", published, err)
	}

	keep, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(filepath.Dir(backupDir), "missing")
	t.Setenv("COMMONS_BACKUP_DIR", missing)
	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"backup"}, &stdout, &stderr); got != 64 {
		t.Fatalf("missing root exit %d want 64", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("missing root wrote stdout %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "rejected") {
		t.Fatalf("missing root stderr %q", stderr.String())
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatal("CLI created a missing backup root")
	}

	t.Setenv("COMMONS_BACKUP_DIR", backupDir)
	hold := filepath.Join(filepath.Dir(backupDir), "hold")
	status := filepath.Join(filepath.Dir(backupDir), "status")
	if err := os.WriteFile(hold, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMONS_OPS_HOLD", hold)
	t.Setenv("COMMONS_OPS_HOLD_POINT", opsfs.HoldPreOpen)
	t.Setenv("COMMONS_OPS_HOLD_STATUS", status)

	done := make(chan int, 1)
	go func() {
		var heldOut, heldErr bytes.Buffer
		done <- run([]string{"backup"}, &heldOut, &heldErr)
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
	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"backup"}, &stdout, &stderr); got != 75 {
		t.Fatalf("busy exit %d want 75 stderr=%q", got, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("busy wrote stdout %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "busy") {
		t.Fatalf("busy stderr %q", stderr.String())
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	heldCode := <-done
	if heldCode != 0 && heldCode != 64 {
		t.Fatalf("held backup exit %d", heldCode)
	}

	got, err := os.ReadFile(published)
	if err != nil || !bytes.Equal(got, keep) {
		t.Fatalf("CLI failure mutated published backup: %v", err)
	}
}
