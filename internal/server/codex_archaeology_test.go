package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

type catalogClient struct{ workspaces []codexauth.Workspace }

func (c catalogClient) ListWorkspaces(context.Context) ([]codexauth.Workspace, error) {
	return append([]codexauth.Workspace(nil), c.workspaces...), nil
}

type oversizedCatalogClient struct{ catalogClient }

func (oversizedCatalogClient) ListWorkspaces(context.Context) ([]codexauth.Workspace, error) {
	return nil, codexauth.ErrLineTooLarge
}

type countingCatalogClient struct {
	workspaces []codexauth.Workspace
	mu         sync.Mutex
	calls      int
}

type availabilityCountingClient struct {
	catalogClient
	mu    sync.Mutex
	calls int
}

type recoveryCatalogClient struct {
	catalogClient
	cwd, title string
	launch     codexauth.TaskLaunch
	found      bool
}

func (c *recoveryCatalogClient) FindHistorianTask(_ context.Context, cwd, title string) (codexauth.TaskLaunch, bool, error) {
	c.cwd, c.title = cwd, title
	return c.launch, c.found, nil
}

func (*availabilityCountingClient) ExperimentalDynamicTools() bool { return true }
func (c *availabilityCountingClient) SupportsModel(context.Context, string, string) (bool, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return true, nil
}
func (*availabilityCountingClient) LaunchHistorianTask(context.Context, string, string, string, string, string, string, codexauth.HistorianPolicy, codexauth.DynamicToolHandler, codexauth.TurnTerminalHandler) (codexauth.TaskLaunch, error) {
	panic("not used")
}
func (*availabilityCountingClient) InterruptTurn(context.Context, string, string) error { return nil }

func (c *countingCatalogClient) ListWorkspaces(context.Context) ([]codexauth.Workspace, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return append([]codexauth.Workspace(nil), c.workspaces...), nil
}
func (*countingCatalogClient) SupportsModel(context.Context, string, string) (bool, error) {
	return true, nil
}
func (*countingCatalogClient) LaunchTask(context.Context, string, string, string, string, string) (codexauth.TaskLaunch, error) {
	panic("not used")
}
func (catalogClient) SupportsModel(context.Context, string, string) (bool, error) { return true, nil }
func (catalogClient) LaunchTask(context.Context, string, string, string, string, string) (codexauth.TaskLaunch, error) {
	panic("not used")
}

