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

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

type fakeHomeRepository struct {
	snapshot domain.HomeDurableSnapshot
	query    domain.HomeReadQuery
}

func (r *fakeHomeRepository) HomeSnapshot(_ context.Context, query domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	r.query = query
	return r.snapshot, nil
}

func TestGeneralHomeJoinsTruthfulPresenceAndDurablePurpose(t *testing.T) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: base}
	live := presence.New(clock)
	live.Connect(presence.Session{ID: "S-old", Actor: "agent-old", Host: "studio", Project: "alpha"})
	clock.now = base.Add(time.Minute)
	live.Disconnect("S-old")
	clock.now = base.Add(time.Hour)
	live.Connect(presence.Session{ID: "S-new", Actor: "agent-new", Host: "plumbob", Project: "alpha"})
	if !live.LeaseExecution("S-new", 10*time.Minute) {
		t.Fatal("lease failed")
	}
	loaded := "D-7"
	live.SetLoaded("S-new", &loaded)
	clock.now = base.Add(time.Hour + time.Minute)

	repo := &fakeHomeRepository{snapshot: domain.HomeDurableSnapshot{
		ProjectsTotal: 3, AttentionTotal: 7, ActivityTotal: 12,
		Sessions: map[string]domain.SessionFact{
			"S-new": {ID: "S-new", Host: "plumbob", ProjectID: "alpha", ProjectName: "Alpha", Purpose: "review checks"},
			"S-old": {ID: "S-old", Host: "studio", ProjectID: "alpha", ProjectName: "Alpha", Purpose: "older task"},
		},
		Attention: []domain.HomeAttention{{ID: "A-1", Severity: "high", Title: "Check failed", SourceRef: "check/1", NextAction: "Inspect", SourceKind: "github_check", Untrusted: true, UpdatedAt: base}},
		Activity:  []domain.HomeActivity{{ID: "EV-1", Kind: "task_claimed", ActorID: "agent-new", ObjectRef: "T-1", ObjectTitle: "Implement", OccurredAt: base}},
	}}
	service := New(repo, live, clock)
	got, err := service.GeneralHome(context.Background(), HomeQuery{
		PresenceLimit: 2, AttentionLimit: 2, AttentionPage: 1,
		ActivityLimit: 4, ActivityPage: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Navigation.Projects != 3 || got.Navigation.People != 2 || got.Presence.Total != 2 {
		t.Fatalf("derived navigation/presence counts=%+v", got)
	}
	if len(got.Presence.Items) != 2 {
		t.Fatalf("presence=%+v", got.Presence)
	}
	newest, stale := got.Presence.Items[0], got.Presence.Items[1]
	if newest.Session != "S-new" || newest.Execution != "executing" || !newest.HostConnected ||
		newest.Purpose != "review checks" || newest.Loaded == nil || *newest.Loaded != "D-7" {
		t.Fatalf("new presence facts conflated=%+v", newest)
	}
	if stale.Session != "S-old" || stale.Execution != "not_running" || stale.HostConnected ||
		stale.Purpose != "older task" || stale.RecencySeconds != 3600 {
		t.Fatalf("stale/disconnected facts conflated=%+v", stale)
	}
	if repo.query.Attention.Offset != 2 || repo.query.Attention.Limit != 2 ||
		repo.query.Activity.Offset != 8 || repo.query.Activity.Limit != 4 {
		t.Fatalf("pagination query=%+v", repo.query)
	}
	if !got.NeedsAttention.Items[0].Untrusted || got.NeedsAttention.HasMore != true ||
		got.RecentActivity.HasMore != false {
		t.Fatalf("page metadata/trust=%+v %+v", got.NeedsAttention, got.RecentActivity)
	}
}

func representativeService() (*Service, HomeQuery) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: base.Add(30 * time.Minute)}
	live := presence.New(clock)
	sessions := make(map[string]domain.SessionFact)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("S-%d", i)
		live.Connect(presence.Session{ID: id, Actor: fmt.Sprintf("agent-%d", i), Host: "plumbob", Project: "alpha"})
		sessions[id] = domain.SessionFact{ID: id, Host: "plumbob", ProjectID: "alpha", ProjectName: "Alpha", Purpose: fmt.Sprintf("Purpose %d", i)}
	}
	snapshot := domain.HomeDurableSnapshot{ProjectsTotal: 4, AttentionTotal: 18, ActivityTotal: 37, Sessions: sessions}
	for i := 0; i < 5; i++ {
		snapshot.Attention = append(snapshot.Attention, domain.HomeAttention{
			ID: fmt.Sprintf("A-%d", i), Severity: "medium", Title: strings.Repeat("a", 80),
			ProjectID: "alpha", ProjectName: "Alpha", SourceRef: fmt.Sprintf("T-%d", i),
			NextAction: strings.Repeat("n", 100), SourceKind: "task", UpdatedAt: base,
		})
	}
	for i := 0; i < 10; i++ {
		snapshot.Activity = append(snapshot.Activity, domain.HomeActivity{
			ID: fmt.Sprintf("EV-%d", i), Kind: "task_status_changed", ProjectID: "alpha",
			ProjectName: "Alpha", ActorID: "agent-0", ObjectRef: fmt.Sprintf("T-%d", i),
			ObjectTitle: strings.Repeat("o", 80), Outcome: "updated", OccurredAt: base,
		})
	}
	return New(&fakeHomeRepository{snapshot: snapshot}, live, clock), HomeQuery{PresenceLimit: 5, AttentionLimit: 5, ActivityLimit: 10}
}

func TestRepresentativeGeneralHomeResponseIsBounded(t *testing.T) {
	service, query := representativeService()
	got, err := service.GeneralHome(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > 8<<10 {
		t.Fatalf("representative response=%d bytes, want <=8192", len(payload))
	}
	if strings.Contains(string(payload), "\"body\"") || strings.Contains(string(payload), "\"basis\"") {
		t.Fatalf("home response leaked full forum fields: %s", payload)
	}
	t.Logf("representative General-home application JSON: %d bytes", len(payload))
}

func BenchmarkGeneralHomeReadModel(b *testing.B) {
	service, query := representativeService()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := service.GeneralHome(ctx, query); err != nil {
			b.Fatal(err)
		}
	}
}
