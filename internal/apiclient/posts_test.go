package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codex-commons/internal/httpapi"
)

func TestPostsClientUsesCompactQueriesAndExplicitOpen(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			if r.URL.Path != "/v1/posts" || r.URL.Query().Get("cursor") != "feed-cursor" ||
				r.URL.Query().Get("limit") != "20" || r.URL.Query().Get("q") != "retry" ||
				r.URL.Query().Get("topic") != "general" || r.URL.Query().Get("project") != "alpha" ||
				r.URL.Query().Get("kind") != "finding" ||
				r.URL.Query().Get("created_from") != from.Format(time.RFC3339Nano) ||
				r.URL.Query().Get("created_to") != to.Format(time.RFC3339Nano) {
				t.Errorf("feed request=%s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"total":0,"limit":20,"items":[]},"meta":{"request_id":"feed"}}`))
		case 2:
			if r.URL.Path != "/v1/posts/P-21" || r.URL.Query().Get("comments_cursor") != "comment-cursor" ||
				r.URL.Query().Get("comments_limit") != "10" {
				t.Errorf("open request=%s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"post":{"id":"P-21","attachments":[],"state":"open","destination":{"kind":"post","ref":"P-21"}},"comments":{"limit":10,"items":[]}},"meta":{"request_id":"open"}}`))
		case 3:
			if r.URL.Path != "/v1/post-states" || r.Method != http.MethodPost || r.Header.Get("Idempotency-Key") != "state-key" {
				t.Errorf("state request=%s method=%s key=%s", r.URL.String(), r.Method, r.Header.Get("Idempotency-Key"))
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"PS-1","revision":0,"persisted":true},"meta":{"request_id":"state"}}`))
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.BrowsePosts(context.Background(), httpapi.PostFeedQuery{
		Cursor: "feed-cursor", Limit: 20, Search: "retry", Topic: "general",
		Project: "alpha", Kind: "finding", CreatedFrom: &from, CreatedTo: &to,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenPost(context.Background(), httpapi.PostOpenQuery{
		Ref: "P-21", CommentsCursor: "comment-cursor", CommentsLimit: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetPostState(context.Background(), httpapi.PostStateWriteRequest{
		Ref: "P-21", State: "resolved",
	}, "state-key"); err != nil {
		t.Fatal(err)
	}
}