func eligibleTestWorkspace(t *testing.T, prefix string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path, err := os.MkdirTemp(home, prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	return path
}

func TestEligibleWorkspaceRejectsBroadInstallationRoots(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.VolumeName(home) + string(filepath.Separator)
	for _, broad := range []string{root, filepath.Dir(home), home, os.TempDir(), filepath.Join(os.TempDir(), "nested-project")} {
		if _, ok := eligibleWorkspace(broad); ok {
			t.Errorf("eligibleWorkspace(%q) accepted a broad installation root", broad)
		}
	}
	workspace := eligibleTestWorkspace(t, "commons-eligible-workspace-")
	if got, ok := eligibleWorkspace(workspace); !ok || got != workspace {
		t.Fatalf("eligibleWorkspace(%q) = %q, %v", workspace, got, ok)
	}
}

func TestCodexCatalogGroupsWorktreesByOriginAndKeepsStandaloneRoots(t *testing.T) {
	base := eligibleTestWorkspace(t, "commons-catalog-test-")
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

func TestNativeBridgeLaunchesInMostRecentlyActiveSameOriginWorktree(t *testing.T) {
	base := eligibleTestWorkspace(t, "commons-newest-worktree-")
	mainRepo := filepath.Join(base, "widgets")
	olderWorktree := filepath.Join(base, "widgets-review")
	newestWorktree := filepath.Join(base, "widgets-release")
	for _, path := range []string{mainRepo, olderWorktree, newestWorktree} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	client := &captureLaunchClient{catalogClient: catalogClient{workspaces: []codexauth.Workspace{
		{CWD: newestWorktree, GitOrigin: "https://github.com/acme/widgets.git", GitBranch: "release", UpdatedAt: now.Add(2 * time.Hour)},
		{CWD: mainRepo, GitOrigin: "git@github.com:acme/widgets.git", GitBranch: "main", UpdatedAt: now},
		{CWD: olderWorktree, GitOrigin: "https://github.com/acme/widgets", GitBranch: "review", UpdatedAt: now.Add(time.Hour)},
	}}}
	bridge := &codexArchaeologyBridge{client: client}
	discovery, err := bridge.DiscoverMetadata(context.Background())
	if err != nil || len(discovery.Candidates) != 1 {
		t.Fatalf("discovery=%+v err=%v", discovery, err)
	}
	candidate := discovery.Candidates[0]
	if candidate.CodexThreadCount != 3 {
		t.Fatalf("candidate=%+v", candidate)
	}
	_, err = bridge.LaunchNative(context.Background(), domain.ArchaeologyNativeJob{
		ID: "job-newest-worktree", CandidateID: candidate.ID, ProjectID: candidate.CanonicalProjectID,
		Policy: domain.ArchaeologyExecutionPolicy{Depth: "quick", Sources: domain.ArchaeologySources{Git: true}},
	}, domain.ArchaeologySession{}, candidate, func(context.Context, application.ArchaeologyNativeToolCall) application.ArchaeologyNativeToolResponse {
		return application.ArchaeologyNativeToolResponse{}
	}, func(domain.ArchaeologyNativeTerminal) {})
	if err != nil {
		t.Fatal(err)
	}
	if client.launch.cwd != newestWorktree {
		t.Fatalf("launch cwd=%q want most recently active worktree %q", client.launch.cwd, newestWorktree)
	}
}

func TestBetterWorkspaceCandidateUsesStablePathQualityTies(t *testing.T) {
	updated := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	if !betterWorkspaceCandidate("/srv/widgets", updated, "/srv/.codex/worktrees/widgets", updated) {
		t.Fatal("non-noisy path did not win equal-activity tie")
	}
	if !betterWorkspaceCandidate("/srv/widgets", updated, "/srv/team/widgets", updated) {
		t.Fatal("shallower path did not win equal-activity tie")
	}
	if !betterWorkspaceCandidate("/srv/alpha", updated, "/srv/widgets", updated) {
		t.Fatal("lexically earlier path did not win equal-activity tie")
	}
	if betterWorkspaceCandidate("/srv/alpha", updated.Add(-time.Second), "/srv/widgets", updated) {
		t.Fatal("path quality overrode newer activity")
	}
}

type launchFailureClient struct{ catalogClient }

func (launchFailureClient) ExperimentalDynamicTools() bool { return true }
func (launchFailureClient) LaunchHistorianTask(context.Context, string, string, string, string, string, string, codexauth.HistorianPolicy, codexauth.DynamicToolHandler, codexauth.TurnTerminalHandler) (codexauth.TaskLaunch, error) {
	return codexauth.TaskLaunch{}, errors.New("response lost")
}
func (launchFailureClient) InterruptTurn(context.Context, string, string) error { return nil }

type capturedHistorianLaunch struct {
	cwd, model, effort, prompt, clientID, title string
	policy                                      codexauth.HistorianPolicy
}

type captureLaunchClient struct {
	catalogClient
	launch capturedHistorianLaunch
}

func (*captureLaunchClient) ExperimentalDynamicTools() bool { return true }
func (c *captureLaunchClient) LaunchHistorianTask(_ context.Context, cwd, model, effort, prompt, clientID, title string, policy codexauth.HistorianPolicy, _ codexauth.DynamicToolHandler, _ codexauth.TurnTerminalHandler) (codexauth.TaskLaunch, error) {
	c.launch = capturedHistorianLaunch{cwd: cwd, model: model, effort: effort, prompt: prompt, clientID: clientID, title: title, policy: policy}
	return codexauth.TaskLaunch{ThreadID: "thread-captured", SessionID: "session-captured", TurnID: "turn-captured"}, nil
}
func (*captureLaunchClient) InterruptTurn(context.Context, string, string) error { return nil }

func TestNativeBridgeUsesExactVisibleHistorianContract(t *testing.T) {
	path := eligibleTestWorkspace(t, "commons-launch-contract-test-")
	client := &captureLaunchClient{catalogClient: catalogClient{workspaces: []codexauth.Workspace{{CWD: path}}}}
	bridge := &codexArchaeologyBridge{client: client}
	discovery, err := bridge.DiscoverMetadata(context.Background())
	if err != nil || len(discovery.Candidates) != 1 {
		t.Fatalf("discovery=%+v err=%v", discovery, err)
	}
	candidate := discovery.Candidates[0]
	policy := domain.ArchaeologyExecutionPolicy{Depth: "standard", Sources: domain.ArchaeologySources{Git: true, Docs: true}}
	result, err := bridge.LaunchNative(context.Background(), domain.ArchaeologyNativeJob{ID: "job-exact", CandidateID: candidate.ID, ProjectID: "project-exact", Policy: policy}, domain.ArchaeologySession{}, candidate, func(context.Context, application.ArchaeologyNativeToolCall) application.ArchaeologyNativeToolResponse {
		return application.ArchaeologyNativeToolResponse{}
	}, func(domain.ArchaeologyNativeTerminal) {})
	if err != nil || result.ThreadID != "thread-captured" || result.TurnID != "turn-captured" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	limits, _ := policy.Limits()
	want := capturedHistorianLaunch{cwd: path, model: "gpt-5.6-luna", effort: "max", prompt: application.HistorianPrompt(policy), clientID: "commons-history-job-exact", title: application.HistorianTitle(candidate.Name, "job-exact"), policy: codexauth.HistorianPolicy{Depth: "standard", Git: true, Docs: true, MaxOutcomes: limits.MaxOutcomes, MaxProvenance: limits.MaxProvenancePerOutcome, MaxContributors: limits.MaxContributorsPerOutcome, MaxHistoricalAliases: limits.MaxHistoricalAliases, MaxHistoricalTasks: limits.MaxHistoricalTasks, MaxSourcesExamined: limits.MaxSourcesExamined}}
	if client.launch != want {
		t.Fatalf("launch=%+v want=%+v", client.launch, want)
	}
}

func TestNativeBridgeRecoversIdentityByExactCachedCWDAndJobTitle(t *testing.T) {
	path := eligibleTestWorkspace(t, "commons-recovery-")
	client := &recoveryCatalogClient{
		catalogClient: catalogClient{workspaces: []codexauth.Workspace{{CWD: path}}},
		launch:        codexauth.TaskLaunch{ThreadID: "thread-exact", SessionID: "session-exact", TurnID: "turn-exact"},
		found:         true,
	}
	bridge := &codexArchaeologyBridge{client: client, catalogKey: [32]byte{1}}
	discovery, err := bridge.DiscoverMetadata(context.Background())
	if err != nil || len(discovery.Candidates) != 1 {
		t.Fatalf("discovery=%+v err=%v", discovery, err)
	}
	candidate := discovery.Candidates[0]
	job := domain.ArchaeologyNativeJob{ID: "ARJ-exact", CandidateID: candidate.ID, ProjectID: candidate.CanonicalProjectID}
	result, found, err := bridge.RecoverNativeIdentity(context.Background(), job, candidate)
	if err != nil || !found || result.ThreadID != "thread-exact" || result.CodexSessionID != "session-exact" || result.TurnID != "turn-exact" {
		t.Fatalf("result=%+v found=%v err=%v", result, found, err)
	}
	if client.cwd != path || client.title != application.HistorianTitle(candidate.Name, job.ID) || !strings.Contains(client.title, job.ID) {
		t.Fatalf("cwd=%q title=%q", client.cwd, client.title)
	}
}

func TestNativeBridgeMarksPostRequestLaunchFailureUncertain(t *testing.T) {
	path := eligibleTestWorkspace(t, "commons-launch-failure-test-")
	client := launchFailureClient{catalogClient{workspaces: []codexauth.Workspace{{CWD: path}}}}
	bridge := &codexArchaeologyBridge{client: client}
	discovery, err := bridge.DiscoverMetadata(context.Background())
	if err != nil || len(discovery.Candidates) != 1 {
		t.Fatalf("discovery=%+v err=%v", discovery, err)
	}
	candidate := discovery.Candidates[0]
	result, err := bridge.LaunchNative(context.Background(), domain.ArchaeologyNativeJob{ID: "job", CandidateID: candidate.ID, ProjectID: "project", Policy: domain.ArchaeologyExecutionPolicy{Depth: "quick", Sources: domain.ArchaeologySources{Git: true}}}, domain.ArchaeologySession{}, candidate, func(context.Context, application.ArchaeologyNativeToolCall) application.ArchaeologyNativeToolResponse {
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

func TestNormalizeGitOriginStripsSCPUserinfoAndNeverProjectsCredentials(t *testing.T) {
	for input, want := range map[string]string{
		"git@github.com:owner/repo.git":         "github.com/owner/repo",
		"oauth-token@github.com:owner/repo.git": "github.com/owner/repo",
		"https://secret@github.com/owner/repo":  "github.com/owner/repo",
	} {
		got := normalizeGitOrigin(input)
		if got != want || strings.Contains(got, "oauth") || strings.Contains(got, "secret") || strings.Contains(repositoryLabel(got), "@") {
			t.Fatalf("normalizeGitOrigin(%q)=%q want=%q label=%q", input, got, want, repositoryLabel(got))
		}
	}
	for _, input := range []string{"token@:owner/repo", "user@host:", "user name@host:owner/repo", "host:owner/repo?token=x"} {
		if got := normalizeGitOrigin(input); got != "" {
			t.Fatalf("malformed credential origin %q projected as %q", input, got)
		}
	}
}

func TestCodexCatalogRejectsConflictingConfiguredRootMappings(t *testing.T) {
	path := eligibleTestWorkspace(t, "commons-conflicting-roots-")
	bridge := &codexArchaeologyBridge{client: catalogClient{}, roots: []ArchaeologyRoot{{ID: "one", Name: "One", Path: path}, {ID: "two", Name: "Two", Path: path}}}
	if _, err := bridge.DiscoverMetadata(context.Background()); err == nil {
		t.Fatal("conflicting configured roots accepted")
	}
}

func TestOversizedAppServerCatalogRefreshPreservesDurableSnapshotAndSelection(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	before, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "durable-catalog"}, domain.ArchaeologyDiscovery{SourceRootsScanned: 1, TasksExamined: 26, ProjectsGrouped: 1, AppServerIdentity: "codex-app-server/0.147.0", Candidates: []domain.ArchaeologyCandidate{{ID: "preserved", Name: "Preserved", PathLabel: "Preserved", HasGit: true, FromCodexMetadata: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	before, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "durable-selection", BaseRevision: before.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"preserved"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(repository, nil, nil)
	service.ConfigureProjectArchaeology(&codexArchaeologyBridge{client: oversizedCatalogClient{}}, nil)
	if _, err = service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "oversized-refresh"); !errors.Is(err, codexauth.ErrLineTooLarge) {
		t.Fatalf("oversized refresh err=%v", err)
	}
	after, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.DiscoveredAt != before.DiscoveredAt || after.TasksExamined != 26 || after.ProjectsGrouped != 1 || after.CatalogTruncated || after.AppServerIdentity != "codex-app-server/0.147.0" || len(after.Candidates) != 1 || after.Candidates[0].ID != "preserved" || len(after.Config.SelectedProjectIDs) != 1 || after.Config.SelectedProjectIDs[0] != "preserved" {
		t.Fatalf("failed oversized refresh replaced or falsified durable catalog: before=%+v after=%+v", before, after)
	}
}

func TestLiteralOversizedThreadListPagePreservesDurableCatalogThroughClientBridgeService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	type clientResult struct {
		client *codexauth.ClientImpl
		err    error
	}
	ready := make(chan clientResult, 1)
	go func() {
		client, err := codexauth.NewWithTransport(ctx, clientSide)
		ready <- clientResult{client: client, err: err}
	}()
	reader := bufio.NewReader(serverSide)
	readRequest := func() map[string]any {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err = json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	initialize := readRequest()
	response, _ := json.Marshal(map[string]any{"id": initialize["id"], "result": map[string]any{"serverInfo": map[string]string{"version": "0.147.0"}}})
	response = append(response, '\n')
	if _, err := serverSide.Write(response); err != nil {
		t.Fatal(err)
	}
	if initialized := readRequest(); initialized["method"] != "initialized" {
		t.Fatalf("initialized=%v", initialized)
	}
	created := <-ready
	if created.err != nil {
		t.Fatal(created.err)
	}
	defer created.client.Close()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	before, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "literal-before"}, domain.ArchaeologyDiscovery{TasksExamined: 26, ProjectsGrouped: 1, AppServerIdentity: "codex-app-server/0.147.0", Candidates: []domain.ArchaeologyCandidate{{ID: "preserved", Name: "Preserved", PathLabel: "Preserved", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	before, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "literal-config", BaseRevision: before.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"preserved"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(repository, nil, nil)
	service.ConfigureProjectArchaeology(&codexArchaeologyBridge{client: created.client}, nil)
	refresh := make(chan error, 1)
	go func() {
		_, refreshErr := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "literal-oversized")
		refresh <- refreshErr
	}()
	listRequest := readRequest()
	if listRequest["method"] != "thread/list" {
		t.Fatalf("request=%v", listRequest)
	}
	rows := make([]map[string]any, 0, 100)
	for index := 0; index < 100; index++ {
		rows = append(rows, map[string]any{"id": fmt.Sprintf("thread-%03d", index), "cwd": fmt.Sprintf("/workspace/project-%03d", index), "updatedAt": int64(1770000000 + index), "preview": strings.Repeat("private-preview-byte", 9000), "name": strings.Repeat("private-name", 32), "gitInfo": map[string]any{"originUrl": fmt.Sprintf("https://github.com/example/project-%03d.git", index), "branch": "main"}})
	}
	oversized, err := json.Marshal(map[string]any{"id": listRequest["id"], "result": map[string]any{"data": rows, "nextCursor": nil}})
	if err != nil {
		t.Fatal(err)
	}
	if len(oversized) <= codexauth.MaxReadLineBytes {
		t.Fatalf("realistic 100-row JSON-RPC page bytes=%d cap=%d", len(oversized), codexauth.MaxReadLineBytes)
	}
	t.Logf("realistic 100-row thread/list serialized_bytes=%d", len(oversized))
	oversized = append(oversized, '\n')
	if _, err = serverSide.Write(oversized); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	if err = <-refresh; !errors.Is(err, codexauth.ErrLineTooLarge) {
		t.Fatalf("refresh err=%v", err)
	}
	after, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.TasksExamined != 26 || after.ProjectsGrouped != 1 || after.CatalogTruncated || len(after.Candidates) != 1 || after.Candidates[0].ID != "preserved" || len(after.Config.SelectedProjectIDs) != 1 || after.Config.SelectedProjectIDs[0] != "preserved" {
		t.Fatalf("literal oversized page changed durable catalog: before=%+v after=%+v", before, after)
	}
}

func TestCodexCatalogSnapshotMakesThirtyConcurrentLaunchResolutionsO1(t *testing.T) {
	base := eligibleTestWorkspace(t, "commons-catalog-cache-")
	workspaces := make([]codexauth.Workspace, 0, 30)
	for index := 0; index < 30; index++ {
		path := filepath.Join(base, fmt.Sprintf("project-%02d", index))
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		workspaces = append(workspaces, codexauth.Workspace{CWD: path, UpdatedAt: time.Now().UTC().Add(time.Duration(index) * time.Second)})
	}
	client := &countingCatalogClient{workspaces: workspaces}
	bridge := &codexArchaeologyBridge{client: client}
	discovery, err := bridge.DiscoverMetadata(context.Background())
	if err != nil || len(discovery.Candidates) != 30 {
		t.Fatalf("discovery=%+v err=%v", discovery, err)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 30)
	for _, candidate := range discovery.Candidates {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			if path, ok := bridge.candidatePath(context.Background(), candidate.ID); !ok || path == "" {
				errs <- candidate.ID
			}
		}()
	}
	wg.Wait()
	close(errs)
	if failed := <-errs; failed != "" {
		t.Fatalf("candidate resolution failed: %s", failed)
	}
	client.mu.Lock()
	calls := client.calls
	client.mu.Unlock()
	if calls != 1 {
		t.Fatalf("ListWorkspaces calls=%d want 1 for discovery + 30 resolutions", calls)
	}
	// A new bridge simulates process restart: the first lazy resolution performs
	// one singleflight inventory even when all 30 queued jobs wake together.
	restarted := &codexArchaeologyBridge{client: client}
	errIDs := make(chan string, len(discovery.Candidates))
	for _, candidate := range discovery.Candidates {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := restarted.candidatePath(context.Background(), candidate.ID); !ok {
				errIDs <- candidate.ID
			}
		}()
	}
	wg.Wait()
	close(errIDs)
	if failed := <-errIDs; failed != "" {
		t.Fatalf("lazy restart resolution failed: %s", failed)
	}
	client.mu.Lock()
	calls = client.calls
	client.mu.Unlock()
	if calls != 2 {
		t.Fatalf("restart inventory calls=%d want 2 total", calls)
	}
}

func TestConfiguredSymlinkRootUsesSameCanonicalCandidatePathForLaunch(t *testing.T) {
	realPath := eligibleTestWorkspace(t, "commons-real-root-")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	linkParent, err := os.MkdirTemp(home, "commons-link-parent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(linkParent) })
	link := filepath.Join(linkParent, "project-link")
	if err = os.Symlink(realPath, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	bridge := &codexArchaeologyBridge{client: catalogClient{}, roots: []ArchaeologyRoot{{ID: "linked", Name: "Linked", Path: link}}}
	discovery, err := bridge.DiscoverMetadata(context.Background())
	if err != nil || len(discovery.Candidates) != 1 {
		t.Fatalf("discovery=%+v err=%v", discovery, err)
	}
	path, ok := bridge.candidatePath(context.Background(), discovery.Candidates[0].ID)
	if !ok || !sameWorkspacePath(path, realPath) {
		t.Fatalf("path=%q ok=%v real=%q", path, ok, realPath)
	}
}

func TestNativeBridgeCachesModelCapabilityAcrossCanonicalPolls(t *testing.T) {
	client := &availabilityCountingClient{}
	bridge := &codexArchaeologyBridge{client: client}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- bridge.Available(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	client.mu.Lock()
	calls := client.calls
	client.mu.Unlock()
	if calls != 1 {
		t.Fatalf("SupportsModel calls=%d want 1 across 20 polls", calls)
	}
	// A bridge is installation-process scoped. Recreating it after process
	// restart must revalidate capability instead of retaining stale support.
	restarted := &codexArchaeologyBridge{client: client}
	if err := restarted.Available(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	calls = client.calls
	client.mu.Unlock()
	if calls != 2 {
		t.Fatalf("SupportsModel calls=%d want 2 after bridge restart", calls)
	}
}
