package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
)

type codexArchaeologyBridge struct {
	client            codexauth.ArchaeologyClient
	roots             []ArchaeologyRoot
	catalogKey        [32]byte
	schedulerEligible func() bool
	// catalogRefreshMu is the installation-local singleflight gate. Explicit
	// discovery refreshes the snapshot; process-restart launch resolution lazily
	// performs at most one inventory even when many queued jobs wake together.
	catalogRefreshMu        sync.Mutex
	catalogMu               sync.Mutex
	catalog                 map[string]string
	capabilityMu            sync.Mutex
	capabilityUntil         time.Time
	capabilityErr           error
	capabilityGeneration    uint64
	capabilityGenerationSet bool
}

func (b *codexArchaeologyBridge) candidateID(key string) string {
	mac := hmac.New(sha256.New, b.catalogKey[:])
	_, _ = mac.Write([]byte(key))
	return "codex-" + hex.EncodeToString(mac.Sum(nil)[:12])
}

type catalogGroup struct {
	key, path, origin   string
	last                time.Time
	count               int
	fromCodex, fromRoot bool
	root                *ArchaeologyRoot
}

func (b *codexArchaeologyBridge) DiscoverMetadata(ctx context.Context) (domain.ArchaeologyDiscovery, error) {
	if b == nil {
		return domain.ArchaeologyDiscovery{}, domain.ErrUnavailable
	}
	b.catalogRefreshMu.Lock()
	defer b.catalogRefreshMu.Unlock()
	return b.discoverMetadata(ctx)
}

