package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchaeologyRootsAreExplicitMetadataOnlyAllowlist(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "roots.json")
	body := `{"roots":[{"id":"commons","name":"Codex Commons","path":` + quoteJSON(root) + `,"path_label":"~/projects/codex-commons","repository_label":"codex-commons"}]}`
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	roots, err := readArchaeologyRoots(config)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := (allowlistedArchaeologyDiscoverer{roots: roots}).DiscoverMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if discovery.SourceRootsScanned != 1 || len(discovery.Candidates) != 1 || !discovery.Candidates[0].HasGit || discovery.Candidates[0].PathLabel != "Codex Commons" {
		t.Fatalf("discovery=%+v", discovery)
	}
	if strings.Contains(discovery.Candidates[0].PathLabel, "/") || strings.Contains(discovery.Candidates[0].PathLabel, "~") {
		t.Fatal("raw filesystem path leaked")
	}
}

func TestArchaeologyRootsFileRejectsBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roots.json")
	if err := os.WriteFile(path, []byte(`{"roots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readArchaeologyRoots(path); err == nil {
		t.Fatal("broadly readable allowlist accepted")
	}
}

func TestArchaeologyRootsFileRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roots.json")
	if err := os.WriteFile(path, []byte(`{"roots":[]} {"roots":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readArchaeologyRoots(path); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}

func quoteJSON(value string) string {
	result := `"`
	for _, r := range value {
		if r == '\\' || r == '"' {
			result += `\`
		}
		result += string(r)
	}
	return result + `"`
}
