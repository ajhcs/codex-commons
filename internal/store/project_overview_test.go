package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func overviewStoreQuery(project string, start time.Time, attentionLimit, workLimit int) domain.ProjectOverviewReadQuery {
	return domain.ProjectOverviewReadQuery{
		ProjectID: project, ActivityStart: start, ActivityEnd: start.Add(14 * 24 * time.Hour),
		AttentionLimit: attentionLimit, WorkLimit: workLimit,
	}
}

func TestProjectOverviewSnapshotEmptyAndMissing(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	if _, err := s.ProjectOverviewSnapshot(ctx, overviewStoreQuery("missing", start, 5, 5)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing project=%v", err)
	}
	must(t, s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "empty overview"}))
	got, err := s.ProjectOverviewSnapshot(ctx, overviewStoreQuery("alpha", start, 5, 5))
	must(t, err)
	if got.Project.ID != "alpha" || got.AttentionTotal != 0 || got.AttentionHigh != 0 || got.OpenWorkTotal != 0 ||
		len(got.Attention) != 0 || len(got.CurrentWork) != 0 || len(got.Activity) != 0 ||
		got.MergedPullRequests != nil || got.LastActionChangingActivity != nil {
		t.Fatalf("empty overview=%+v", got)
	}
}

func TestProjectOverviewSnapshotPopulatedAndUTCBoundaries(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	end := start.Add(14 * 24 * time.Hour)
	must(t, s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "overview", Status: "active", Milestone: "pilot", Now: "slice 8"}))
	must(t, s.CreateProject(ctx, domain.Project{ID: "other", Name: "Other", Purpose: "scope check"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-1", Host: "plumbob", ProjectID: "alpha", Purpose: "work"}))

	for i, severity := range []string{"low", "high", "high", "medium"} {
		now = testNow.Add(time.Duration(i) * time.Minute)
		sourceRef := fmt.Sprintf("T-%d", i)
		if i == 3 {
			sourceRef = "T-progress"
		}
		must(t, s.RecordAttention(ctx, domain.AttentionEvent{
			EventID: fmt.Sprintf("AE-%d", i), AttentionID: fmt.Sprintf("A-%d", i),
			State: domain.AttentionOpen, Severity: severity, Title: fmt.Sprintf("Attention %d", i),
			ProjectID: "alpha", SourceRef: sourceRef, NextAction: "Inspect",
			SourceKind: "task",
		}))
	}
	now = testNow.Add(5 * time.Minute)
	must(t, s.RecordAttention(ctx, domain.AttentionEvent{
		EventID: "AE-resolve", AttentionID: "A-0", State: domain.AttentionResolved,
		Severity: "low", Title: "Attention 0", ProjectID: "alpha", SourceRef: "T-0",
		NextAction: "None", SourceKind: "task",
	}))
	must(t, s.RecordAttention(ctx, domain.AttentionEvent{
		EventID: "AE-other", AttentionID: "A-other", State: domain.AttentionOpen,
		Severity: "high", Title: "Other", ProjectID: "other", SourceRef: "other",
		NextAction: "Inspect", SourceKind: "task",
	}))

	for _, task := range []domain.Task{
		{ID: "T-progress", ProjectID: "alpha", State: "in_progress", Title: "Progress", Priority: 2, OwnerSessionID: "S-1"},
		{ID: "T-blocked", ProjectID: "alpha", State: "blocked", Title: "Blocked", Priority: 1},
		{ID: "T-ready", ProjectID: "alpha", State: "ready", Title: "Ready", Priority: 0},
		{ID: "T-done", ProjectID: "alpha", State: "done", Title: "Done", Priority: 0},
		{ID: "T-other", ProjectID: "other", State: "ready", Title: "Other", Priority: 0},
	} {
		must(t, s.CreateTask(ctx, task))
	}

	events := []domain.ActivityEvent{
		{ID: "before", Kind: "task_status_changed", ProjectID: "alpha", ActorID: "agent", ObjectRef: "T-ready", ObjectTitle: "Before", OccurredAt: start.Add(-time.Nanosecond)},
		{ID: "start", Kind: "task_status_changed", ProjectID: "alpha", ActorID: "agent", ObjectRef: "T-ready", ObjectTitle: "Start", OccurredAt: start},
		{ID: "last", Kind: "task_claimed", ProjectID: "alpha", ActorID: "agent", ObjectRef: "T-progress", ObjectTitle: "Last", OccurredAt: end.Add(-time.Nanosecond)},
		{ID: "end", Kind: "task_status_changed", ProjectID: "alpha", ActorID: "agent", ObjectRef: "T-ready", ObjectTitle: "End", OccurredAt: end},
		{ID: "other-event", Kind: "task_status_changed", ProjectID: "other", ActorID: "agent", ObjectRef: "T-other", ObjectTitle: "Other", OccurredAt: start},
	}
	for _, event := range events {
		must(t, s.RecordActivity(ctx, event))
	}

	query := overviewStoreQuery("alpha", start, 2, 3)
	query.SessionIDs = []string{"S-1"}
	got, err := s.ProjectOverviewSnapshot(ctx, query)
	must(t, err)
	if got.AttentionTotal != 3 || got.AttentionHigh != 2 || len(got.Attention) != 2 || got.Attention[0].ID != "A-3" {
		t.Fatalf("attention=%+v total=%d high=%d", got.Attention, got.AttentionTotal, got.AttentionHigh)
	}
	if got.Attention[0].Destination == nil || got.Attention[0].Destination.Kind != "task" || got.Attention[0].Destination.Ref != "T-progress" {
		t.Fatalf("validated overview destination=%+v", got.Attention[0].Destination)
	}
	if got.OpenWorkTotal != 3 || len(got.CurrentWork) != 3 || got.CurrentWork[0].ID != "T-progress" || got.CurrentWork[1].ID != "T-blocked" || got.CurrentWork[2].ID != "T-ready" {
		t.Fatalf("work=%+v total=%d", got.CurrentWork, got.OpenWorkTotal)
	}
	if got.CurrentWork[0].UpdatedAt == nil || !got.CurrentWork[0].UpdatedAt.Equal(end.Add(-time.Nanosecond)) {
		t.Fatalf("canonical work update=%+v", got.CurrentWork[0])
	}
	if len(got.Activity) != 2 || got.Activity[0].Day != start || got.Activity[0].Count != 1 || got.Activity[1].Day != end.Add(-24*time.Hour) || got.Activity[1].Count != 1 {
		t.Fatalf("UTC boundary activity=%+v", got.Activity)
	}
	if got.LastActionChangingActivity == nil || !got.LastActionChangingActivity.Equal(end) {
		t.Fatalf("last action timestamp=%v", got.LastActionChangingActivity)
	}
	if got.MergedPullRequests != nil {
		t.Fatalf("invented merged PR count=%v", *got.MergedPullRequests)
	}
	if got.Sessions["S-1"].ProjectID != "alpha" {
		t.Fatalf("same-snapshot overview attribution=%+v", got.Sessions)
	}
}

func TestProjectOverviewSnapshotConsistentWithConcurrentActivity(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	must(t, s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "snapshot"}))
	const writes = 12
	begin := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-begin
		for i := 0; i < writes; i++ {
			if err := s.RecordActivity(ctx, domain.ActivityEvent{
				ID: fmt.Sprintf("EV-%02d", i), Kind: "project_updated", ProjectID: "alpha",
				ActorID: "writer", ObjectRef: "alpha", ObjectTitle: fmt.Sprintf("Revision %d", i),
				OccurredAt: start.Add(time.Duration(i) * time.Minute),
			}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	close(begin)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.ProjectOverviewSnapshot(ctx, overviewStoreQuery("alpha", start, 5, 5))
			if err != nil {
				t.Errorf("snapshot: %v", err)
				return
			}
			total := 0
			for _, day := range got.Activity {
				total += day.Count
			}
			if total == 0 && got.LastActionChangingActivity != nil || total > 0 && got.LastActionChangingActivity == nil {
				t.Errorf("mixed activity snapshot total=%d last=%v", total, got.LastActionChangingActivity)
			}
		}()
	}
	wg.Wait()
	must(t, <-done)
	got, err := s.ProjectOverviewSnapshot(ctx, overviewStoreQuery("alpha", start, 5, 5))
	must(t, err)
	if len(got.Activity) != 1 || got.Activity[0].Count != writes {
		t.Fatalf("final activity=%+v", got.Activity)
	}
}