func (b *codexArchaeologyBridge) discoverMetadata(ctx context.Context) (domain.ArchaeologyDiscovery, error) {
	// One explicit discovery refresh owns the inventory scan. Concurrent launch
	// resolution waits for this bounded snapshot instead of multiplying scans.
	b.catalogMu.Lock()
	defer b.catalogMu.Unlock()
	groups := map[string]*catalogGroup{}
	pathKeys := map[string]string{}
	tasksExamined := 0
	if b != nil && b.client != nil {
		workspaces, err := b.client.ListWorkspaces(ctx)
		if err != nil {
			return domain.ArchaeologyDiscovery{}, err
		}
		tasksExamined = len(workspaces)
		for _, item := range workspaces {
			path, ok := eligibleWorkspace(item.CWD)
			if !ok {
				continue
			}
			origin := normalizeGitOrigin(item.GitOrigin)
			key := "path:" + path
			if origin != "" {
				key = "origin:" + origin
			}
			group := groups[key]
			if group == nil {
				group = &catalogGroup{key: key, path: path, origin: origin, last: item.UpdatedAt, fromCodex: true}
				groups[key] = group
			}
			group.count++
			group.fromCodex = true
			if betterWorkspaceCandidate(path, item.UpdatedAt, group.path, group.last) {
				group.path = path
			}
			if item.UpdatedAt.After(group.last) {
				group.last = item.UpdatedAt
			}
			pathKeys[path] = key
		}
	}
	for index := range b.roots {
		root := b.roots[index]
		path, ok := eligibleWorkspace(root.Path)
		if !ok {
			return domain.ArchaeologyDiscovery{}, domain.ErrInvalid
		}
		key := pathKeys[path]
		if key == "" {
			key = "path:" + path
		}
		group := groups[key]
		if group == nil {
			group = &catalogGroup{key: key, path: path}
			groups[key] = group
		}
		copy := root
		if group.root != nil && group.root.ID != copy.ID {
			return domain.ArchaeologyDiscovery{}, domain.ErrConflict
		}
		group.root = &copy
		group.fromRoot = true
		pathKeys[path] = key
	}
	identityDigest := sha256.Sum256(b.catalogKey[:])
	out := domain.ArchaeologyDiscovery{SourceRootsScanned: len(b.roots), TasksExamined: tasksExamined, ProjectsGrouped: len(groups), Truncated: tasksExamined >= 10000, AppServerIdentity: "paired-" + hex.EncodeToString(identityDigest[:8]), Candidates: make([]domain.ArchaeologyCandidate, 0, len(groups))}
	paths := make(map[string]string, len(groups))
	for _, group := range groups {
		info, err := os.Stat(group.path)
		localDirectory := err == nil && info.IsDir()
		if !localDirectory && !group.fromCodex {
			continue
		}
		name := filepath.Base(group.path)
		pathLabel := name
		repositoryLabel := repositoryLabel(group.origin)
		if group.root != nil {
			name = group.root.Name
			pathLabel = group.root.Name
			if group.root.RepositoryLabel != "" {
				repositoryLabel = group.root.RepositoryLabel
			}
		}
		last := group.last
		if localDirectory && (last.IsZero() || info.ModTime().After(last)) {
			last = info.ModTime().UTC()
		}
		now := time.Now().UTC()
		if last.After(now.Add(5 * time.Minute)) {
			last = now
		}
		hasGit, hasDocs := group.origin != "", false
		if localDirectory {
			if _, gitErr := os.Stat(filepath.Join(group.path, ".git")); gitErr == nil {
				hasGit = true
			}
			_, docsErr := os.Stat(filepath.Join(group.path, "docs"))
			hasDocs = docsErr == nil
		}
		candidateID := b.candidateID(group.key)
		canonicalProjectID := candidateID
		if group.root != nil {
			canonicalProjectID = group.root.ID
		}
		candidate := domain.ArchaeologyCandidate{
			ID: candidateID, CanonicalProjectID: canonicalProjectID, Name: name, PathLabel: pathLabel,
			RepositoryLabel: repositoryLabel, LastActivityAt: last,
			HasGit: hasGit, HasDocs: hasDocs, HasCodexHistory: group.count > 0,
			FromCodexMetadata: group.fromCodex, FromConfiguredRoot: group.fromRoot, CodexThreadCount: group.count,
			DurationMinSeconds: 60, DurationMaxSeconds: 600, RelativeCost: "medium",
			PrivacyNote: "Catalog uses workspace metadata only. Codex 0.147 protocol preview bytes may arrive, but Commons does not represent, retain, persist, project, or log them.",
		}
		out.Candidates = append(out.Candidates, candidate)
		paths[candidateID] = group.path
	}
	sort.Slice(out.Candidates, func(i, j int) bool {
		if !out.Candidates[i].LastActivityAt.Equal(out.Candidates[j].LastActivityAt) {
			return out.Candidates[i].LastActivityAt.After(out.Candidates[j].LastActivityAt)
		}
		return out.Candidates[i].ID < out.Candidates[j].ID
	})
	snapshot := make(map[string]string, len(out.Candidates))
	for _, candidate := range out.Candidates {
		snapshot[candidate.ID] = paths[candidate.ID]
	}
	b.catalog = snapshot
	return out, nil
}

func eligibleWorkspace(value string) (string, bool) {
	if !filepath.IsAbs(value) || strings.ContainsAny(value, "\r\n\x00") {
		return "", false
	}
	path, err := filepath.EvalSymlinks(filepath.Clean(value))
	if err != nil {
		path = filepath.Clean(value)
	}
	slashPath := filepath.ToSlash(path)
	if broadWorkspaceRoot(path) || workspacePathWithin(path, os.TempDir()) ||
		strings.Contains(slashPath, "/.codex/worktrees/") || strings.Contains(slashPath, "/.codex/tasks/") ||
		strings.Contains(slashPath, "/node_modules/") || strings.Contains(slashPath, "/.local/builds/") {
		return "", false
	}
	return path, true
}

func broadWorkspaceRoot(value string) bool {
	value = filepath.Clean(value)
	root := filepath.VolumeName(value) + string(filepath.Separator)
	if sameWorkspacePath(value, root) {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	home = filepath.Clean(home)
	if evaluated, evalErr := filepath.EvalSymlinks(home); evalErr == nil {
		home = evaluated
	}
	return sameWorkspacePath(value, home) || sameWorkspacePath(value, filepath.Dir(home))
}

func workspacePathWithin(value, parent string) bool {
	value = filepath.Clean(value)
	parent = filepath.Clean(parent)
	if sameWorkspacePath(value, parent) {
		return true
	}
	prefix := parent + string(filepath.Separator)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix))
	}
	return strings.HasPrefix(value, prefix)
}

func sameWorkspacePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func betterWorkspacePath(candidate, current string) bool {
	if current == "" {
		return true
	}
	candidateSlash, currentSlash := filepath.ToSlash(candidate), filepath.ToSlash(current)
	candidateNoise := strings.Contains(candidateSlash, "/.codex/") || strings.Contains(candidateSlash, "/worktree")
	currentNoise := strings.Contains(currentSlash, "/.codex/") || strings.Contains(currentSlash, "/worktree")
	if candidateNoise != currentNoise {
		return !candidateNoise
	}
	depth := func(path string) int { return strings.Count(filepath.Clean(path), string(filepath.Separator)) }
	if depth(candidate) != depth(current) {
		return depth(candidate) < depth(current)
	}
	return candidate < current
}

func betterWorkspaceCandidate(candidate string, candidateUpdated time.Time, current string, currentUpdated time.Time) bool {
	if current == "" || candidateUpdated.After(currentUpdated) {
		return true
	}
	if candidateUpdated.Before(currentUpdated) {
		return false
	}
	return betterWorkspacePath(candidate, current)
}

func normalizeGitOrigin(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if filepath.IsAbs(value) || strings.HasPrefix(lower, "file:") || strings.ContainsAny(value, "\r\n\x00\\") {
		return ""
	}
	if !strings.Contains(value, "://") && strings.Contains(value, ":") {
		// SCP syntax is [userinfo@]host:path. Never project its userinfo; it may
		// be a username, deploy token, or OAuth credential.
		left, right, ok := strings.Cut(value, ":")
		if !ok || left == "" || right == "" || strings.Contains(left, "/") {
			return ""
		}
		if strings.ContainsAny(left, " \t") || strings.EqualFold(left, "http") || strings.EqualFold(left, "https") || strings.EqualFold(left, "ssh") {
			return ""
		}
		if at := strings.LastIndex(left, "@"); at >= 0 {
			left = left[at+1:]
		}
		if left == "" || len(left) > 253 || len(right) > 500 || strings.ContainsAny(left+right, " @?#") {
			return ""
		}
		value = left + "/" + right
	} else if parsed, err := url.Parse(value); err == nil {
		if parsed.Scheme != "" && parsed.Host == "" {
			return ""
		}
		if parsed.Host != "" {
			value = parsed.Host + parsed.Path
		}
	}
	value = strings.TrimSuffix(strings.TrimSuffix(strings.Trim(value, "/"), ".git"), "/")
	if value == "" || len(value) > 800 || strings.Contains(value, "@") {
		return ""
	}
	return strings.ToLower(value)
}

