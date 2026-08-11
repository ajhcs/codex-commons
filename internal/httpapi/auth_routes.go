package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *handler) authRoute(w http.ResponseWriter, r *http.Request, rid string) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/session":
		if h.humanAuth == nil {
			h.write(w, http.StatusOK, envelope{OK: true, Data: authSessionResult{Authenticated: false}, Meta: responseMeta{RequestID: rid}})
			return true
		}
		principal, ok := h.authenticateHuman(r)
		if !ok {
			h.write(w, http.StatusOK, envelope{OK: true, Data: authSessionResult{Authenticated: false}, Meta: responseMeta{RequestID: rid}})
			return true
		}
		session := humanSession{csrf: principal.csrfToken}
		h.write(w, http.StatusOK, envelope{OK: true, Data: h.humanAuth.result(&session), Meta: responseMeta{RequestID: rid}})
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/login":
		h.login(w, r, rid)
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/logout":
		h.logout(w, r, rid)
		return true
	case strings.HasPrefix(r.URL.Path, "/v1/auth/"):
		h.writeError(w, rid, http.StatusNotFound, "not_found", "route not found")
		return true
	default:
		return false
	}
}

func (h *handler) login(w http.ResponseWriter, r *http.Request, rid string) {
	if h.humanAuth == nil {
		h.writeError(w, rid, http.StatusServiceUnavailable, "auth_unavailable", "human authentication is not configured")
		return
	}
	if !sameOrigin(r) {
		h.writeError(w, rid, http.StatusForbidden, "origin_forbidden", "same-origin request required")
		return
	}
	if limited, retry := h.humanAuth.rateLimited(); limited {
		seconds := int((retry + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		h.writeError(w, rid, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	}
	meta := RequestMeta{RequestID: rid}
	var input loginRequest
	if !h.decode(w, r, meta, &input) {
		return
	}
	if input.Secret == "" || !h.humanAuth.verifySecret(input.Secret) {
		h.humanAuth.recordFailure()
		h.writeError(w, rid, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}
	token, session, ok := h.humanAuth.create()
	if !ok {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "could not create session")
		return
	}
	setHumanCookie(w, r, token, session.expiresAt, int(h.humanAuth.ttl/time.Second))
	h.write(w, http.StatusOK, envelope{OK: true, Data: h.humanAuth.result(&session), Meta: responseMeta{RequestID: rid}})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request, rid string) {
	principal, ok := h.authenticateHuman(r)
	if !ok {
		h.writeError(w, rid, http.StatusUnauthorized, "unauthorized", "authenticated human session required")
		return
	}
	if !sameOrigin(r) {
		h.writeError(w, rid, http.StatusForbidden, "origin_forbidden", "same-origin request required")
		return
	}
	if !constantTimeTokenEqual(r.Header.Get("X-Commons-CSRF"), principal.csrfToken) {
		h.writeError(w, rid, http.StatusForbidden, "csrf_failed", "valid CSRF token required")
		return
	}
	var body struct{}
	if !h.decode(w, r, RequestMeta{RequestID: rid}, &body) {
		return
	}
	h.humanAuth.remove(principal.cookie)
	setHumanCookie(w, r, "", time.Unix(1, 0), -1)
	h.write(w, http.StatusOK, envelope{OK: true, Data: authSessionResult{Authenticated: false}, Meta: responseMeta{RequestID: rid}})
}

func constantTimeTokenEqual(a, b string) bool {
	aDigest := sha256.Sum256([]byte(a))
	bDigest := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aDigest[:], bDigest[:]) == 1
}
