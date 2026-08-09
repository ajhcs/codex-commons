package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func TestSlice7BrowseSnapshotsEmpty(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	attention, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 10})
	must(t, err)
	projects, err := s.ProjectBrowseSnapshot(ctx, domain.ProjectBrowseQuery{Limit: 10})
	must(t, err)
	people, err := s.PeopleFactsSnapshot(ctx, domain.PeopleFactsQuery{})
	must(t, err)
	if attention.Total != 0 || len(attention.Items) != 0 || attention.Facets.Sources == nil ||
		projects.Total != 0 || len(projects.Items) != 0 || len(people.Sessions) != 0 {
		t.Fatalf("attention=%+v projects=%+v people=%+v", attention, projects, people)
	}
}

func seedBrowseStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	for _, project := range []domain.Project{
		{ID: "alpha", Name: "Alpha", Purpose: "Build the agent commons"},
		{ID: "beta", Name: "Beta", Purpose: "Billing experiments"},
		{ID: "zeta", Name: "Zeta", Purpose: "Quiet project"},
	} {
		must(t, s.CreateProject(ctx, project))
	}
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-1", Host: "plumbob", ProjectID: "alpha", Purpose: "Implement browse"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-2", Host: "studio", ProjectID: "beta", Purpose: "Review filters"}))
	must(t, s.CreateTask(ctx, domain.Task{ID: "T-1", ProjectID: "alpha", State: "in_progress", Title: "Build browse", Priority: 9}))
	must(t, s.CreateTask(ctx, domain.Task{ID: "T-2", ProjectID: "alpha", State: "ready", Title: "Write docs", Priority: 3}))

	events := []domain.AttentionEvent{
		{EventID: "AE-1", AttentionID: "A-1", State: domain.AttentionOpen, Severity: "high", Title: "Task needs evidence", ProjectID: "alpha", SourceRef: "T-1", AccountableSessionID: "S-1", NextAction: "Attach evidence", SourceKind: "task"},
		{EventID: "AE-2", AttentionID: "A-2", State: domain.AttentionOpen, Severity: "low", Title: "Remote issue changed", ProjectID: "beta", SourceRef: "issue/2", AccountableSessionID: "S-2", NextAction: "Inspect issue", SourceKind: "github_issue"},
		{EventID: "AE-3", AttentionID: "A-3", State: domain.AttentionOpen, Severity: "medium", Title: "Missing task reference", ProjectID: "alpha", SourceRef: "T-missing", NextAction: "Verify source", SourceKind: "task"},
	}
	for i, event := range events {
		now = testNow.Add(time.Duration(i) * time.Minute)
		must(t, s.RecordAttention(ctx, event))
	}
	now = testNow.Add(3 * time.Minute)
	must(t, s.RecordActivity(ctx, domain.ActivityEvent{ID: "EV-alpha", Kind: "task_status_changed", ProjectID: "alpha", ActorID: "agent-1", ObjectRef: "T-1", ObjectTitle: "Build browse", Outcome: "in_progress", OccurredAt: now}))
	return s, &now
}

