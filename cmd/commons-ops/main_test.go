//go:build linux

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunExposesIdentityHelpAndBackup(t *testing.T) {
	old := releaseID
	releaseID = "test-release"
	t.Cleanup(func() { releaseID = old })

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "empty help", want: "commons-ops is the packaged Commons operations boundary."},
		{name: "explicit help", args: []string{"--help"}, want: "Usage:"},
		{name: "short help", args: []string{"-h"}, want: "commons-ops backup"},
		{name: "version", args: []string{"--version"}, want: "commons-ops test-release\n"},
		{name: "build id", args: []string{"--build-id"}, want: "test-release\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tc.args, &stdout, &stderr); got != 0 {
				t.Fatalf("run(%q) exit code = %d, want 0", tc.args, got)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("run(%q) stdout = %q, want substring %q", tc.args, stdout.String(), tc.want)
			}
			if stderr.Len() != 0 {
				t.Fatalf("run(%q) stderr = %q, want empty", tc.args, stderr.String())
			}
		})
	}
}

func TestRunRejectsDisabledAndIncompleteBackup(t *testing.T) {
	t.Setenv("COMMONS_DB", "")
	t.Setenv("COMMONS_BACKUP_DIR", "")
	for _, tc := range []struct {
		name string
		args []string
		code int
		err  string
	}{
		{name: "backup extra", args: []string{"backup", "--force"}, code: 2, err: "commons-ops: no operational command is enabled\n"},
		{name: "backup missing env", args: []string{"backup"}, code: 64, err: "commons-ops: rejected: COMMONS_DB and COMMONS_BACKUP_DIR are required\n"},
		{name: "version extra", args: []string{"--version", "extra"}, code: 2, err: "commons-ops: no operational command is enabled\n"},
		{name: "build id extra", args: []string{"--build-id", "extra"}, code: 2, err: "commons-ops: no operational command is enabled\n"},
		{name: "help extra", args: []string{"--help", "extra"}, code: 2, err: "commons-ops: no operational command is enabled\n"},
		{name: "seal", args: []string{"seal-archive"}, code: 2, err: "commons-ops: no operational command is enabled\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := run(tc.args, &stdout, &stderr)
			if stdout.Len() != 0 {
				t.Fatalf("run(%q) stdout = %q, want empty", tc.args, stdout.String())
			}
			if got != tc.code {
				t.Fatalf("run(%q) exit code = %d, want %d", tc.args, got, tc.code)
			}
			if stderr.String() != tc.err {
				t.Fatalf("run(%q) stderr = %q, want %q", tc.args, stderr.String(), tc.err)
			}
		})
	}
}
