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

func openHomeTest(t *testing.T, now *time.Time) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "home.sqlite"), WithClock(func() time.Time { return *now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func homeQuery(attentionOffset, attentionLimit, activityOffset, activityLimit int, sessions ...string) domain.HomeReadQuery {
	return domain.HomeReadQuery{
		Attention:  domain.HomePageRequest{Offset: attentionOffset, Limit: attentionLimit},
		Activity:   domain.HomePageRequest{Offset: activityOffset, Limit: activityLimit},
		SessionIDs: sessions,
	}
}

func TestHomeSnapshotEmptyIsBounded(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	got, err := s.HomeSnapshot(context.Background(), homeQuery(0, 5, 0, 10))
	must(t, err)
	if got.ProjectsTotal != 0 || got.AttentionTotal != 0 || got.ActivityTotal != 0 ||
		len(got.Attention) != 0 || len(got.Activity) != 0 || len(got.Sessions) != 0 {
		t.Fatalf("empty=%+v", got)
	}
}

func TestHomeAttentionUsesChronologicalFractionalTimestampOrder(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	must(t, s.RecordAttention(ctx, domain.AttentionEvent{EventID: "AE-exact", AttentionID: "A-exact", State: domain.AttentionOpen,
		Severity: "medium", Title: "Exact", SourceRef: "T-exact", NextAction: "Inspect", SourceKind: "task"}))
	now = testNow.Add(100 * time.Millisecond)
	must(t, s.RecordAttention(ctx, domain.AttentionEvent{EventID: "AE-fraction", AttentionID: "A-fraction", State: domain.AttentionOpen,
		Severity: "medium", Title: "Fraction", SourceRef: "T-fraction", NextAction: "Inspect", SourceKind: "task"}))
	got, err := s.HomeSnapshot(ctx, homeQuery(0, 2, 0, 1))
	must(t, err)
	if len(got.Attention) != 2 || got.Attention[0].ID != "A-fraction" {
		t.Fatalf("chronological General attention=%+v", got.Attention)
	}
}

func TestHomeCanonicalSignalsPaginationOrderingAndTrust(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	must(t, s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "test home"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-owner", Host: "plumbob", ProjectID: "alpha", Purpose: "fix checks"}))

	for i := 0; i < 7; i++ {
		now = testNow.Add(time.Duration(i) * time.Minute)
		kind := "task"
		if i == 6 {
			kind = "github_check"
		}
		must(t, s.RecordAttention(ctx, domain.AttentionEvent{
			EventID: fmt.Sprintf("AE-%d", i), AttentionID: fmt.Sprintf("A-%d", i),
			State: domain.AttentionOpen, Severity: []string{"high", "medium", "low"}[i%3],
			Title: fmt.Sprintf("Attention %d", i), ProjectID: "alpha",
			SourceRef: fmt.Sprintf("T-%d", i), AccountableSessionID: "S-owner",
			NextAction: fmt.Sprintf("Perform action %d", i), SourceKind: kind,
		}))
	}
	now = testNow.Add(8 * time.Minute)
	must(t, s.RecordAttention(ctx, domain.AttentionEvent{
		EventID: "AE-resolve", AttentionID: "A-1", State: domain.AttentionResolved,
		Severity: "medium", Title: "Attention 1", ProjectID: "alpha", SourceRef: "T-1",
		AccountableSessionID: "S-owner", NextAction: "No action", SourceKind: "task",
	}))

	for i := 0; i < 12; i++ {
		kind := "task_status_changed"
		if i == 11 {
			kind = "post_published"
		}
		must(t, s.RecordActivity(ctx, domain.ActivityEvent{
			ID: fmt.Sprintf("EV-%02d", i), Kind: kind, ProjectID: "alpha",
			ActorID: "agent-a", ObjectRef: fmt.Sprintf("T-%d", i),
			ObjectTitle: fmt.Sprintf("Object %d", i), Outcome: "updated",
			OccurredAt: testNow.Add(time.Duration(i) * time.Minute),
		}))
	}

	got, err := s.HomeSnapshot(ctx, homeQuery(2, 2, 3, 4, "S-owner", "S-missing"))
	must(t, err)
	if got.ProjectsTotal != 1 || got.AttentionTotal != 6 || got.ActivityTotal != 12 {
		t.Fatalf("totals=%+v", got)
	}
	if len(got.Attention) != 2 || got.Attention[0].ID != "A-4" || got.Attention[1].ID != "A-3" {
		t.Fatalf("attention page/order=%+v", got.Attention)
	}
	if got.Attention[0].ProjectName != "Alpha" || got.Attention[0].NextAction == "" {
		t.Fatalf("canonical attention fields=%+v", got.Attention[0])
	}
	first, err := s.HomeSnapshot(ctx, homeQuery(0, 1, 0, 1))
	must(t, err)
	if len(first.Attention) != 1 || first.Attention[0].ID != "A-6" || !first.Attention[0].Untrusted {
		t.Fatalf("GitHub text not explicitly untrusted: %+v", first.Attention)
	}
	if len(first.Activity) != 1 || first.Activity[0].ID != "EV-11" || !first.Activity[0].Untrusted {
		t.Fatalf("forum activity not explicitly untrusted: %+v", first.Activity)
	}
	if fact := got.Sessions["S-owner"]; fact.Purpose != "fix checks" || fact.ProjectName != "Alpha" {
		t.Fatalf("session fact=%+v", fact)
	}
	if _, ok := got.Sessions["S-missing"]; ok {
		t.Fatal("invented persisted facts for unknown session")
	}

	if err := s.RecordActivity(ctx, domain.ActivityEvent{ID: "heartbeat-1", Kind: "heartbeat", ActorID: "system", ObjectRef: "host", ObjectTitle: "heartbeat", OccurredAt: now}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("ordinary heartbeat accepted: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, "UPDATE activity_events SET outcome='changed' WHERE id='EV-00'"); err == nil {
		t.Fatal("activity history was mutable")
	}
	if _, err := s.DB().ExecContext(ctx, "DELETE FROM attention_events WHERE attention_id='A-0'"); err == nil {
		t.Fatal("attention history was mutable")
	}
}

func TestHomeSignalReplayConflictsOnChangedPayload(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	event := domain.AttentionEvent{EventID: "AE-1", AttentionID: "A-1", State: domain.AttentionOpen, Severity: "high", Title: "Check failed", SourceRef: "check/1", NextAction: "Inspect check", SourceKind: "github_check"}
	must(t, s.RecordAttention(ctx, event))
	must(t, s.RecordAttention(ctx, event))
	event.NextAction = "Different"
	if err := s.RecordAttention(ctx, event); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed attention replay=%v", err)
	}
	activity := domain.ActivityEvent{ID: "EV-1", Kind: "host_disconnected", ActorID: "system", ObjectRef: "host-1", ObjectTitle: "host-1", Outcome: "disconnected", OccurredAt: now}
	must(t, s.RecordActivity(ctx, activity))
	must(t, s.RecordActivity(ctx, activity))
	activity.Outcome = "other"
	if err := s.RecordActivity(ctx, activity); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed activity replay=%v", err)
	}
}

func TestHomeSnapshotRemainsConsistentWithConcurrentWriters(t *testing.T) {
	now := testNow
	s := openHomeTest(t, &now)
	ctx := context.Background()
	const writes = 12
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		for i := 0; i < writes; i++ {
			err := s.RecordActivity(ctx, domain.ActivityEvent{
				ID: fmt.Sprintf("EV-%02d", i), Kind: "project_updated", ActorID: "writer",
				ObjectRef: "project", ObjectTitle: fmt.Sprintf("Revision %d", i),
				OccurredAt: testNow.Add(time.Duration(i) * time.Second),
			})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	close(start)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.HomeSnapshot(ctx, homeQuery(0, 20, 0, 20))
			if err != nil {
				t.Errorf("snapshot: %v", err)
				return
			}
			if got.ActivityTotal != len(got.Activity) {
				t.Errorf("mixed snapshot total=%d rows=%d", got.ActivityTotal, len(got.Activity))
			}
			for j := 1; j < len(got.Activity); j++ {
				if got.Activity[j-1].OccurredAt.Before(got.Activity[j].OccurredAt) {
					t.Errorf("unstable ordering: %+v", got.Activity)
					break
				}
			}
		}()
	}
	wg.Wait()
	must(t, <-done)
	final, err := s.HomeSnapshot(ctx, homeQuery(0, 20, 0, 20))
	must(t, err)
	if final.ActivityTotal != writes || len(final.Activity) != writes {
		t.Fatalf("final=%+v", final)
	}
}

func BenchmarkHomeSnapshot(b *testing.B) {
	ctx := context.Background()
	now := testNow
	s, err := Open(ctx, filepath.Join(b.TempDir(), "home-bench.sqlite"), WithClock(func() time.Time { return now }))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "benchmark"}); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("S-%d", i)
		if err := s.UpsertSession(ctx, domain.Session{ID: id, Host: "plumbob", ProjectID: "alpha", Purpose: "bounded purpose"}); err != nil {
			b.Fatal(err)
		}
		if err := s.RecordAttention(ctx, domain.AttentionEvent{
			EventID: fmt.Sprintf("AE-%d", i), AttentionID: fmt.Sprintf("A-%d", i),
			State: domain.AttentionOpen, Severity: "medium", Title: "Representative attention item",
			ProjectID: "alpha", SourceRef: fmt.Sprintf("T-%d", i),
			AccountableSessionID: id, NextAction: "Perform the explicit next action", SourceKind: "task",
		}); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		if err := s.RecordActivity(ctx, domain.ActivityEvent{
			ID: fmt.Sprintf("EV-%d", i), Kind: "task_status_changed", ProjectID: "alpha",
			ActorID: "agent", ObjectRef: fmt.Sprintf("T-%d", i),
			ObjectTitle: "Representative object", Outcome: "updated",
			OccurredAt: testNow.Add(time.Duration(i) * time.Second),
		}); err != nil {
			b.Fatal(err)
		}
	}
	query := homeQuery(0, 5, 0, 10, "S-0", "S-1", "S-2", "S-3", "S-4")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.HomeSnapshot(ctx, query); err != nil {
			b.Fatal(err)
		}
	}
}
