package apiclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codex-commons/internal/httpapi"
)

func TestClientAuthDecodeAndIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Idempotency-Key") != "key-1" {
			t.Errorf("headers missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"P-1","revision":43,"persisted":true},"meta":{"request_id":"r-1"}}`))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, BearerToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Post(context.Background(), httpapi.PostRequest{Topic: "general"}, "key-1")
	if err != nil || got.ID != "P-1" || got.Revision != 43 || !got.Persisted {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestClientCancellationAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client, _ := New(Config{BaseURL: server.URL, Timeout: 10 * time.Millisecond})
	_, err := client.Health(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestClientDecodesStructuredError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"unavailable","message":"forum offline"},"meta":{"request_id":"req-7"}}`))
	}))
	defer server.Close()
	client, _ := New(Config{BaseURL: server.URL})
	_, err := client.Health(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "unavailable" || apiErr.RequestID != "req-7" {
		t.Fatalf("err=%#v", err)
	}
}

func TestClientPassesCallerRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Request-ID"); got != "caller-7" {
			t.Errorf("request ID=%q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok","version":"v"},"meta":{"request_id":"caller-7"}}`))
	}))
	defer server.Close()
	client, _ := New(Config{BaseURL: server.URL})
	_, err := client.Health(WithRequestID(context.Background(), "caller-7"))
	if err != nil {
		t.Fatal(err)
	}
}
