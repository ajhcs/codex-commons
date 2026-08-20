//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"codex-commons/internal/opsbackup"
	"golang.org/x/sys/unix"
)

// releaseID is set by the repository-owned release builder.
var releaseID = "dev"

const usage = `commons-ops is the packaged Commons operations boundary.

Enabled operations create SQLite backups through fd-relative Linux path
validation. Restore, archive, evidence, and deployment commands remain
disabled.

Usage:
  commons-ops --help
  commons-ops --version
  commons-ops --build-id
  commons-ops backup
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		_, _ = io.WriteString(stdout, usage)
		return 0
	}
	if len(args) == 1 {
		switch args[0] {
		case "--build-id":
			_, _ = fmt.Fprintln(stdout, releaseID)
			return 0
		case "--version":
			_, _ = fmt.Fprintf(stdout, "commons-ops %s\n", releaseID)
			return 0
		case "backup":
			dbPath := os.Getenv("COMMONS_DB")
			backupDir := os.Getenv("COMMONS_BACKUP_DIR")
			if dbPath == "" || backupDir == "" {
				_, _ = fmt.Fprintln(stderr, "commons-ops: rejected: COMMONS_DB and COMMONS_BACKUP_DIR are required")
				return 64
			}
			return invokeBackup(stdout, stderr, dbPath, backupDir)
		}
	}
	_, _ = fmt.Fprintln(stderr, "commons-ops: no operational command is enabled")
	return 2
}

func invokeBackup(stdout, stderr io.Writer, dbPath, backupDir string) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, unix.SIGHUP, unix.SIGINT, unix.SIGTERM)
	defer signal.Stop(sigCh)

	type outcome struct {
		path string
		err  error
	}
	outCh := make(chan outcome, 1)
	go func() {
		path, err := opsbackup.Backup(ctx, dbPath, backupDir)
		outCh <- outcome{path: path, err: err}
	}()

	select {
	case out := <-outCh:
		select {
		case sig := <-sigCh:
			if out.err == nil {
				_, _ = fmt.Fprintln(stdout, out.path)
				return 0
			}
			return signalCode(sig)
		default:
			if out.err != nil {
				if errors.Is(out.err, opsbackup.ErrBusy) {
					_, _ = fmt.Fprintln(stderr, "commons-ops: backup directory busy")
					return 75
				}
				_, _ = fmt.Fprintf(stderr, "commons-ops: rejected: %v\n", out.err)
				if errors.Is(out.err, context.Canceled) {
					return 64
				}
				return 64
			}
			_, _ = fmt.Fprintln(stdout, out.path)
			return 0
		}
	case sig := <-sigCh:
		cancel()
		select {
		case <-outCh:
		case <-time.After(30 * time.Second):
		}
		return signalCode(sig)
	}
}

func signalCode(sig os.Signal) int {
	switch sig {
	case unix.SIGHUP:
		return 129
	case unix.SIGINT:
		return 130
	case unix.SIGTERM:
		return 143
	default:
		return 64
	}
}