func repositoryLabel(origin string) string {
	if origin == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(origin, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return parts[len(parts)-1]
}

func (b *codexArchaeologyBridge) candidatePath(ctx context.Context, candidateID string) (string, bool) {
	if b == nil || candidateID == "" {
		return "", false
	}
	b.catalogMu.Lock()
	path, ready := b.catalog[candidateID]
	initialized := b.catalog != nil
	b.catalogMu.Unlock()
	if !initialized {
		b.catalogRefreshMu.Lock()
		b.catalogMu.Lock()
		if b.catalog == nil {
			b.catalogMu.Unlock()
			if _, err := b.discoverMetadata(ctx); err != nil {
				b.catalogRefreshMu.Unlock()
				return "", false
			}
			b.catalogMu.Lock()
		}
		path, ready = b.catalog[candidateID]
		b.catalogMu.Unlock()
		b.catalogRefreshMu.Unlock()
	}
	if !ready {
		return "", false
	}
	validated, ok := eligibleWorkspace(path)
	return validated, ok && sameWorkspacePath(path, validated)
}

func (b *codexArchaeologyBridge) Available(ctx context.Context) error {
	if b == nil || b.client == nil {
		return codexauth.ErrUnavailable
	}
	experimental, ok := b.client.(codexauth.ExperimentalArchaeologyClient)
	if !ok || !experimental.ExperimentalDynamicTools() {
		return codexauth.ErrUnavailable
	}
	if b.schedulerEligible != nil {
		// The runtime monitor owns the expensive supervisor/account/model
		// observations. A configured gate is an immutable, atomic projection of
		// SchedulerEligible; request-path availability must never call
		// SupportsModel or otherwise perform Codex I/O.
		if !b.schedulerEligible() {
			return codexauth.ErrUnavailable
		}
		return nil
	}
	// Read the supervisor generation before taking the capability cache lock.
	// Snapshot is a detached, read-only view; keeping it outside capabilityMu
	// preserves the existing singleflight gate without coupling its lock order
	// to the managed-process reporter.
	generation, generationKnown := uint64(0), false
	if reporter, ok := b.client.(codexauth.Reporter); ok {
		generation = reporter.Snapshot().Generation
		generationKnown = generation != 0
	}
	b.capabilityMu.Lock()
	defer b.capabilityMu.Unlock()
	if generationKnown && (!b.capabilityGenerationSet || b.capabilityGeneration != generation) {
		// A new managed-process generation must revalidate both successful and
		// failed capability results immediately. Generation zero deliberately
		// retains the legacy TTL-only behavior for reporters without a usable
		// generation and for clients that do not implement Reporter.
		b.capabilityGeneration = generation
		b.capabilityGenerationSet = true
		b.capabilityUntil = time.Time{}
		b.capabilityErr = nil
	}
	now := time.Now()
	if now.Before(b.capabilityUntil) {
		return b.capabilityErr
	}
	supported, err := b.client.SupportsModel(ctx, "gpt-5.6-luna", "max")
	if err != nil {
		// Transport errors may recover, but do not let 1.5-second UI polling
		// multiply model inventories while the process is unhealthy.
		b.capabilityErr, b.capabilityUntil = err, now.Add(5*time.Second)
		return err
	}
	if !supported {
		b.capabilityErr, b.capabilityUntil = codexauth.ErrUnavailable, now.Add(time.Minute)
		return b.capabilityErr
	}
	b.capabilityErr, b.capabilityUntil = nil, now.Add(time.Minute)
	return nil
}

// Launch is retained only for the pre-native launcher interface. Direct
// per-project creation uses LaunchProject.
func (b *codexArchaeologyBridge) Launch(context.Context, domain.ArchaeologySession) error {
	return domain.ErrUnavailable
}

func (b *codexArchaeologyBridge) LaunchProject(ctx context.Context, session domain.ArchaeologySession, candidate domain.ArchaeologyCandidate, grant, launchID string) (domain.ArchaeologyLaunchResult, error) {
	_ = ctx
	_ = session
	_ = grant
	return domain.ArchaeologyLaunchResult{LaunchID: launchID, ProjectID: candidate.ID}, codexauth.ErrUnavailable
}

func boolText(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func (b *codexArchaeologyBridge) LaunchNative(ctx context.Context, job domain.ArchaeologyNativeJob, session domain.ArchaeologySession, candidate domain.ArchaeologyCandidate, onTool func(context.Context, application.ArchaeologyNativeToolCall) application.ArchaeologyNativeToolResponse, onTerminal func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	experimental, ok := b.client.(codexauth.ExperimentalArchaeologyClient)
	if !ok || !experimental.ExperimentalDynamicTools() {
		return domain.ArchaeologyLaunchResult{LaunchID: job.ID, ProjectID: job.ProjectID}, codexauth.ErrUnavailable
	}
	cwd, ok := b.candidatePath(ctx, candidate.ID)
	if !ok {
		return domain.ArchaeologyLaunchResult{LaunchID: job.ID, ProjectID: job.ProjectID}, codexauth.ErrUnavailable
	}
	limits, validPolicy := job.Policy.Limits()
	if !validPolicy {
		return domain.ArchaeologyLaunchResult{LaunchID: job.ID, ProjectID: job.ProjectID}, domain.ErrInvalid
	}
	policy := codexauth.HistorianPolicy{Depth: job.Policy.Depth, Git: job.Policy.Sources.Git, Docs: job.Policy.Sources.Docs, CodexHistory: job.Policy.Sources.CodexHistory, MaxOutcomes: limits.MaxOutcomes, MaxProvenance: limits.MaxProvenancePerOutcome, MaxContributors: limits.MaxContributorsPerOutcome, MaxHistoricalAliases: limits.MaxHistoricalAliases, MaxHistoricalTasks: limits.MaxHistoricalTasks, MaxSourcesExamined: limits.MaxSourcesExamined}
	title := application.HistorianTitle(candidate.Name, job.ID)
	if title == "" {
		return domain.ArchaeologyLaunchResult{LaunchID: job.ID, ProjectID: job.ProjectID}, domain.ErrInvalid
	}
	result, err := experimental.LaunchHistorianTask(ctx, cwd, "gpt-5.6-luna", "max", application.HistorianPrompt(job.Policy), "commons-history-"+job.ID, title, policy, func(toolCtx context.Context, call codexauth.DynamicToolCall) codexauth.DynamicToolResponse {
		response := onTool(toolCtx, application.ArchaeologyNativeToolCall{ThreadID: call.ThreadID, TurnID: call.TurnID, Tool: call.Tool, Arguments: append([]byte(nil), call.Arguments...)})
		if !response.Success {
			return codexauth.DynamicToolResponse{}
		}
		return codexauth.DynamicToolResponse{Success: true, ContentItems: []codexauth.DynamicToolContent{{Type: "inputText", Text: response.Message}}}
	}, func(terminal codexauth.TurnTerminal) {
		onTerminal(domain.ArchaeologyNativeTerminal{JobID: job.ID, ThreadID: terminal.ThreadID, TurnID: terminal.TurnID, Status: terminal.Status, DurationMS: terminal.DurationMS})
	})
	state := ""
	if err != nil {
		// Once the App Server launch call begins, a transport or protocol error
		// cannot prove that thread/start was rejected. Never retry silently.
		state = "uncertain"
	}
	return domain.ArchaeologyLaunchResult{LaunchID: job.ID, ProjectID: job.ProjectID, State: state, ThreadID: result.ThreadID, CodexSessionID: result.SessionID, TurnID: result.TurnID, Error: result.Stage}, err
}

func (b *codexArchaeologyBridge) FinalizeNative(ctx context.Context, _ domain.ArchaeologyNativeJob, candidate domain.ArchaeologyCandidate, result domain.ArchaeologyLaunchResult) error {
	renamer, ok := b.client.(codexauth.HistorianTaskRenamer)
	if !ok {
		return codexauth.ErrUnavailable
	}
	return renamer.RenameHistorianTask(ctx, result.ThreadID, application.HistorianVisibleTitle(candidate.Name))
}

func (b *codexArchaeologyBridge) RecoverNativeIdentity(ctx context.Context, job domain.ArchaeologyNativeJob, candidate domain.ArchaeologyCandidate) (domain.ArchaeologyLaunchResult, bool, error) {
	finder, ok := b.client.(codexauth.HistorianTaskFinder)
	if !ok {
		return domain.ArchaeologyLaunchResult{}, false, nil
	}
	cwd, ok := b.candidatePath(ctx, candidate.ID)
	if !ok {
		return domain.ArchaeologyLaunchResult{}, false, codexauth.ErrUnavailable
	}
	title := application.HistorianTitle(candidate.Name, job.ID)
	if title == "" {
		return domain.ArchaeologyLaunchResult{}, false, domain.ErrInvalid
	}
	result, found, err := finder.FindHistorianTask(ctx, cwd, title)
	if err != nil || !found {
		return domain.ArchaeologyLaunchResult{}, false, err
	}
	return domain.ArchaeologyLaunchResult{LaunchID: job.ID, ProjectID: job.ProjectID, ThreadID: result.ThreadID, CodexSessionID: result.SessionID, TurnID: result.TurnID}, true, nil
}

func (b *codexArchaeologyBridge) InterruptNative(ctx context.Context, job domain.ArchaeologyNativeJob) error {
	experimental, ok := b.client.(codexauth.ExperimentalArchaeologyClient)
	if !ok {
		return codexauth.ErrUnavailable
	}
	return experimental.InterruptTurn(ctx, job.ThreadID, job.TurnID)
}
