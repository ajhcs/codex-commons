//go:build linux

package opsbackup

import (
	"sort"
	"strings"

	"codex-commons/internal/opsfs"
	"golang.org/x/sys/unix"
)

type retainedFile struct {
	name  string
	mtime unix.Timespec
}

func retainValidated(dir *opsfs.Dir, keep int) error {
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
		files = append(files, retainedFile{name: name, mtime: st.Mtim})
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
		if err := unlinkValidated(dir, file.name); err != nil {
			return err
		}
		_ = unlinkValidated(dir, file.name+".sha256")
		_ = unlinkValidated(dir, file.name+".receipt.json")
	}
	return nil
}

func unlinkValidated(dir *opsfs.Dir, name string) error {
	fd, _, err := dir.OpenValidatedRegular(name)
	if err != nil {
		return err
	}
	_ = unix.Close(fd)
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
