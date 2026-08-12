package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"codex-commons/internal/codexauth"
)

const (
	defaultMaxRequestBytes  = int64(32 << 10)
	defaultMaxResponseBytes = int64(1 << 20)
)

type Credential struct {
	BearerToken    string
	HostCredential string
	Actor          string
	Session        string
	Host           string
	Project        string
	Purpose        string
}

type Config struct {
	Credentials            []Credential
	MaxRequestBytes        int64
	ExpectedHost           string
	Version                string
	HumanAuth              *HumanAuthConfig
	HumanBindingStore      HumanAccountBindingStore
	HumanAuthEvents        HumanAuthEventStore
	OnHumanIdentityUpdated func(displayName, handle string, revision int64)
	CodexAuth              *CodexAuthConfig
}

type handler struct {
	backend   Backend
	config    Config
	humanAuth *humanAuth
	pairings  *pairingManager
	serial    atomic.Uint64
}

type responseMeta struct {
	RequestID string `json:"request_id"`
	Untrusted bool   `json:"untrusted"`
}

type envelope struct {
	OK    bool          `json:"ok"`
	Data  any           `json:"data,omitempty"`
	Error *errorPayload `json:"error,omitempty"`
	Meta  responseMeta  `json:"meta"`
}

type errorPayload struct {
	Code              string     `json:"code"`
	Message           string     `json:"message"`
	RetryAfterSeconds int        `json:"retry_after_seconds,omitempty"`
	RetryAt           *time.Time `json:"retry_at,omitempty"`
}

func NewHandler(backend Backend, config Config) http.Handler {
	if config.MaxRequestBytes <= 0 {
		config.MaxRequestBytes = defaultMaxRequestBytes
	}
	if config.Version == "" {
		config.Version = "dev"
	}
	h := &handler{backend: backend, config: config, humanAuth: newHumanAuth(config.HumanAuth), pairings: newPairingManager()}
	if config.CodexAuth != nil && config.CodexAuth.Client != nil {
		config.CodexAuth.Client.SetEventHandler(func(event codexauth.Event) {
			if h.humanAuth != nil && event.Kind == "account_updated" {
				h.humanAuth.revokeCodexSessions()
			}
		})
	}
	return h
}

type headResponseWriter struct {
	http.ResponseWriter
}

