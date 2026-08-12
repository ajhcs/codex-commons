package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"codex-commons/internal/domain"
)

type ArchaeologyRoot struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	PathLabel       string `json:"path_label"`
	RepositoryLabel string `json:"repository_label,omitempty"`
}

var archaeologyRootID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,119}$`)

func readArchaeologyRoots(path string) ([]ArchaeologyRoot, error) {
	if path == "" {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, errors.New("archaeology roots file must be a regular file no larger than 64 KiB")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("archaeology roots file must not be accessible by group or other users")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var payload struct {
		Roots []ArchaeologyRoot `json:"roots"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&payload); err != nil {
		return nil, err
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("archaeology roots file must contain exactly one JSON object")
	}
	if len(payload.Roots) > 100 {
		return nil, errors.New("at most 100 archaeology roots are allowed")
	}
	seen := map[string]bool{}
	for i := range payload.Roots {
		root := &payload.Roots[i]
		root.Path = filepath.Clean(root.Path)
		if !archaeologyRootID.MatchString(root.ID) || seen[root.ID] || strings.TrimSpace(root.Name) == "" || len(root.Name) > 200 || !filepath.IsAbs(root.Path) || strings.TrimSpace(root.PathLabel) == "" || len(root.PathLabel) > 300 || len(root.RepositoryLabel) > 300 {
			return nil, errors.New("invalid archaeology root allowlist entry")
		}
		entryInfo, statErr := os.Stat(root.Path)
		if statErr != nil || !entryInfo.IsDir() {
			return nil, errors.New("archaeology root must be an existing directory")
		}
		seen[root.ID] = true
	}
	sort.Slice(payload.Roots, func(i, j int) bool { return payload.Roots[i].ID < payload.Roots[j].ID })
	return payload.Roots, nil
}

type allowlistedArchaeologyDiscoverer struct{ roots []ArchaeologyRoot }

func (d allowlistedArchaeologyDiscoverer) DiscoverMetadata(ctx context.Context) (domain.ArchaeologyDiscovery, error) {
	_ = ctx
	out := domain.ArchaeologyDiscovery{SourceRootsScanned: len(d.roots), Candidates: make([]domain.ArchaeologyCandidate, 0, len(d.roots))}
	for _, root := range d.roots {
		info, err := os.Stat(root.Path)
		if err != nil {
			return domain.ArchaeologyDiscovery{}, err
		}
		_, gitErr := os.Stat(filepath.Join(root.Path, ".git"))
		_, docsErr := os.Stat(filepath.Join(root.Path, "docs"))
		candidate := domain.ArchaeologyCandidate{ID: root.ID, Name: root.Name, PathLabel: root.PathLabel, RepositoryLabel: root.RepositoryLabel, LastActivityAt: info.ModTime().UTC(), HasGit: gitErr == nil, HasDocs: docsErr == nil, DurationMinSeconds: 60, DurationMaxSeconds: 600, RelativeCost: "medium", PrivacyNote: "Only allowlisted metadata is shown; source bodies remain outside Commons until a Codex-owned historian is claimed."}
		if candidate.LastActivityAt.After(time.Now().Add(5 * time.Minute)) {
			candidate.LastActivityAt = time.Now().UTC()
		}
		out.Candidates = append(out.Candidates, candidate)
	}
	return out, nil
}
