package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"codex-commons/internal/httpapi"
)

func TestClientGeneralHomeUsesCompactPaginationQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/home/general" ||
			r.URL.Query().Get("presence_limit") != "3" ||
			r.URL.Query().Get("attention_limit") != "4" ||
			r.URL.Query().Get("attention_page") != "2" ||
			r.URL.Query().Get("activity_limit") != "7" ||
			r.URL.Query().Get("activity_page") != "1" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"navigation":{"projects":2,"people":3},"presence":{"total":3,"items":[]},"needs_attention":{"total":18,"page":2,"limit":4,"items":[],"has_more":true},"recent_activity":{"total":37,"page":1,"limit":7,"items":[],"has_more":true}},"meta":{"request_id":"home-1"}}`))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.GeneralHome(context.Background(), httpapi.GeneralHomeQuery{
		PresenceLimit: 3, AttentionLimit: 4, AttentionPage: 2,
		ActivityLimit: 7, ActivityPage: 1,
	})
	if err != nil || got.Navigation.Projects != 2 || got.NeedsAttention.Total != 18 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
