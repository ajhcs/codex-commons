//go:build linux

package opsfs

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

// PinnedDB is a directory-fd pin of a SQLite main file. The directory
// descriptor stays open so SQLite can be pointed at
// /proc/self/fd/<dirfd>/<leaf>. Sidecars then land next to the real leaf
// rather than under a file-descriptor proc name.
//
// This is not an absolute TOCTOU close against a hostile same-uid actor:
// SQLite's own open does not take O_NOFOLLOW, flock(2) is advisory, and the
// kernel does not offer "open this inode as a SQLite database". Cooperating
// Commons processes are serialized on the backup-root directory. A same-uid
// attacker can still rename the leaf between revalidation and SQLite's
// open; we detect inode/mode/nlink drift immediately after open and refuse
// to write. Residual races are stated, not papered over.
type PinnedDB struct {
	Dir    *Dir
	File   *File
	Path   string
	Leaf   string
	locked bool
}

func (p *PinnedDB) Close() error {
	if p == nil {
		return nil
	}
	if p.locked && p.Dir != nil && p.Dir.FD >= 0 {
		_ = unix.Flock(p.Dir.FD, unix.LOCK_UN)
		p.locked = false
	}
	var err error
	if p.File != nil {
		p.File.DirFD = -1
		err = p.File.Close()
		p.File = nil
	}
	if p.Dir != nil {
		if cerr := p.Dir.Close(); cerr != nil && err == nil {
			err = cerr
		}
		p.Dir = nil
	}
	return err
}

// PinDatabase validates the main database file and any present
// -wal/-shm/-journal siblings, then exclusive-flocks the parent directory.
func PinDatabase(path string) (*PinnedDB, error) {
	return pinDatabase(path, true)
}

// PinDatabaseUnlocked validates the main database file and siblings
// without flocking the parent. Backup serializes on the backup-root
// directory instead, so the live database parent is not a second lock
// domain.
func PinDatabaseUnlocked(path string) (*PinnedDB, error) {
	return pinDatabase(path, false)
}

func pinDatabase(path string, lock bool) (*PinnedDB, error) {
	f, err := OpenExistingFile(path, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	dir := &Dir{FD: f.DirFD, Path: f.Parent}
	f.DirFD = -1
	if f.FD >= 0 {
		_ = unix.Close(f.FD)
		f.FD = -1
	}
	if err := unix.Fstat(dir.FD, &dir.Stat); err != nil {
		_ = dir.Close()
		return nil, err
	}
	if lock {
		if err := unix.Flock(dir.FD, unix.LOCK_EX); err != nil {
			_ = f.Close()
			_ = dir.Close()
			return nil, err
		}
	}
	uid, gid, err := currentIDs()
	if err != nil {
		if lock {
			_ = unix.Flock(dir.FD, unix.LOCK_UN)
		}
		_ = f.Close()
		_ = dir.Close()
		return nil, err
	}
	if err := validateFileStat(&f.Stat, uid, gid, FileMode); err != nil {
		if lock {
			_ = unix.Flock(dir.FD, unix.LOCK_UN)
		}
		_ = f.Close()
		_ = dir.Close()
		return nil, err
	}
	if err := validateSiblings(dir.FD, f.Leaf, uid, gid); err != nil {
		if lock {
			_ = unix.Flock(dir.FD, unix.LOCK_UN)
		}
		_ = f.Close()
		_ = dir.Close()
		return nil, err
	}
	if err := restatFile(dir.FD, f.Leaf, &f.Stat, uid, gid); err != nil {
		if lock {
			_ = unix.Flock(dir.FD, unix.LOCK_UN)
		}
		_ = f.Close()
		_ = dir.Close()
		return nil, err
	}
	return &PinnedDB{Dir: dir, File: f, Path: path, Leaf: f.Leaf, locked: lock}, nil
}

func validateSiblings(dirfd int, leaf string, uid, gid uint32) error {
	for _, name := range sqliteSiblings(leaf) {
		st, err := fstatatNoFollow(dirfd, name)
		if err != nil {
			if isENOENT(err) {
				continue
			}
			return err
		}
		if isLnk(&st) {
			return fmt.Errorf("symlinked sqlite sibling %s", name)
		}
		if isDir(&st) {
			return fmt.Errorf("directory sqlite sibling %s", name)
		}
		if !isReg(&st) {
			return fmt.Errorf("non-regular sqlite sibling %s", name)
		}
		fd, err := unix.Openat(dirfd, name, unix.O_RDONLY|openFlags|unix.O_NONBLOCK, 0)
		if err != nil {
			if isELOOP(err) {
				return fmt.Errorf("symlinked sqlite sibling %s", name)
			}
			return err
		}
		var opened unix.Stat_t
		if err := unix.Fstat(fd, &opened); err != nil {
			_ = unix.Close(fd)
			return err
		}
		_ = unix.Close(fd)
		if err := validateFileStat(&opened, uid, gid, FileMode); err != nil {
			return fmt.Errorf("sqlite sibling %s: %w", name, err)
		}
	}
	return nil
}

// Revalidate repeats main-file and sibling checks against the originally
// pinned inode.
func (p *PinnedDB) Revalidate() error {
	if p == nil || p.Dir == nil || p.File == nil {
		return fmt.Errorf("closed database pin")
	}
	uid, gid, err := currentIDs()
	if err != nil {
		return err
	}
	if err := restatFile(p.Dir.FD, p.Leaf, &p.File.Stat, uid, gid); err != nil {
		return err
	}
	return validateSiblings(p.Dir.FD, p.Leaf, uid, gid)
}

// URI is the pinned directory-fd SQLite filename. Sidecars are named
// <uri>-wal and <uri>-shm, which resolve through the directory fd.
func (p *PinnedDB) URI(extra string) string {
	u := "/proc/self/fd/" + strconv.Itoa(p.Dir.FD) + "/" + p.Leaf
	dsn := "file:" + u
	if extra == "" {
		return dsn
	}
	return dsn + "?" + extra
}

func sqliteOpenParams(writable bool) string {
	mode := "ro"
	if writable {
		mode = "rw"
	}
	return "mode=" + mode + "&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
}

// OpenPinned opens the pinned database through the directory-fd URI and
// revalidates inode/mode/nlink as soon as the driver is alive.
func OpenPinned(ctx context.Context, pin *PinnedDB, writable bool) (*sql.DB, error) {
	if err := pin.Revalidate(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", pin.URI(sqliteOpenParams(writable)))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := pin.Revalidate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
