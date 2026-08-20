//go:build !linux

package privatefile

import (
	"fmt"
	"os"
)

// Open fails closed on non-Linux GOOS. The Linux implementation uses
// unix.Open with O_NOFOLLOW; this stub does not open the path.
func Open(path, label string, maxSize int64) (*os.File, error) {
	return nil, unsupported(label, maxSize)
}

// Read fails closed on non-Linux GOOS and never returns file contents.
func Read(path, label string, maxSize int64) ([]byte, error) {
	return nil, unsupported(label, maxSize)
}

func unsupported(label string, maxSize int64) error {
	label = normalizeLabel(label)
	if maxSize <= 0 {
		return fmt.Errorf("%s: maximum size must be positive", label)
	}
	return fmt.Errorf("%s: private files are only supported on Linux", label)
}
