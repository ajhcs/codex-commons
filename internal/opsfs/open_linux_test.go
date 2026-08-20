//go:build linux

package opsfs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWritePublishAndRetentionSafety(t *testing.T) {
	root := testDir(t)
	dirPath := filepath.Join(root, "daily")
	if err := os.Mkdir(dirPath, DirMode); err != nil {
		t.Fatal(err)
	}
	dir, err := OpenBackupDir(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	tmp, tmpName, err := dir.MkdirPrivate("backup")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = tmp.Close()
		_ = dir.RemoveDir(tmpName)
	}()
	if err := tmp.WriteExclusive("leaf.sqlite3", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := dir.PublishNoReplace(tmp, "leaf.sqlite3", "leaf.sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := dir.PublishNoReplace(tmp, "leaf.sqlite3", "leaf.sqlite3"); err == nil {
		t.Fatal("replaced existing leaf")
	}
	fd, st, err := dir.OpenValidatedRegular("leaf.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	_ = unix.Close(fd)
	if st.Nlink != 1 || perm(&st) != FileMode {
		t.Fatalf("published file nlink=%d mode=%o", st.Nlink, perm(&st))
	}

	if err := os.Symlink("leaf.sqlite3", filepath.Join(dirPath, "link.sqlite3")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dir.OpenValidatedRegular("link.sqlite3"); err == nil {
		t.Fatal("accepted symlink")
	}
	if err := os.Mkdir(filepath.Join(dirPath, "subdir.sqlite3"), DirMode); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dir.OpenValidatedRegular("subdir.sqlite3"); err == nil {
		t.Fatal("accepted directory")
	}
	hard := filepath.Join(dirPath, "hard.sqlite3")
	if err := os.Link(filepath.Join(dirPath, "leaf.sqlite3"), hard); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dir.OpenValidatedRegular("hard.sqlite3"); err == nil {
		t.Fatal("accepted hard link")
	}
	wide := filepath.Join(dirPath, "wide.sqlite3")
	if err := os.WriteFile(wide, []byte("wide"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wide, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dir.OpenValidatedRegular("wide.sqlite3"); err == nil {
		t.Fatal("accepted mode 0644")
	}
}

func TestOpenBackupDirRequiresExisting(t *testing.T) {
	root := testDir(t)
	missing := filepath.Join(root, "missing")
	if _, err := OpenBackupDir(missing); err == nil {
		t.Fatal("created or accepted a missing backup root")
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatalf("OpenBackupDir created %s: %v", missing, err)
	}
}

func TestOpenDirRejectsFIFO(t *testing.T) {
	root := testDir(t)
	parent := filepath.Join(root, "backups")
	if err := os.Mkdir(parent, DirMode); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(parent, "daily")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	dir, err := OpenBackupDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.OpenDir("daily"); err == nil {
		t.Fatal("accepted FIFO child")
	}
}
