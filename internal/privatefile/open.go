//go:build linux

// Package privatefile opens Linux private configuration files without
// check/use races. Callers read or decode only from the returned descriptor.
package privatefile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

// Open opens path with O_NOFOLLOW and O_CLOEXEC, then validates the already-open
// descriptor with fstat: a regular file owned by the effective user, with no
// group/other permission bits, and no larger than maxSize. Reads from the
// returned file stay on that inode, so a later replacement at path is harmless.
func Open(path, label string, maxSize int64) (*os.File, error) {
	label = normalizeLabel(label)
	if maxSize <= 0 {
		return nil, fmt.Errorf("%s: maximum size must be positive", label)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, openError(label, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%s: failed to adopt descriptor", label)
	}
	if err := validate(file, label, maxSize); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// Read opens path with Open, reads at most maxSize bytes from that descriptor,
// and closes it. The returned slice never includes bytes from a replaced path.
func Read(path, label string, maxSize int64) ([]byte, error) {
	file, err := Open(path, label, maxSize)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(file, maxSize))
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("%s: %w", normalizeLabel(label), readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("%s: %w", normalizeLabel(label), closeErr)
	}
	return body, nil
}

func validate(file *os.File, label string, maxSize int64) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		runtime.KeepAlive(file)
		return fmt.Errorf("%s: %w", label, err)
	}
	runtime.KeepAlive(file)
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("%s must be a regular file", label)
	}
	if st.Uid != uint32(unix.Geteuid()) {
		return fmt.Errorf("%s must be owned by the effective user", label)
	}
	if st.Mode&0o077 != 0 {
		return fmt.Errorf("%s must not be accessible by group or other users", label)
	}
	if st.Size > maxSize {
		return fmt.Errorf("%s exceeds %d bytes", label, maxSize)
	}
	return nil
}

func openError(label string, err error) error {
	if errors.Is(err, unix.ELOOP) {
		return fmt.Errorf("%s must not be a symlink", label)
	}
	if errors.Is(err, unix.EISDIR) {
		return fmt.Errorf("%s must be a regular file", label)
	}
	return fmt.Errorf("%s: %w", label, err)
}

func normalizeLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "private configuration file"
	}
	return label
}