func TestAttentionBrowseFiltersFacetsCursorDestinationsAndTrust(t *testing.T) {
	s, _ := seedBrowseStore(t)
	ctx := context.Background()
	first, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 3})
	must(t, err)
	if first.Total != 3 || len(first.Items) != 3 || first.Items[0].ID != "A-3" || first.Items[1].ID != "A-2" {
		t.Fatalf("attention ordering=%+v", first)
	}
	if first.Items[0].Destination != nil {
		t.Fatalf("invented missing-task destination=%+v", first.Items[0].Destination)
	}
	if !first.Items[1].Untrusted || first.Items[1].Destination != nil {
		t.Fatalf("remote provenance/destination=%+v", first.Items[1])
	}
	if first.Items[2].Destination == nil || first.Items[2].Destination.Kind != "task" || first.Items[2].Destination.Ref != "T-1" {
		t.Fatalf("backed task destination=%+v", first.Items[2])
	}
	if len(first.Facets.Sources) != 2 || len(first.Facets.Severities) != 3 || len(first.Facets.Projects) != 2 || len(first.Facets.Owners) != 2 {
		t.Fatalf("facets=%+v", first.Facets)
	}

	from := testNow.Add(30 * time.Second)
	to := testNow.Add(2 * time.Minute)
	filtered, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 10,
		Filters: domain.AttentionFilters{ProjectID: "alpha", Severity: "medium", UpdatedFrom: &from, UpdatedTo: &to}})
	must(t, err)
	if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != "A-3" {
		t.Fatalf("filtered=%+v", filtered)
	}
	after := &domain.BrowseCursor{Time: first.Items[1].UpdatedAt, ID: first.Items[1].ID}
	next, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 2, After: after})
	must(t, err)
	if next.Total != 3 || len(next.Items) != 1 || next.Items[0].ID != "A-1" {
		t.Fatalf("keyset next=%+v", next)
	}
	if _, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 10, Filters: domain.AttentionFilters{Severity: "urgent"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid severity=%v", err)
	}
}

func TestAttentionBrowseMetadataSearchIsCaseInsensitiveLiteralAndComposable(t *testing.T) {
	s, _ := seedBrowseStore(t)
	ctx := context.Background()
	tests := []struct {
		query, severity, want string
	}{
		{query: "TASK NEEDS", want: "A-1"},
		{query: "issue/2", want: "A-2"},
		{query: "beta", want: "A-2"},
		{query: "a-3", want: "A-3"},
		{query: "task", severity: "high", want: "A-1"},
	}
	for _, tc := range tests {
		got, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 10,
			Filters: domain.AttentionFilters{Search: tc.query, Severity: tc.severity}})
		must(t, err)
		if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != tc.want {
			t.Fatalf("query=%q severity=%q got=%+v", tc.query, tc.severity, got)
		}
	}
	literal, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 10, Filters: domain.AttentionFilters{Search: "%"}})
	must(t, err)
	if literal.Total != 0 || len(literal.Items) != 0 {
		t.Fatalf("SQL wildcard was not literal: %+v", literal)
	}
	for _, query := range []string{" padded ", strings.Repeat("x", maxBrowseSearch+1)} {
		if _, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 10, Filters: domain.AttentionFilters{Search: query}}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid query length/trim accepted: len=%d err=%v", len(query), err)
		}
	}
}

func TestAttentionBrowseUsesChronologicalFractionalTimestampOrderFiltersAndCursor(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	must(t, s.RecordAttention(ctx, domain.AttentionEvent{EventID: "AE-exact", AttentionID: "A-exact", State: domain.AttentionOpen,
		Severity: "medium", Title: "Exact second", SourceRef: "T-exact", NextAction: "Inspect", SourceKind: "task"}))
	now = testNow.Add(500 * time.Millisecond)
	must(t, s.RecordAttention(ctx, domain.AttentionEvent{EventID: "AE-fraction", AttentionID: "A-fraction", State: domain.AttentionOpen,
		Severity: "medium", Title: "Fractional second", SourceRef: "T-fraction", NextAction: "Inspect", SourceKind: "task"}))
	got, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 1})
	must(t, err)
	if len(got.Items) != 2 || got.Items[0].ID != "A-fraction" {
		t.Fatalf("chronological order=%+v", got.Items)
	}
	after := &domain.BrowseCursor{Time: got.Items[0].UpdatedAt, ID: got.Items[0].ID}
	next, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 1, After: after})
	must(t, err)
	if len(next.Items) != 1 || next.Items[0].ID != "A-exact" {
		t.Fatalf("chronological cursor=%+v", next.Items)
	}
	from := testNow.Add(250 * time.Millisecond)
	filtered, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 10, Filters: domain.AttentionFilters{UpdatedFrom: &from}})
	must(t, err)
	if filtered.Total != 1 || filtered.Items[0].ID != "A-fraction" {
		t.Fatalf("chronological date filter=%+v", filtered)
	}
}

