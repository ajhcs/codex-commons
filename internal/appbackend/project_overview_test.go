package appbackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

func TestProjectOverviewStorePresenceApplicationAndHTTPCompose(t *testing.T) {
	ctx := context.Background()
	clock := integrationClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "overview.sqlite"), commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "integrated overview"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTask(ctx, domain.Task{ID: "T-1", ProjectID: "alpha", State: "in_progress", Title: "Build overview"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAttention(ctx, domain.AttentionEvent{
		EventID: "AE-1", AttentionID: "A-1", State: domain.AttentionOpen,
		Severity: "high", Title: "Check failed", ProjectID: "alpha",
		SourceRef: "check/1", NextAction: "Inspect", SourceKind: "github_check",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordActivity(ctx, domain.ActivityEvent{
		ID: "EV-1", Kind: "task_status_changed", ProjectID: "alpha", ActorID: "agent-1",
		ObjectRef: "T-1", ObjectTitle: "Build overview", Outcome: "started", OccurredAt: clock.now,
	}); err != nil {
		t.Fatal(err)
	}
	live := presence.New(clock)
	live.Connect(presence.Session{ID: "S-1", Actor: "agent-1", Host: "plumbob", Project: "alpha"})
	service := application.New(store, live, clock)
	backend, err := New(legacyStub{}, service)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(backend, httpapi.Config{Credentials: []httpapi.Credential{{
		BearerToken: "secret", Actor: "agent-1", Session: "S-1", Host: "plumbob",
	}}})
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/alpha/overview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`"purpose":"integrated overview"`, `"attention_total":1`, `"attention_high":1`,
		`"open_work":1`, `"active_sessions":1`, `"available":false`,
		`"title":"Build overview"`, `"timezone":"UTC"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s: code=%d body=%s", want, rec.Code, body)
		}
	}
	if rec.Code != http.StatusOK || strings.Contains(strings.ToLower(body), "queue") || strings.Contains(body, `"body"`) {
		t.Fatalf("unsafe overview response: code=%d body=%s", rec.Code, body)
	}
}

func TestProjectOverviewAdapterRequiresAttestedIdentity(t *testing.T) {
	clock := integrationClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	store, err := commonsstore.Open(context.Background(), filepath.Join(t.TempDir(), "identity.sqlite"), commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend, err := New(legacyStub{}, application.New(store, presence.New(clock), clock))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ProjectOverview(context.Background(), httpapi.ProjectOverviewQuery{Project: "alpha"}, httpapi.RequestMeta{}); err == nil {
		t.Fatal("missing attested identity accepted")
	}
}
