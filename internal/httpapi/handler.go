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
}

type Config struct {
	Credentials     []Credential
	MaxRequestBytes int64
	Version         string
}

type handler struct {
	backend Backend
	config  Config
	serial  atomic.Uint64
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
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewHandler(backend Backend, config Config) http.Handler {
	if config.MaxRequestBytes <= 0 {
		config.MaxRequestBytes = defaultMaxRequestBytes
	}
	if config.Version == "" {
		config.Version = "dev"
	}
	return &handler{backend: backend, config: config}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")

	rid, err := h.requestID(r.Header.Get("X-Request-ID"))
	if err != nil {
		h.writeError(w, rid, http.StatusBadRequest, "bad_request_id", err.Error())
		return
	}
	if r.URL.Path == "/v1/health" {
		h.health(w, r, rid)
		return
	}
	identity, ok := h.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="commons"`)
		h.writeError(w, rid, http.StatusUnauthorized, "unauthorized", "valid configured credential required")
		return
	}
	meta := RequestMeta{Actor: identity.Actor, Session: identity.Session, Host: identity.Host, RequestID: rid, IdempotencyKey: r.Header.Get("Idempotency-Key")}
	if len(meta.IdempotencyKey) > 200 {
		h.writeError(w, rid, http.StatusBadRequest, "bad_idempotency_key", "maximum length is 200")
		return
	}
	h.route(w, r, meta)
}

func (h *handler) route(w http.ResponseWriter, r *http.Request, meta RequestMeta) {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/context/"):
		project, ok := pathPart(path, "/v1/context/")
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
		project, ok := pathPart(path, "/v1/inbox/")
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
		project, ok := pathPart(path, "/v1/search/")
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
		project, ok := pathPart(path, "/v1/next/")
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
		out, err := h.backend.Claim(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, false)
	case r.Method == http.MethodPost && path == "/v1/posts":
		var in PostRequest
		if !h.decode(w, r, meta, &in) {
			return
		}
		if blank(in.Topic) || !validPostKind(in.Kind) || blank(in.Title) || blank(in.Body) || blank(in.Basis) {
			h.badBody(w, meta, "topic, kind, title, body, and basis are required")
			return
		}
		if in.Kind == "topic_request" && in.Topic != "general" {
			h.badBody(w, meta, "topic_request must use the General topic")
			return
		}
		out, err := h.backend.Post(r.Context(), in, meta)
		allowGlobalRevision := in.Topic == "general"
		h.finishWrite(w, meta, out, err, allowGlobalRevision)
	case r.Method == http.MethodPost && path == "/v1/comments":
		var in CommentRequest
		if !h.decode(w, r, meta, &in) {
			return
		}
		if in.Ref == "" || in.Body == "" || in.Basis == "" {
			h.badBody(w, meta, "ref, body, and basis are required")
			return
		}
		out, err := h.backend.Comment(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, false)
	case r.Method == http.MethodPost && path == "/v1/status":
		var in StatusRequest
		if !h.decode(w, r, meta, &in) {
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

func (h *handler) authenticate(r *http.Request) (Credential, bool) {
	bearer := ""
	if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
		bearer = strings.TrimPrefix(value, "Bearer ")
	}
	host := r.Header.Get("X-Commons-Host-Credential")
	for _, c := range h.config.Credentials {
		if (c.BearerToken != "" && secureEqual(bearer, c.BearerToken)) || (c.HostCredential != "" && secureEqual(host, c.HostCredential)) {
			if c.Actor != "" && c.Session != "" && c.Host != "" {
				return c, true
			}
		}
	}
	return Credential{}, false
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

func blank(value string) bool { return strings.TrimSpace(value) == "" }

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

func pathPart(path, prefix string) (string, bool) {
	raw := strings.TrimPrefix(path, prefix)
	if raw == path || raw == "" || strings.Contains(raw, "/") {
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
func (h *handler) write(w http.ResponseWriter, status int, payload envelope) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
