package application_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

func TestProjectCoreTaskEventsSupportBoundedLoadMore(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "events.sqlite"), commonsstore.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	meta := domain.CoreWriteMeta{ActorID: "agent", SessionID: "S-1", RequestID: "project"}
	if _, err = store.CreateCanonicalProject(ctx, domain.CreateProjectCommand{ID: "alpha", Name: "Alpha", Purpose: "Event paging", Meta: meta}); err != nil {
		t.Fatal(err)
	}
	meta.RequestID = "task"
	task, err := store.CreateCanonicalTask(ctx, domain.CreateTaskCommand{ProjectID: "alpha", Title: "Task", Meta: meta})
	if err != nil {
		t.Fatal(err)
	}
	meta.RequestID = "update"
	updated, err := store.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{ID: task.ID, Title: "Task updated", BaseRevision: task.Revision, Meta: meta})
	if err != nil {
		t.Fatal(err)
	}
	meta.RequestID = "done"
	if _, err = store.ChangeCanonicalTaskState(ctx, domain.ChangeTaskStateCommand{ID: task.ID, State: "done", Basis: "Verified", BaseRevision: updated.Revision, Meta: meta}); err != nil {
		t.Fatal(err)
	}
	service := application.New(store, nil, projectCoreClock{now: now})
	first, err := service.OpenCanonicalTask(ctx, application.TaskOpenRequest{Task: task.ID, EventsLimit: 1})
	if err != nil || len(first.Events) != 1 || first.EventsNextCursor == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := service.ListTaskEvents(ctx, application.TaskEventListRequest{Task: task.ID, Cursor: first.EventsNextCursor, Limit: 1})
	if err != nil || len(second.Items) != 1 || second.NextCursor == "" || second.Items[0].ID == first.Events[0].ID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	third, err := service.ListTaskEvents(ctx, application.TaskEventListRequest{Task: task.ID, Cursor: second.NextCursor, Limit: 1})
	if err != nil || len(third.Items) != 1 || third.NextCursor != "" {
		t.Fatalf("third=%+v err=%v", third, err)
	}
}
