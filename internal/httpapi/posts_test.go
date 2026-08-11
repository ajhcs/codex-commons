package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postWithKey(handler http.Handler, target, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer bearer-secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestPostsRoutesAreBoundedExplicitAndUntrusted(t *testing.T) {
	backend := &fakeBackend{}
	handler := testHandler(backend, 0)
	for _, target := range []string{
		"/v1/posts?limit=20&q=retry&topic=general&kind=finding",
		"/v1/posts/P-21?comments_limit=10",
	} {
		rec := request(handler, http.MethodGet, target, "", "bearer-secret")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"untrusted":true`) {
			t.Fatalf("target=%s code=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
	if strings.Join(backend.calls, ",") != "posts,open_post" {
		t.Fatalf("calls=%v", backend.calls)
	}

	for _, target := range []string{
		"/v1/posts?limit=51",
		"/v1/posts?kind=poll",
		"/v1/posts?created_from=2026-08-10T00:00:00Z&created_to=2026-08-09T00:00:00Z",
		"/v1/posts/P-21?comments_limit=21",
	} {
		rec := request(handler, http.MethodGet, target, "", "bearer-secret")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid target=%s code=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}

func TestPostAttachmentAndStateValidation(t *testing.T) {
	handler := testHandler(&fakeBackend{}, 0)
	valid := `{"topic":"general","kind":"finding","title":"t","body":"b","basis":"e","attachments":[{"kind":"github","url":"https://github.com/openai/codex","title":"Codex"}]}`
	missing := request(handler, http.MethodPost, "/v1/posts", valid, "bearer-secret")
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "Idempotency-Key") {
		t.Fatalf("post without key code=%d body=%s", missing.Code, missing.Body.String())
	}
	rec := postWithKey(handler, "/v1/posts", valid, "post-attachment")
	if rec.Code != http.StatusOK {
		t.Fatalf("valid attachment code=%d body=%s", rec.Code, rec.Body.String())
	}
	invalid := `{"topic":"general","kind":"finding","title":"t","body":"b","basis":"e","attachments":[{"kind":"link","url":"http://example.com"}]}`
	rec = postWithKey(handler, "/v1/posts", invalid, "post-attachment-invalid")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid attachment code=%d body=%s", rec.Code, rec.Body.String())
	}

	state := `{"ref":"P-21","state":"resolved"}`
	rec = request(handler, http.MethodPost, "/v1/post-states", state, "bearer-secret")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Idempotency-Key") {
		t.Fatalf("state without key code=%d body=%s", rec.Code, rec.Body.String())
	}
	keyed := postWithKey(handler, "/v1/post-states", state, "state-1")
	if keyed.Code != http.StatusOK || !strings.Contains(keyed.Body.String(), `"persisted":true`) {
		t.Fatalf("state code=%d body=%s", keyed.Code, keyed.Body.String())
	}
	bad := postWithKey(handler, "/v1/post-states", `{"ref":"P-21","state":"superseded"}`, "state-2")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad supersession code=%d body=%s", bad.Code, bad.Body.String())
	}
}

func TestCanonicalContentWritesRequireIdempotencyKey(t *testing.T) {
	handler := testHandler(&fakeBackend{}, 0)
	for _, tc := range []struct {
		path string
		body string
	}{
		{path: "/v1/comments", body: `{"ref":"P-21","body":"b","intent":"clarify"}`},
		{path: "/v1/topic-requests", body: `{"title":"Atlas","body":"b","basis":"e"}`},
	} {
		rec := request(handler, http.MethodPost, tc.path, tc.body, "bearer-secret")
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Idempotency-Key") {
			t.Fatalf("path=%s code=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}
	invalid := postWithKey(handler, "/v1/comments", `{"ref":"P-21","body":"b","intent":"clarify"}`, " surrounding ")
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "bad_idempotency_key") {
		t.Fatalf("invalid key code=%d body=%s", invalid.Code, invalid.Body.String())
	}
}
