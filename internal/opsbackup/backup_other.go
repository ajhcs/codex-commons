//go:build !linux

package opsbackup

import (
	"context"
	"fmt"
)

// Backup fails closed on non-Linux GOOS.
func Backup(ctx context.Context, dbPath, backupDir string) (string, error) {
	return "", fmt.Errorf("backup is only supported on Linux")
}
