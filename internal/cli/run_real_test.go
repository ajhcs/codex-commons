package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAgentConfig(t *testing.T, baseURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.json")
	payload := `{"base_url":` + strconvQuote(baseURL) + `,"bearer_token":"test-token","default_project":"alpha"}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func strconvQuote(value string) string {
	body, _ := json.Marshal(value)
	return string(body)
}

func TestRealCLIUsesAuthenticatedHTTPAndPersistedAcknowledgement(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/context/alpha":
			_, _ = w.Write([]byte(`{"ok":true,"data":{"project":"alpha","revision":7,"cursor":7,"unchanged":false,"budget":{"requested":800,"used":10,"unit":"estimated_tokens"},"packet":{"purpose":"real"}},"meta":{"request_id":"req-1"}}`))
		case "/v1/posts":
			if r.Header.Get("Idempotency-Key") != "post-1" {
				t.Fatalf("idempotency=%q", r.Header.Get("Idempotency-Key"))
			}
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"P-real","revision":8,"persisted":true},"meta":{"request_id":"req-2"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config := writeAgentConfig(t, server.URL)

	var out, errOut bytes.Buffer
	code := RunContext(context.Background(), []string{"--config", config, "context"}, strings.NewReader(""), &out, &errOut)
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "CONTEXT project=alpha revision=7") {
		t.Fatalf("context code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	out.Reset()
	code = RunContext(context.Background(), []string{"--config", config, "post", "alpha-posts", "question", "--title", "Question", "--body", "-", "--basis", "Observed", "--mention", "human:local-admin", "--request-id", "post-1"}, strings.NewReader("Body from stdin\n"), &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "PERSISTED id=P-real revision=8 persisted=true") {
		t.Fatalf("post code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if received["body"] != "Body from stdin" {
		t.Fatalf("post body=%#v", received["body"])
	}
	mentions, ok := received["mentions"].([]any)
	if !ok || len(mentions) != 1 || mentions[0].(map[string]any)["principal"] != "human:local-admin" {
		t.Fatalf("mentions=%#v", received["mentions"])
	}
}

func TestRealCLIStableConflictExitAndNoFixtureFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"conflict","message":"changed replay"},"meta":{"request_id":"req-c"}}`))
	}))
	defer server.Close()
	config := writeAgentConfig(t, server.URL)
	var out, errOut bytes.Buffer
	code := RunContext(context.Background(), []string{"--config", config, "claim", "T-1", "--request-id", "claim-1"}, strings.NewReader(""), &out, &errOut)
	if code != exitConflict || !strings.Contains(errOut.String(), "ERROR CONFLICT changed replay") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = RunContext(context.Background(), []string{"context", "commons-lab"}, strings.NewReader(""), &out, &errOut)
	if code != exitUsage || strings.Contains(out.String(), "PURPOSE") || !strings.Contains(errOut.String(), "ERROR CONFIG") {
		t.Fatalf("implicit fallback code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
