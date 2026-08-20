// Package opsfs is the Linux filesystem boundary for packaged Commons ops.
// Path names are a deliberately small grammar. Opened objects are reached
// with openat(2) and O_NOFOLLOW so intermediate aliases cannot retarget a
// later lookup.
package opsfs

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// FileMode is the only accepted mode for operator database, receipt,
	// checksum, and backup files.
	FileMode = 0o600
	// DirMode is the only accepted mode for backup root, daily, monthly,
	// and private temporary directories.
	DirMode = 0o700
	// MaxReceiptBytes is the fail-closed receipt size budget.
	MaxReceiptBytes = 4096
	// MaxChecksumBytes covers one canonical sha256sum record for a
	// grammar-limited absolute path plus a trailing newline.
	MaxChecksumBytes = 4096
)

// ErrBusy is returned when a nonblocking exclusive directory flock cannot
// be acquired. Cooperating Commons processes treat this as exit 75.
var ErrBusy = errors.New("directory busy")

func invalidPath(reason string) error {
	return fmt.Errorf("invalid path: %s", reason)
}

// ValidAbsPath accepts one absolute, slash-separated operator path. Each
// component is [A-Za-z0-9._-]+ and is neither "." nor "..". Controls,
// whitespace, SQL/shell punctuation, traversal, and empty components fail.
func ValidAbsPath(path string) error {
	if path == "" || path[0] != '/' {
		return invalidPath("must be absolute")
	}
	if path == "/" {
		return invalidPath("root is not an operator leaf")
	}
	if strings.Contains(path, "//") {
		return invalidPath("empty component")
	}
	if path[len(path)-1] == '/' {
		return invalidPath("trailing slash")
	}
	for _, part := range strings.Split(path[1:], "/") {
		if err := validComponent(part); err != nil {
			return err
		}
	}
	return nil
}

func validComponent(part string) error {
	if part == "" {
		return invalidPath("empty component")
	}
	if part == "." || part == ".." {
		return invalidPath("traversal component")
	}
	for i := 0; i < len(part); i++ {
		c := part[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return invalidPath("unsafe character")
		}
	}
	return nil
}

// SplitParentLeaf returns the canonical parent directory and final component.
func SplitParentLeaf(path string) (parent, leaf string, err error) {
	if err = ValidAbsPath(path); err != nil {
		return "", "", err
	}
	i := strings.LastIndexByte(path, '/')
	leaf = path[i+1:]
	if i == 0 {
		return "/", leaf, nil
	}
	return path[:i], leaf, nil
}

func sqliteSiblings(leaf string) []string {
	return []string{leaf + "-wal", leaf + "-shm", leaf + "-journal"}
}
