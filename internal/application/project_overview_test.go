package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/internal/presence"
)

type fakeOverviewRepository struct {
	snapshot domain.ProjectOverviewDurableSnapshot
	query    domain.ProjectOverviewReadQuery
}

func (r *fakeOverviewRepository) HomeSnapshot(context.Context, domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	return domain.HomeDurableSnapshot{}, nil
}

func (r *fakeOverviewRepository) ProjectOverviewSnapshot(_ context.Context, query domain.ProjectOverviewReadQuery) (domain.ProjectOverviewDurableSnapshot, error) {
	r.query = query
	return r.snapshot, nil
}

func TestProjectOverviewUsesUTCWindowAndTruthfulLiveCount(t *testing.T) {
	now := time.Date(2026, 8, 9, 23, 45, 0, 0, time.FixedZone("local", -7*60*60))
	clock := &testClock{now: now}
	live := presence.New(clock)
	live.Connect(presence.Session{ID: "S-connected", Host: "plumbob"})
	live.Connect(presence.Session{ID: "S-disconnected", Host: "studio", Project: "alpha"})
	live.Disconnect("S-disconnected")
	live.Connect(presence.Session{ID: "S-other", Host: "plumbob", Project: "other"})

	day := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	last := day.Add(5 * time.Hour)
	repo := &fakeOverviewRepository{snapshot: domain.ProjectOverviewDurableSnapshot{
		Project:        domain.ProjectOverviewProject{ID: "alpha", Name: "Alpha", Purpose: "ship safely", Revision: 7},
		Activity:       []domain.ProjectOverviewActivityDay{{Day: day, Count: 3}},
		AttentionTotal: 8, AttentionHigh: 2,
		Attention:                  []domain.HomeAttention{{ID: "A-1", Severity: "high", Title: "Task failed", ProjectID: "alpha", ProjectName: "Alpha", SourceRef: "T-1", NextAction: "Inspect", SourceKind: "task", UpdatedAt: last, Destination: &domain.BrowseDestination{Kind: "task", Ref: "T-1"}}},
		OpenWorkTotal:              4,
		CurrentWork:                []domain.ProjectOverviewWork{{ID: "T-1", Title: "Implement", State: "in_progress", Priority: 1, OwnerSessionID: "S-connected", UpdatedAt: &last}},
		LastActionChangingActivity: &last,
		Sessions:                   map[string]domain.PeopleSessionFact{"S-connected": {ID: "S-connected", ProjectID: "alpha", ProjectName: "Alpha"}},
	}}
	service := New(repo, live, clock)
	got, err := service.ProjectOverview(context.Background(), ProjectOverviewQuery{Project: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity.Timezone != "UTC" || len(got.Activity.Days) != 14 ||
		got.Activity.Start != time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC) ||
		got.Activity.EndExclusive != time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("UTC window=%+v", got.Activity)
	}
	if got.Activity.Days[11].Count != 0 || got.Activity.Days[12].Day != "2026-08-09" || got.Activity.Days[12].Count != 3 ||
		got.Activity.Days[13].Day != "2026-08-10" || got.Activity.Days[13].Count != 0 {
		t.Fatalf("zero-filled activity=%+v", got.Activity.Days)
	}
	if got.Metrics.ActiveSessions != 1 || got.Metrics.AttentionTotal != 8 || got.Metrics.AttentionHigh != 2 || got.Metrics.OpenWork != 4 {
		t.Fatalf("metrics=%+v", got.Metrics)
	}
	if got.Metrics.MergedPullRequests.Available || got.Metrics.MergedPullRequests.Count != nil {
		t.Fatalf("invented GitHub metric=%+v", got.Metrics.MergedPullRequests)
	}
	if got.NeedsAttention.Items[0].Destination == nil || got.NeedsAttention.Items[0].Destination.Kind != "task" || got.NeedsAttention.Items[0].Destination.Ref != "T-1" {
		t.Fatalf("typed attention destination=%+v", got.NeedsAttention.Items[0])
	}
	if len(got.CurrentWork.Items) != 1 || got.CurrentWork.Items[0].Target.Kind != "task" || got.CurrentWork.Items[0].Target.Ref != "T-1" {
		t.Fatalf("typed work target=%+v", got.CurrentWork)
	}
	if repo.query.AttentionLimit != 5 || repo.query.WorkLimit != 5 || repo.query.ActivityStart != got.Activity.Start || repo.query.ActivityEnd != got.Activity.EndExclusive {
		t.Fatalf("repository query=%+v", repo.query)
	}
}

func TestProjectOverviewRejectsUnboundedPreview(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	service := New(&fakeOverviewRepository{}, presence.New(clock), clock)
	for _, query := range []ProjectOverviewQuery{
		{},
		{Project: "alpha", AttentionLimit: 21},
		{Project: "alpha", WorkLimit: -1},
	} {
		if _, err := service.ProjectOverview(context.Background(), query); err == nil {
			t.Fatalf("accepted invalid query=%+v", query)
		}
	}
}

func representativeProjectOverviewService() (*Service, ProjectOverviewQuery) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: base}
	live := presence.New(clock)
	for i := 0; i < 20; i++ {
		live.Connect(presence.Session{ID: fmt.Sprintf("S-%02d", i), Host: "plumbob", Project: "alpha"})
	}
	snapshot := domain.ProjectOverviewDurableSnapshot{
		Project:        domain.ProjectOverviewProject{ID: "alpha", Name: "Alpha", Purpose: strings.Repeat("p", 200), Revision: 42},
		AttentionTotal: 100, AttentionHigh: 25, OpenWorkTotal: 80,
	}
	for i := 0; i < 14; i++ {
		snapshot.Activity = append(snapshot.Activity, domain.ProjectOverviewActivityDay{Day: utcDay(base).AddDate(0, 0, -i), Count: i})
	}
	for i := 0; i < 20; i++ {
		snapshot.Attention = append(snapshot.Attention, domain.HomeAttention{
			ID: fmt.Sprintf("A-%02d", i), Severity: "medium", Title: strings.Repeat("a", 200),
			ProjectID: "alpha", ProjectName: "Alpha", SourceRef: fmt.Sprintf("T-%02d", i),
			NextAction: strings.Repeat("n", 240), SourceKind: "task", UpdatedAt: base,
		})
		snapshot.CurrentWork = append(snapshot.CurrentWork, domain.ProjectOverviewWork{
			ID: fmt.Sprintf("T-%02d", i), Title: strings.Repeat("w", 200), State: "ready", Priority: i,
		})
	}
	return New(&fakeOverviewRepository{snapshot: snapshot}, live, clock), ProjectOverviewQuery{Project: "alpha", AttentionLimit: 20, WorkLimit: 20}
}

func TestRepresentativeProjectOverviewResponseIsBounded(t *testing.T) {
	service, query := representativeProjectOverviewService()
	got, err := service.ProjectOverview(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > 32<<10 {
		t.Fatalf("representative response=%d bytes, want <=32768", len(payload))
	}
	if strings.Contains(string(payload), "review queue") || strings.Contains(string(payload), "background work") || strings.Contains(string(payload), `"body"`) {
		t.Fatalf("overview leaked deferred concepts: %s", payload)
	}
	t.Logf("representative Project Overview JSON: %d bytes", len(payload))
}

func BenchmarkProjectOverviewReadModel(b *testing.B) {
	service, query := representativeProjectOverviewService()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := service.ProjectOverview(ctx, query); err != nil {
			b.Fatal(err)
		}
	}
}
