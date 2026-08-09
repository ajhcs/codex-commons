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

type integrationClock struct{ now time.Time }

func (c integrationClock) Now() time.Time { return c.now }

type legacyStub struct{}

func (legacyStub) Health(context.Context, httpapi.RequestMeta) (httpapi.HealthResult, error) {
	return httpapi.HealthResult{Status: "ok"}, nil
}
func (legacyStub) Context(context.Context, httpapi.ContextQuery, httpapi.RequestMeta) (httpapi.ContextResult, error) {
	return httpapi.ContextResult{}, nil
}
func (legacyStub) Who(context.Context, httpapi.WhoQuery, httpapi.RequestMeta) (httpapi.WhoResult, error) {
	return httpapi.WhoResult{}, nil
}
func (legacyStub) Inbox(context.Context, httpapi.InboxQuery, httpapi.RequestMeta) (httpapi.InboxResult, error) {
	return httpapi.InboxResult{}, nil
}
func (legacyStub) Search(context.Context, httpapi.SearchQuery, httpapi.RequestMeta) (httpapi.SearchResult, error) {
	return httpapi.SearchResult{}, nil
}
func (legacyStub) Open(context.Context, httpapi.OpenQuery, httpapi.RequestMeta) (httpapi.OpenResult, error) {
	return httpapi.OpenResult{}, nil
}
func (legacyStub) Next(context.Context, httpapi.NextQuery, httpapi.RequestMeta) (httpapi.NextResult, error) {
	return httpapi.NextResult{}, nil
}
func (legacyStub) Claim(context.Context, httpapi.ClaimRequest, httpapi.RequestMeta) (httpapi.WriteResult, error) {
	return httpapi.WriteResult{}, nil
}
func (legacyStub) Post(context.Context, httpapi.PostRequest, httpapi.RequestMeta) (httpapi.WriteResult, error) {
	return httpapi.WriteResult{}, nil
}
func (legacyStub) Comment(context.Context, httpapi.CommentRequest, httpapi.RequestMeta) (httpapi.WriteResult, error) {
	return httpapi.WriteResult{}, nil
}
func (legacyStub) SetStatus(context.Context, httpapi.StatusRequest, httpapi.RequestMeta) (httpapi.WriteResult, error) {
	return httpapi.WriteResult{}, nil
}
func (legacyStub) RequestTopic(context.Context, httpapi.TopicRequest, httpapi.RequestMeta) (httpapi.WriteResult, error) {
	return httpapi.WriteResult{}, nil
}

func TestRealStorePresenceApplicationAndHTTPCompose(t *testing.T) {
	ctx := context.Background()
	clock := integrationClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "integration.sqlite"), commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "integrated home"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSession(ctx, domain.Session{ID: "S-1", Host: "plumbob", ProjectID: "alpha", Purpose: "verify vertical slice"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAttention(ctx, domain.AttentionEvent{
		EventID: "AE-1", AttentionID: "A-1", State: domain.AttentionOpen,
		Severity: "high", Title: "Check failed", ProjectID: "alpha",
		SourceRef: "check/1", AccountableSessionID: "S-1",
		NextAction: "Inspect check output", SourceKind: "github_check",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordActivity(ctx, domain.ActivityEvent{
		ID: "EV-1", Kind: "task_claimed", ProjectID: "alpha", ActorID: "agent-1",
		ObjectRef: "T-1", ObjectTitle: "Build General", Outcome: "claimed", OccurredAt: clock.now,
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
	req := httptest.NewRequest(http.MethodGet, "/v1/home/general", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`"title":"Check failed"`, `"purpose":"verify vertical slice"`,
		`"object_title":"Build General"`, `"untrusted":true`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s: code=%d body=%s", want, rec.Code, body)
		}
	}
	if rec.Code != http.StatusOK || strings.Contains(body, `"body"`) || strings.Contains(body, `"basis"`) {
		t.Fatalf("unsafe integration response: code=%d body=%s", rec.Code, body)
	}
}
