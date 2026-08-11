package demodata

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

func TestSeedIsExplicitRepresentativeAndIdempotent(t *testing.T) {
	ctx := context.Background()
	clock := fixedClock{value: Anchor}
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "commons-demo.sqlite"), commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	live := presence.New(clock)

	if err := Seed(ctx, store, live, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := Seed(ctx, store, live, clock.Now().Add(time.Hour)); err != nil {
		t.Fatalf("second seed must be safe: %v", err)
	}

	service := application.New(store, live, clock)
	home, err := service.GeneralHome(ctx, application.HomeQuery{PresenceLimit: 20, AttentionLimit: 20, ActivityLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if home.Navigation.Projects != 4 || home.Navigation.People != 6 || home.NeedsAttention.Total != 5 || home.RecentActivity.Total != len(activity) {
		t.Fatalf("unexpected demo totals: %+v", home)
	}
	if len(home.Presence.Items) != 6 || home.Presence.Items[0].Session == "" {
		t.Fatalf("presence not populated: %+v", home.Presence)
	}

	projectPage, err := service.BrowseProjects(ctx, application.ProjectsBrowseRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if projectPage.Total != 4 || len(projectPage.Items) != 4 {
		t.Fatalf("project browse not representative: %+v", projectPage)
	}
	people, err := service.BrowsePeople(ctx, application.PeopleBrowseRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if people.Total != 6 || len(people.Facets.Projects) != 4 || len(people.Facets.Connectivity) != 2 {
		t.Fatalf("people browse not representative: %+v", people)
	}
	attentionPage, err := service.BrowseAttention(ctx, application.AttentionBrowseRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	hasDestination := false
	for _, item := range attentionPage.Items {
		hasDestination = hasDestination || item.Destination != nil
	}
	if attentionPage.Total != 5 || len(attentionPage.Facets.Severities) != 3 || !hasDestination {
		t.Fatalf("attention browse not representative: %+v", attentionPage)
	}
	overview, err := service.ProjectOverview(ctx, application.ProjectOverviewQuery{Project: "demo-billing-orchestrator", AttentionLimit: 5, WorkLimit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Project.Status != "demo" || overview.Metrics.AttentionTotal != 2 || overview.Metrics.OpenWork != 4 || overview.Metrics.ActiveSessions != 2 {
		t.Fatalf("overview not representative: %+v", overview)
	}
	if overview.Metrics.MergedPullRequests.Available || overview.Metrics.MergedPullRequests.Count != nil {
		t.Fatalf("unavailable GitHub metric was guessed: %+v", overview.Metrics.MergedPullRequests)
	}

	var projectRows, taskRows, attentionRows, activityRows, commentRows int
	for query, target := range map[string]*int{
		`SELECT count(*) FROM projects WHERE status='demo'`:                                  &projectRows,
		`SELECT count(*) FROM tasks WHERE id LIKE 'DEMO-TASK-%'`:                             &taskRows,
		`SELECT count(*) FROM attention_events WHERE event_id LIKE 'DEMO-ATTENTION-EVENT-%'`: &attentionRows,
		`SELECT count(*) FROM activity_events WHERE id LIKE 'DEMO-ACTIVITY-%'`:               &activityRows,
		`SELECT count(*) FROM comments WHERE request_id LIKE '%demo-comment-finding-%'`:      &commentRows,
	} {
		if err := store.DB().QueryRowContext(ctx, query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if projectRows != len(projects) || taskRows != len(tasks) || attentionRows != len(attention) || activityRows != len(activity) || commentRows != len(demoComments) {
		t.Fatalf("second seed duplicated rows: projects=%d tasks=%d attention=%d activity=%d comments=%d", projectRows, taskRows, attentionRows, activityRows, commentRows)
	}
}

func TestSeedRejectsMissingDependencies(t *testing.T) {
	if err := Seed(context.Background(), nil, nil, time.Time{}); err == nil {
		t.Fatal("invalid seed dependencies accepted")
	}
}
