package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchaeologyRootsAreExplicitMetadataOnlyAllowlist(t *testing.T) {
	root := eligibleTestWorkspace(t, "commons-roots-allowlist-")
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

func TestArchaeologyRootsRejectInstallationWideAndSymlinkEquivalentPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	linkParent, err := os.MkdirTemp(home, "commons-broad-link-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(linkParent) })
	link := filepath.Join(linkParent, "home-link")
	if err = os.Symlink(home, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for index, root := range []string{filepath.VolumeName(home) + string(filepath.Separator), filepath.Dir(home), home, link} {
		config := filepath.Join(t.TempDir(), "roots.json")
		body := `{"roots":[{"id":"broad","name":"Broad","path":` + quoteJSON(root) + `,"path_label":"Broad"}]}`
		if err = os.WriteFile(config, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, readErr := readArchaeologyRoots(config); readErr == nil {
			t.Fatalf("case %d accepted broad root %q", index, root)
		}
	}
}

func TestArchaeologyRootsFileRejectsBroadPermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o400, 0o700} {
		path := filepath.Join(t.TempDir(), "roots.json")
		if err := os.WriteFile(path, []byte(`{"roots":[]}`), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readArchaeologyRoots(path); err == nil || !strings.Contains(err.Error(), "mode 0600") {
			t.Fatalf("mode %04o allowlist accepted: %v", mode, err)
		}
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
