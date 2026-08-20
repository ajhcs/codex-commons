package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAgentConfigPreservesStrictJSONAndPrivateMode(t *testing.T) {
	dir := t.TempDir()
	valid := `{"base_url":"http://127.0.0.1:8088","bearer_token":"test-token"}`
	path := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readAgentConfig(path)
	if err != nil || got.BaseURL != "http://127.0.0.1:8088" || got.BearerToken != "test-token" {
		t.Fatalf("valid agent config rejected: err=%v", err)
	}
	trailing := filepath.Join(dir, "trailing.json")
	if err := os.WriteFile(trailing, []byte(valid+" "+valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAgentConfig(trailing); err == nil || !strings.Contains(err.Error(), "one JSON value") {
		t.Fatalf("trailing JSON accepted: %v", err)
	}
	malformed := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(malformed, []byte(`{"base_url":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAgentConfig(malformed); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	for _, mode := range []os.FileMode{0o644, 0o400, 0o700} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readAgentConfig(path); err == nil || !strings.Contains(err.Error(), "mode 0600") {
			t.Fatalf("mode %04o agent config accepted: %v", mode, err)
		}
	}
}

func TestReadAgentConfigRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(target, []byte(`{"base_url":"http://127.0.0.1:8088","bearer_token":"test-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "agent-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readAgentConfig(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink agent config accepted: %v", err)
	}
}
