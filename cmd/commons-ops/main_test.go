package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunExposesOnlyIdentityAndHelp(t *testing.T) {
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
		{name: "short help", args: []string{"-h"}, want: "No operational commands are enabled"},
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

func TestRunRejectsOperationalArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "backup", args: []string{"backup"}},
		{name: "backup extra", args: []string{"backup", "--force"}},
		{name: "version extra", args: []string{"--version", "extra"}},
		{name: "build id extra", args: []string{"--build-id", "extra"}},
		{name: "help extra", args: []string{"--help", "extra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tc.args, &stdout, &stderr); got != 2 {
				t.Fatalf("run(%q) exit code = %d, want 2", tc.args, got)
			}
			if stdout.Len() != 0 {
				t.Fatalf("run(%q) stdout = %q, want empty", tc.args, stdout.String())
			}
			if got, want := stderr.String(), "commons-ops: no operational command is enabled\n"; got != want {
				t.Fatalf("run(%q) stderr = %q, want %q", tc.args, got, want)
			}
		})
	}
}
