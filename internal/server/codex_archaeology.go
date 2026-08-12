package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
)

type codexArchaeologyBridge struct {
	client     codexauth.ArchaeologyClient
	roots      []ArchaeologyRoot
	catalogKey [32]byte
}

func (b *codexArchaeologyBridge) candidateID(key string) string {
	mac := hmac.New(sha256.New, b.catalogKey[:])
	_, _ = mac.Write([]byte(key))
	return "codex-" + hex.EncodeToString(mac.Sum(nil)[:12])
}

type catalogGroup struct {
	key, path, origin, branch string
	last                      time.Time
	count                     int
	fromCodex, fromRoot       bool
	root                      *ArchaeologyRoot
}

func (b *codexArchaeologyBridge) DiscoverMetadata(ctx context.Context) (domain.ArchaeologyDiscovery, error) {
	groups := map[string]*catalogGroup{}
	pathKeys := map[string]string{}
	if b != nil && b.client != nil {
		workspaces, err := b.client.ListWorkspaces(ctx)
		if err != nil {
			return domain.ArchaeologyDiscovery{}, err
		}
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
				group = &catalogGroup{key: key, path: path, origin: origin, branch: item.GitBranch, fromCodex: true}
				groups[key] = group
			}
			group.count++
			group.fromCodex = true
			if item.UpdatedAt.After(group.last) {
				group.last = item.UpdatedAt
			}
			if betterWorkspacePath(path, group.path) {
				group.path = path
			}
			pathKeys[path] = key
		}
	}
	for index := range b.roots {
		root := b.roots[index]
		path, err := filepath.EvalSymlinks(root.Path)
		if err != nil {
			path = root.Path
		}
		path = filepath.Clean(path)
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
	out := domain.ArchaeologyDiscovery{SourceRootsScanned: len(b.roots), Candidates: make([]domain.ArchaeologyCandidate, 0, len(groups))}
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
			PrivacyNote: "Cataloged from bounded Codex thread metadata and configured roots; prompts and message bodies are not read.",
		}
		out.Candidates = append(out.Candidates, candidate)
	}
	sort.Slice(out.Candidates, func(i, j int) bool {
		if !out.Candidates[i].LastActivityAt.Equal(out.Candidates[j].LastActivityAt) {
			return out.Candidates[i].LastActivityAt.After(out.Candidates[j].LastActivityAt)
		}
		return out.Candidates[i].ID < out.Candidates[j].ID
	})
	if len(out.Candidates) > 100 {
		out.Candidates = out.Candidates[:100]
	}
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
	if path == "/" || path == "/home" || path == "/home/plumbob" || strings.HasPrefix(path, "/tmp/") ||
		strings.Contains(path, "/.codex/worktrees/") || strings.Contains(path, "/.codex/tasks/") ||
		strings.Contains(path, "/node_modules/") || strings.Contains(path, "/.local/builds/") {
		return "", false
	}
	return path, true
}

func betterWorkspacePath(candidate, current string) bool {
	if current == "" {
		return true
	}
	candidateNoise := strings.Contains(candidate, "/.codex/") || strings.Contains(candidate, "/worktree")
	currentNoise := strings.Contains(current, "/.codex/") || strings.Contains(current, "/worktree")
	if candidateNoise != currentNoise {
		return !candidateNoise
	}
	depth := func(path string) int { return strings.Count(filepath.Clean(path), string(filepath.Separator)) }
	if depth(candidate) != depth(current) {
		return depth(candidate) < depth(current)
	}
	return candidate < current
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
	if strings.HasPrefix(value, "git@") {
		value = strings.TrimPrefix(value, "git@")
		value = strings.Replace(value, ":", "/", 1)
	} else if parsed, err := url.Parse(value); err == nil {
		if parsed.Scheme != "" && parsed.Host == "" {
			return ""
		}
		if parsed.Host != "" {
			value = parsed.Host + parsed.Path
		}
	}
	value = strings.TrimSuffix(strings.TrimSuffix(strings.Trim(value, "/"), ".git"), "/")
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
	discovery, err := b.DiscoverMetadata(ctx)
	if err != nil {
		return "", false
	}
	// Repeat the grouping in the same deterministic order and resolve only the
	// selected server-side ID. Raw paths are never persisted or returned.
	workspaces, err := b.client.ListWorkspaces(ctx)
	if err != nil {
		return "", false
	}
	groups := map[string]string{}
	for _, item := range workspaces {
		path, ok := eligibleWorkspace(item.CWD)
		if !ok {
			continue
		}
		key := "path:" + path
		if origin := normalizeGitOrigin(item.GitOrigin); origin != "" {
			key = "origin:" + origin
		}
		if betterWorkspacePath(path, groups[key]) {
			groups[key] = path
		}
	}
	for _, root := range b.roots {
		path := filepath.Clean(root.Path)
		key := "path:" + path
		for groupKey, groupPath := range groups {
			if groupPath == path {
				key = groupKey
				break
			}
		}
		if groups[key] == "" {
			groups[key] = path
		}
	}
	for key, path := range groups {
		if b.candidateID(key) == candidateID {
			for _, candidate := range discovery.Candidates {
				if candidate.ID == candidateID {
					return path, true
				}
			}
		}
	}
	return "", false
}

func (b *codexArchaeologyBridge) Available(ctx context.Context) error {
	if b == nil || b.client == nil {
		return codexauth.ErrUnavailable
	}
	experimental, ok := b.client.(codexauth.ExperimentalArchaeologyClient)
	if !ok || !experimental.ExperimentalDynamicTools() {
		return codexauth.ErrUnavailable
	}
	supported, err := b.client.SupportsModel(ctx, "gpt-5.6-luna", "max")
	if err != nil {
		return err
	}
	if !supported {
		return codexauth.ErrUnavailable
	}
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
	result, err := experimental.LaunchHistorianTask(ctx, cwd, "gpt-5.6-luna", "max", application.HistorianPrompt(candidate), "commons-history-"+job.ID, application.HistorianTitle(candidate.Name), func(toolCtx context.Context, call codexauth.DynamicToolCall) codexauth.DynamicToolResponse {
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
	return domain.ArchaeologyLaunchResult{LaunchID: job.ID, ProjectID: job.ProjectID, State: state, ThreadID: result.ThreadID, CodexSessionID: result.SessionID, TurnID: result.TurnID}, err
}

func (b *codexArchaeologyBridge) InterruptNative(ctx context.Context, job domain.ArchaeologyNativeJob) error {
	experimental, ok := b.client.(codexauth.ExperimentalArchaeologyClient)
	if !ok {
		return codexauth.ErrUnavailable
	}
	return experimental.InterruptTurn(ctx, job.ThreadID, job.TurnID)
}