func TestAttentionHighCardinalityFacetsAreBoundedAndTruthful(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	for i := 0; i < maxAttentionFacets+1; i++ {
		projectID := fmt.Sprintf("project-%02d", i)
		must(t, s.CreateProject(ctx, domain.Project{ID: projectID, Name: fmt.Sprintf("Project %02d", i), Purpose: "facet bound"}))
		now = testNow.Add(time.Duration(i) * time.Second)
		must(t, s.RecordAttention(ctx, domain.AttentionEvent{EventID: fmt.Sprintf("AE-%02d", i), AttentionID: fmt.Sprintf("A-%02d", i),
			State: domain.AttentionOpen, Severity: "low", Title: "Bounded facet", ProjectID: projectID,
			SourceRef: fmt.Sprintf("T-%02d", i), AccountableSessionID: fmt.Sprintf("S-%02d", i), NextAction: "Inspect", SourceKind: "task"}))
	}
	got, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 1})
	must(t, err)
	if len(got.Facets.Owners) != maxAttentionFacets || !got.Facets.OwnersTruncated ||
		len(got.Facets.Projects) != maxAttentionFacets || !got.Facets.ProjectsTruncated {
		t.Fatalf("unbounded or untruthful facets=%+v", got.Facets)
	}
}

func TestAttentionBrowseUsesLatestExplicitState(t *testing.T) {
	s, now := seedBrowseStore(t)
	*now = (*now).Add(time.Minute)
	must(t, s.RecordAttention(context.Background(), domain.AttentionEvent{EventID: "AE-resolve", AttentionID: "A-3", State: domain.AttentionResolved,
		Severity: "medium", Title: "Missing task reference", ProjectID: "alpha", SourceRef: "T-missing", NextAction: "No action", SourceKind: "task"}))
	got, err := s.AttentionBrowseSnapshot(context.Background(), domain.AttentionBrowseQuery{Limit: 10})
	must(t, err)
	if got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("latest state=%+v", got)
	}
}

func TestProjectBrowseCanonicalWorkActivitySearchAndCursor(t *testing.T) {
	s, _ := seedBrowseStore(t)
	ctx := context.Background()
	must(t, s.CreateTask(ctx, domain.Task{ID: "T-0", ProjectID: "alpha", State: "in_progress", Title: "Canonical first", Priority: 1}))
	fractional := testNow.Add(3*time.Minute + 500*time.Millisecond)
	must(t, s.RecordActivity(ctx, domain.ActivityEvent{ID: "EV-alpha-fraction", Kind: "task_status_changed", ProjectID: "alpha",
		ActorID: "agent-1", ObjectRef: "T-0", ObjectTitle: "Canonical first", Outcome: "in_progress", OccurredAt: fractional}))
	first, err := s.ProjectBrowseSnapshot(ctx, domain.ProjectBrowseQuery{Limit: 1, SessionIDs: []string{"S-1"}})
	must(t, err)
	if first.Total != 3 || len(first.Items) != 2 || first.Items[0].ID != "alpha" {
		t.Fatalf("first keyset window=%+v", first)
	}
	alpha := first.Items[0]
	if alpha.CurrentWork == nil || alpha.CurrentWork.ID != "T-0" || alpha.CurrentWork.Priority != 1 || alpha.OpenTasks != 3 ||
		alpha.LatestActivity == nil || !alpha.LatestActivity.Equal(fractional) {
		t.Fatalf("canonical derived project=%+v", alpha)
	}
	if first.Sessions["S-1"].ProjectID != "alpha" {
		t.Fatalf("same-snapshot session attribution=%+v", first.Sessions)
	}
	after := &domain.BrowseCursor{Text: "Alpha", ID: "alpha"}
	next, err := s.ProjectBrowseSnapshot(ctx, domain.ProjectBrowseQuery{Limit: 1, After: after})
	must(t, err)
	if len(next.Items) != 2 || next.Items[0].ID != "beta" {
		t.Fatalf("project cursor=%+v", next)
	}
	search, err := s.ProjectBrowseSnapshot(ctx, domain.ProjectBrowseQuery{Search: "agent commons", Limit: 5})
	must(t, err)
	if search.Total != 1 || len(search.Items) != 1 || search.Items[0].ID != "alpha" {
		t.Fatalf("project search=%+v", search)
	}
}

