//go:build !linux

package privatefile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFailsClosedOnNonLinux(t *testing.T) {
	want := []byte("must-not-be-returned")
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path, testLabel, 4096)
	if err == nil {
		t.Fatal("non-Linux Read succeeded")
	}
	if !strings.Contains(err.Error(), "only supported on Linux") {
		t.Fatalf("non-Linux Read poorly reported: %v", err)
	}
	if strings.Contains(err.Error(), string(want)) {
		t.Fatal("non-Linux Read leaked file contents")
	}
	if len(got) != 0 {
		t.Fatal("non-Linux Read returned file contents")
	}
	file, err := Open(path, testLabel, 4096)
	if file != nil || err == nil || !strings.Contains(err.Error(), "only supported on Linux") {
		t.Fatalf("non-Linux Open did not fail closed: file=%v err=%v", file, err)
	}
}

const testLabel = "private test file"
