//go:build linux

package opsfs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

const openFlags = unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NOCTTY

// Dir is an O_DIRECTORY|O_NOFOLLOW directory descriptor.
type Dir struct {
	FD     int
	Path   string
	Stat   unix.Stat_t
	locked bool
}

func (d *Dir) Close() error {
	if d == nil || d.FD < 0 {
		return nil
	}
	if d.locked {
		_ = unix.Flock(d.FD, unix.LOCK_UN)
		d.locked = false
	}
	err := unix.Close(d.FD)
	d.FD = -1
	return err
}

func (d *Dir) keep() int {
	fd := d.FD
	d.FD = -1
	d.locked = false
	return fd
}

// File is an O_NOFOLLOW file descriptor plus the pinned parent directory.
type File struct {
	FD     int
	DirFD  int
	Path   string
	Leaf   string
	Parent string
	Stat   unix.Stat_t
}

func (f *File) ReadLimited(limit int) ([]byte, error) {
	if f == nil || f.FD < 0 {
		return nil, fmt.Errorf("closed file")
	}
	if _, err := unix.Seek(f.FD, 0, unix.SEEK_SET); err != nil {
		return nil, err
	}
	return readFD(f.FD, limit)
}

func (f *File) Revalidate() error {
	if f == nil || f.FD < 0 {
		return fmt.Errorf("closed file")
	}
	uid, gid, err := currentIDs()
	if err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstat(f.FD, &st); err != nil {
		return err
	}
	if err := validateFileStat(&st, uid, gid, FileMode); err != nil {
		return err
	}
	if !sameFile(&f.Stat, &st) {
		return fmt.Errorf("inode changed")
	}
	if f.DirFD >= 0 {
		return restatFile(f.DirFD, f.Leaf, &f.Stat, uid, gid)
	}
	return nil
}

func (f *File) Close() error {
	if f == nil {
		return nil
	}
	var err error
	if f.FD >= 0 {
		err = unix.Close(f.FD)
		f.FD = -1
	}
	if f.DirFD >= 0 {
		if cerr := unix.Close(f.DirFD); cerr != nil && err == nil {
			err = cerr
		}
		f.DirFD = -1
	}
	return err
}

func currentIDs() (uint32, uint32, error) {
	uid := unix.Geteuid()
	gid := unix.Getegid()
	if uid < 0 || gid < 0 {
		return 0, 0, fmt.Errorf("invalid credentials")
	}
	return uint32(uid), uint32(gid), nil
}

func isENOENT(err error) bool {
	return err == unix.ENOENT || err == syscall.ENOENT
}

func isEEXIST(err error) bool {
	return err == unix.EEXIST || err == syscall.EEXIST
}

func isELOOP(err error) bool {
	return err == unix.ELOOP || err == syscall.ELOOP
}

func isBusy(err error) bool {
	return err == unix.EWOULDBLOCK || err == unix.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EAGAIN
}

func openRoot() (int, error) {
	return unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
}

// OpenDirNoFollow walks every component of path with O_NOFOLLOW|O_DIRECTORY.
func OpenDirNoFollow(path string) (*Dir, error) {
	if path == "/" {
		fd, err := openRoot()
		if err != nil {
			return nil, err
		}
		d := &Dir{FD: fd, Path: "/"}
		if err := unix.Fstat(fd, &d.Stat); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		if !isDir(&d.Stat) {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("not a directory")
		}
		return d, nil
	}
	if err := ValidAbsPath(path); err != nil {
		return nil, err
	}
	fd, err := openRoot()
	if err != nil {
		return nil, err
	}
	for _, part := range splitComponents(path) {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|openFlags, 0)
		_ = unix.Close(fd)
		if err != nil {
			if isELOOP(err) {
				return nil, fmt.Errorf("symlinked path component")
			}
			return nil, err
		}
		fd = next
	}
	d := &Dir{FD: fd, Path: path}
	if err := unix.Fstat(fd, &d.Stat); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if !isDir(&d.Stat) {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("not a directory")
	}
	if err := confirmOpenedPath(fd, path); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return d, nil
}