func TestPeopleFactsSnapshotEmptyPopulatedAndIdentityExact(t *testing.T) {
	s, _ := seedBrowseStore(t)
	ctx := context.Background()
	empty, err := s.PeopleFactsSnapshot(ctx, domain.PeopleFactsQuery{})
	must(t, err)
	if len(empty.Sessions) != 0 {
		t.Fatalf("empty=%+v", empty)
	}
	got, err := s.PeopleFactsSnapshot(ctx, domain.PeopleFactsQuery{SessionIDs: []string{"S-2", "S-missing", "S-1"}})
	must(t, err)
	if len(got.Sessions) != 2 || got.Sessions["S-1"].Purpose != "Implement browse" || got.Sessions["S-2"].ProjectName != "Beta" {
		t.Fatalf("facts=%+v", got)
	}
	if _, exists := got.Sessions["S-missing"]; exists {
		t.Fatal("invented session facts")
	}
	if _, err := s.PeopleFactsSnapshot(ctx, domain.PeopleFactsQuery{SessionIDs: []string{"S-1", "S-1"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("duplicate identity=%v", err)
	}
}

func TestAttentionBrowseCountRowsAndFacetsShareSnapshot(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	const writes = 20
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		for i := 0; i < writes; i++ {
			now = testNow.Add(time.Duration(i) * time.Second)
			err := s.RecordAttention(ctx, domain.AttentionEvent{EventID: fmt.Sprintf("AE-%02d", i), AttentionID: fmt.Sprintf("A-%02d", i),
				State: domain.AttentionOpen, Severity: "medium", Title: "Concurrent item", SourceRef: fmt.Sprintf("T-%02d", i),
				NextAction: "Inspect explicit source", SourceKind: "task"})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	close(start)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 100})
			if err != nil {
				t.Errorf("snapshot: %v", err)
				return
			}
			facetTotal := 0
			for _, facet := range got.Facets.Severities {
				facetTotal += facet.Count
			}
			if got.Total != len(got.Items) || facetTotal != got.Total {
				t.Errorf("mixed snapshot total=%d rows=%d facets=%d", got.Total, len(got.Items), facetTotal)
			}
		}()
	}
	wg.Wait()
	must(t, <-done)
}

func BenchmarkAttentionBrowseSnapshot(b *testing.B) {
	ctx := context.Background()
	now := testNow
	s, err := Open(ctx, filepath.Join(b.TempDir(), "browse-bench.sqlite"), WithClock(func() time.Time { return now }))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "benchmark browse"}); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		now = testNow.Add(time.Duration(i) * time.Second)
		if err := s.RecordAttention(ctx, domain.AttentionEvent{EventID: fmt.Sprintf("AE-%03d", i), AttentionID: fmt.Sprintf("A-%03d", i),
			State: domain.AttentionOpen, Severity: []string{"high", "medium", "low"}[i%3], Title: "Representative bounded attention",
			ProjectID: "alpha", SourceRef: fmt.Sprintf("T-%03d", i), AccountableSessionID: "S-bench",
			NextAction: "Inspect canonical source", SourceKind: "task"}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.AttentionBrowseSnapshot(ctx, domain.AttentionBrowseQuery{Limit: 25}); err != nil {
			b.Fatal(err)
		}
	}
}
