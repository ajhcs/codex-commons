package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/internal/presence"
)

func TestBrowseCapabilityAbsenceIsUnavailable(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	service := New(&fakeHomeRepository{}, presence.New(clock), clock)
	if _, err := service.BrowseAttention(context.Background(), AttentionBrowseRequest{}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("attention capability error=%v", err)
	}
	if _, err := service.BrowseProjects(context.Background(), ProjectsBrowseRequest{}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("projects capability error=%v", err)
	}
	if _, err := service.BrowsePeople(context.Background(), PeopleBrowseRequest{}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("people capability error=%v", err)
	}
}

type fakeBrowseRepository struct {
	attention domain.AttentionBrowseSnapshot
	projects  domain.ProjectBrowseSnapshot
	people    domain.PeopleFactsSnapshot
	lastA     domain.AttentionBrowseQuery
	lastP     domain.ProjectBrowseQuery
	lastWho   domain.PeopleFactsQuery
}

func (r *fakeBrowseRepository) HomeSnapshot(context.Context, domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	return domain.HomeDurableSnapshot{}, nil
}
func (r *fakeBrowseRepository) AttentionBrowseSnapshot(_ context.Context, query domain.AttentionBrowseQuery) (domain.AttentionBrowseSnapshot, error) {
	r.lastA = query
	return r.attention, nil
}
func (r *fakeBrowseRepository) ProjectBrowseSnapshot(_ context.Context, query domain.ProjectBrowseQuery) (domain.ProjectBrowseSnapshot, error) {
	r.lastP = query
	return r.projects, nil
}
func (r *fakeBrowseRepository) PeopleFactsSnapshot(_ context.Context, query domain.PeopleFactsQuery) (domain.PeopleFactsSnapshot, error) {
	r.lastWho = query
	return r.people, nil
}

func TestBrowseAttentionMapsFiltersOpaqueCursorAndBackedDestination(t *testing.T) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	repo := &fakeBrowseRepository{attention: domain.AttentionBrowseSnapshot{Total: 3,
		Items: []domain.AttentionBrowseItem{
			{HomeAttention: domain.HomeAttention{ID: "A-3", Severity: "high", Title: "Third", SourceRef: "T-3", NextAction: "Inspect", SourceKind: "task", UpdatedAt: base.Add(3 * time.Minute), Destination: &domain.BrowseDestination{Kind: "task", Ref: "T-3"}}},
			{HomeAttention: domain.HomeAttention{ID: "A-2", Severity: "medium", Title: "Second", SourceRef: "issue/2", NextAction: "Open", SourceKind: "github_issue", UpdatedAt: base.Add(2 * time.Minute), Untrusted: true}},
			{HomeAttention: domain.HomeAttention{ID: "A-1", Severity: "low", Title: "First", SourceRef: "T-1", NextAction: "Review", SourceKind: "task", UpdatedAt: base}},
		}, Facets: domain.AttentionFacets{Severities: []domain.FacetCount{{Value: "high", Count: 1}}}}}
	service := New(repo, presence.New(&testClock{now: base}), &testClock{now: base})
	from := base.Add(-time.Hour)
	got, err := service.BrowseAttention(context.Background(), AttentionBrowseRequest{Limit: 2, Search: "evidence", Source: "task", Owner: "S-1", Severity: "high", Project: "alpha", UpdatedFrom: &from})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || len(got.Items) != 2 || got.NextCursor == "" || got.Items[0].Destination == nil || !got.Items[1].Untrusted {
		t.Fatalf("attention=%+v", got)
	}
	if repo.lastA.Filters.Search != "evidence" || repo.lastA.Filters.SourceKind != "task" || repo.lastA.Filters.OwnerSessionID != "S-1" || repo.lastA.Limit != 2 {
		t.Fatalf("query=%+v", repo.lastA)
	}
	cursor, err := decodeCursor("attention", got.NextCursor)
	if err != nil || cursor.ID != "A-2" || !cursor.Time.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	if _, err := service.BrowseAttention(context.Background(), AttentionBrowseRequest{Cursor: got.NextCursor, Limit: 2}); err != nil || repo.lastA.After.ID != "A-2" {
		t.Fatalf("cursor round trip query=%+v err=%v", repo.lastA, err)
	}
	if _, err := service.BrowseAttention(context.Background(), AttentionBrowseRequest{Cursor: encodeCursor("people", *cursor)}); err == nil {
		t.Fatal("cross-resource cursor accepted")
	}
	if _, err := service.BrowseAttention(context.Background(), AttentionBrowseRequest{Search: strings.Repeat("x", 201)}); err == nil {
		t.Fatal("unbounded attention search accepted")
	}
}

