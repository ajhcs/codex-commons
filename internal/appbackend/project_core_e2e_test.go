package appbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	commonsstore "codex-commons/internal/store"
)

const projectCoreAdminSecret = "project-core-test-admin-secret"

type projectCoreEnvelope struct {
	Data json.RawMessage `json:"data"`
	Meta struct {
		Untrusted bool `json:"untrusted"`
	} `json:"meta"`
}

func projectCoreRequest(handler http.Handler, method, target, body string, bearer string, cookie *http.Cookie, csrf, key string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://commons.test"+target, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != nil {
		request.AddCookie(cookie)
		request.Header.Set("Origin", "http://commons.test")
	}
	if csrf != "" {
		request.Header.Set("X-Commons-CSRF", csrf)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func loginProjectCoreHuman(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://commons.test/v1/auth/login", strings.NewReader(`{"secret":"`+projectCoreAdminSecret+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://commons.test")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || len(recorder.Result().Cookies()) != 1 {
		t.Fatalf("login code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope.Data.CSRF == "" {
		t.Fatalf("login decode err=%v body=%s", err, recorder.Body.String())
	}
	return recorder.Result().Cookies()[0], envelope.Data.CSRF
}

func TestProjectCoreRealStoreHTTPAuthorityAndEncodedIDs(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	clock := integrationClock{now: now}
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "project-core.sqlite"), commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, project := range []domain.Project{
		{ID: "team/alpha", Name: "Slash Project", Purpose: "Encoded ID"},
		{ID: "team%2Falpha", Name: "Literal Percent Project", Purpose: "Once decoded"},
	} {
		if err = store.CreateProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	meta := domain.CoreWriteMeta{ActorID: "admin", SessionID: "human", RequestID: "alpha-project"}
	if _, err = store.CreateCanonicalProject(ctx, domain.CreateProjectCommand{ID: "alpha", Name: "Alpha", Purpose: "Canonical workspace", Meta: meta}); err != nil {
		t.Fatal(err)
	}
	meta.RequestID = "alpha-milestone"
	milestone, err := store.CreateMilestone(ctx, domain.CreateMilestoneCommand{ProjectID: "alpha", Title: "Pilot", Status: "active", Position: 0, Meta: meta})
	if err != nil {
		t.Fatal(err)
	}
	meta.RequestID = "alpha-task"
	seedTask, err := store.CreateCanonicalTask(ctx, domain.CreateTaskCommand{ProjectID: "alpha", Title: "Seed task", MilestoneID: milestone.ID, Meta: meta})
	if err != nil {
		t.Fatal(err)
	}
	meta.RequestID = "alpha-wiki-1"
	if _, err = store.AppendWikiRevision(ctx, domain.AppendWikiRevisionCommand{ProjectID: "alpha", Slug: "home", Title: "Home", Summary: "First", Body: "explicit body one", BaseRevision: 0, Meta: meta}); err != nil {
		t.Fatal(err)
	}
	meta.RequestID = "alpha-wiki-2"
	if _, err = store.AppendWikiRevision(ctx, domain.AppendWikiRevisionCommand{ProjectID: "alpha", Slug: "home", Title: "Home", Summary: "Second", Body: "explicit body two", BaseRevision: 1, Meta: meta}); err != nil {
		t.Fatal(err)
	}

	service := application.New(store, nil, clock)
	backend, err := New(legacyStub{}, service)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(backend, httpapi.Config{
		Credentials: []httpapi.Credential{{BearerToken: "agent-secret", Actor: "agent-1", Session: "S-agent", Host: "plumbob"}},
		HumanAuth:   &httpapi.HumanAuthConfig{AdminSecret: projectCoreAdminSecret, DisplayName: "Admin", Actor: "local-admin", Session: "human-local-admin", Host: "browser", SessionTTL: time.Hour, RecoveryEnabled: true},
	})

	for target, id := range map[string]string{
		"/v1/projects/team%2Falpha":   "team/alpha",
		"/v1/projects/team%252Falpha": "team%2Falpha",
	} {
		recorder := projectCoreRequest(handler, http.MethodGet, target, "", "agent-secret", nil, "", "")
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":"`+id+`"`) {
			t.Errorf("encoded route %s code=%d body=%s", target, recorder.Code, recorder.Body.String())
		}
	}
	detail := projectCoreRequest(handler, http.MethodGet, "/v1/projects/alpha", "", "agent-secret", nil, "", "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"active_milestone"`) ||
		!strings.Contains(detail.Body.String(), `"start":"2026-07-28"`) || !strings.Contains(detail.Body.String(), `"end_exclusive":"2026-08-11"`) {
		t.Fatalf("detail code=%d body=%s", detail.Code, detail.Body.String())
	}
	var detailEnvelope projectCoreEnvelope
	if err = json.Unmarshal(detail.Body.Bytes(), &detailEnvelope); err != nil || !detailEnvelope.Meta.Untrusted {
		t.Fatalf("detail trust envelope err=%v body=%s", err, detail.Body.String())
	}
	taskOpen := projectCoreRequest(handler, http.MethodGet, "/v1/tasks/"+seedTask.ID+"?events_limit=50", "", "agent-secret", nil, "", "")
	if taskOpen.Code != http.StatusOK || !strings.Contains(taskOpen.Body.String(), `"milestone":{"id":"`+milestone.ID+`"`) {
		t.Fatalf("task open code=%d body=%s", taskOpen.Code, taskOpen.Body.String())
	}
	wikiHistory := projectCoreRequest(handler, http.MethodGet, "/v1/projects/alpha/wiki/home/revisions?limit=1", "", "agent-secret", nil, "", "")
	if wikiHistory.Code != http.StatusOK || !strings.Contains(wikiHistory.Body.String(), `"next_cursor"`) ||
		strings.Contains(wikiHistory.Body.String(), `"body"`) || strings.Contains(wikiHistory.Body.String(), "explicit body") {
		t.Fatalf("wiki history code=%d body=%s", wikiHistory.Code, wikiHistory.Body.String())
	}
	wikiOpen := projectCoreRequest(handler, http.MethodGet, "/v1/projects/alpha/wiki/home/revisions/1", "", "agent-secret", nil, "", "")
	if wikiOpen.Code != http.StatusOK || !strings.Contains(wikiOpen.Body.String(), "explicit body one") {
		t.Fatalf("wiki historical open code=%d body=%s", wikiOpen.Code, wikiOpen.Body.String())
	}

	agentProject := projectCoreRequest(handler, http.MethodPost, "/v1/projects", `{"id":"agent-project","name":"Agent Project","purpose":"Forbidden"}`, "agent-secret", nil, "", "agent-project")
	if agentProject.Code != http.StatusForbidden || !strings.Contains(agentProject.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("agent project write code=%d body=%s", agentProject.Code, agentProject.Body.String())
	}
	agentWiki := projectCoreRequest(handler, http.MethodPost, "/v1/projects/alpha/wiki/agent/revisions", `{"title":"Agent","summary":"No","body":"No","base_revision":0}`, "agent-secret", nil, "", "agent-wiki")
	if agentWiki.Code != http.StatusForbidden {
		t.Fatalf("agent wiki write code=%d body=%s", agentWiki.Code, agentWiki.Body.String())
	}
	agentTask := projectCoreRequest(handler, http.MethodPost, "/v1/projects/alpha/tasks", `{"title":"Agent task","description":"Derived actor"}`, "agent-secret", nil, "", "agent-task")
	if agentTask.Code != http.StatusOK {
		t.Fatalf("agent task write code=%d body=%s", agentTask.Code, agentTask.Body.String())
	}

	cookie, csrf := loginProjectCoreHuman(t, handler)
	humanCreate := projectCoreRequest(handler, http.MethodPost, "/v1/projects", `{"id":"human-project","name":"Human Project","purpose":"Local pilot"}`, "", cookie, csrf, "human-project")
	if humanCreate.Code != http.StatusOK || !strings.Contains(humanCreate.Body.String(), `"revision":1`) {
		t.Fatalf("human project create code=%d body=%s", humanCreate.Code, humanCreate.Body.String())
	}
	updateBody := `{"name":"Human Project","status":"active","purpose":"Local pilot","now":"Build","base_revision":1}`
	humanUpdate := projectCoreRequest(handler, http.MethodPut, "/v1/projects/human-project", updateBody, "", cookie, csrf, "human-project-update")
	if humanUpdate.Code != http.StatusOK || !strings.Contains(humanUpdate.Body.String(), `"revision":2`) {
		t.Fatalf("human project update code=%d body=%s", humanUpdate.Code, humanUpdate.Body.String())
	}
	stale := projectCoreRequest(handler, http.MethodPut, "/v1/projects/human-project", updateBody, "", cookie, csrf, "human-project-stale")
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"code":"conflict"`) {
		t.Fatalf("stale update code=%d body=%s", stale.Code, stale.Body.String())
	}
	humanTask := projectCoreRequest(handler, http.MethodPost, "/v1/projects/human-project/tasks", `{"title":"Human task","acceptance":"Visible result"}`, "", cookie, csrf, "human-task")
	if humanTask.Code != http.StatusOK {
		t.Fatalf("human task code=%d body=%s", humanTask.Code, humanTask.Body.String())
	}
	var writeEnvelope struct {
		Data httpapi.WriteResult `json:"data"`
	}
	if err = json.Unmarshal(humanTask.Body.Bytes(), &writeEnvelope); err != nil || writeEnvelope.Data.ID == "" {
		t.Fatalf("human task decode err=%v body=%s", err, humanTask.Body.String())
	}
	humanTaskOpen := projectCoreRequest(handler, http.MethodGet, "/v1/tasks/"+writeEnvelope.Data.ID, "", "agent-secret", nil, "", "")
	if humanTaskOpen.Code != http.StatusOK || !strings.Contains(humanTaskOpen.Body.String(), `"actor":"local-admin"`) ||
		!strings.Contains(humanTaskOpen.Body.String(), `"session":"human-local-admin"`) {
		t.Fatalf("server-derived event identity code=%d body=%s", humanTaskOpen.Code, humanTaskOpen.Body.String())
	}
}
