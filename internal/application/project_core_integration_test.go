package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

type projectCoreClock struct{ now time.Time }

func (c projectCoreClock) Now() time.Time { return c.now }

type homeOnlyRepository struct{}

func (homeOnlyRepository) HomeSnapshot(context.Context, domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	return domain.HomeDurableSnapshot{}, nil
}

func TestProjectCoreMissingCapabilityIsUnavailable(t *testing.T) {
	service := application.New(homeOnlyRepository{}, nil, projectCoreClock{now: time.Now()})
	for name, call := range map[string]func() error{
		"detail": func() error {
			_, err := service.ProjectCoreDetail(context.Background(), "")
			return err
		},
		"milestones": func() error {
			_, err := service.ListMilestones(context.Background(), application.MilestoneListRequest{})
			return err
		},
		"tasks": func() error {
			_, err := service.ListCanonicalTasks(context.Background(), application.TaskListRequest{})
			return err
		},
		"wiki": func() error {
			_, err := service.ListWikiPages(context.Background(), application.WikiListRequest{})
			return err
		},
	} {
		if err := call(); !errors.Is(err, domain.ErrUnavailable) {
			t.Errorf("%s err=%v", name, err)
		}
	}
}

func TestProjectCoreCompactJSONOmitsUnknownDatesAndHistoryBodies(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "core.sqlite"), commonsstore.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.CreateProject(ctx, domain.Project{ID: "legacy", Name: "Legacy", Purpose: "Unknown-era dates", Milestone: "Pilot"}); err != nil {
		t.Fatal(err)
	}
	if err = store.CreateTask(ctx, domain.Task{ID: "legacy-task", ProjectID: "legacy", State: "ready", Title: "Legacy task"}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE projects SET created_at='1970-01-01T00:00:00Z',updated_at='1970-01-01T00:00:00Z' WHERE id='legacy'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE tasks SET created_at='1970-01-01T00:00:00Z',updated_at='1970-01-01T00:00:00Z' WHERE id='legacy-task'`); err != nil {
		t.Fatal(err)
	}
	meta := domain.CoreWriteMeta{ActorID: "admin", SessionID: "human", RequestID: "wiki-1"}
	if _, err = store.AppendWikiRevision(ctx, domain.AppendWikiRevisionCommand{
		ProjectID: "legacy", Slug: "home", Title: "Home", Summary: "First", Body: "secret explicit body v1", BaseRevision: 0, Meta: meta,
	}); err != nil {
		t.Fatal(err)
	}
	meta.RequestID = "wiki-2"
	if _, err = store.AppendWikiRevision(ctx, domain.AppendWikiRevisionCommand{
		ProjectID: "legacy", Slug: "home", Title: "Home", Summary: "Second", Body: "secret explicit body v2", BaseRevision: 1, Meta: meta,
	}); err != nil {
		t.Fatal(err)
	}

	service := application.New(store, nil, projectCoreClock{now: now})
	detail, err := service.ProjectCoreDetail(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Activity.Timezone != "UTC" || detail.Activity.Start != "2026-07-28" ||
		detail.Activity.EndExclusive != "2026-08-11" || len(detail.Activity.Days) != 14 ||
		detail.Project.CreatedAt != nil || detail.Project.UpdatedAt == nil {
		// The Wiki writes truthfully establish updated_at while created_at remains unknown.
		t.Fatalf("detail=%+v", detail)
	}
	task, err := service.OpenCanonicalTask(ctx, application.TaskOpenRequest{Task: "legacy-task", EventsLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if task.Task.CreatedAt != nil || task.Task.UpdatedAt != nil {
		t.Fatalf("legacy task dates=%+v", task.Task)
	}
	history, err := service.WikiHistory(ctx, application.WikiHistoryRequest{Project: "legacy", Slug: "home", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 2 || history.Items[0].Provenance == nil || history.Items[0].Provenance.Kind != "attested" || history.Items[0].Provenance.Session != "human" {
		t.Fatalf("wiki history provenance=%+v", history.Items)
	}
	encoded, err := json.Marshal(struct {
		Detail  application.ProjectCoreDetailResult `json:"detail"`
		Task    application.TaskOpenResult          `json:"task"`
		History application.WikiHistoryResult       `json:"history"`
	}{detail, task, history})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "1970-01-01") || strings.Contains(text, `"body"`) || strings.Contains(text, "secret explicit body") {
		t.Fatalf("unsafe compact JSON=%s", text)
	}
	opened, err := service.OpenWikiPage(ctx, "legacy", "home", 1)
	if err != nil || opened.Page.Body != "secret explicit body v1" || opened.Page.Provenance == nil ||
		opened.Page.Provenance.Kind != "attested" || opened.Page.Provenance.Session != "human" {
		t.Fatalf("explicit historical open=%+v err=%v", opened, err)
	}
}

func TestProjectCoreUnknownTimestampTaskCursorStillPages(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "cursor.sqlite"), commonsstore.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.CreateProject(ctx, domain.Project{ID: "legacy", Name: "Legacy", Purpose: "Cursor compatibility"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 27; i++ {
		id := fmt.Sprintf("legacy-%02d", i)
		if err = store.CreateTask(ctx, domain.Task{ID: id, ProjectID: "legacy", State: "ready", Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE tasks SET created_at='1970-01-01T00:00:00Z',updated_at='1970-01-01T00:00:00Z' WHERE project_id='legacy'`); err != nil {
		t.Fatal(err)
	}
	service := application.New(store, nil, projectCoreClock{now: now})
	first, err := service.ListCanonicalTasks(ctx, application.TaskListRequest{Project: "legacy", Limit: 25})
	if err != nil || len(first.Items) != 25 || first.NextCursor == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := service.ListCanonicalTasks(ctx, application.TaskListRequest{Project: "legacy", Limit: 25, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 2 || second.NextCursor != "" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}