func BenchmarkProjectOverviewSnapshot(b *testing.B) {
	ctx := context.Background()
	now := testNow
	s, err := Open(ctx, filepath.Join(b.TempDir(), "overview-bench.sqlite"), WithClock(func() time.Time { return now }))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "benchmark"}); err != nil {
		b.Fatal(err)
	}
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		if err := s.CreateTask(ctx, domain.Task{ID: fmt.Sprintf("T-%02d", i), ProjectID: "alpha", State: "ready", Title: "Representative task", Priority: i}); err != nil {
			b.Fatal(err)
		}
		if err := s.RecordAttention(ctx, domain.AttentionEvent{
			EventID: fmt.Sprintf("AE-%02d", i), AttentionID: fmt.Sprintf("A-%02d", i), State: domain.AttentionOpen,
			Severity: "medium", Title: "Representative attention", ProjectID: "alpha", SourceRef: fmt.Sprintf("T-%02d", i),
			NextAction: "Inspect", SourceKind: "task",
		}); err != nil {
			b.Fatal(err)
		}
		if err := s.RecordActivity(ctx, domain.ActivityEvent{
			ID: fmt.Sprintf("EV-%02d", i), Kind: "task_status_changed", ProjectID: "alpha", ActorID: "agent",
			ObjectRef: fmt.Sprintf("T-%02d", i), ObjectTitle: "Representative task", OccurredAt: start.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			b.Fatal(err)
		}
	}
	query := overviewStoreQuery("alpha", start, 20, 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ProjectOverviewSnapshot(ctx, query); err != nil {
			b.Fatal(err)
		}
	}
}
