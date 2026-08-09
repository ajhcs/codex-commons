package store

import (
	"context"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func TestProjectOverviewTreatsFractionalRFC3339TimestampsChronologically(t *testing.T) {
	now := testNow
	store := openHomeTest(t, &now)
	ctx := context.Background()
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	must(t, store.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "timestamp regression"}))
	for _, event := range []domain.ActivityEvent{
		{ID: "exact", Kind: "project_updated", ProjectID: "alpha", ActorID: "agent", ObjectRef: "alpha", ObjectTitle: "Exact", OccurredAt: start},
		{ID: "fraction", Kind: "project_updated", ProjectID: "alpha", ActorID: "agent", ObjectRef: "alpha", ObjectTitle: "Fraction", OccurredAt: start.Add(100 * time.Millisecond)},
	} {
		must(t, store.RecordActivity(ctx, event))
	}
	got, err := store.ProjectOverviewSnapshot(ctx, overviewStoreQuery("alpha", start, 5, 5))
	must(t, err)
	if len(got.Activity) != 1 || got.Activity[0].Count != 2 {
		t.Fatalf("fractional event lost at inclusive boundary: %+v", got.Activity)
	}
	if got.LastActionChangingActivity == nil || !got.LastActionChangingActivity.Equal(start.Add(100*time.Millisecond)) {
		t.Fatalf("last activity ordered lexically: %v", got.LastActionChangingActivity)
	}
}
