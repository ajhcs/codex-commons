package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

type fakeBackend struct {
	last   RequestMeta
	calls  []string
	outage bool
	global bool
}

func (f *fakeBackend) seen(name string, meta RequestMeta) {
	f.calls = append(f.calls, name)
	f.last = meta
}
func (f *fakeBackend) Health(_ context.Context, meta RequestMeta) (HealthResult, error) {
	f.seen("health", meta)
	return HealthResult{Status: "ok", Version: "slice-2-test"}, nil
}
func (f *fakeBackend) GeneralHome(_ context.Context, q GeneralHomeQuery, meta RequestMeta) (GeneralHomeResult, error) {
	f.seen("general_home", meta)
	out := GeneralHomeResult{}
	out.Navigation.Projects, out.Navigation.People = 2, 1
	out.Presence.Total = 1
	out.NeedsAttention.Total, out.NeedsAttention.Page, out.NeedsAttention.Limit = 1, q.AttentionPage, q.AttentionLimit
	out.RecentActivity.Total, out.RecentActivity.Page, out.RecentActivity.Limit = 1, q.ActivityPage, q.ActivityLimit
	return out, nil
}
func (f *fakeBackend) BrowseAttention(_ context.Context, q AttentionBrowseQuery, meta RequestMeta) (AttentionBrowseResult, error) {
	f.seen("attention", meta)
	return AttentionBrowseResult{Total: 1, Limit: q.Limit}, nil
}
func (f *fakeBackend) BrowseProjects(_ context.Context, q ProjectsBrowseQuery, meta RequestMeta) (ProjectsBrowseResult, error) {
	f.seen("projects", meta)
	return ProjectsBrowseResult{Total: 1, Limit: q.Limit}, nil
}
func (f *fakeBackend) BrowsePeople(_ context.Context, q PeopleBrowseQuery, meta RequestMeta) (PeopleBrowseResult, error) {
	f.seen("people", meta)
	return PeopleBrowseResult{Total: 1, Limit: q.Limit}, nil
}
func (f *fakeBackend) BrowseTopics(_ context.Context, q TopicsQuery, meta RequestMeta) (TopicsResult, error) {
	f.seen("topics", meta)
	return TopicsResult{Items: []TopicItem{{ID: "general", Name: "General"}}}, nil
}
func (f *fakeBackend) Context(_ context.Context, q ContextQuery, meta RequestMeta) (ContextResult, error) {
	f.seen("context", meta)
	unchanged := q.Since != nil && *q.Since == 42
	return ContextResult{Project: q.Project, Revision: 42, Cursor: 42, Unchanged: unchanged, Budget: Budget{Requested: q.Budget, Used: 12, Unit: "estimated_tokens"}, Packet: map[string]any{"purpose": "test"}}, nil
}
func (f *fakeBackend) Who(_ context.Context, q WhoQuery, meta RequestMeta) (WhoResult, error) {
	f.seen("who", meta)
	return WhoResult{Sessions: []PresenceItem{{Session: "S-1", Host: "plumbob", HostConnected: true, Execution: "not_running", LastActivity: "2026-08-09T12:00:00Z", Project: q.Project}}}, nil
}
func (f *fakeBackend) Inbox(_ context.Context, q InboxQuery, meta RequestMeta) (InboxResult, error) {
	f.seen("inbox", meta)
	return InboxResult{Project: q.Project, Unread: 1, Replies: 1, Items: []InboxItem{{ID: "M-3", Kind: "reply", Ref: "P-21"}}}, nil
}
func (f *fakeBackend) Search(_ context.Context, q SearchQuery, meta RequestMeta) (SearchResult, error) {
	f.seen("search", meta)
	return SearchResult{Project: q.Project, Hits: []SearchHit{{Ref: "P-21", Revision: 39, Kind: "finding", Title: q.Query, Timestamp: "2026-08-09T12:00:00Z", Snippet: "bounded discovery"}}}, nil
}
func (f *fakeBackend) Open(_ context.Context, q OpenQuery, meta RequestMeta) (OpenResult, error) {
	f.seen("open", meta)
	if f.outage {
		return OpenResult{}, NewError(CodeUnavailable, "forum unavailable")
	}
	return OpenResult{Ref: q.Ref, Kind: "finding", Revision: 39, Object: map[string]any{"body": "<script>alert(1)</script>"}, Budget: Budget{Requested: q.Budget, Used: 10, Unit: "estimated_tokens"}}, nil
}
func (f *fakeBackend) Next(_ context.Context, q NextQuery, meta RequestMeta) (NextResult, error) {
	f.seen("next", meta)
	return NextResult{Project: q.Project, Tasks: []TaskItem{{ID: "T-102", State: "ready"}}}, nil
}
func writeResult() WriteResult { return WriteResult{ID: "A-1", Revision: 43, Persisted: true} }
func (f *fakeBackend) Claim(_ context.Context, _ ClaimRequest, meta RequestMeta) (WriteResult, error) {
	f.seen("claim", meta)
	return writeResult(), nil
}
func (f *fakeBackend) Post(_ context.Context, _ PostRequest, meta RequestMeta) (WriteResult, error) {
	f.seen("post", meta)
	if f.global {
		return WriteResult{ID: "A-global", Revision: 0, Persisted: true}, nil
	}
	return writeResult(), nil
}
func (f *fakeBackend) Comment(_ context.Context, _ CommentRequest, meta RequestMeta) (WriteResult, error) {
	f.seen("comment", meta)
	return writeResult(), nil
}
func (f *fakeBackend) SetStatus(_ context.Context, _ StatusRequest, meta RequestMeta) (WriteResult, error) {
	f.seen("status", meta)
	return writeResult(), nil
}
func (f *fakeBackend) RequestTopic(_ context.Context, _ TopicRequest, meta RequestMeta) (WriteResult, error) {
	f.seen("topic", meta)
	return WriteResult{ID: "A-global", Revision: 0, Persisted: true}, nil
}

