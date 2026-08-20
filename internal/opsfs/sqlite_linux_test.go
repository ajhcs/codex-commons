//go:build linux

package opsfs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

func TestMain(m *testing.M) {
	unix.Umask(0o077)
	os.Exit(m.Run())
}

func testDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t(x INTEGER); INSERT INTO t VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, FileMode); err != nil {
		t.Fatal(err)
	}
}

func TestDirFDURICreatesWALSidecarsBesideLeaf(t *testing.T) {
	dir := testDir(t)
	path := filepath.Join(dir, "pinned.sqlite3")
	writeDB(t, path)
	pin, err := PinDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	ctx := context.Background()
	db, err := OpenPinned(ctx, pin, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO t VALUES(2)`); err != nil {
		t.Fatal(err)
	}
	if err := pin.Revalidate(); err != nil {
		t.Fatal(err)
	}
	wal := path + "-wal"
	shm := path + "-shm"
	for _, sibling := range []string{wal, shm} {
		var st unix.Stat_t
		if err := unix.Lstat(sibling, &st); err != nil {
			t.Fatalf("missing sidecar %s: %v", sibling, err)
		}
		if !isReg(&st) {
			t.Fatalf("%s is not a regular file", sibling)
		}
		if perm(&st)&0o777 != FileMode {
			t.Fatalf("%s mode %o", sibling, perm(&st))
		}
		if st.Nlink != 1 {
			t.Fatalf("%s nlink=%d", sibling, st.Nlink)
		}
	}
}

func TestPinnedURIUsesDirectoryFDAndLeaf(t *testing.T) {
	dir := testDir(t)
	path := filepath.Join(dir, "pinned.sqlite3")
	writeDB(t, path)
	pin, err := PinDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	uri := pin.URI(sqliteOpenParams(true))
	want := "file:/proc/self/fd/" + strconv.Itoa(pin.Dir.FD) + "/" + pin.Leaf
	if uri != want+"?"+sqliteOpenParams(true) {
		t.Fatalf("uri=%q want prefix %q", uri, want)
	}
}

func TestPinDatabaseRejectsHardlinkAndModeDrift(t *testing.T) {
	dir := testDir(t)
	path := filepath.Join(dir, "main.sqlite3")
	writeDB(t, path)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PinDatabase(path); err == nil {
		t.Fatal("accepted mode 0644")
	}
	if err := os.Chmod(path, FileMode); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "hard.sqlite3")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PinDatabase(path); err == nil {
		t.Fatal("accepted hardlinked main db")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	wal := path + "-wal"
	if err := os.WriteFile(wal, []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wal, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PinDatabase(path); err == nil {
		t.Fatal("accepted 0644 WAL")
	}
	if err := os.Remove(wal); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-wal", path+"-shm"); err != nil {
		t.Fatal(err)
	}
	if _, err := PinDatabase(path); err == nil {
		t.Fatal("accepted SHM symlink")
	}
}

func TestOpenBackupDirRejectsSymlinkAndMode(t *testing.T) {
	root := testDir(t)
	realDir := filepath.Join(root, "backups")
	if err := os.Mkdir(realDir, DirMode); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenBackupDir(realDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenBackupDir(realDir); err == nil {
		t.Fatal("accepted mode 0755 backup dir")
	}
	if err := os.Chmod(realDir, DirMode); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenBackupDir(alias); err == nil {
		t.Fatal("accepted symlinked backup dir")
	}
}

func TestFlockExclusiveNonblockBusy(t *testing.T) {
	root := testDir(t)
	path := filepath.Join(root, "backups")
	if err := os.Mkdir(path, DirMode); err != nil {
		t.Fatal(err)
	}
	first, err := OpenBackupDir(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.FlockExclusiveNonblock(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenBackupDir(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.FlockExclusiveNonblock(); err != ErrBusy {
		t.Fatalf("second flock = %v, want ErrBusy", err)
	}
}

func TestPinnedDBRevalidateRejectsParentSwap(t *testing.T) {
	root := testDir(t)
	srcDir := filepath.Join(root, "src")
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(srcDir, "commons.sqlite3")
	writeDB(t, path)
	pin, err := PinDatabaseUnlocked(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()

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

	if err := pin.Revalidate(); err == nil {
		t.Fatal("revalidate succeeded after parent swap")
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
}
