//go:build linux

package privatefile

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// Open opens path with O_RDONLY, O_CLOEXEC, O_NOFOLLOW, and O_NONBLOCK, then
// validates the already-open descriptor with fstat: a regular file owned by
// the effective user, with exact mode 0600, and no larger than maxSize.
// O_NONBLOCK is required so a FIFO cannot block open before fstat rejects it;
// it is harmless for regular files. Reads from the returned file stay on that
// inode, so a later replacement at path is harmless.
func Open(path, label string, maxSize int64) (*os.File, error) {
	label = normalizeLabel(label)
	if maxSize <= 0 {
		return nil, fmt.Errorf("%s: maximum size must be positive", label)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
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

// Read opens path with Open, reads at most maxSize+1 bytes from that
// descriptor (saturating at math.MaxInt64), and closes it. The file is
// rejected if it contains more than maxSize bytes, including when the
// already-open inode grows after fstat. The returned slice never includes
// bytes from a replaced path and is never a silently truncated prefix.
func Read(path, label string, maxSize int64) ([]byte, error) {
	file, err := Open(path, label, maxSize)
	if err != nil {
		return nil, err
	}
	body, readErr := readFromOpened(file, normalizeLabel(label), maxSize)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("%s: %w", normalizeLabel(label), closeErr)
	}
	return body, nil
}

// readFromOpened reads at most maxSize+1 bytes from an already-open private
// descriptor, saturating the probe length at math.MaxInt64, and rejects the
// read if the inode now contains more than maxSize bytes. Callers that decode
// JSON must decode from these bounded bytes.
func readFromOpened(file *os.File, label string, maxSize int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(file, saturatedReadLimit(maxSize)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if int64(len(body)) > maxSize {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, maxSize)
	}
	return body, nil
}

// saturatedReadLimit is maxSize+1 unless that addition would overflow
// int64. At math.MaxInt64 the extra probe byte cannot be represented, so
// the limit saturates; a later len(body) > maxSize check is then
// impossible, and Read still returns the contents that fit in a slice.
func saturatedReadLimit(maxSize int64) int64 {
	if maxSize < math.MaxInt64 {
		return maxSize + 1
	}
	return math.MaxInt64
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
	if st.Mode&^unix.S_IFMT != 0o600 {
		return fmt.Errorf("%s must have mode 0600", label)
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
