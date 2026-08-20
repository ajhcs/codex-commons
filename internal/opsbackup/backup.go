// Package opsbackup publishes sanitized SQLite backups through the packaged
// commons-ops boundary. It records only deterministic metadata; it never
// copies secrets, transcripts, or arbitrary payloads into sidecars.
package opsbackup

import (
	"codex-commons/internal/opsfs"
)

// ErrBusy is returned when another backup already holds the backup-root
// directory flock. commons-ops maps this to exit 75.
var ErrBusy = opsfs.ErrBusy

const (
	dailyKeep   = 30
	monthlyKeep = 12
)
