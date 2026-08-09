package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codex-commons/internal/httpapi"
)

func TestClientBrowseQueriesAreCompactAndExact(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	connected := true
	wants := map[string]map[string]string{
		"/v1/attention": {"cursor": "a-cursor", "limit": "20", "q": "rollback", "source": "task", "owner": "S-1", "severity": "high", "project": "alpha", "updated_from": from.Format(time.RFC3339Nano), "updated_to": to.Format(time.RFC3339Nano)},
		"/v1/projects":  {"cursor": "p-cursor", "limit": "30", "q": "commons"},
		"/v1/people":    {"cursor": "s-cursor", "limit": "40", "q": "review", "project": "alpha", "execution": "executing", "host": "plumbob", "host_connected": "true"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want, ok := wants[r.URL.Path]
		if !ok {
			t.Errorf("unexpected path=%s", r.URL.Path)
		}
		for key, value := range want {
			if got := r.URL.Query().Get(key); got != value {
				t.Errorf("path=%s %s=%q want %q", r.URL.Path, key, got, value)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"total":0,"limit":25,"items":[],"facets":{"owners_truncated":true,"projects_truncated":true}},"meta":{"request_id":"browse"}}`))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	attention, err := client.BrowseAttention(context.Background(), httpapi.AttentionBrowseQuery{Cursor: "a-cursor", Limit: 20, Search: "rollback", Source: "task", Owner: "S-1", Severity: "high", Project: "alpha", UpdatedFrom: &from, UpdatedTo: &to})
	if err != nil {
		t.Fatal(err)
	}
	if !attention.Facets.OwnersTruncated || !attention.Facets.ProjectsTruncated {
		t.Fatalf("facet truncation lost in client=%+v", attention.Facets)
	}
	if _, err := client.BrowseProjects(context.Background(), httpapi.ProjectsBrowseQuery{Cursor: "p-cursor", Limit: 30, Search: "commons"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BrowsePeople(context.Background(), httpapi.PeopleBrowseQuery{Cursor: "s-cursor", Limit: 40, Search: "review", Project: "alpha", Execution: "executing", Host: "plumbob", HostConnected: &connected}); err != nil {
		t.Fatal(err)
	}
}
