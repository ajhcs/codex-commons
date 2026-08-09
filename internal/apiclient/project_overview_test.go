package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"codex-commons/internal/httpapi"
)

func TestClientProjectOverviewUsesEscapedProjectAndCompactLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/projects/alpha%20pilot/overview" ||
			r.URL.Query().Get("attention_limit") != "4" || r.URL.Query().Get("work_limit") != "7" {
			t.Fatalf("request=%s?%s", r.URL.EscapedPath(), r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"project":{"id":"alpha pilot","name":"Alpha","purpose":"pilot","revision":1},"activity":{"timezone":"UTC","start":"2026-07-27T00:00:00Z","end_exclusive":"2026-08-10T00:00:00Z","days":[]},"metrics":{"attention_total":0,"attention_high":0,"open_work":0,"merged_pull_requests":{"available":false},"active_sessions":0},"needs_attention":{"total":0,"limit":4,"items":[]},"current_work":{"total":0,"limit":7,"items":[]},"snapshot_at":"2026-08-09T12:00:00Z"},"meta":{"request_id":"r1"}}`))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.ProjectOverview(context.Background(), httpapi.ProjectOverviewQuery{
		Project: "alpha pilot", AttentionLimit: 4, WorkLimit: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Project.ID != "alpha pilot" || got.NeedsAttention.Limit != 4 || got.CurrentWork.Limit != 7 || got.Metrics.MergedPullRequests.Available {
		t.Fatalf("decoded=%+v", got)
	}
}
