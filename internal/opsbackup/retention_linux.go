//go:build linux

package opsbackup

import (
	"context"
	"sort"
	"strings"

	"codex-commons/internal/opsfs"
	"golang.org/x/sys/unix"
)

type retainedFile struct {
	name  string
	mtime unix.Timespec
	dev   uint64
	ino   uint64
}

func retainValidated(ctx context.Context, dir *opsfs.Dir, keep int) error {
	if keep < 0 {
		keep = 0
	}
	names, err := dir.Names()
	if err != nil {
		return err
	}
	var files []retainedFile
	for _, name := range names {
		if !isBackupLeaf(name) {
			continue
		}
		fd, st, err := dir.OpenValidatedRegular(name)
		if err != nil {
			continue
		}
		_ = unix.Close(fd)
		files = append(files, retainedFile{name: name, mtime: st.Mtim, dev: st.Dev, ino: st.Ino})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].mtime.Sec != files[j].mtime.Sec {
			return files[i].mtime.Sec > files[j].mtime.Sec
		}
		if files[i].mtime.Nsec != files[j].mtime.Nsec {
			return files[i].mtime.Nsec > files[j].mtime.Nsec
		}
		return files[i].name > files[j].name
	})
	if len(files) <= keep {
		return nil
	}
	for _, file := range files[keep:] {
		if err := unlinkValidated(ctx, dir, file.name, file.dev, file.ino); err != nil {
			return err
		}
		_ = unlinkValidated(ctx, dir, file.name+".sha256", 0, 0)
		_ = unlinkValidated(ctx, dir, file.name+".receipt.json", 0, 0)
	}
	return nil
}

// unlinkValidated opens a regular nlink=1 mode-0600 operator-owned child,
// closes that descriptor, then unlinks the name. A same-uid actor can still
// retarget the name after the last check; unlinkat(2) is name-based and that
// remaining race is not claimed closed. If the name is no longer the inspected
// inode or is no longer a validated regular file, the unlink is skipped.
func unlinkValidated(ctx context.Context, dir *opsfs.Dir, name string, dev, ino uint64) error {
	fd, st, err := dir.OpenValidatedRegular(name)
	if err != nil {
		return nil
	}
	if (dev != 0 || ino != 0) && (st.Dev != dev || st.Ino != ino) {
		_ = unix.Close(fd)
		return nil
	}
	wantDev, wantIno := st.Dev, st.Ino
	_ = unix.Close(fd)
	if err := opsfs.WaitHold(ctx, opsfs.HoldPreUnlink); err != nil {
		return err
	}
	fd, st, err = dir.OpenValidatedRegular(name)
	if err != nil {
		return nil
	}
	same := st.Dev == wantDev && st.Ino == wantIno
	_ = unix.Close(fd)
	if !same {
		return nil
	}
	// Residual same-uid race: the name may still be replaced after this last
	// validated close and before unlinkat(2).
	return dir.Unlink(name)
}

func isBackupLeaf(name string) bool {
	if !strings.HasPrefix(name, "commons-") || !strings.HasSuffix(name, ".sqlite3") {
		return false
	}
	if strings.Contains(name, "/") || name == "." || name == ".." {
		return false
	}
	core := strings.TrimSuffix(strings.TrimPrefix(name, "commons-"), ".sqlite3")
	if core == "" {
		return false
	}
	for i := 0; i < len(core); i++ {
		c := core[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == 'T' || c == 'Z':
		default:
			return false
		}
	}
	return true
}