// OpenBackupDir opens path as a canonical, operator-owned, mode-0700
// non-symlink directory. The directory must already exist; this does not
// create it. Intermediate aliases fail closed.
func OpenBackupDir(path string) (*Dir, error) {
	d, err := OpenDirNoFollow(path)
	if err != nil {
		return nil, err
	}
	if err := d.ValidateExact(DirMode); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

func splitComponents(path string) []string {
	if path == "/" {
		return nil
	}
	return splitNonEmpty(path[1:], '/')
}

func splitNonEmpty(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func confirmOpenedPath(fd int, want string) error {
	got, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("opened path %q does not match requested %q", got, want)
	}
	return nil
}

func isDir(st *unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR
}

func isReg(st *unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG
}

func isLnk(st *unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFLNK
}

func perm(st *unix.Stat_t) uint32 {
	return st.Mode & 0o7777
}

func groupWorldWritable(st *unix.Stat_t) bool {
	return st.Mode&0o022 != 0
}

func validateParentStat(st *unix.Stat_t, uid, gid uint32) error {
	if !isDir(st) {
		return fmt.Errorf("parent is not a directory")
	}
	if st.Uid != uid || st.Gid != gid {
		return fmt.Errorf("parent owner/group mismatch")
	}
	if groupWorldWritable(st) {
		return fmt.Errorf("parent is group or world writable")
	}
	return nil
}

func validateFileStat(st *unix.Stat_t, uid, gid uint32, mode uint32) error {
	if !isReg(st) {
		return fmt.Errorf("not a regular file")
	}
	if st.Nlink != 1 {
		return fmt.Errorf("hardlinked file")
	}
	if st.Uid != uid || st.Gid != gid {
		return fmt.Errorf("owner/group mismatch")
	}
	if perm(st) != mode {
		return fmt.Errorf("mode mismatch")
	}
	return nil
}

func validateExactDirStat(st *unix.Stat_t, uid, gid uint32, mode uint32) error {
	if !isDir(st) {
		return fmt.Errorf("not a directory")
	}
	if st.Uid != uid || st.Gid != gid {
		return fmt.Errorf("owner/group mismatch")
	}
	if perm(st) != mode {
		return fmt.Errorf("mode mismatch")
	}
	return nil
}

// ValidateExact requires this directory still be operator-owned with exact mode.
func (d *Dir) ValidateExact(mode uint32) error {
	if d == nil || d.FD < 0 {
		return fmt.Errorf("closed directory")
	}
	uid, gid, err := currentIDs()
	if err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstat(d.FD, &st); err != nil {
		return err
	}
	if err := validateExactDirStat(&st, uid, gid, mode); err != nil {
		return err
	}
	if d.Stat.Ino != 0 && !sameFile(&d.Stat, &st) {
		return fmt.Errorf("inode changed")
	}
	d.Stat = st
	return confirmOpenedPath(d.FD, d.Path)
}

// OpenExistingFile pins the canonical parent directory and opens the leaf
// with O_NOFOLLOW. The file must be a regular, nlink=1, operator-owned
// mode-0600 object. The parent must be an operator-owned directory that is
// not group or world writable.
func OpenExistingFile(path string, flags int) (*File, error) {
	parent, leaf, err := SplitParentLeaf(path)
	if err != nil {
		return nil, err
	}
	uid, gid, err := currentIDs()
	if err != nil {
		return nil, err
	}
	dir, err := OpenDirNoFollow(parent)
	if err != nil {
		return nil, err
	}
	if err := validateParentStat(&dir.Stat, uid, gid); err != nil {
		_ = dir.Close()
		return nil, err
	}
	pre, err := fstatatNoFollow(dir.FD, leaf)
	if err != nil {
		_ = dir.Close()
		if isELOOP(err) {
			return nil, fmt.Errorf("symlinked leaf")
		}
		return nil, err
	}
	if isLnk(&pre) {
		_ = dir.Close()
		return nil, fmt.Errorf("symlinked leaf")
	}
	if !isReg(&pre) {
		_ = dir.Close()
		return nil, fmt.Errorf("not a regular file")
	}
	fd, err := unix.Openat(dir.FD, leaf, flags|openFlags|unix.O_NONBLOCK, 0)
	if err != nil {
		_ = dir.Close()
		if isELOOP(err) {
			return nil, fmt.Errorf("symlinked leaf")
		}
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		_ = dir.Close()
		return nil, err
	}
	if err := validateFileStat(&st, uid, gid, FileMode); err != nil {
		_ = unix.Close(fd)
		_ = dir.Close()
		return nil, err
	}
	return &File{FD: fd, DirFD: dir.keep(), Path: path, Leaf: leaf, Parent: parent, Stat: st}, nil
}

// OpenAbsentLeaf pins the parent and requires that the leaf is currently
// absent, including as a dangling symlink.
func OpenAbsentLeaf(path string) (*Dir, string, error) {
	parent, leaf, err := SplitParentLeaf(path)
	if err != nil {
		return nil, "", err
	}
	uid, gid, err := currentIDs()
	if err != nil {
		return nil, "", err
	}
	dir, err := OpenDirNoFollow(parent)
	if err != nil {
		return nil, "", err
	}
	if err := validateParentStat(&dir.Stat, uid, gid); err != nil {
		_ = dir.Close()
		return nil, "", err
	}
	var st unix.Stat_t
	err = unix.Fstatat(dir.FD, leaf, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		_ = dir.Close()
		return nil, "", fmt.Errorf("destination exists")
	}
	if !isENOENT(err) {
		_ = dir.Close()
		return nil, "", err
	}
	return dir, leaf, nil
}

func sameFile(a, b *unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino
}

func restatFile(dirfd int, leaf string, want *unix.Stat_t, uid, gid uint32) error {
	fd, err := unix.Openat(dirfd, leaf, unix.O_RDONLY|openFlags|unix.O_NONBLOCK, 0)
	if err != nil {
		if isELOOP(err) {
			return fmt.Errorf("symlinked leaf")
		}
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if err := validateFileStat(&st, uid, gid, FileMode); err != nil {
		return err
	}
	if want != nil && !sameFile(want, &st) {
		return fmt.Errorf("inode changed")
	}
	return nil
}

func fstatatNoFollow(dirfd int, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(dirfd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

func umaskPrivate() func() {
	old := unix.Umask(0o077)
	return func() { unix.Umask(old) }
}

// SetPrivateUmask sets umask 077 so SQLite sidecars and other creators
// cannot produce group/world-readable files.
func SetPrivateUmask() func() { return umaskPrivate() }

func readFD(fd int, limit int) ([]byte, error) {
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	total := 0
	for {
		n, err := unix.Read(fd, tmp)
		if n > 0 {
			total += n
			if total > limit {
				return nil, fmt.Errorf("oversized input")
			}
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return buf, nil
		}
	}
}

// FlockExclusiveNonblock takes a nonblocking exclusive flock on this
// directory descriptor. It never opens a pathname lock file.
func (d *Dir) FlockExclusiveNonblock() error {
	if d == nil || d.FD < 0 {
		return fmt.Errorf("closed directory")
	}
	err := unix.Flock(d.FD, unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		if isBusy(err) {
			return ErrBusy
		}
		return err
	}
	d.locked = true
	return nil
}

func (d *Dir) Unlock() {
	if d == nil || d.FD < 0 || !d.locked {
		return
	}
	_ = unix.Flock(d.FD, unix.LOCK_UN)
	d.locked = false
}

// OpenDir opens a direct child directory with O_NOFOLLOW|O_DIRECTORY and
// requires it to be contained, canonical, operator-owned, and mode 0700.
func (d *Dir) OpenDir(name string) (*Dir, error) {
	if d == nil || d.FD < 0 {
		return nil, fmt.Errorf("closed directory")
	}
	if err := validComponent(name); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(d.FD, name, unix.O_RDONLY|unix.O_DIRECTORY|openFlags, 0)
	if err != nil {
		if isELOOP(err) {
			return nil, fmt.Errorf("symlinked path component")
		}
		return nil, err
	}
	childPath := d.Path + "/" + name
	child := &Dir{FD: fd, Path: childPath}
	if err := unix.Fstat(fd, &child.Stat); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if err := child.ValidateExact(DirMode); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return child, nil
}

// Mkdir creates a direct child directory with mkdirat(2). The last
// component is not followed.
func (d *Dir) Mkdir(name string, mode uint32) error {
	if d == nil || d.FD < 0 {
		return fmt.Errorf("closed directory")
	}
	if err := validComponent(name); err != nil {
		return err
	}
	return unix.Mkdirat(d.FD, name, mode)
}

// OpenOrCreateDir opens name as a contained mode-0700 child, creating it
// with mkdirat if it is absent.
func (d *Dir) OpenOrCreateDir(name string) (*Dir, error) {
	child, err := d.OpenDir(name)
	if err == nil {
		return child, nil
	}
	if !isENOENT(err) {
		return nil, err
	}
	restore := umaskPrivate()
	mkdirErr := d.Mkdir(name, DirMode)
	restore()
	if mkdirErr != nil && !isEEXIST(mkdirErr) {
		return nil, mkdirErr
	}
	return d.OpenDir(name)
}

// MkdirPrivate creates an exclusive random mode-0700 directory inside d.
func (d *Dir) MkdirPrivate(prefix string) (*Dir, string, error) {
	if d == nil || d.FD < 0 {
		return nil, "", fmt.Errorf("closed directory")
	}
	if err := validComponent(prefix); err != nil {
		return nil, "", err
	}
	restore := umaskPrivate()
	defer restore()
	uid, gid, err := currentIDs()
	if err != nil {
		return nil, "", err
	}
	for i := 0; i < 8; i++ {
		var rnd [8]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return nil, "", err
		}
		name := "." + prefix + ".tmp." + hex.EncodeToString(rnd[:])
		if err := validComponent(name); err != nil {
			return nil, "", err
		}
		err := unix.Mkdirat(d.FD, name, DirMode)
		if isEEXIST(err) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		fd, err := unix.Openat(d.FD, name, unix.O_RDONLY|unix.O_DIRECTORY|openFlags, 0)
		if err != nil {
			_ = unix.Unlinkat(d.FD, name, unix.AT_REMOVEDIR)
			return nil, "", err
		}
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(d.FD, name, unix.AT_REMOVEDIR)
			return nil, "", err
		}
		if err := validateExactDirStat(&st, uid, gid, DirMode); err != nil {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(d.FD, name, unix.AT_REMOVEDIR)
			return nil, "", err
		}
		childPath := d.Path + "/" + name
		if err := confirmOpenedPath(fd, childPath); err != nil {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(d.FD, name, unix.AT_REMOVEDIR)
			return nil, "", err
		}
		return &Dir{FD: fd, Path: childPath, Stat: st}, name, nil
	}
	return nil, "", fmt.Errorf("unable to allocate private directory")
}

// ChildURI is the directory-fd SQLite filename for leaf. Sidecars land
// next to the real leaf rather than under a file-descriptor proc name.
func (d *Dir) ChildURI(leaf string) string {
	return "file:/proc/self/fd/" + strconv.Itoa(d.FD) + "/" + leaf
}

// Lstat stats name without following a final symlink.
func (d *Dir) Lstat(name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	if d == nil || d.FD < 0 {
		return st, fmt.Errorf("closed directory")
	}
	if err := validComponent(name); err != nil {
		return st, err
	}
	err := unix.Fstatat(d.FD, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

// Absent reports whether name is currently missing, including as a dangling
// symlink. Any existing object, including a symlink, is "not absent".
func (d *Dir) Absent(name string) (bool, error) {
	_, err := d.Lstat(name)
	if err == nil {
		return false, nil
	}
	if isENOENT(err) {
		return true, nil
	}
	return false, err
}

// Unlink removes a direct child name. Callers must have already validated
// the object as a regular file; this does not follow a final symlink, but
// unlinking a name that became a symlink would remove the symlink.
func (d *Dir) Unlink(name string) error {
	if d == nil || d.FD < 0 {
		return fmt.Errorf("closed directory")
	}
	if err := validComponent(name); err != nil {
		return err
	}
	return unix.Unlinkat(d.FD, name, 0)
}

// RemoveDir removes an empty direct child directory.
func (d *Dir) RemoveDir(name string) error {
	if d == nil || d.FD < 0 {
		return fmt.Errorf("closed directory")
	}
	if err := validComponent(name); err != nil {
		return err
	}
	return unix.Unlinkat(d.FD, name, unix.AT_REMOVEDIR)
}

// Names lists direct child names using a duplicated directory descriptor.
func (d *Dir) Names() ([]string, error) {
	if d == nil || d.FD < 0 {
		return nil, fmt.Errorf("closed directory")
	}
	nfd, err := unix.Dup(d.FD)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(nfd), d.Path)
	if file == nil {
		_ = unix.Close(nfd)
		return nil, fmt.Errorf("failed to adopt directory descriptor")
	}
	defer file.Close()
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	names, err := file.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	out := names[:0]
	for _, name := range names {
		if name == "." || name == ".." {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func (d *Dir) openChild(name string, flags int, mode uint32) (int, error) {
	if d == nil || d.FD < 0 {
		return -1, fmt.Errorf("closed directory")
	}
	if err := validComponent(name); err != nil {
		return -1, err
	}
	fd, err := unix.Openat(d.FD, name, flags|openFlags|unix.O_NONBLOCK, mode)
	if err != nil {
		if isELOOP(err) {
			return -1, fmt.Errorf("symlinked leaf")
		}
		return -1, err
	}
	return fd, nil
}

// WriteExclusive creates name with O_EXCL, writes data, forces mode 0600,
// and fsyncs the file and directory.
func (d *Dir) WriteExclusive(name string, data []byte) error {
	restore := umaskPrivate()
	defer restore()
	fd, err := d.openChild(name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, FileMode)
	if err != nil {
		if isEEXIST(err) {
			return fmt.Errorf("destination exists")
		}
		return err
	}
	off := 0
	for off < len(data) {
		n, err := unix.Write(fd, data[off:])
		if err != nil {
			_ = unix.Close(fd)
			_ = d.Unlink(name)
			return err
		}
		off += n
	}
	if err := finalizeOpened(fd, FileMode); err != nil {
		_ = unix.Close(fd)
		_ = d.Unlink(name)
		return err
	}
	if err := unix.Fsync(fd); err != nil {
		_ = unix.Close(fd)
		_ = d.Unlink(name)
		return err
	}
	if err := unix.Close(fd); err != nil {
		_ = d.Unlink(name)
		return err
	}
	return unix.Fsync(d.FD)
}

func finalizeOpened(fd int, mode uint32) error {
	if err := unix.Fchmod(fd, mode); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	uid, gid, err := currentIDs()
	if err != nil {
		return err
	}
	return validateFileStat(&st, uid, gid, mode)
}

// FinalizeRegularFile forces mode 0600 nlink=1 on an already-created child
// and fsyncs it.
func (d *Dir) FinalizeRegularFile(name string) error {
	fd, err := d.openChild(name, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	err = finalizeOpened(fd, FileMode)
	if err == nil {
		err = unix.Fsync(fd)
	}
	_ = unix.Close(fd)
	if err != nil {
		return err
	}
	return unix.Fsync(d.FD)
}

// SHA256 hashes a direct child regular file through the directory fd.
func (d *Dir) SHA256(name string) (string, error) {
	fd, err := d.openChild(name, unix.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	uid, gid, err := currentIDs()
	if err != nil {
		return "", err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return "", err
	}
	if err := validateFileStat(&st, uid, gid, FileMode); err != nil {
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if err != nil {
			return "", err
		}
		if n == 0 {
			return hex.EncodeToString(h.Sum(nil)), nil
		}
	}
}

// ReadValidatedRegular reads a direct child that is a regular nlink=1
// mode-0600 operator-owned file, up to limit bytes.
func (d *Dir) ReadValidatedRegular(name string, limit int) ([]byte, error) {
	fd, _, err := d.OpenValidatedRegular(name)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	return readFD(fd, limit)
}

// PublishNoReplace renameat2(RENAME_NOREPLACE)s fromName in from onto toName
// in d, then fsyncs the published leaf and destination directory. The private
// source leaf is revalidated immediately before the rename. Its descriptor
// remains open through the rename and destination identity check so the
// validated inode cannot be recycled in between; immediately after rename the
// published destination must still be that same regular inode or the publish
// fails closed.
//
// A same-uid actor can still replace the source name between validation and
// renameat2, or retarget the destination name after rename. Those pre-rename
// / post-rename name races are detected by the post-publication identity
// check; they are not atomically prevented.
func (d *Dir) PublishNoReplace(from *Dir, fromName, toName string) error {
	if d == nil || d.FD < 0 || from == nil || from.FD < 0 {
		return fmt.Errorf("closed directory")
	}
	if err := validComponent(fromName); err != nil {
		return err
	}
	if err := validComponent(toName); err != nil {
		return err
	}
	if err := from.ValidateExact(DirMode); err != nil {
		return err
	}
	if err := d.ValidateExact(DirMode); err != nil {
		return err
	}
	src, srcStat, err := from.OpenValidatedRegular(fromName)
	if err != nil {
		return err
	}
	defer unix.Close(src)
	if err := WaitHold(context.Background(), HoldPrePublishRename); err != nil {
		return err
	}
	err = unix.Renameat2(from.FD, fromName, d.FD, toName, unix.RENAME_NOREPLACE)
	if isEEXIST(err) {
		return fmt.Errorf("destination exists")
	}
	if err != nil {
		return err
	}
	fd, err := d.openChild(toName, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	var dstStat unix.Stat_t
	if err := unix.Fstat(fd, &dstStat); err != nil {
		_ = unix.Close(fd)
		return err
	}
	uid, gid, err := currentIDs()
	if err != nil {
		_ = unix.Close(fd)
		return err
	}
	if err := validateFileStat(&dstStat, uid, gid, FileMode); err != nil {
		_ = unix.Close(fd)
		return err
	}
	if !sameFile(&srcStat, &dstStat) {
		_ = unix.Close(fd)
		return fmt.Errorf("published inode changed")
	}
	syncErr := unix.Fsync(fd)
	_ = unix.Close(fd)
	if syncErr != nil {
		return syncErr
	}
	return unix.Fsync(d.FD)
}

// CopyExclusive copies a validated regular child into dest as destName.
// The source directory and leaf are revalidated immediately before the copy.
func (d *Dir) CopyExclusive(name string, dest *Dir, destName string) error {
	if dest == nil || dest.FD < 0 {
		return fmt.Errorf("closed directory")
	}
	if err := d.ValidateExact(DirMode); err != nil {
		return err
	}
	if err := dest.ValidateExact(DirMode); err != nil {
		return err
	}
	src, err := d.openChild(name, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer unix.Close(src)
	uid, gid, err := currentIDs()
	if err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstat(src, &st); err != nil {
		return err
	}
	if err := validateFileStat(&st, uid, gid, FileMode); err != nil {
		return err
	}
	restore := umaskPrivate()
	defer restore()
	out, err := dest.openChild(destName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, FileMode)
	if err != nil {
		if isEEXIST(err) {
			return fmt.Errorf("destination exists")
		}
		return err
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := unix.Read(src, buf)
		if n > 0 {
			off := 0
			for off < n {
				w, werr := unix.Write(out, buf[off:n])
				if werr != nil {
					_ = unix.Close(out)
					_ = dest.Unlink(destName)
					return werr
				}
				off += w
			}
		}
		if err != nil {
			_ = unix.Close(out)
			_ = dest.Unlink(destName)
			return err
		}
		if n == 0 {
			break
		}
	}
	if err := finalizeOpened(out, FileMode); err != nil {
		_ = unix.Close(out)
		_ = dest.Unlink(destName)
		return err
	}
	if err := unix.Fsync(out); err != nil {
		_ = unix.Close(out)
		_ = dest.Unlink(destName)
		return err
	}
	if err := unix.Close(out); err != nil {
		_ = dest.Unlink(destName)
		return err
	}
	return unix.Fsync(dest.FD)
}

// OpenValidatedRegular opens name only when it is a direct regular file
// with expected owner, mode 0600, and nlink=1. Symlinks, directories, and
// foreign entries fail closed.
func (d *Dir) OpenValidatedRegular(name string) (int, unix.Stat_t, error) {
	var st unix.Stat_t
	fd, err := d.openChild(name, unix.O_RDONLY, 0)
	if err != nil {
		return -1, st, err
	}
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return -1, st, err
	}
	uid, gid, err := currentIDs()
	if err != nil {
		_ = unix.Close(fd)
		return -1, st, err
	}
	if err := validateFileStat(&st, uid, gid, FileMode); err != nil {
		_ = unix.Close(fd)
		return -1, st, err
	}
	return fd, st, nil
}
