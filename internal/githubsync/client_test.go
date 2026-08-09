package githubsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	referencedSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	checkSHA      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestSyncReadOnlyHeadersAndSelectiveCommits(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	seen := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != apiVersion || r.Header.Get("User-Agent") == "" {
			t.Error("required GitHub headers missing")
		}
		mu.Lock()
		seen[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("ETag", `"fixture"`)
		switch r.URL.Path {
		case "/repos/acme/widgets":
			io.WriteString(w, `{"id":1,"full_name":"acme/widgets","description":"<script>untrusted</script>","default_branch":"main"}`)
		case "/repos/acme/widgets/issues":
			io.WriteString(w, `[{"id":2,"number":7,"title":"issue","state":"open","user":{"login":"octo"}},{"id":3,"number":8,"title":"PR in issues","pull_request":{}}]`)
		case "/repos/acme/widgets/pulls":
			io.WriteString(w, `[{"id":3,"number":8,"title":"pr","state":"open","head":{"sha":"`+checkSHA+`"},"base":{"ref":"main"}}]`)
		case "/repos/acme/widgets/commits/" + checkSHA + "/check-runs":
			io.WriteString(w, `{"total_count":1,"check_runs":[{"id":9,"name":"test","status":"completed","conclusion":"success"}]}`)
		case "/repos/acme/widgets/commits/" + referencedSHA:
			io.WriteString(w, `{"sha":"`+referencedSHA+`","commit":{"message":"activity commit","author":{"name":"A","email":"a@example.test","date":"2026-01-02T03:04:05Z"}}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := mustClient(t, Config{BaseURL: server.URL, Token: "secret", HTTPClient: server.Client()})
	result, err := client.Sync(context.Background(), Request{Owner: "acme", Repository: "widgets", ReferencedCommits: []string{referencedSHA, referencedSHA}, CheckSHAs: []string{checkSHA}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Untrusted || result.Repository == nil || result.Repository.Description != "<script>untrusted</script>" {
		t.Fatalf("remote text contract lost: %+v", result)
	}
	if len(result.Issues) != 1 || len(result.Issues[0].Items) != 1 || len(result.PullRequests) != 1 || len(result.Checks) != 1 || len(result.Commits) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if seen["/repos/acme/widgets/commits/"+referencedSHA] != 1 {
		t.Fatalf("referenced commit requests = %d", seen["/repos/acme/widgets/commits/"+referencedSHA])
	}
	if seen["/repos/acme/widgets/commits/"+checkSHA] != 0 {
		t.Fatal("PR/check SHA was fetched as commit metadata")
	}
}

func TestConditional304ReturnsTinyReceipt(t *testing.T) {
	t.Parallel()
	validators := map[string]Validator{
		"repository": {ETag: `"repo"`, LastModified: "Wed, 21 Oct 2015 07:28:00 GMT"},
		"issues:1":   {ETag: `"issues"`},
		"pulls:1":    {ETag: `"pulls"`},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "repository"
		if strings.HasSuffix(r.URL.Path, "/issues") {
			key = "issues:1"
		}
		if strings.HasSuffix(r.URL.Path, "/pulls") {
			key = "pulls:1"
		}
		if r.Header.Get("If-None-Match") != validators[key].ETag {
			t.Errorf("If-None-Match for %s = %q", key, r.Header.Get("If-None-Match"))
		}
		if key == "repository" && r.Header.Get("If-Modified-Since") != validators[key].LastModified {
			t.Error("If-Modified-Since missing")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	client := mustClient(t, Config{BaseURL: server.URL, HTTPClient: server.Client()})
	result, err := client.Sync(context.Background(), Request{Owner: "acme", Repository: "widgets", Validators: validators})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Receipt.Unchanged || result.Receipt.Requests != 3 || result.Receipt.NotModified != 3 || result.Receipt.BodyBytes != 0 {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	payload, _ := json.Marshal(result)
	if len(payload) > 100 {
		t.Fatalf("unchanged receipt is %d bytes: %s", len(payload), payload)
	}
}

func TestPaginationIsBoundedAndExplicitlyTruncated(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case strings.HasSuffix(r.URL.Path, "/issues"):
			fmt.Fprintf(w, `[{"id":%s,"number":1,"title":"issue"}]`, r.URL.Query().Get("page"))
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			fmt.Fprintf(w, `[{"id":%s,"number":1,"title":"pr"}]`, r.URL.Query().Get("page"))
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer server.Close()
	client := mustClient(t, Config{BaseURL: server.URL, HTTPClient: server.Client(), PerPage: 1, MaxPages: 2})
	result, err := client.Sync(context.Background(), Request{Owner: "acme", Repository: "widgets"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Receipt.Truncated || requests != 5 || len(result.Issues) != 2 || len(result.PullRequests) != 2 {
		t.Fatalf("result=%+v requests=%d", result.Receipt, requests)
	}
}

func TestCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := mustClient(t, Config{BaseURL: server.URL, HTTPClient: server.Client()})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.Sync(ctx, Request{Owner: "acme", Repository: "widgets"}); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation was not propagated")
	}
}

func TestRateLimitSemantics(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1893456000")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"do not expose me"}`)
	}))
	defer server.Close()
	client := mustClient(t, Config{BaseURL: server.URL, Token: "secret", HTTPClient: server.Client()})
	_, err := client.Sync(context.Background(), Request{Owner: "acme", Repository: "widgets"})
	var rate *RateLimitError
	if !errors.As(err, &rate) || rate.Remaining != 0 || rate.RetryAfter != 7*time.Second || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "expose") {
		t.Fatalf("error = %#v", err)
	}
}

func TestMalformedAndOversizedBodies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body string
		limit      int64
		want       error
	}{
		{name: "malformed", body: `{`, limit: 64, want: ErrMalformed},
		{name: "trailing", body: `{} {}`, limit: 64, want: ErrMalformed},
		{name: "oversized", body: strings.Repeat("x", 65), limit: 64, want: ErrTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, test.body) }))
			defer server.Close()
			client := mustClient(t, Config{BaseURL: server.URL, HTTPClient: server.Client(), MaxBodyBytes: test.limit})
			_, err := client.Sync(context.Background(), Request{Owner: "acme", Repository: "widgets"})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestRejectsUnsafeInputsBeforeNetwork(t *testing.T) {
	t.Parallel()
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("network called"); return nil, nil })
	client := mustClient(t, Config{BaseURL: "https://api.github.test", HTTPClient: &http.Client{Transport: transport}, MaxReferences: 1})
	for _, request := range []Request{
		{Owner: "../evil", Repository: "widgets"},
		{Owner: "acme", Repository: "widgets", ReferencedCommits: []string{"not-a-sha"}},
		{Owner: "acme", Repository: "widgets", ReferencedCommits: []string{referencedSHA, checkSHA}},
	} {
		if _, err := client.Sync(context.Background(), request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("error = %v", err)
		}
	}
}

func BenchmarkUnchangedReceipt(b *testing.B) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotModified, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	client, _ := New(Config{BaseURL: "https://api.github.test", HTTPClient: &http.Client{Transport: transport}})
	request := Request{Owner: "acme", Repository: "widgets", Validators: map[string]Validator{"repository": {ETag: `"r"`}, "issues:1": {ETag: `"i"`}, "pulls:1": {ETag: `"p"`}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := client.Sync(context.Background(), request)
		if err != nil || !result.Receipt.Unchanged {
			b.Fatal(err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustClient(t testing.TB, config Config) *Client {
	t.Helper()
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
