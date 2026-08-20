package server

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialsFilePreservesStrictJSON(t *testing.T) {
	dir := t.TempDir()
	valid := writeModeFile(t, dir, "ok.json", []byte(`{"credentials":[{"bearer_token":"t","actor":"agent","session":"S-1","host":"plumbob"}]}`), 0o600)
	got, err := readCredentials(valid)
	if err != nil || len(got) != 1 || got[0].Actor != "agent" || got[0].Host != "plumbob" {
		t.Fatalf("valid credentials rejected: err=%v count=%d", err, len(got))
	}
	trailing := writeModeFile(t, dir, "trailing.json", []byte(`{"credentials":[]} {"credentials":[]}`), 0o600)
	if _, err := readCredentials(trailing); err == nil || !strings.Contains(err.Error(), "one JSON value") {
		t.Fatalf("trailing JSON accepted: %v", err)
	}
	malformed := writeModeFile(t, dir, "bad.json", []byte(`{"credentials":[`), 0o600)
	if _, err := readCredentials(malformed); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	unknown := writeModeFile(t, dir, "unknown.json", []byte(`{"credentials":[],"extra":true}`), 0o600)
	if _, err := readCredentials(unknown); err == nil {
		t.Fatal("unknown JSON field accepted")
	}
}

func TestCredentialsFileRejectsSymlinkAndBroadMode(t *testing.T) {
	dir := t.TempDir()
	target := writeModeFile(t, dir, "target.json", []byte(`{"credentials":[]}`), 0o600)
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Logf("symlinks unavailable: %v", err)
	} else if _, err := readCredentials(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink credentials accepted: %v", err)
	}
	wide := writeModeFile(t, dir, "wide.json", []byte(`{"credentials":[]}`), 0o644)
	if _, err := readCredentials(wide); err == nil || !strings.Contains(err.Error(), "group or other") {
		t.Fatalf("broad credentials accepted: %v", err)
	}
}

func TestHumanSecretFileRejectsSymlinkAndOversized(t *testing.T) {
	dir := t.TempDir()
	target := writeModeFile(t, dir, "secret", []byte("disposable-human-secret\n"), 0o600)
	got, err := readHumanSecret("", target)
	if err != nil || got != "disposable-human-secret" {
		t.Fatalf("valid secret file rejected err=%v", err)
	}
	link := filepath.Join(dir, "secret-link")
	if err := os.Symlink(target, link); err != nil {
		t.Logf("symlinks unavailable: %v", err)
	} else if _, err := readHumanSecret("", link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink secret accepted: %v", err)
	}
	big := writeModeFile(t, dir, "big", bytes.Repeat([]byte("a"), 4097), 0o600)
	if _, err := readHumanSecret("", big); err == nil || !strings.Contains(err.Error(), "exceeds 4096 bytes") {
		t.Fatalf("oversized secret accepted: %v", err)
	}
}

func TestCodexBindingKeyFileUsesPrivateOpener(t *testing.T) {
	dir := t.TempDir()
	key := bytes.Repeat([]byte{0x2a}, 32)
	path := writeModeFile(t, dir, "binding.key", key, 0o600)
	got, err := readCodexBindingKey(path)
	if err != nil || got == [32]byte{} || got[0] != 0x2a {
		t.Fatalf("valid binding key rejected: err=%v", err)
	}
	link := filepath.Join(dir, "binding-link")
	if err := os.Symlink(path, link); err != nil {
		t.Logf("symlinks unavailable: %v", err)
	} else if _, err := readCodexBindingKey(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink binding key accepted: %v", err)
	}
	short := writeModeFile(t, dir, "short.key", bytes.Repeat([]byte{0x2a}, 31), 0o600)
	if _, err := readCodexBindingKey(short); err == nil || !strings.Contains(err.Error(), "exactly 32 bytes") {
		t.Fatalf("short binding key accepted: %v", err)
	}
}

func TestArchaeologyRootsFileRejectsMalformedJSON(t *testing.T) {
	path := writeModeFile(t, t.TempDir(), "roots.json", []byte(`{"roots":[`), 0o600)
	if _, err := readArchaeologyRoots(path); err == nil {
		t.Fatal("malformed archaeology roots JSON accepted")
	}
	unknown := writeModeFile(t, t.TempDir(), "unknown.json", []byte(`{"roots":[],"extra":true}`), 0o600)
	if _, err := readArchaeologyRoots(unknown); err == nil {
		t.Fatal("unknown archaeology roots field accepted")
	}
}

func TestArchaeologyRootsFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := writeModeFile(t, dir, "roots.json", []byte(`{"roots":[]}`), 0o600)
	link := filepath.Join(dir, "roots-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readArchaeologyRoots(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink archaeology roots accepted: %v", err)
	}
}

func writeModeFile(t *testing.T, dir, name string, body []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}
