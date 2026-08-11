package appbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"codex-commons/internal/application"
	"codex-commons/internal/demodata"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

func TestSlice85SeedStorePresenceApplicationAndHTTPFullPath(t *testing.T) {
	ctx := context.Background()
	clock := integrationClock{now: demodata.Anchor}
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "slice85.sqlite"), commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	live := presence.New(clock)
	if err := demodata.Seed(ctx, store, live, clock.Now()); err != nil {
		t.Fatal(err)
	}
	service := application.New(store, live, clock)
	backend, err := New(legacyStub{}, service)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(backend, httpapi.Config{Credentials: []httpapi.Credential{{
		BearerToken: "slice85-secret", Actor: "demo-human", Session: "DEMO-HUMAN-1", Host: "lan-browser",
	}}})

	cases := []struct {
		path  string
		wants []string
	}{
		{"/v1/home/general?presence_limit=10&attention_limit=10&activity_limit=10", []string{`"projects":4`, `"people":6`, `"[Demo] Ranking smoke tests are failing`, `"recent_activity"`}},
		{"/v1/attention?limit=10", []string{`"total":5`, `"severities"`, `"kind":"task"`, `"ref":"DEMO-TASK-REPLAY"`}},
		{"/v1/projects?limit=10", []string{`"total":4`, `"id":"demo-billing-orchestrator"`, `"current_work"`, `"active_sessions":2`}},
		{"/v1/people?limit=10", []string{`"total":6`, `"session":"DEMO-SES-4179"`, `"execution":"executing"`, `"host_connected":false`}},
		{"/v1/projects/demo-billing-orchestrator/overview?attention_limit=5&work_limit=5", []string{`"status":"demo"`, `"attention_total":2`, `"open_work":4`, `"active_sessions":2`, `"available":false`}},
		{"/v1/posts?limit=10", []string{`"total":0`, `"items":[]`}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer slice85-secret")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d body=%s", rec.Code, body)
			}
			var envelope map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope["ok"] != true {
				t.Fatalf("invalid success envelope: err=%v body=%s", err, body)
			}
			for _, want := range tc.wants {
				if !strings.Contains(body, want) {
					t.Fatalf("missing %s in %s", want, body)
				}
			}
			lower := strings.ToLower(body)
			if strings.Contains(lower, `"body"`) || strings.Contains(lower, `"basis"`) || strings.Contains(lower, "queue") {
				t.Fatalf("unsafe or out-of-scope payload: %s", body)
			}
		})
	}

	for _, target := range []string{"/v1/posts", "/v1/projects", "/v1/people", "/v1/projects/demo-billing-orchestrator/overview"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("core handler accepted anonymous request target=%s code=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}
