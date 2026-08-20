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
	var stdout, stderr bytes.Buffer
	if got := run([]string{"backup"}, &stdout, &stderr); got != 2 {
		t.Fatalf("run(backup) exit code = %d, want 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("run(backup) stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no operational command is enabled") {
		t.Fatalf("run(backup) stderr = %q, want dormant-boundary error", stderr.String())
	}
}
