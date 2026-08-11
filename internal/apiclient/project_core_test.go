package apiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"codex-commons/internal/httpapi"
)

func TestProjectCoreClientUsesCompactReadsExplicitOpenAndTaskWrites(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/projects/team%2Falpha" {
				t.Errorf("detail method=%s path=%s", r.Method, r.URL.EscapedPath())
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"project":{"id":"team/alpha","name":"Alpha","status":"active","purpose":"Test","revision":1},"counts":{"tasks":0,"milestones":0,"wiki_pages":0},"activity":{"timezone":"UTC","start":"2026-07-28","end_exclusive":"2026-08-11","days":[]},"snapshot_at":"2026-08-10T00:00:00Z"},"meta":{"request_id":"detail","untrusted":true}}`))
		case 2:
			if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/tasks" || r.URL.Query().Get("cursor") != "next" ||
				r.URL.Query().Get("limit") != "25" || r.URL.Query().Get("state") != "ready" || r.URL.Query().Get("milestone") != "MS-1" {
				t.Errorf("tasks request=%s method=%s", r.URL.String(), r.Method)
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"total":0,"limit":25,"state_counts":{"ready":0,"in_progress":0,"blocked":0,"done":0,"cancelled":0,"total":0},"items":[]},"meta":{"request_id":"tasks","untrusted":true}}`))
		case 3:
			if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/wiki/home/revisions/3" {
				t.Errorf("wiki open=%s method=%s", r.URL.String(), r.Method)
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"page":{"id":"W-1","project":"alpha","slug":"home","title":"Home","revision":3,"summary":"Third","body":"explicit","author_session_id":"human"}},"meta":{"request_id":"wiki","untrusted":true}}`))
		case 4:
			if r.Method != http.MethodPut || r.URL.EscapedPath() != "/v1/tasks/T%2F1" || r.Header.Get("Idempotency-Key") != "task-update" {
				t.Errorf("task update path=%s method=%s key=%s", r.URL.EscapedPath(), r.Method, r.Header.Get("Idempotency-Key"))
			}
			var input httpapi.UpdateCoreTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.BaseRevision == nil || *input.BaseRevision != 7 || input.Title != "Updated" {
				t.Errorf("task update input=%+v err=%v", input, err)
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"T/1","revision":8,"persisted":true},"meta":{"request_id":"update"}}`))
		case 5:
			if r.Method != http.MethodPost || r.URL.Path != "/v1/projects/alpha/tasks" || r.Header.Get("Idempotency-Key") != "task-create" {
				t.Errorf("task create=%s method=%s key=%s", r.URL.String(), r.Method, r.Header.Get("Idempotency-Key"))
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"T-2","revision":9,"persisted":true},"meta":{"request_id":"create"}}`))
		case 6:
			if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v1/tasks/T%2F1/state" || r.Header.Get("Idempotency-Key") != "task-state" {
				t.Errorf("task state path=%s method=%s key=%s", r.URL.EscapedPath(), r.Method, r.Header.Get("Idempotency-Key"))
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"T/1","revision":10,"persisted":true},"meta":{"request_id":"state"}}`))
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, BearerToken: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.ProjectCoreDetail(context.Background(), "team/alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ListProjectTasks(context.Background(), httpapi.ProjectTaskListQuery{Project: "alpha", Cursor: "next", Limit: 25, State: "ready", Milestone: "MS-1"}); err != nil {
		t.Fatal(err)
	}
	opened, err := client.OpenProjectWiki(context.Background(), httpapi.ProjectWikiOpenQuery{Project: "alpha", Slug: "home", Revision: 3})
	if err != nil || opened.Page.Body != "explicit" {
		t.Fatalf("wiki open=%+v err=%v", opened, err)
	}
	base := int64(7)
	if _, err = client.UpdateCoreTask(context.Background(), "T/1", httpapi.UpdateCoreTaskRequest{Title: "Updated", BaseRevision: &base}, "task-update"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.CreateCoreTask(context.Background(), "alpha", httpapi.CreateCoreTaskRequest{Title: "Created"}, "task-create"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ChangeCoreTaskState(context.Background(), "T/1", httpapi.ChangeCoreTaskStateRequest{State: "done", Basis: "verified", BaseRevision: &base}, "task-state"); err != nil {
		t.Fatal(err)
	}
	if calls != 6 {
		t.Fatalf("calls=%d", calls)
	}
}