func TestBrowseProjectsCombinesCanonicalRowsWithConnectedLiveCount(t *testing.T) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: base}
	live := presence.New(clock)
	live.Connect(presence.Session{ID: "S-1", Host: "plumbob", Project: "alpha"})
	live.Connect(presence.Session{ID: "S-2", Host: "studio", Project: "alpha"})
	live.Disconnect("S-2")
	live.Connect(presence.Session{ID: "S-3", Host: "plumbob"})
	repo := &fakeBrowseRepository{projects: domain.ProjectBrowseSnapshot{Total: 2, Items: []domain.ProjectBrowseItem{
		{ID: "alpha", Name: "Alpha", Purpose: "Build commons", OpenTasks: 2, CurrentWork: &domain.ProjectCurrentWork{ID: "T-1", Title: "Browse", State: "in_progress", Priority: 8}},
		{ID: "beta", Name: "Beta", Purpose: "Other"},
	}, Sessions: map[string]domain.PeopleSessionFact{"S-3": {ID: "S-3", ProjectID: "alpha", ProjectName: "Alpha"}}}}
	service := New(repo, live, clock)
	got, err := service.BrowseProjects(context.Background(), ProjectsBrowseRequest{Search: "commons", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || len(got.Items) != 1 || got.Items[0].ActiveSessions != 2 || got.Items[0].CurrentWork.ID != "T-1" || got.Items[0].Destination.Kind != "project" || got.NextCursor == "" {
		t.Fatalf("projects=%+v", got)
	}
	if repo.lastP.Search != "commons" || repo.lastP.Limit != 1 {
		t.Fatalf("query=%+v", repo.lastP)
	}
}

func TestBrowsePeopleFiltersPaginatesAndBuildsFacetsFromCompleteCapture(t *testing.T) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: base}
	live := presence.New(clock)
	live.Connect(presence.Session{ID: "S-a", Actor: "agent-a", Host: "plumbob"})
	clock.now = base.Add(time.Minute)
	live.Connect(presence.Session{ID: "S-b", Actor: "agent-b", Host: "plumbob", Project: "alpha"})
	live.LeaseExecution("S-b", time.Hour)
	clock.now = base.Add(2 * time.Minute)
	live.Connect(presence.Session{ID: "S-c", Actor: "agent-c", Host: "studio", Project: "beta"})
	live.Disconnect("S-c")
	clock.now = base.Add(3 * time.Minute)
	loaded := "task T-2"
	live.SetLoaded("S-b", &loaded)
	repo := &fakeBrowseRepository{people: domain.PeopleFactsSnapshot{Sessions: map[string]domain.PeopleSessionFact{
		"S-a": {ID: "S-a", Host: "plumbob", ProjectID: "alpha", ProjectName: "Alpha", Purpose: "Write docs"},
		"S-b": {ID: "S-b", Host: "plumbob", ProjectID: "alpha", ProjectName: "Alpha", Purpose: "Build filters"},
		"S-c": {ID: "S-c", Host: "studio", ProjectID: "beta", ProjectName: "Beta", Purpose: "Review"},
	}}}
	service := New(repo, live, clock)
	first, err := service.BrowsePeople(context.Background(), PeopleBrowseRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || len(first.Items) != 2 || first.Items[0].Session != "S-b" || first.NextCursor == "" ||
		len(first.Facets.Projects) != 2 || len(first.Facets.Hosts) != 2 || len(first.Facets.Connectivity) != 2 {
		t.Fatalf("people first=%+v", first)
	}
	second, err := service.BrowsePeople(context.Background(), PeopleBrowseRequest{Limit: 2, Cursor: first.NextCursor})
	if err != nil || second.Total != 3 || len(second.Items) != 1 || second.Items[0].Session != "S-a" {
		t.Fatalf("people second=%+v err=%v", second, err)
	}
	disconnected := false
	filtered, err := service.BrowsePeople(context.Background(), PeopleBrowseRequest{Search: "review", HostConnected: &disconnected, Limit: 10})
	if err != nil || filtered.Total != 1 || filtered.Items[0].Session != "S-c" || len(filtered.Facets.Projects) != 2 {
		t.Fatalf("people filtered=%+v err=%v", filtered, err)
	}
	if len(repo.lastWho.SessionIDs) != 3 {
		t.Fatalf("identity join=%+v", repo.lastWho)
	}
}

func TestBrowseResponsesRemainBounded(t *testing.T) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	repo := &fakeBrowseRepository{attention: domain.AttentionBrowseSnapshot{Total: 100}}
	for i := 0; i < 101; i++ {
		repo.attention.Items = append(repo.attention.Items, domain.AttentionBrowseItem{HomeAttention: domain.HomeAttention{
			ID: "A", Severity: "medium", Title: "bounded title", SourceRef: "T", NextAction: "bounded action", SourceKind: "task", UpdatedAt: base,
		}})
	}
	service := New(repo, presence.New(&testClock{now: base}), &testClock{now: base})
	got, err := service.BrowseAttention(context.Background(), AttentionBrowseRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(got)
	if len(got.Items) != 100 || len(payload) > 64<<10 {
		t.Fatalf("items=%d bytes=%d", len(got.Items), len(payload))
	}
}
