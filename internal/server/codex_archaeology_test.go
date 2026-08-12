package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codex-commons/internal/codexauth"
)

type catalogClient struct{ workspaces []codexauth.Workspace }

func (c catalogClient) ListWorkspaces(context.Context) ([]codexauth.Workspace, error) {
	return append([]codexauth.Workspace(nil), c.workspaces...), nil
}
func (catalogClient) SupportsModel(context.Context, string, string) (bool, error) { return true, nil }
func (catalogClient) LaunchTask(context.Context, string, string, string, string, string) (codexauth.TaskLaunch, error) {
	panic("not used")
}

func TestCodexCatalogGroupsWorktreesByOriginAndKeepsStandaloneRoots(t *testing.T) {
	base, err := os.MkdirTemp("/home/plumbob", "commons-catalog-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	mainRepo := filepath.Join(base, "widgets")
	worktree := filepath.Join(base, "widgets-review")
	standalone := filepath.Join(base, "notes")
	explicit := filepath.Join(base, "explicit")
	for _, path := range []string{mainRepo, worktree, standalone, explicit} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	remote := "/srv/paired-codex/remote-project"
	client := catalogClient{workspaces: []codexauth.Workspace{
		{CWD: mainRepo, GitOrigin: "git@github.com:acme/widgets.git", UpdatedAt: now},
		{CWD: worktree, GitOrigin: "https://github.com/acme/widgets.git", UpdatedAt: now.Add(time.Minute)},
		{CWD: standalone, UpdatedAt: now.Add(2 * time.Minute)},
		{CWD: remote, GitOrigin: "https://github.com/acme/remote-project.git", UpdatedAt: now.Add(3 * time.Minute)},
	}}
	bridge := &codexArchaeologyBridge{client: client, roots: []ArchaeologyRoot{{
		ID: "explicit", Name: "Explicit Project", Path: explicit, PathLabel: "~/work/explicit",
	}}}
	discovery, err := bridge.DiscoverMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Candidates) != 4 {
		t.Fatalf("candidate groups=%d want 4: %+v", len(discovery.Candidates), discovery.Candidates)
	}
	var repo, root, remoteFound bool
	for _, candidate := range discovery.Candidates {
		if candidate.RepositoryLabel == "acme/widgets" {
			repo = candidate.FromCodexMetadata && candidate.CodexThreadCount == 2 && candidate.PathLabel != mainRepo && candidate.PathLabel != worktree
		}
		if candidate.Name == "Explicit Project" {
			root = candidate.FromConfiguredRoot && !candidate.FromCodexMetadata && candidate.CodexThreadCount == 0 && candidate.PathLabel == "Explicit Project"
		}
		if candidate.RepositoryLabel == "acme/remote-project" {
			path, ok := bridge.candidatePath(context.Background(), candidate.ID)
			remoteFound = ok && path == remote && candidate.FromCodexMetadata && candidate.HasCodexHistory
		}
	}
	if !repo || !root || !remoteFound {
		t.Fatalf("repo=%v root=%v remote=%v discovery=%+v", repo, root, remoteFound, discovery)
	}
}

func TestNormalizeGitOriginRejectsPathLikeOrigins(t *testing.T) {
	for _, origin := range []string{
		"file:///home/private/project",
		"/home/private/project",
		`C:\Users\private\project`,
		"https:project-without-host",
	} {
		if got := normalizeGitOrigin(origin); got != "" {
			t.Errorf("normalizeGitOrigin(%q) = %q, want empty", origin, got)
		}
	}
}
