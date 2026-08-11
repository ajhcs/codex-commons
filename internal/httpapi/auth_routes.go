package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"time"

	"codex-commons/internal/domain"
)

func (h *handler) login(w http.ResponseWriter, r *http.Request, rid string) {
	if h.humanAuth == nil || !h.humanAuth.recoveryEnabled || h.humanAuth.secretDigest == [sha256.Size]byte{} {
		h.writeError(w, rid, http.StatusNotFound, "recovery_disabled", "Recovery login is not enabled")
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
	bindingRevision := int64(0)
	if binding, err := h.binding(r.Context()); err == nil {
		bindingRevision = binding.Revision
	} else if !errors.Is(err, domain.ErrNotFound) {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Commons authentication is temporarily unavailable")
		return
	}
	auditKey, ok := randomAuthToken()
	if !ok {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Commons authentication is temporarily unavailable")
		return
	}
	if events, ok := h.config.HumanAuthEvents.(HumanAuthEventStore); ok {
		if err := events.RecordHumanAuthEvent(r.Context(), domain.HumanAuthEventRequest{Principal: domain.HumanLocalPrincipal, EventType: "recovery_login", BindingRevision: bindingRevision, IdempotencyKey: "recovery:" + auditKey}); err != nil {
			h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Commons authentication is temporarily unavailable")
			return
		}
	}
	token, session, ok := h.humanAuth.create("recovery", bindingRevision)
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
