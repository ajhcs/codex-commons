package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
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
			root = candidate.FromConfiguredRoot && !candidate.FromCodexMetadata && candidate.CodexThreadCount == 0 && candidate.PathLabel == "Explicit Project" && candidate.CanonicalProjectID == "explicit"
		}
		if candidate.RepositoryLabel == "acme/remote-project" {
			path, ok := bridge.candidatePath(context.Background(), candidate.ID)
			remoteFound = ok && path == remote && candidate.FromCodexMetadata && candidate.HasCodexHistory && candidate.CanonicalProjectID == candidate.ID
		}
	}
	if !repo || !root || !remoteFound {
		t.Fatalf("repo=%v root=%v remote=%v discovery=%+v", repo, root, remoteFound, discovery)
	}
}

type launchFailureClient struct{ catalogClient }

func (launchFailureClient) ExperimentalDynamicTools() bool { return true }
func (launchFailureClient) LaunchHistorianTask(context.Context, string, string, string, string, string, string, codexauth.DynamicToolHandler, codexauth.TurnTerminalHandler) (codexauth.TaskLaunch, error) {
	return codexauth.TaskLaunch{}, errors.New("response lost")
}
func (launchFailureClient) InterruptTurn(context.Context, string, string) error { return nil }

func TestNativeBridgeMarksPostRequestLaunchFailureUncertain(t *testing.T) {
	path, err := os.MkdirTemp("/home/plumbob", "commons-launch-failure-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(path)
	client := launchFailureClient{catalogClient{workspaces: []codexauth.Workspace{{CWD: path}}}}
	bridge := &codexArchaeologyBridge{client: client}
	discovery, err := bridge.DiscoverMetadata(context.Background())
	if err != nil || len(discovery.Candidates) != 1 {
		t.Fatalf("discovery=%+v err=%v", discovery, err)
	}
	candidate := discovery.Candidates[0]
	result, err := bridge.LaunchNative(context.Background(), domain.ArchaeologyNativeJob{ID: "job", CandidateID: candidate.ID, ProjectID: "project"}, domain.ArchaeologySession{}, candidate, func(context.Context, application.ArchaeologyNativeToolCall) application.ArchaeologyNativeToolResponse {
		return application.ArchaeologyNativeToolResponse{}
	}, func(domain.ArchaeologyNativeTerminal) {})
	if err == nil || result.State != "uncertain" || result.ThreadID != "" {
		t.Fatalf("result=%+v err=%v", result, err)
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

func TestCodexCatalogRejectsConflictingConfiguredRootMappings(t *testing.T) {
	path := t.TempDir()
	bridge := &codexArchaeologyBridge{client: catalogClient{}, roots: []ArchaeologyRoot{{ID: "one", Name: "One", Path: path}, {ID: "two", Name: "Two", Path: path}}}
	if _, err := bridge.DiscoverMetadata(context.Background()); err == nil {
		t.Fatal("conflicting configured roots accepted")
	}
}