func testHandler(backend Backend, max int64) http.Handler {
	return NewHandler(backend, Config{Credentials: []Credential{{BearerToken: "bearer-secret", HostCredential: "host-secret", Actor: "agent-7", Session: "S-7", Host: "plumbob"}}, MaxRequestBytes: max, Version: "test"})
}
func request(h http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, rec.Body.String())
	}
	return got
}

func TestHealthIsMinimalAndUnauthenticated(t *testing.T) {
	rec := request(testHandler(&fakeBackend{}, 0), http.MethodGet, "/v1/health", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"slice-2-test"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHeadMirrorsGetWithoutResponseBody(t *testing.T) {
	h := testHandler(&fakeBackend{}, 0)
	for _, target := range []string{
		"/v1/health",
		"/v1/projects?limit=10",
		"/v1/posts?limit=10",
		"/v1/projects/commons-lab/overview",
	} {
		t.Run(target, func(t *testing.T) {
			get := request(h, http.MethodGet, target, "", "bearer-secret")
			head := request(h, http.MethodHead, target, "", "bearer-secret")
			if head.Code != get.Code {
				t.Fatalf("HEAD code=%d, GET code=%d", head.Code, get.Code)
			}
			if !reflect.DeepEqual(head.Header(), get.Header()) {
				t.Fatalf("HEAD headers=%v, GET headers=%v", head.Header(), get.Header())
			}
			if head.Body.Len() != 0 {
				t.Fatalf("HEAD body=%q, want empty", head.Body.String())
			}
			if get.Body.Len() == 0 {
				t.Fatal("GET body unexpectedly empty")
			}
		})
	}
}

func TestAuthenticationAndConfiguredHostCredential(t *testing.T) {
	h := testHandler(&fakeBackend{}, 0)
	if got := request(h, http.MethodGet, "/v1/who", "", "bad"); got.Code != http.StatusUnauthorized {
		t.Fatalf("bad token code=%d", got.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/who", nil)
	req.Header.Set("X-Commons-Host-Credential", "host-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("host credential code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCriticalReadAndWriteRoutes(t *testing.T) {
	backend := &fakeBackend{}
	h := testHandler(backend, 0)
	tests := []struct{ method, path, body, marker string }{
		{http.MethodGet, "/v1/home/general?presence_limit=5&attention_limit=5&activity_limit=10", "", `"projects":2`},
		{http.MethodGet, "/v1/context/commons-lab?budget=300", "", `"project":"commons-lab"`},
		{http.MethodGet, "/v1/who?project=commons-lab", "", `"session":"S-1"`},
		{http.MethodGet, "/v1/inbox/commons-lab", "", `"id":"M-3"`},
		{http.MethodGet, "/v1/search/commons-lab?q=context+budget", "", `"ref":"P-21"`},
		{http.MethodGet, "/v1/open?ref=P-21", "", `"kind":"finding"`},
		{http.MethodGet, "/v1/next/commons-lab", "", `"id":"T-102"`},
		{http.MethodPost, "/v1/claims", `{"task":"T-102","lease":"2h"}`, `"persisted":true`},
		{http.MethodPost, "/v1/posts", `{"topic":"commons-lab","kind":"finding","title":"t","body":"b","basis":"e"}`, `"revision":43`},
		{http.MethodPost, "/v1/comments", `{"ref":"P-21","body":"b","intent":"clarify"}`, `"id":"A-1"`},
		{http.MethodPost, "/v1/status", `{"ref":"T-102","status":"done","basis":"e"}`, `"id":"A-1"`},
		{http.MethodPost, "/v1/topic-requests", `{"title":"Atlas","body":"b","basis":"e"}`, `"revision":0`},
	}
	for _, tc := range tests {
		rec := request(h, tc.method, tc.path, tc.body, "bearer-secret")
		if tc.method == http.MethodPost {
			rec = postWithKey(h, tc.path, tc.body, "critical:"+tc.path)
		}
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.marker) {
			t.Errorf("%s %s code=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestGeneralHomeIsAuthenticatedBoundedAndIdentityAttested(t *testing.T) {
	backend := &fakeBackend{}
	h := NewHandler(backend, Config{Credentials: []Credential{
		{BearerToken: "one", Actor: "agent-1", Session: "S-1", Host: "plumbob"},
		{BearerToken: "two", Actor: "agent-2", Session: "S-2", Host: "studio"},
	}})
	if rec := request(h, http.MethodGet, "/v1/home/general", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated code=%d", rec.Code)
	}
	for token, actor := range map[string]string{"one": "agent-1", "two": "agent-2"} {
		rec := request(h, http.MethodGet, "/v1/home/general?presence_limit=3&attention_limit=4&attention_page=2&activity_limit=7&activity_page=1", "", token)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, `"untrusted":true`) ||
			strings.Contains(strings.ToLower(body), "review queue") ||
			strings.Contains(strings.ToLower(body), "background work") {
			t.Fatalf("token=%s code=%d body=%s", token, rec.Code, body)
		}
		if backend.last.Actor != actor || backend.last.Session != "S-"+strings.TrimPrefix(actor, "agent-") {
			t.Fatalf("identity not attested: %+v", backend.last)
		}
	}
	if backend.calls[len(backend.calls)-1] != "general_home" {
		t.Fatalf("calls=%v", backend.calls)
	}
}

func TestGeneralHomeRejectsUnboundedPagination(t *testing.T) {
	backend := &fakeBackend{}
	h := testHandler(backend, 0)
	for _, target := range []string{
		"/v1/home/general?presence_limit=21",
		"/v1/home/general?attention_page=-1",
		"/v1/home/general?activity_limit=0",
	} {
		rec := request(h, http.MethodGet, target, "", "bearer-secret")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("target=%s code=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}

func TestCursorNoChangeAndBudgetMetadata(t *testing.T) {
	rec := request(testHandler(&fakeBackend{}, 0), http.MethodGet, "/v1/context/commons-lab?since=42&budget=300", "", "bearer-secret")
	for _, want := range []string{`"unchanged":true`, `"cursor":42`, `"requested":300`, `"unit":"estimated_tokens"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("missing %s in %s", want, rec.Body.String())
		}
	}
}

func TestSearchIsTimestampedDiscoveryMetadataOnly(t *testing.T) {
	rec := request(testHandler(&fakeBackend{}, 0), http.MethodGet, "/v1/search/commons-lab?q=context+budget", "", "bearer-secret")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"timestamp":"2026-08-09T12:00:00Z"`) || !strings.Contains(body, `"snippet":"bounded discovery"`) {
		t.Fatalf("missing discovery metadata: code=%d body=%s", rec.Code, body)
	}
	if strings.Contains(body, `"body"`) || strings.Contains(body, `"basis"`) {
		t.Fatalf("search leaked canonical fields: %s", body)
	}
}

func TestIdentitySpoofRejectedAndAttestedMetaPassed(t *testing.T) {
	backend := &fakeBackend{}
	h := testHandler(backend, 0)
	rec := request(h, http.MethodPost, "/v1/claims", `{"task":"T-102","actor":"attacker"}`, "bearer-secret")
	if rec.Code != http.StatusBadRequest || len(backend.calls) != 0 {
		t.Fatalf("spoof reached backend: code=%d calls=%v body=%s", rec.Code, backend.calls, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/claims", strings.NewReader(`{"task":"T-102"}`))
	req.Header.Set("Authorization", "Bearer bearer-secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "request-9")
	req.Header.Set("Idempotency-Key", "idem-9")
	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, req)
	if backend.last.Actor != "agent-7" || backend.last.Session != "S-7" || backend.last.Host != "plumbob" || backend.last.RequestID != "request-9" || backend.last.IdempotencyKey != "idem-9" {
		t.Fatalf("meta=%#v", backend.last)
	}
}

func TestTopicRequestsAreGovernanceRoutedToGeneral(t *testing.T) {
	backend := &fakeBackend{}
	h := testHandler(backend, 0)
	rec := postWithKey(h, "/v1/posts", `{"topic":"commons-lab","kind":"topic_request","title":"t","body":"b","basis":"e"}`, "governance-project")
	if rec.Code != http.StatusBadRequest || len(backend.calls) != 0 {
		t.Fatalf("governance bypass reached backend: code=%d calls=%v body=%s", rec.Code, backend.calls, rec.Body.String())
	}
	backend.global = true
	rec = postWithKey(h, "/v1/posts", `{"topic":"general","kind":"topic_request","title":"t","body":"b","basis":"e"}`, "governance-general")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revision":0`) || backend.last.Actor != "agent-7" || backend.last.Session != "S-7" {
		t.Fatalf("attested topic request code=%d meta=%+v body=%s", rec.Code, backend.last, rec.Body.String())
	}
}

func TestGeneralAcceptsNormalCommittedPostsAtGlobalRevisionZero(t *testing.T) {
	backend := &fakeBackend{}
	backend.global = true
	rec := postWithKey(testHandler(backend, 0), "/v1/posts", `{"topic":"general","kind":"finding","title":"shared","body":"b","basis":"e"}`, "general-finding")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revision":0`) {
		t.Fatalf("General finding code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequestSizeLimit(t *testing.T) {
	rec := request(testHandler(&fakeBackend{}, 32), http.MethodPost, "/v1/claims", `{"task":"`+strings.Repeat("x", 64)+`"}`, "bearer-secret")
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"code":"too_large"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestForumOutageAndDeterministicErrors(t *testing.T) {
	backend := &fakeBackend{outage: true}
	h := testHandler(backend, 0)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/open?ref=P-21", nil)
		req.Header.Set("Authorization", "Bearer bearer-secret")
		req.Header.Set("X-Request-ID", "fixed-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "{\"ok\":false,\"error\":{\"code\":\"unavailable\",\"message\":\"forum unavailable\"},\"meta\":{\"request_id\":\"fixed-1\",\"untrusted\":false}}\n" {
			t.Fatalf("unexpected outage response: code=%d body=%q", rec.Code, rec.Body.String())
		}
	}
}

func TestUnexpectedBackendErrorDoesNotLeak(t *testing.T) {
	backend := &errorBackend{fakeBackend: fakeBackend{}}
	rec := request(testHandler(backend, 0), http.MethodGet, "/v1/open?ref=P-21", "", "bearer-secret")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "database password") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

type errorBackend struct{ fakeBackend }

func (e *errorBackend) Open(context.Context, OpenQuery, RequestMeta) (OpenResult, error) {
	return OpenResult{}, errors.New("database password secret")
}

func TestStableBackendErrorMapping(t *testing.T) {
	for code, want := range map[string]int{CodeNotFound: 404, CodeConflict: 409, CodeInvalid: 400, CodeUnavailable: 503, "unknown": 500} {
		if got := statusForCode(code); got != want {
			t.Errorf("code=%s got=%d want=%d", code, got, want)
		}
	}
}

func TestForumTextIsInertAndMarkedUntrusted(t *testing.T) {
	rec := request(testHandler(&fakeBackend{}, 0), http.MethodGet, "/v1/open?ref=P-21", "", "bearer-secret")
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Body.String(), `"untrusted":true`) || strings.Contains(rec.Body.String(), "<script>") {
		t.Fatalf("unsafe response: headers=%v body=%s", rec.Header(), rec.Body.String())
	}
}

type topicStoreBackend struct {
	fakeBackend
	store *commonsstore.Store
}

func (b *topicStoreBackend) RequestTopic(ctx context.Context, in TopicRequest, meta RequestMeta) (WriteResult, error) {
	post, err := b.store.Post(ctx, domain.PostRequest{
		TopicID:   domain.TopicGeneral,
		Kind:      "topic_request",
		Title:     in.Title,
		Body:      in.Body,
		Basis:     in.Basis,
		SessionID: meta.Session,
		RequestID: meta.IdempotencyKey,
	})
	if err != nil {
		return WriteResult{}, err
	}
	return WriteResult{ID: post.ID, Revision: post.Revision, Persisted: true}, nil
}

func TestGlobalTopicRequestRevisionZeroIsSuccessful(t *testing.T) {
	ctx := context.Background()
	s, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "topic.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	h := testHandler(&topicStoreBackend{store: s}, 0)
	req := httptest.NewRequest(http.MethodPost, "/v1/topic-requests", strings.NewReader(`{"title":"Atlas","body":"Please add it","basis":"needed"}`))
	req.Header.Set("Authorization", "Bearer bearer-secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "topic-zero")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revision":0`) || !strings.Contains(rec.Body.String(), `"persisted":true`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}
func (f *fakeBackend) BrowsePosts(_ context.Context, q PostFeedQuery, meta RequestMeta) (PostFeedResult, error) {
	f.seen("posts", meta)
	return PostFeedResult{Total: 1, Limit: q.Limit}, nil
}
func (f *fakeBackend) OpenPost(_ context.Context, q PostOpenQuery, meta RequestMeta) (PostOpenResult, error) {
	f.seen("open_post", meta)
	return PostOpenResult{}, nil
}
func (f *fakeBackend) SetPostState(_ context.Context, _ PostStateWriteRequest, meta RequestMeta) (WriteResult, error) {
	f.seen("post_state", meta)
	return writeResult(), nil
}
