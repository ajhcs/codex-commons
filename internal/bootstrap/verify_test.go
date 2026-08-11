package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyRequiresExplicitConfiguration(t *testing.T) {
	m := testManifest()
	if _, err := Run(context.Background(), m, Config{Apply: true, Secret: "x"}); err == nil || !strings.Contains(err.Error(), "--base-url") {
		t.Fatalf("base error=%v", err)
	}
	if _, err := Run(context.Background(), m, Config{Apply: true, BaseURL: "http://127.0.0.1:1"}); err == nil || !strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("secret error=%v", err)
	}
}

func TestVerificationRejectsIncompleteCanonicalReads(t *testing.T) {
	tests := []struct {
		name, path string
		mutate     func(map[string]any)
	}{
		{"milestone fields", "/milestones", func(data map[string]any) {
			items := data["items"].([]any)
			items[0].(map[string]any)["title"] = "corrupt"
		}},
		{"dependency IDs", "/v1/tasks/T-t2", func(data map[string]any) { data["task"].(map[string]any)["dependencies"] = []any{} }},
		{"post attachments", "/v1/posts/P-p1", func(data map[string]any) { data["post"].(map[string]any)["attachments"] = []any{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeAPI()
			handler := &corruptHandler{next: fake, path: tt.path, mutate: tt.mutate}
			server := httptest.NewServer(handler)
			defer server.Close()
			receipt, err := Run(context.Background(), testManifest(), Config{Apply: true, BaseURL: server.URL, Secret: "correct"})
			if err == nil || receipt.Verified {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
			if fake.logouts != 1 {
				t.Fatalf("logouts=%d", fake.logouts)
			}
		})
	}
}

type corruptHandler struct {
	next   http.Handler
	path   string
	mutate func(map[string]any)
}

func (h *corruptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, h.path) {
		h.next.ServeHTTP(w, r)
		return
	}
	rec := httptest.NewRecorder()
	h.next.ServeHTTP(rec, r)
	var env map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &env) != nil {
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
		return
	}
	data := env["data"].(map[string]any)
	h.mutate(data)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rec.Code)
	_ = json.NewEncoder(w).Encode(env)
}