func (w headResponseWriter) Write(body []byte) (int, error) {
	return len(body), nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w = headResponseWriter{ResponseWriter: w}
		clone := r.Clone(r.Context())
		clone.Method = http.MethodGet
		r = clone
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")

	rid, err := h.requestID(r.Header.Get("X-Request-ID"))
	if err != nil {
		h.writeError(w, rid, http.StatusBadRequest, "bad_request_id", err.Error())
		return
	}
	if h.config.ExpectedHost != "" && !strings.EqualFold(r.Host, h.config.ExpectedHost) {
		h.writeError(w, rid, http.StatusMisdirectedRequest, "misdirected_request", "request Host does not match this Commons listener")
		return
	}
	if h.authRoute(w, r, rid) {
		return
	}
	if r.URL.Path == "/v1/health" {
		h.health(w, r, rid)
		return
	}
	if h.projectArchaeologyGrantRoute(w, r, rid) {
		return
	}
	identity, ok := h.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="commons"`)
		h.writeError(w, rid, http.StatusUnauthorized, "unauthorized", "valid configured credential required")
		return
	}
	meta := RequestMeta{PrincipalKind: identity.Kind, Principal: identity.Principal, Actor: identity.Actor, Session: identity.Session, Host: identity.Host, RequestID: rid, IdempotencyKey: r.Header.Get("Idempotency-Key")}
	if !validIdempotencyKey(meta.IdempotencyKey) {
		h.writeError(w, rid, http.StatusBadRequest, "bad_idempotency_key", "must be at most 200 visible ASCII characters without surrounding whitespace")
		return
	}
	if identity.Kind == "human" && r.Method != http.MethodGet && r.Method != http.MethodHead {
		if !isHumanWritePath(r.URL.Path) {
			h.writeError(w, rid, http.StatusForbidden, "forbidden", "human session cannot use this route")
			return
		}
		if !sameOrigin(r) {
			h.writeError(w, rid, http.StatusForbidden, "origin_forbidden", "same-origin request required")
			return
		}
		if !constantTimeTokenEqual(r.Header.Get("X-Commons-CSRF"), identity.csrfToken) {
			h.writeError(w, rid, http.StatusForbidden, "csrf_failed", "valid CSRF token required")
			return
		}
		if meta.IdempotencyKey == "" {
			h.writeError(w, rid, http.StatusBadRequest, "bad_idempotency_key", "Idempotency-Key required")
			return
		}
	}
	h.route(w, r, meta)
}

func (h *handler) route(w http.ResponseWriter, r *http.Request, meta RequestMeta) {
	if h.projectArchaeologyRoute(w, r, meta) {
		return
	}
	if h.projectCoreRoute(w, r, meta) {
		return
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/v1/notifications":
		limit, err := intParam(r.URL.Query().Get("limit"), 20, 1, 50)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		unread := r.URL.Query().Get("unread")
		if unread != "" && unread != "true" && unread != "false" {
			h.badQuery(w, meta, errors.New("unread must be true or false"))
			return
		}
		notificationBackend, ok := h.backend.(NotificationBackend)
		if !ok {
			h.finish(w, meta, nil, NewError(CodeUnavailable, "notifications unavailable"), false)
			return
		}
		out, err := notificationBackend.Notifications(r.Context(), NotificationListQuery{Cursor: r.URL.Query().Get("cursor"), UnreadOnly: unread == "true", Limit: limit}, meta)
		h.finish(w, meta, out, err, false)
	case r.Method == http.MethodPost && path == "/v1/notification-reads":
		var in NotificationReadRequest
		if !h.decode(w, r, meta, &in) {
			return
		}
		if blank(in.ID) {
			h.badBody(w, meta, "id is required")
			return
		}
		notificationBackend, ok := h.backend.(NotificationBackend)
		if !ok {
			h.finish(w, meta, nil, NewError(CodeUnavailable, "notifications unavailable"), false)
			return
		}
		out, err := notificationBackend.MarkNotificationRead(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, true)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/comments/"):
		id, ok := pathPart(r.URL, "/v1/comments/")
		if !ok {
			h.notFound(w, meta)
			return
		}
		commentBackend, ok := h.backend.(CommentReadBackend)
		if !ok {
			h.finish(w, meta, nil, NewError(CodeUnavailable, "comment open unavailable"), true)
			return
		}
		out, err := commentBackend.OpenComment(r.Context(), CommentOpenQuery{ID: id}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && path == "/v1/home/general":
		query, err := parseHomeQuery(r.URL.Query())
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.GeneralHome(r.Context(), query, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && path == "/v1/attention":
		query, err := parseAttentionBrowseQuery(r.URL.Query())
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.BrowseAttention(r.Context(), query, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && path == "/v1/projects":
		query, err := parseProjectsBrowseQuery(r.URL.Query())
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.BrowseProjects(r.Context(), query, meta)
		h.finish(w, meta, out, err, false)
	case r.Method == http.MethodGet && path == "/v1/people":
		query, err := parsePeopleBrowseQuery(r.URL.Query())
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.BrowsePeople(r.Context(), query, meta)
		h.finish(w, meta, out, err, false)
	case r.Method == http.MethodGet && path == "/v1/contributors":
		limit, err := intParam(r.URL.Query().Get("limit"), 10, 1, 20)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		search := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(search) > 100 {
			h.badQuery(w, meta, errors.New("q maximum length is 100"))
			return
		}
		backend, ok := h.backend.(AddressabilityBackend)
		if !ok {
			h.finish(w, meta, nil, NewError(CodeUnavailable, "contributors unavailable"), true)
			return
		}
		out, err := backend.LookupContributors(r.Context(), ContributorLookupQuery{Cursor: r.URL.Query().Get("cursor"), Search: search, Project: r.URL.Query().Get("project"), Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && path == "/v1/topics":
		limit, err := intParam(r.URL.Query().Get("limit"), 100, 1, 100)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.BrowseTopics(r.Context(), TopicsQuery{Limit: limit}, meta)
		h.finish(w, meta, out, err, false)
	case r.Method == http.MethodGet && path == "/v1/posts":
		query, err := parsePostFeedQuery(r.URL.Query())
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.BrowsePosts(r.Context(), query, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/posts/"):
		ref, ok := pathPart(r.URL, "/v1/posts/")
		if !ok {
			h.notFound(w, meta)
			return
		}
		limit, err := intParam(r.URL.Query().Get("comments_limit"), 10, 1, 20)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.OpenPost(r.Context(), PostOpenQuery{Ref: ref, CommentsCursor: r.URL.Query().Get("comments_cursor"), CommentsLimit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/projects/"):
		project, ok := projectOverviewPath(r.URL)
		if !ok {
			h.notFound(w, meta)
			return
		}
		attentionLimit, err := intParam(r.URL.Query().Get("attention_limit"), 5, 1, 20)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		workLimit, err := intParam(r.URL.Query().Get("work_limit"), 5, 1, 20)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.ProjectOverview(r.Context(), ProjectOverviewQuery{
			Project: project, AttentionLimit: attentionLimit, WorkLimit: workLimit,
		}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/context/"):
		project, ok := pathPart(r.URL, "/v1/context/")
		if !ok {
			h.notFound(w, meta)
			return
		}
		since, err := optionalInt64(r.URL.Query().Get("since"), 0, 1<<63-1)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		budget, err := intParam(r.URL.Query().Get("budget"), 800, 100, 2000)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.Context(r.Context(), ContextQuery{Project: project, Since: since, Budget: budget}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && path == "/v1/who":
		limit, err := intParam(r.URL.Query().Get("limit"), 5, 1, 20)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		state := r.URL.Query().Get("state")
		if state == "" {
			state = "active"
		}
		if !validPresenceState(state) {
			h.badQuery(w, meta, errors.New("state must be active, live, idle, inactive, or all"))
			return
		}
		out, err := h.backend.Who(r.Context(), WhoQuery{Project: r.URL.Query().Get("project"), State: state, Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/inbox/"):
		project, ok := pathPart(r.URL, "/v1/inbox/")
		if !ok {
			h.notFound(w, meta)
			return
		}
		limit, err := intParam(r.URL.Query().Get("limit"), 5, 1, 20)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.Inbox(r.Context(), InboxQuery{Project: project, Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/search/"):
		project, ok := pathPart(r.URL, "/v1/search/")
		if !ok {
			h.notFound(w, meta)
			return
		}
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			h.badQuery(w, meta, errors.New("q is required"))
			return
		}
		limit, err := intParam(r.URL.Query().Get("limit"), 5, 1, 10)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.Search(r.Context(), SearchQuery{Project: project, Query: query, Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && path == "/v1/open":
		ref := strings.TrimSpace(r.URL.Query().Get("ref"))
		if ref == "" {
			h.badQuery(w, meta, errors.New("ref is required"))
			return
		}
		budget, err := intParam(r.URL.Query().Get("budget"), 600, 100, 2000)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.Open(r.Context(), OpenQuery{Ref: ref, Budget: budget}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/next/"):
		project, ok := pathPart(r.URL, "/v1/next/")
		if !ok {
			h.notFound(w, meta)
			return
		}
		limit, err := intParam(r.URL.Query().Get("limit"), 1, 1, 10)
		if err != nil {
			h.badQuery(w, meta, err)
			return
		}
		out, err := h.backend.Next(r.Context(), NextQuery{Project: project, Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodPost && path == "/v1/claims":
		var in ClaimRequest
		if !h.decode(w, r, meta, &in) {
			return
		}
		if strings.TrimSpace(in.Task) == "" {
			h.badBody(w, meta, "task is required")
			return
		}
		if meta.IdempotencyKey == "" {
			h.badBody(w, meta, "Idempotency-Key required")
			return
		}
		out, err := h.backend.Claim(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, false)
	case r.Method == http.MethodPost && path == "/v1/posts":
		var in PostRequest
		if meta.IdempotencyKey == "" {
			h.badBody(w, meta, "Idempotency-Key required")
			return
		}
		if !h.decode(w, r, meta, &in) {
			return
		}
		if blank(in.Topic) || !validPostKind(in.Kind) || blank(in.Title) || blank(in.Body) || blank(in.Basis) {
			h.badBody(w, meta, "topic, kind, title, body, and basis are required")
			return
		}
		if !validMentionRequests(in.Mentions) {
			h.badBody(w, meta, "mentions require at most 5 deduplicated structured principals")
			return
		}
		if len(in.Attachments) > 8 {
			h.badBody(w, meta, "at most 8 attachments are allowed")
			return
		}
		for _, attachment := range in.Attachments {
			if !validPostAttachment(attachment) {
				h.badBody(w, meta, "attachments require a supported kind and HTTPS URL")
				return
			}
		}
		if in.Kind == "topic_request" && in.Topic != "general" {
			h.badBody(w, meta, "topic_request must use the General topic")
			return
		}
		out, err := h.backend.Post(r.Context(), in, meta)
		allowGlobalRevision := in.Topic == "general"
		h.finishWrite(w, meta, out, err, allowGlobalRevision)
	case r.Method == http.MethodPost && path == "/v1/post-states":
		var in PostStateWriteRequest
		if !h.decode(w, r, meta, &in) {
			return
		}
		if meta.IdempotencyKey == "" {
			h.badBody(w, meta, "Idempotency-Key required")
			return
		}
		if in.Ref == "" || (in.State != "open" && in.State != "resolved" && in.State != "superseded") ||
			(in.State == "superseded") != (in.SupersededBy != "") || in.Ref == in.SupersededBy {
			h.badBody(w, meta, "valid ref, state, and superseded_by are required")
			return
		}
		out, err := h.backend.SetPostState(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, true)
	case r.Method == http.MethodPost && path == "/v1/post-perspective-scopes":
		var in PerspectiveScopeWriteRequest
		if !h.decode(w, r, meta, &in) {
			return
		}
		if in.Ref == "" || (in.Scope != "closed" && in.Scope != "project" && in.Scope != "commons") || in.BaseRevision < 0 {
			h.badBody(w, meta, "valid ref, scope, and base_revision are required")
			return
		}
		backend, ok := h.backend.(AddressabilityBackend)
		if !ok {
			h.finishWrite(w, meta, WriteResult{}, NewError(CodeUnavailable, "perspective scope unavailable"), true)
			return
		}
		if meta.IdempotencyKey == "" {
			h.badBody(w, meta, "Idempotency-Key required")
			return
		}
		out, err := backend.SetPerspectiveScope(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, true)
	case r.Method == http.MethodPost && path == "/v1/comments":
		var in CommentRequest
		if meta.IdempotencyKey == "" {
			h.badBody(w, meta, "Idempotency-Key required")
			return
		}
		if !h.decode(w, r, meta, &in) {
			return
		}
		if !validMentionRequests(in.Mentions) {
			h.badBody(w, meta, "mentions require at most 5 deduplicated structured principals")
			return
		}
		if in.Ref == "" || in.Body == "" || !validCommentIntent(in.Intent) {
			h.badBody(w, meta, "ref, body, and valid intent are required")
			return
		}
		out, err := h.backend.Comment(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, true)
	case r.Method == http.MethodPost && path == "/v1/status":
		var in StatusRequest
		if !h.decode(w, r, meta, &in) {
			return
		}
		if meta.IdempotencyKey == "" {
			h.badBody(w, meta, "Idempotency-Key required")
			return
		}
		if in.Ref == "" || in.Status == "" || in.Basis == "" {
			h.badBody(w, meta, "ref, status, and basis are required")
			return
		}
		out, err := h.backend.SetStatus(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, false)
	case r.Method == http.MethodPost && path == "/v1/topic-requests":
		var in TopicRequest
		if meta.IdempotencyKey == "" {
			h.badBody(w, meta, "Idempotency-Key required")
			return
		}
		if !h.decode(w, r, meta, &in) {
			return
		}
		if in.Title == "" || in.Body == "" || in.Basis == "" {
			h.badBody(w, meta, "title, body, and basis are required")
			return
		}
		out, err := h.backend.RequestTopic(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, true)
	default:
		h.notFound(w, meta)
	}
}

func (h *handler) health(w http.ResponseWriter, r *http.Request, rid string) {
	if r.Method != http.MethodGet {
		h.writeError(w, rid, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	if h.backend == nil {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "backend unavailable")
		return
	}
	out, err := h.backend.Health(r.Context(), RequestMeta{RequestID: rid})
	if out.Version == "" {
		out.Version = h.config.Version
	}
	h.finish(w, RequestMeta{RequestID: rid}, out, err, false)
}

func (h *handler) authenticate(r *http.Request) (authPrincipal, bool) {
	bearer := ""
	if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
		bearer = strings.TrimPrefix(value, "Bearer ")
	}
	host := r.Header.Get("X-Commons-Host-Credential")
	for _, c := range h.config.Credentials {
		if (c.BearerToken != "" && secureEqual(bearer, c.BearerToken)) || (c.HostCredential != "" && secureEqual(host, c.HostCredential)) {
			if c.Actor != "" && c.Session != "" && c.Host != "" {
				return authPrincipal{Credential: c, Kind: "agent", Principal: c.Session}, true
			}
		}
	}
	return h.authenticateHuman(r)
}

func (h *handler) authenticateHuman(r *http.Request) (authPrincipal, bool) {
	if h.humanAuth == nil {
		return authPrincipal{}, false
	}
	cookie, err := r.Cookie(humanSessionCookie)
	if err != nil {
		return authPrincipal{}, false
	}
	session, ok := h.humanAuth.lookup(cookie.Value)
	if !ok {
		return authPrincipal{}, false
	}
	principal := h.humanAuth.principal()
	principal.csrfToken = session.csrf
	principal.cookie = cookie.Value
	principal.authMethod = session.authMethod
	principal.bindingRevision = session.bindingRevision
	return principal, true
}

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (h *handler) decode(w http.ResponseWriter, r *http.Request, meta RequestMeta, dst any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		h.writeError(w, meta.RequestID, http.StatusUnsupportedMediaType, "content_type", "application/json required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.config.MaxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.decodeError(w, meta, err)
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		h.badBody(w, meta, "one JSON value required")
		return false
	}
	return true
}

func (h *handler) decodeError(w http.ResponseWriter, meta RequestMeta, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		h.writeError(w, meta.RequestID, http.StatusRequestEntityTooLarge, "too_large", "request body exceeds limit")
		return
	}
	h.badBody(w, meta, "invalid JSON")
}

func (h *handler) finishWrite(w http.ResponseWriter, meta RequestMeta, data WriteResult, err error, allowGlobalRevision bool) {
	if err == nil && (data.ID == "" || data.Revision < 0 || (!allowGlobalRevision && data.Revision == 0) || !data.Persisted) {
		err = errors.New("backend returned uncommitted write acknowledgement")
	}
	h.finish(w, meta, data, err, false)
}

func validPresenceState(state string) bool {
	switch state {
	case "active", "live", "idle", "inactive", "all":
		return true
	default:
		return false
	}
}

func validPostKind(kind string) bool {
	switch kind {
	case "finding", "question", "notice", "decision", "topic_request":
		return true
	default:
		return false
	}
}

func validMentionRequests(items []MentionRequest) bool {
	if len(items) > 20 {
		return false
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if (item.Principal == "") == (item.Session == "") {
			return false
		}
		value := item.Principal
		if value == "" {
			value = item.Session
		}
		if len(value) > 200 || strings.TrimSpace(value) != value {
			return false
		}
		seen[value] = struct{}{}
		if len(seen) > 5 {
			return false
		}
	}
	return true
}

func validCommentIntent(intent string) bool {
	switch intent {
	case "answer", "add_evidence", "challenge", "clarify":
		return true
	default:
		return false
	}
}

func blank(value string) bool { return strings.TrimSpace(value) == "" }
func validIdempotencyKey(value string) bool {
	if len(value) > 200 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func (h *handler) finish(w http.ResponseWriter, meta RequestMeta, data any, err error, untrusted bool) {
	if err == nil {
		h.write(w, http.StatusOK, envelope{OK: true, Data: data, Meta: responseMeta{RequestID: meta.RequestID, Untrusted: untrusted}})
		return
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		status := statusForCode(apiErr.Code)
		code := apiErr.Code
		if code == "" {
			code = "internal"
		}
		message := apiErr.Message
		if message == "" {
			message = "request failed"
		}
		h.writeError(w, meta.RequestID, status, code, message)
		return
	}
	h.writeError(w, meta.RequestID, http.StatusInternalServerError, "internal", "request failed")
}

func statusForCode(code string) int {
	switch code {
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeInvalid:
		return http.StatusBadRequest
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func (h *handler) requestID(value string) (string, error) {
	next := func() string { return fmt.Sprintf("req-%d", h.serial.Add(1)) }
	if value == "" {
		return next(), nil
	}
	if len(value) > 128 || strings.TrimSpace(value) != value {
		return next(), errors.New("request ID must be 1..128 non-space-bounded characters")
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return next(), errors.New("request ID contains invalid characters")
		}
	}
	return value, nil
}

func pathPart(requestURL *url.URL, prefix string) (string, bool) {
	if requestURL == nil {
		return "", false
	}
	escaped := requestURL.EscapedPath()
	raw := strings.TrimPrefix(escaped, prefix)
	if raw == escaped || raw == "" || strings.Contains(raw, "/") {
		return "", false
	}
	value, err := url.PathUnescape(raw)
	return value, err == nil && value != ""
}

func optionalInt64(raw string, min, max int64) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < min || v > max {
		return nil, fmt.Errorf("integer must be in %d..%d", min, max)
	}
	return &v, nil
}

func intParam(raw string, fallback, min, max int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < min || v > max {
		return 0, fmt.Errorf("integer must be in %d..%d", min, max)
	}
	return v, nil
}

func parseHomeQuery(values url.Values) (GeneralHomeQuery, error) {
	presenceLimit, err := intParam(values.Get("presence_limit"), 5, 1, 20)
	if err != nil {
		return GeneralHomeQuery{}, err
	}
	attentionLimit, err := intParam(values.Get("attention_limit"), 5, 1, 20)
	if err != nil {
		return GeneralHomeQuery{}, err
	}
	attentionPage, err := intParam(values.Get("attention_page"), 0, 0, 500)
	if err != nil {
		return GeneralHomeQuery{}, err
	}
	activityLimit, err := intParam(values.Get("activity_limit"), 10, 1, 20)
	if err != nil {
		return GeneralHomeQuery{}, err
	}
	activityPage, err := intParam(values.Get("activity_page"), 0, 0, 500)
	if err != nil {
		return GeneralHomeQuery{}, err
	}
	return GeneralHomeQuery{PresenceLimit: presenceLimit, AttentionLimit: attentionLimit,
		AttentionPage: attentionPage, ActivityLimit: activityLimit, ActivityPage: activityPage}, nil
}

func optionalTimeParam(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, errors.New("timestamp must be RFC3339")
	}
	value = value.UTC()
	return &value, nil
}

func parseAttentionBrowseQuery(values url.Values) (AttentionBrowseQuery, error) {
	limit, err := intParam(values.Get("limit"), 25, 1, 100)
	if err != nil {
		return AttentionBrowseQuery{}, err
	}
	from, err := optionalTimeParam(values.Get("updated_from"))
	if err != nil {
		return AttentionBrowseQuery{}, err
	}
	to, err := optionalTimeParam(values.Get("updated_to"))
	if err != nil {
		return AttentionBrowseQuery{}, err
	}
	if from != nil && to != nil && from.After(*to) {
		return AttentionBrowseQuery{}, errors.New("updated_from must not be after updated_to")
	}
	search := strings.TrimSpace(values.Get("q"))
	if len(search) > 200 {
		return AttentionBrowseQuery{}, errors.New("q maximum length is 200")
	}
	return AttentionBrowseQuery{Cursor: values.Get("cursor"), Search: search, Limit: limit,
		Source: values.Get("source"), Owner: values.Get("owner"),
		Severity: values.Get("severity"), Project: values.Get("project"),
		UpdatedFrom: from, UpdatedTo: to}, nil
}

func parseProjectsBrowseQuery(values url.Values) (ProjectsBrowseQuery, error) {
	limit, err := intParam(values.Get("limit"), 25, 1, 100)
	if err != nil {
		return ProjectsBrowseQuery{}, err
	}
	return ProjectsBrowseQuery{Cursor: values.Get("cursor"), Search: strings.TrimSpace(values.Get("q")), Limit: limit}, nil
}

func parsePeopleBrowseQuery(values url.Values) (PeopleBrowseQuery, error) {
	limit, err := intParam(values.Get("limit"), 25, 1, 100)
	if err != nil {
		return PeopleBrowseQuery{}, err
	}
	var connected *bool
	if raw := values.Get("host_connected"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return PeopleBrowseQuery{}, errors.New("host_connected must be true or false")
		}
		connected = &value
	}
	return PeopleBrowseQuery{Cursor: values.Get("cursor"), Search: strings.TrimSpace(values.Get("q")),
		Project: values.Get("project"), Execution: values.Get("execution"), Host: values.Get("host"),
		HostConnected: connected, Limit: limit}, nil
}

func projectOverviewPath(requestURL *url.URL) (string, bool) {
	const prefix, suffix = "/v1/projects/", "/overview"
	if requestURL == nil {
		return "", false
	}
	escaped := requestURL.EscapedPath()
	if !strings.HasPrefix(escaped, prefix) || !strings.HasSuffix(escaped, suffix) {
		return "", false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(escaped, prefix), suffix)
	if raw == "" || strings.Contains(raw, "/") {
		return "", false
	}
	value, err := url.PathUnescape(raw)
	return value, err == nil && value != ""
}

func (h *handler) badQuery(w http.ResponseWriter, meta RequestMeta, err error) {
	h.writeError(w, meta.RequestID, http.StatusBadRequest, "bad_query", err.Error())
}
func (h *handler) badBody(w http.ResponseWriter, meta RequestMeta, message string) {
	h.writeError(w, meta.RequestID, http.StatusBadRequest, "bad_body", message)
}
func (h *handler) notFound(w http.ResponseWriter, meta RequestMeta) {
	h.writeError(w, meta.RequestID, http.StatusNotFound, "not_found", "route not found")
}
func (h *handler) writeError(w http.ResponseWriter, rid string, status int, code, message string) {
	h.write(w, status, envelope{OK: false, Error: &errorPayload{Code: code, Message: message}, Meta: responseMeta{RequestID: rid}})
}
func (h *handler) writeCooldownError(w http.ResponseWriter, rid, code, message string, retry time.Duration) {
	seconds := int((retry + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	retryAt := h.pairings.now().UTC().Add(time.Duration(seconds) * time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	h.write(w, http.StatusTooManyRequests, envelope{OK: false, Error: &errorPayload{Code: code, Message: message, RetryAfterSeconds: seconds, RetryAt: &retryAt}, Meta: responseMeta{RequestID: rid}})
}
func (h *handler) write(w http.ResponseWriter, status int, payload envelope) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
