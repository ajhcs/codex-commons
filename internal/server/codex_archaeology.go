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

	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
)

type codexArchaeologyBridge struct {
	client     codexauth.ArchaeologyClient
	roots      []ArchaeologyRoot
	baseURL    string
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
		candidate := domain.ArchaeologyCandidate{
			ID: b.candidateID(group.key), Name: name, PathLabel: pathLabel,
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
	out := domain.ArchaeologyLaunchResult{LaunchID: launchID, ProjectID: candidate.ID}
	if err := b.Available(ctx); err != nil {
		return out, err
	}
	cwd, ok := b.candidatePath(ctx, candidate.ID)
	if !ok {
		return out, domain.ErrNotFound
	}
	prompt := "You are the Codex Commons project historian for one explicitly selected project.\\n\\n" +
		"Immutable launch: " + launchID + "\\nProject ID: " + candidate.ID +
		"\\nCodex thread: {{CODEX_THREAD_ID}}\\nCodex session: {{CODEX_SESSION_ID}}" +
		"\\nDepth: " + session.Config.Depth +
		"\\nSources: git=" + boolText(session.Config.Sources.Git) + ", docs=" + boolText(session.Config.Sources.Docs) + ", codex_history=" + boolText(session.Config.Sources.CodexHistory) +
		"\\n\\nInspect only this working directory and the enabled source kinds. Do not mutate source material or Commons. Produce source-grounded historical import proposals with exact sha256 digests and exact contributor session IDs. Current Commons data wins. Human review and digest confirmation are mandatory before import." +
		"\\n\\nSingle-purpose report grant (30 minute launch possession; not App Server attestation): " + grant +
		"\\nCommons base URL: " + b.baseURL +
		"\\nClaim this launch with POST /v1/project-archaeology/task/claim using launch_id, project_id, the exact thread_id and session_id above, and grant. Preserve the returned report_token only for this task." +
		"\\nReport proposals with POST /v1/project-archaeology/task/report using the same exact identities, report_token, outcomes, and a stable Idempotency-Key. This route can submit proposals for human review only; it cannot apply them."
	result, err := b.client.LaunchTask(ctx, cwd, "gpt-5.6-luna", "max", prompt, "commons-"+launchID)
	out.ThreadID, out.CodexSessionID, out.TurnID = result.ThreadID, result.SessionID, result.TurnID
	return out, err
}

func boolText(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}
