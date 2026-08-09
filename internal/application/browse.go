package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"codex-commons/internal/domain"
)

const (
	defaultBrowseLimit = 25
	maxBrowseLimit     = 100
	maxPeopleScan      = 500
)

type BrowseRepository interface {
	AttentionBrowseSnapshot(context.Context, domain.AttentionBrowseQuery) (domain.AttentionBrowseSnapshot, error)
	ProjectBrowseSnapshot(context.Context, domain.ProjectBrowseQuery) (domain.ProjectBrowseSnapshot, error)
	PeopleFactsSnapshot(context.Context, domain.PeopleFactsQuery) (domain.PeopleFactsSnapshot, error)
}

type opaqueCursor struct {
	Version  int    `json:"v"`
	Resource string `json:"r"`
	Time     string `json:"t,omitempty"`
	Text     string `json:"x,omitempty"`
	ID       string `json:"id"`
}

func encodeCursor(resource string, cursor domain.BrowseCursor) string {
	w := opaqueCursor{Version: 1, Resource: resource, Text: cursor.Text, ID: cursor.ID}
	if !cursor.Time.IsZero() {
		w.Time = cursor.Time.UTC().Format(time.RFC3339Nano)
	}
	payload, _ := json.Marshal(w)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(resource, raw string) (*domain.BrowseCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 512 {
		return nil, domain.ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, domain.ErrInvalid
	}
	var w opaqueCursor
	if err := json.Unmarshal(payload, &w); err != nil || w.Version != 1 || w.Resource != resource || w.ID == "" {
		return nil, domain.ErrInvalid
	}
	out := &domain.BrowseCursor{Text: w.Text, ID: w.ID}
	if w.Time != "" {
		out.Time, err = time.Parse(time.RFC3339Nano, w.Time)
		if err != nil {
			return nil, domain.ErrInvalid
		}
	}
	return out, nil
}

func browseLimit(value int) (int, bool) {
	if value == 0 {
		return defaultBrowseLimit, true
	}
	return value, value >= 1 && value <= maxBrowseLimit
}

type AttentionBrowseRequest struct {
	Cursor, Search, Source, Owner, Severity, Project string
	UpdatedFrom, UpdatedTo                           *time.Time
	Limit                                            int
}

type BrowseDestination struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type BrowseAttentionItem struct {
	AttentionItem
}

type AttentionBrowseResult struct {
	Total      int                    `json:"total"`
	Limit      int                    `json:"limit"`
	Items      []BrowseAttentionItem  `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
	Facets     domain.AttentionFacets `json:"facets"`
}

func (s *Service) BrowseAttention(ctx context.Context, request AttentionBrowseRequest) (AttentionBrowseResult, error) {
	if s == nil || len(request.Search) > 200 || strings.TrimSpace(request.Search) != request.Search {
		return AttentionBrowseResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(BrowseRepository)
	if !ok {
		return AttentionBrowseResult{}, domain.ErrUnavailable
	}
	limit, ok := browseLimit(request.Limit)
	if !ok {
		return AttentionBrowseResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("attention", request.Cursor)
	if err != nil || after != nil && after.Time.IsZero() {
		return AttentionBrowseResult{}, domain.ErrInvalid
	}
	snapshot, err := repository.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{
		Filters: domain.AttentionFilters{Search: request.Search, SourceKind: request.Source, OwnerSessionID: request.Owner,
			Severity: request.Severity, ProjectID: request.Project,
			UpdatedFrom: request.UpdatedFrom, UpdatedTo: request.UpdatedTo},
		After: after, Limit: limit,
	})
	if err != nil {
		return AttentionBrowseResult{}, err
	}
	hasMore := len(snapshot.Items) > limit
	if hasMore {
		snapshot.Items = snapshot.Items[:limit]
	}
	out := AttentionBrowseResult{Total: snapshot.Total, Limit: limit,
		Items: make([]BrowseAttentionItem, 0, len(snapshot.Items)), Facets: snapshot.Facets}
	for _, item := range snapshot.Items {
		view := BrowseAttentionItem{AttentionItem: AttentionItem{
			ID: item.ID, Severity: item.Severity, Title: item.Title,
			Project: item.ProjectID, ProjectName: item.ProjectName,
			SourceRef: item.SourceRef, Owner: item.AccountableSessionID,
			NextAction: item.NextAction, SourceKind: item.SourceKind,
			UpdatedAt: item.UpdatedAt, Untrusted: item.Untrusted,
		}}
		if item.Destination != nil {
			view.AttentionItem.Destination = &BrowseDestination{Kind: item.Destination.Kind, Ref: item.Destination.Ref}
		}
		out.Items = append(out.Items, view)
	}
	if hasMore && len(snapshot.Items) > 0 {
		last := snapshot.Items[len(snapshot.Items)-1]
		out.NextCursor = encodeCursor("attention", domain.BrowseCursor{Time: last.UpdatedAt, ID: last.ID})
	}
	return out, nil
}

type ProjectsBrowseRequest struct {
	Cursor, Search string
	Limit          int
}

type ProjectWork struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Priority int    `json:"priority"`
}

type ProjectBrowseItem struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Status         string            `json:"status,omitempty"`
	Purpose        string            `json:"purpose"`
	CurrentWork    *ProjectWork      `json:"current_work,omitempty"`
	OpenTasks      int               `json:"open_tasks"`
	ActiveSessions int               `json:"active_sessions"`
	LastActivity   *time.Time        `json:"last_activity,omitempty"`
	Destination    BrowseDestination `json:"destination"`
}

type ProjectsBrowseResult struct {
	Total      int                 `json:"total"`
	Limit      int                 `json:"limit"`
	Items      []ProjectBrowseItem `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

func (s *Service) BrowseProjects(ctx context.Context, request ProjectsBrowseRequest) (ProjectsBrowseResult, error) {
	if s == nil {
		return ProjectsBrowseResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(BrowseRepository)
	if !ok {
		return ProjectsBrowseResult{}, domain.ErrUnavailable
	}
	if s.presence == nil || len(request.Search) > 200 || strings.TrimSpace(request.Search) != request.Search {
		return ProjectsBrowseResult{}, domain.ErrInvalid
	}
	limit, ok := browseLimit(request.Limit)
	if !ok {
		return ProjectsBrowseResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("projects", request.Cursor)
	if err != nil || after != nil && after.Text == "" {
		return ProjectsBrowseResult{}, domain.ErrInvalid
	}
	live := s.presence.List("")
	if len(live) > maxPeopleScan {
		return ProjectsBrowseResult{}, domain.ErrUnavailable
	}
	ids := make([]string, len(live))
	for i := range live {
		ids[i] = live[i].Session
	}
	snapshot, err := repository.ProjectBrowseSnapshot(ctx, domain.ProjectBrowseQuery{Search: request.Search, After: after, Limit: limit, SessionIDs: ids})
	if err != nil {
		return ProjectsBrowseResult{}, err
	}
	hasMore := len(snapshot.Items) > limit
	if hasMore {
		snapshot.Items = snapshot.Items[:limit]
	}
	active := make(map[string]int)
	for _, item := range live {
		project := item.Project
		if project == "" {
			project = snapshot.Sessions[item.Session].ProjectID
		}
		if item.HostConnected && project != "" {
			active[project]++
		}
	}
	out := ProjectsBrowseResult{Total: snapshot.Total, Limit: limit, Items: make([]ProjectBrowseItem, 0, len(snapshot.Items))}
	for _, item := range snapshot.Items {
		view := ProjectBrowseItem{ID: item.ID, Name: item.Name, Status: item.Status,
			Purpose: item.Purpose, OpenTasks: item.OpenTasks, ActiveSessions: active[item.ID],
			LastActivity: item.LatestActivity, Destination: BrowseDestination{Kind: "project", Ref: item.ID}}
		if item.CurrentWork != nil {
			view.CurrentWork = &ProjectWork{ID: item.CurrentWork.ID, Title: item.CurrentWork.Title,
				State: item.CurrentWork.State, Priority: item.CurrentWork.Priority}
		}
		out.Items = append(out.Items, view)
	}
	if hasMore && len(snapshot.Items) > 0 {
		last := snapshot.Items[len(snapshot.Items)-1]
		out.NextCursor = encodeCursor("projects", domain.BrowseCursor{Text: last.Name, ID: last.ID})
	}
	return out, nil
}

type PeopleBrowseRequest struct {
	Cursor, Search, Project, Execution, Host string
	HostConnected                            *bool
	Limit                                    int
}

type PeopleBrowseItem struct {
	Session        string     `json:"session"`
	Actor          string     `json:"actor,omitempty"`
	Purpose        string     `json:"purpose,omitempty"`
	Project        string     `json:"project,omitempty"`
	ProjectName    string     `json:"project_name,omitempty"`
	Execution      string     `json:"execution"`
	Host           string     `json:"host"`
	HostConnected  bool       `json:"host_connected"`
	LeaseExpires   *time.Time `json:"lease_expires,omitempty"`
	LastActivity   time.Time  `json:"last_activity"`
	RecencySeconds int64      `json:"recency_seconds"`
	Loaded         *string    `json:"loaded,omitempty"`
}

type PeopleBrowseResult struct {
	Total      int                `json:"total"`
	Limit      int                `json:"limit"`
	Items      []PeopleBrowseItem `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
	Facets     PeopleFacets       `json:"facets"`
}

type PeopleFacets struct {
	Projects     []domain.FacetCount `json:"projects"`
	Execution    []domain.FacetCount `json:"execution"`
	Hosts        []domain.FacetCount `json:"hosts"`
	Connectivity []domain.FacetCount `json:"connectivity"`
}

func facetCounts(values map[string]domain.FacetCount) []domain.FacetCount {
	out := make([]domain.FacetCount, 0, len(values))
	for _, item := range values {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Label == out[j].Label {
			return out[i].Value < out[j].Value
		}
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}

func peopleFacets(items []PeopleBrowseItem) PeopleFacets {
	projects, execution := map[string]domain.FacetCount{}, map[string]domain.FacetCount{}
	hosts, connectivity := map[string]domain.FacetCount{}, map[string]domain.FacetCount{}
	add := func(dst map[string]domain.FacetCount, value, label string) {
		if value == "" {
			return
		}
		if label == "" {
			label = value
		}
		item := dst[value]
		item.Value, item.Label, item.Count = value, label, item.Count+1
		dst[value] = item
	}
	for _, item := range items {
		add(projects, item.Project, item.ProjectName)
		add(execution, item.Execution, item.Execution)
		add(hosts, item.Host, item.Host)
		state := "disconnected"
		if item.HostConnected {
			state = "connected"
		}
		add(connectivity, state, state)
	}
	return PeopleFacets{Projects: facetCounts(projects), Execution: facetCounts(execution),
		Hosts: facetCounts(hosts), Connectivity: facetCounts(connectivity)}
}

func (s *Service) BrowsePeople(ctx context.Context, request PeopleBrowseRequest) (PeopleBrowseResult, error) {
	if s == nil {
		return PeopleBrowseResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(BrowseRepository)
	if !ok {
		return PeopleBrowseResult{}, domain.ErrUnavailable
	}
	if s.presence == nil || len(request.Search) > 200 || len(request.Project) > 200 || len(request.Host) > 200 ||
		strings.TrimSpace(request.Search) != request.Search ||
		request.Execution != "" && request.Execution != "executing" && request.Execution != "not_running" {
		return PeopleBrowseResult{}, domain.ErrInvalid
	}
	limit, ok := browseLimit(request.Limit)
	if !ok {
		return PeopleBrowseResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("people", request.Cursor)
	if err != nil || after != nil && after.Time.IsZero() {
		return PeopleBrowseResult{}, domain.ErrInvalid
	}
	live := s.presence.List("")
	if len(live) > maxPeopleScan {
		return PeopleBrowseResult{}, domain.ErrUnavailable
	}
	ids := make([]string, len(live))
	for i := range live {
		ids[i] = live[i].Session
	}
	facts, err := repository.PeopleFactsSnapshot(ctx, domain.PeopleFactsQuery{SessionIDs: ids})
	if err != nil {
		return PeopleBrowseResult{}, err
	}
	now := s.clock.Now().UTC()
	all := make([]PeopleBrowseItem, 0, len(live))
	search := strings.ToLower(request.Search)
	for _, item := range live {
		fact := facts.Sessions[item.Session]
		project := item.Project
		if project == "" {
			project = fact.ProjectID
		}
		host := item.Host
		if host == "" {
			host = fact.Host
		}
		recency := now.Sub(item.LastActivity).Seconds()
		if recency < 0 {
			recency = 0
		}
		all = append(all, PeopleBrowseItem{Session: item.Session, Actor: item.Actor,
			Purpose: fact.Purpose, Project: project, ProjectName: fact.ProjectName,
			Execution: item.Execution, Host: host, HostConnected: item.HostConnected,
			LeaseExpires: item.LeaseExpires, LastActivity: item.LastActivity,
			RecencySeconds: int64(recency), Loaded: item.LoadedFact})
	}
	facets := peopleFacets(all)
	items := make([]PeopleBrowseItem, 0, len(all))
	for _, item := range all {
		if request.Project != "" && item.Project != request.Project || request.Execution != "" && item.Execution != request.Execution ||
			request.Host != "" && item.Host != request.Host || request.HostConnected != nil && item.HostConnected != *request.HostConnected {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(strings.Join([]string{item.Session, item.Actor, item.Purpose, item.Project, item.ProjectName, item.Host}, "\n")), search) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastActivity.Equal(items[j].LastActivity) {
			return items[i].Session < items[j].Session
		}
		return items[i].LastActivity.After(items[j].LastActivity)
	})
	total := len(items)
	if after != nil {
		remaining := items[:0]
		for _, item := range items {
			if item.LastActivity.Before(after.Time) || item.LastActivity.Equal(after.Time) && item.Session > after.ID {
				remaining = append(remaining, item)
			}
		}
		items = remaining
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	out := PeopleBrowseResult{Total: total, Limit: limit, Items: items, Facets: facets}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		out.NextCursor = encodeCursor("people", domain.BrowseCursor{Time: last.LastActivity, ID: last.Session})
	}
	return out, nil
}
