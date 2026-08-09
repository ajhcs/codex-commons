package appbackend

import (
	"context"
	"errors"
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

func TestSlice7RealStorePresenceApplicationAndHTTPCompose(t *testing.T) {
	ctx := context.Background()
	clock := integrationClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "slice7.sqlite"), commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "Test browse foundations"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTask(ctx, domain.Task{ID: "T-1", ProjectID: "alpha", State: "in_progress", Title: "Build Slice 7", Priority: 9}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSession(ctx, domain.Session{ID: "S-1", Host: "plumbob", ProjectID: "alpha", Purpose: "Verify browse"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAttention(ctx, domain.AttentionEvent{EventID: "AE-1", AttentionID: "A-1", State: domain.AttentionOpen,
		Severity: "high", Title: "Task needs evidence", ProjectID: "alpha", SourceRef: "T-1",
		AccountableSessionID: "S-1", NextAction: "Attach evidence", SourceKind: "task"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordActivity(ctx, domain.ActivityEvent{ID: "EV-1", Kind: "task_status_changed", ProjectID: "alpha",
		ActorID: "agent-1", ObjectRef: "T-1", ObjectTitle: "Build Slice 7", Outcome: "in_progress", OccurredAt: clock.now}); err != nil {
		t.Fatal(err)
	}
	live := presence.New(clock)
	live.Connect(presence.Session{ID: "S-1", Actor: "agent-1", Host: "plumbob", Project: "alpha"})
	live.LeaseExecution("S-1", time.Hour)
	service := application.New(store, live, clock)
	backend, err := New(legacyStub{}, service)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(backend, httpapi.Config{Credentials: []httpapi.Credential{{
		BearerToken: "secret", Actor: "agent-1", Session: "S-1", Host: "plumbob",
	}}})
	for path, wants := range map[string][]string{
		"/v1/attention?q=TASK+NEEDS&severity=high&project=alpha": {`"title":"Task needs evidence"`, `"kind":"task"`, `"ref":"T-1"`},
		"/v1/projects?q=Alpha":           {`"purpose":"Test browse foundations"`, `"active_sessions":1`, `"current_work"`},
		"/v1/people?execution=executing": {`"purpose":"Verify browse"`, `"host_connected":true`, `"execution":"executing"`},
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("path=%s code=%d body=%s", path, rec.Code, rec.Body.String())
		}
		for _, want := range wants {
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("path=%s missing=%s body=%s", path, want, rec.Body.String())
			}
		}
		if strings.Contains(strings.ToLower(rec.Body.String()), "queue") {
			t.Fatalf("queue concept leaked: %s", rec.Body.String())
		}
	}
}

func TestSlice7AdapterRequiresAttestedIdentity(t *testing.T) {
	service := application.New(&fakeBrowseRepositoryAdapter{}, presence.New(nil), nil)
	backend, err := New(legacyStub{}, service)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.BrowsePeople(context.Background(), httpapi.PeopleBrowseQuery{}, httpapi.RequestMeta{}); err == nil {
		t.Fatal("missing identity accepted")
	}
}

func TestSlice7MissingBrowseCapabilityMapsUnavailable(t *testing.T) {
	service := application.New(&homeOnlyRepositoryAdapter{}, presence.New(nil), nil)
	backend, err := New(legacyStub{}, service)
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.BrowseProjects(context.Background(), httpapi.ProjectsBrowseQuery{}, httpapi.RequestMeta{Actor: "agent", Session: "S-1", Host: "plumbob"})
	var apiErr *httpapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != httpapi.CodeUnavailable {
		t.Fatalf("composition error=%#v", err)
	}
	handler := httpapi.NewHandler(backend, httpapi.Config{Credentials: []httpapi.Credential{{
		BearerToken: "secret", Actor: "agent", Session: "S-1", Host: "plumbob",
	}}})
	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("composition HTTP status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type homeOnlyRepositoryAdapter struct{}

func (*homeOnlyRepositoryAdapter) HomeSnapshot(context.Context, domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	return domain.HomeDurableSnapshot{}, nil
}

type fakeBrowseRepositoryAdapter struct{}

func (*fakeBrowseRepositoryAdapter) HomeSnapshot(context.Context, domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	return domain.HomeDurableSnapshot{}, nil
}
func (*fakeBrowseRepositoryAdapter) AttentionBrowseSnapshot(context.Context, domain.AttentionBrowseQuery) (domain.AttentionBrowseSnapshot, error) {
	return domain.AttentionBrowseSnapshot{}, nil
}
func (*fakeBrowseRepositoryAdapter) ProjectBrowseSnapshot(context.Context, domain.ProjectBrowseQuery) (domain.ProjectBrowseSnapshot, error) {
	return domain.ProjectBrowseSnapshot{}, nil
}
func (*fakeBrowseRepositoryAdapter) PeopleFactsSnapshot(context.Context, domain.PeopleFactsQuery) (domain.PeopleFactsSnapshot, error) {
	return domain.PeopleFactsSnapshot{Sessions: map[string]domain.PeopleSessionFact{}}, nil
}
