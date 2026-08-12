package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"codex-commons/internal/domain"
)

type pairingLimitError struct {
	code  string
	retry time.Duration
}

func (e *pairingLimitError) Error() string { return e.code }

const (
	pairingCookieName = "commons_pairing"
	pairingCookiePath = "/v1/auth/codex"
	pairingTTL        = 10 * time.Minute
	maxPairings       = 4
	maxPairingPolls   = 60
	minPollInterval   = time.Second
)

type pairingState string

const (
	pairingCreated      pairingState = "created"
	pairingWaiting      pairingState = "waiting_for_user"
	pairingNeedsProfile pairingState = "needs_profile"
	pairingCompleted    pairingState = "completed"
	pairingCancelled    pairingState = "cancelled"
	pairingExpired      pairingState = "expired"
	pairingFailed       pairingState = "failed"
)

type pairingAttempt struct {
	id              string
	cookieHash      string
	loginID         string
	verificationURL string
	userCode        string
	createdAt       time.Time
	expiresAt       time.Time
	state           pairingState
	digest          [32]byte
	hasDigest       bool
	errorCode       string
	polls           int
	lastPoll        time.Time
	polling         bool
}

type pairingManager struct {
	mu       sync.Mutex
	now      func() time.Time
	attempts map[string]*pairingAttempt
	byCookie map[string]string
	starts   map[string][]time.Time
}

func newPairingManager() *pairingManager {
	return &pairingManager{now: time.Now, attempts: make(map[string]*pairingAttempt), byCookie: make(map[string]string), starts: make(map[string][]time.Time)}
}

func (m *pairingManager) cleanupLocked(now time.Time) {
	for id, attempt := range m.attempts {
		if !attempt.expiresAt.After(now) {
			delete(m.attempts, id)
			if m.byCookie[attempt.cookieHash] == id {
				delete(m.byCookie, attempt.cookieHash)
			}
		}
	}
	cutoff := now.Add(-10 * time.Minute)
	for remote, entries := range m.starts {
		kept := entries[:0]
		for _, entry := range entries {
			if entry.After(cutoff) {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			delete(m.starts, remote)
		} else {
			m.starts[remote] = kept
		}
	}
}

func (m *pairingManager) reserve(cookieHash, remote string) (*pairingAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	m.cleanupLocked(now)
	if id := m.byCookie[cookieHash]; id != "" {
		return nil, domain.ErrConflict
	}
	if len(m.attempts) >= maxPairings {
		return nil, &pairingLimitError{code: "pairing_capacity_limited", retry: time.Minute}
	}
	entries := m.starts[remote]
	if len(entries) >= 3 {
		retry := entries[0].Add(10 * time.Minute).Sub(now)
		return nil, &pairingLimitError{code: "auth_start_limited", retry: retry}
	}
	id, ok := randomAuthToken()
	if !ok {
		return nil, errors.New("secure randomness unavailable")
	}
	attempt := &pairingAttempt{id: id, cookieHash: cookieHash, createdAt: now, expiresAt: now.Add(pairingTTL), state: pairingCreated}
	m.attempts[id] = attempt
	m.byCookie[cookieHash] = id
	m.starts[remote] = append(entries, now)
	snapshot := clonePairing(attempt)
	return &snapshot, nil
}

func (m *pairingManager) attachLogin(id, loginID, verificationURL, userCode string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	attempt := m.attempts[id]
	if attempt == nil || attempt.state != pairingCreated || loginID == "" || verificationURL == "" || userCode == "" {
		return false
	}
	attempt.loginID = loginID
	attempt.verificationURL = verificationURL
	attempt.userCode = userCode
	attempt.state = pairingWaiting
	return true
}

func (m *pairingManager) activeForCookie(cookieHash string) (pairingAttempt, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	m.cleanupLocked(now)
	id := m.byCookie[cookieHash]
	attempt := m.attempts[id]
	if attempt == nil || attempt.verificationURL == "" || attempt.userCode == "" {
		return pairingAttempt{}, false
	}
	if attempt.state != pairingWaiting && attempt.state != pairingNeedsProfile {
		return pairingAttempt{}, false
	}
	return clonePairing(attempt), true
}

func (m *pairingManager) lookup(id, cookieHash string) (pairingAttempt, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	attempt := m.attempts[id]
	if attempt == nil || attempt.cookieHash != cookieHash {
		return pairingAttempt{}, false, false
	}
	if !attempt.expiresAt.After(now) {
		copy := clonePairing(attempt)
		copy.state = pairingExpired
		m.removeLocked(id)
		return copy, true, true
	}
	return clonePairing(attempt), true, false
}

func (m *pairingManager) beginPoll(id string) (pairingAttempt, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	attempt := m.attempts[id]
	if attempt == nil {
		return pairingAttempt{}, "", domain.ErrNotFound
	}
	now := m.now().UTC()
	if !attempt.expiresAt.After(now) {
		m.removeLocked(id)
		return pairingAttempt{state: pairingExpired}, "", nil
	}
	if attempt.state != pairingWaiting {
		return clonePairing(attempt), "", nil
	}
	if attempt.polling {
		return clonePairing(attempt), "", errors.New("poll already active")
	}
	if !attempt.lastPoll.IsZero() && now.Sub(attempt.lastPoll) < minPollInterval {
		return clonePairing(attempt), "", errors.New("poll too soon")
	}
	attempt.polls++
	if attempt.polls > maxPairingPolls {
		m.removeLocked(id)
		return pairingAttempt{state: pairingExpired}, "", nil
	}
	attempt.lastPoll = now
	attempt.polling = true
	return clonePairing(attempt), attempt.loginID, nil
}

func (m *pairingManager) finishPoll(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if attempt := m.attempts[id]; attempt != nil {
		attempt.polling = false
	}
}

func (m *pairingManager) needsProfile(id string, digest [32]byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	attempt := m.attempts[id]
	if attempt == nil || attempt.state != pairingWaiting {
		return false
	}
	attempt.state = pairingNeedsProfile
	attempt.digest = digest
	attempt.hasDigest = true
	attempt.polling = false
	return true
}

func (m *pairingManager) markFailed(id, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if attempt := m.attempts[id]; attempt != nil {
		attempt.state = pairingFailed
		attempt.errorCode = code
		m.removeLocked(id)
	}
}

func (m *pairingManager) complete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if attempt := m.attempts[id]; attempt != nil {
		attempt.state = pairingCompleted
		m.removeLocked(id)
		return true
	}
	return false
}

func (m *pairingManager) remove(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removeLocked(id)
}

func (m *pairingManager) removeLocked(id string) string {
	attempt := m.attempts[id]
	if attempt == nil {
		return ""
	}
	delete(m.attempts, id)
	if m.byCookie[attempt.cookieHash] == id {
		delete(m.byCookie, attempt.cookieHash)
	}
	return attempt.loginID
}

func clonePairing(attempt *pairingAttempt) pairingAttempt {
	if attempt == nil {
		return pairingAttempt{}
	}
	return *attempt
}

type codexStatusResult struct {
	Available        bool   `json:"available"`
	BindingState     string `json:"binding_state"`
	AccountState     string `json:"account_state"`
	FirstBindAllowed bool   `json:"first_bind_allowed"`
}

type codexStartResult struct {
	AttemptID       string    `json:"attempt_id"`
	VerificationURL string    `json:"verification_url"`
	UserCode        string    `json:"user_code"`
	ExpiresAt       time.Time `json:"expires_at"`
	PollAfterMS     int       `json:"poll_after_ms"`
	Resumed         bool      `json:"resumed"`
}

type codexPollResult struct {
	State       string             `json:"state"`
	Code        string             `json:"code,omitempty"`
	Message     string             `json:"message,omitempty"`
	Session     *authSessionResult `json:"-"`
	PollAfterMS int                `json:"poll_after_ms,omitempty"`
}

type codexProfileRequest struct {
	AttemptID   string `json:"attempt_id"`
	DisplayName string `json:"display_name"`
	Handle      string `json:"handle"`
}

type codexPollRequest struct {
	AttemptID string `json:"attempt_id"`
}
type codexCancelRequest struct {
	AttemptID string `json:"attempt_id"`
}

func (h *handler) codexConfig() (*CodexAuthConfig, bool) {
	if h == nil || h.config.CodexAuth == nil || h.config.CodexAuth.Client == nil || h.humanAuth == nil || !h.humanAuth.codexEnabled {
		return nil, false
	}
	return h.config.CodexAuth, true
}

func (h *handler) authHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func pairingCookieValue(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(pairingCookieName)
	return cookieValue(cookie, err)
}

func cookieValue(cookie *http.Cookie, err error) (string, bool) {
	return func() (string, bool) {
		if err != nil || cookie == nil || cookie.Value == "" {
			return "", false
		}
		return cookie.Value, true
	}()
}

func pairingHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return string(digest[:])
}

func setPairingCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: pairingCookieName, Value: value, Path: pairingCookiePath, MaxAge: maxAge, Expires: time.Now().Add(pairingTTL), HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
}

func clearPairingCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: pairingCookieName, Value: "", Path: pairingCookiePath, MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
}

func requestRemoteKey(r *http.Request) string {
	if r == nil || r.RemoteAddr == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func requestIsLoopback(r *http.Request) bool {
	host := requestRemoteKey(r)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func validateVerificationURL(value string) bool {
	if len(value) == 0 || len(value) > 2048 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() != "auth.openai.com" || parsed.Port() != "" {
		return false
	}
	return parsed.Path != "" && !strings.ContainsAny(value, "\r\n")
}

func validDeviceCode(value string) bool {
	if len(value) < 1 || len(value) > 64 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func (h *handler) binding(ctx context.Context) (domain.HumanAccountBinding, error) {
	if h.config.HumanBindingStore == nil {
		return domain.HumanAccountBinding{}, domain.ErrNotFound
	}
	return h.config.HumanBindingStore.GetHumanAccountBinding(ctx)
}

func (h *handler) firstBindAllowed(r *http.Request) (bool, error) {
	_, err := h.binding(r.Context())
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return false, err
	}
	config, ok := h.codexConfig()
	return requestIsLoopback(r) || ok && config.AllowFirstBindLAN, nil
}

func (h *handler) authRoute(w http.ResponseWriter, r *http.Request, rid string) bool {
	h.authHeaders(w)
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
		session := humanSession{csrf: principal.csrfToken, authMethod: principal.authMethod, bindingRevision: principal.bindingRevision}
		h.write(w, http.StatusOK, envelope{OK: true, Data: h.humanAuth.result(&session), Meta: responseMeta{RequestID: rid}})
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/codex/status":
		h.codexStatus(w, r, rid)
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/codex/start":
		h.codexStart(w, r, rid)
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/codex/poll":
		h.codexPoll(w, r, rid)
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/codex/profile":
		h.codexProfile(w, r, rid)
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/codex/cancel":
		h.codexCancel(w, r, rid)
		return true
	case r.Method == http.MethodPut && r.URL.Path == "/v1/auth/profile":
		h.updateProfile(w, r, rid)
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

func (h *handler) codexStatus(w http.ResponseWriter, r *http.Request, rid string) {
	result := codexStatusResult{Available: false, BindingState: "unbound", AccountState: "unavailable", FirstBindAllowed: false}
	if _, err := h.binding(r.Context()); err == nil {
		result.BindingState = "bound"
		result.FirstBindAllowed = false
	} else if !errors.Is(err, domain.ErrNotFound) {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Commons authentication is temporarily unavailable")
		return
	}
	config, ok := h.codexConfig()
	if ok {
		result.Available = config.Client.Available()
		result.FirstBindAllowed, _ = h.firstBindAllowed(r)
		if result.Available {
			state, err := config.Client.AccountState(r.Context())
			if err != nil {
				result.AccountState = "unknown"
			} else {
				result.AccountState = string(state)
			}
		}
	}
	h.write(w, http.StatusOK, envelope{OK: true, Data: result, Meta: responseMeta{RequestID: rid}})
}

func (h *handler) codexStart(w http.ResponseWriter, r *http.Request, rid string) {
	config, ok := h.codexConfig()
	if !ok || !config.Client.Available() {
		h.writeError(w, rid, http.StatusServiceUnavailable, "codex_unavailable", "Codex authentication is unavailable")
		return
	}
	if !sameOrigin(r) {
		h.writeError(w, rid, http.StatusForbidden, "origin_forbidden", "same-origin request required")
		return
	}
	var body struct{}
	if !h.decode(w, r, RequestMeta{RequestID: rid}, &body) {
		return
	}
	allowed, err := h.firstBindAllowed(r)
	if err != nil {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Commons authentication is temporarily unavailable")
		return
	}
	if !allowed {
		h.writeError(w, rid, http.StatusForbidden, "first_bind_lan_forbidden", "The first Codex account binding must begin on loopback or with explicit LAN acknowledgement.")
		return
	}
	pairingValue, hasCookie := pairingCookieValue(r)
	setCookie := false
	if !hasCookie {
		var ok bool
		pairingValue, ok = randomAuthToken()
		if !ok {
			h.writeError(w, rid, http.StatusServiceUnavailable, "randomness_unavailable", "Secure browser randomness is unavailable")
			return
		}
		setCookie = true
	}
	cookieHash := pairingHash(pairingValue)
	if hasCookie {
		if active, found := h.pairings.activeForCookie(cookieHash); found {
			h.write(w, http.StatusOK, envelope{OK: true, Data: codexStartResult{AttemptID: active.id, VerificationURL: active.verificationURL, UserCode: active.userCode, ExpiresAt: active.expiresAt, PollAfterMS: 1500, Resumed: true}, Meta: responseMeta{RequestID: rid}})
			return
		}
	}
	attempt, err := h.pairings.reserve(cookieHash, requestRemoteKey(r))
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			h.writeError(w, rid, http.StatusConflict, "pairing_attempt_active", "A Codex sign-in is already active for this browser.")
		} else if limit := new(pairingLimitError); errors.As(err, &limit) {
			h.writeCooldownError(w, rid, limit.code, "Codex sign-in is cooling down. Your reading session is unchanged.", limit.retry)
		} else {
			h.writeError(w, rid, http.StatusServiceUnavailable, "randomness_unavailable", "Secure browser randomness is unavailable")
		}
		return
	}
	device, err := config.Client.StartDeviceCode(r.Context())
	if err != nil || !validateVerificationURL(device.VerificationURL) || !validDeviceCode(device.UserCode) {
		h.pairings.remove(attempt.id)
		h.writeError(w, rid, http.StatusServiceUnavailable, "codex_unavailable", "Codex authentication is unavailable")
		return
	}
	if !h.pairings.attachLogin(attempt.id, device.LoginID, device.VerificationURL, device.UserCode) {
		h.pairings.remove(attempt.id)
		h.writeError(w, rid, http.StatusServiceUnavailable, "codex_unavailable", "Codex authentication is unavailable")
		return
	}
	if setCookie {
		setPairingCookie(w, r, pairingValue, int(pairingTTL/time.Second))
	}
	h.write(w, http.StatusOK, envelope{OK: true, Data: codexStartResult{AttemptID: attempt.id, VerificationURL: device.VerificationURL, UserCode: device.UserCode, ExpiresAt: attempt.expiresAt, PollAfterMS: 1500}, Meta: responseMeta{RequestID: rid}})
}

func (h *handler) codexPoll(w http.ResponseWriter, r *http.Request, rid string) {
	config, ok := h.codexConfig()
	if !ok {
		h.writeError(w, rid, http.StatusServiceUnavailable, "codex_unavailable", "Codex authentication is unavailable")
		return
	}
	if !sameOrigin(r) {
		h.writeError(w, rid, http.StatusForbidden, "origin_forbidden", "same-origin request required")
		return
	}
	var input codexPollRequest
	if !h.decode(w, r, RequestMeta{RequestID: rid}, &input) || input.AttemptID == "" {
		if input.AttemptID == "" {
			h.badBody(w, RequestMeta{RequestID: rid}, "attempt_id is required")
		}
		return
	}
	pairingValue, hasCookie := pairingCookieValue(r)
	if !hasCookie {
		h.writeError(w, rid, http.StatusNotFound, "pairing_not_found", "The Codex sign-in attempt is no longer available.")
		return
	}
	attempt, found, expired := h.pairings.lookup(input.AttemptID, pairingHash(pairingValue))
	if !found {
		h.writeError(w, rid, http.StatusNotFound, "pairing_not_found", "The Codex sign-in attempt is no longer available.")
		return
	}
	if expired || attempt.state == pairingExpired {
		clearPairingCookie(w, r)
		h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingExpired)}, Meta: responseMeta{RequestID: rid}})
		return
	}
	if attempt.state == pairingNeedsProfile {
		h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingNeedsProfile)}, Meta: responseMeta{RequestID: rid}})
		return
	}
	if attempt.state != pairingWaiting {
		h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(attempt.state), Code: attempt.errorCode}, Meta: responseMeta{RequestID: rid}})
		return
	}
	active, loginID, err := h.pairings.beginPoll(input.AttemptID)
	if err != nil {
		if strings.Contains(err.Error(), "too soon") {
			retry := minPollInterval - h.pairings.now().UTC().Sub(attempt.lastPoll)
			h.writeCooldownError(w, rid, "auth_poll_wait", "Codex authorization is still pending. Commons will check again shortly.", retry)
		} else if strings.Contains(err.Error(), "active") {
			h.writeError(w, rid, http.StatusConflict, "poll_active", "A Codex sign-in check is already in progress.")
		} else {
			h.writeError(w, rid, http.StatusNotFound, "pairing_not_found", "The Codex sign-in attempt is no longer available.")
		}
		return
	}
	if active.state == pairingExpired || loginID == "" {
		clearPairingCookie(w, r)
		h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingExpired)}, Meta: responseMeta{RequestID: rid}})
		return
	}
	defer h.pairings.finishPoll(input.AttemptID)
	result, err := config.Client.PollLogin(r.Context(), loginID)
	if err != nil {
		h.pairings.markFailed(input.AttemptID, "codex_unavailable")
		clearPairingCookie(w, r)
		h.writeError(w, rid, http.StatusServiceUnavailable, "codex_unavailable", "Codex authentication is unavailable")
		return
	}
	if result.State == "pending" {
		h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingWaiting), PollAfterMS: 1500}, Meta: responseMeta{RequestID: rid}})
		return
	}
	if result.State == "cancelled" || result.State == "failed" {
		h.pairings.markFailed(input.AttemptID, "authorization_cancelled")
		clearPairingCookie(w, r)
		code, message := "authorization_failed", "Codex authorization was not completed."
		if result.State == "cancelled" {
			code, message = "authorization_cancelled", "Codex authorization was cancelled."
		}
		h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingFailed), Code: code, Message: message}, Meta: responseMeta{RequestID: rid}})
		return
	}
	if result.State != "success" || result.Account == nil || result.Account.Type != "chatgpt" || result.Account.Email == nil || strings.TrimSpace(*result.Account.Email) == "" {
		h.pairings.markFailed(input.AttemptID, "codex_identity_unavailable")
		clearPairingCookie(w, r)
		h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingFailed), Code: "codex_identity_unavailable", Message: "Codex did not provide a bindable account identity."}, Meta: responseMeta{RequestID: rid}})
		return
	}
	digest := hmac.New(sha256.New, config.BindingKey[:])
	_, _ = digest.Write([]byte(strings.ToLower(strings.TrimSpace(*result.Account.Email))))
	computed := digest.Sum(nil)
	var subject [32]byte
	copy(subject[:], computed)
	binding, bindingErr := h.binding(r.Context())
	if bindingErr == nil {
		matches := len(binding.ProviderSubjectDigest) == len(subject) && subtle.ConstantTimeCompare(binding.ProviderSubjectDigest, subject[:]) == 1
		if !matches {
			h.pairings.markFailed(input.AttemptID, "account_mismatch")
			clearPairingCookie(w, r)
			h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingFailed), Code: "account_mismatch", Message: "This Commons installation is bound to a different Codex account."}, Meta: responseMeta{RequestID: rid}})
			return
		}
		if !h.pairings.complete(input.AttemptID) {
			h.writeError(w, rid, http.StatusConflict, "pairing_not_found", "The Codex sign-in attempt is no longer available.")
			return
		}
		token, session, ok := h.humanAuth.create("codex", binding.Revision)
		if !ok {
			h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Could not create a Commons session")
			return
		}
		setHumanCookie(w, r, token, session.expiresAt, int(h.humanAuth.ttl/time.Second))
		clearPairingCookie(w, r)
		h.write(w, http.StatusOK, envelope{OK: true, Data: h.humanAuth.result(&session), Meta: responseMeta{RequestID: rid}})
		return
	}
	if !errors.Is(bindingErr, domain.ErrNotFound) {
		h.pairings.markFailed(input.AttemptID, "unavailable")
		clearPairingCookie(w, r)
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Commons authentication is temporarily unavailable")
		return
	}
	if allowed, _ := h.firstBindAllowed(r); !allowed {
		h.pairings.markFailed(input.AttemptID, "first_bind_lan_forbidden")
		clearPairingCookie(w, r)
		h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingFailed), Code: "first_bind_lan_forbidden", Message: "The first Codex account binding must begin on loopback or with explicit LAN acknowledgement."}, Meta: responseMeta{RequestID: rid}})
		return
	}
	if !h.pairings.needsProfile(input.AttemptID, subject) {
		return
	}
	h.write(w, http.StatusOK, envelope{OK: true, Data: codexPollResult{State: string(pairingNeedsProfile)}, Meta: responseMeta{RequestID: rid}})
}

func (h *handler) codexProfile(w http.ResponseWriter, r *http.Request, rid string) {
	if _, ok := h.codexConfig(); !ok {
		h.writeError(w, rid, http.StatusServiceUnavailable, "codex_unavailable", "Codex authentication is unavailable")
		return
	}
	if !sameOrigin(r) {
		h.writeError(w, rid, http.StatusForbidden, "origin_forbidden", "same-origin request required")
		return
	}
	var input codexProfileRequest
	if !h.decode(w, r, RequestMeta{RequestID: rid}, &input) {
		return
	}
	if input.AttemptID == "" {
		h.badBody(w, RequestMeta{RequestID: rid}, "attempt_id is required")
		return
	}
	pairingValue, ok := pairingCookieValue(r)
	if !ok {
		h.writeError(w, rid, http.StatusNotFound, "pairing_not_found", "The Codex sign-in attempt is no longer available.")
		return
	}
	attempt, found, expired := h.pairings.lookup(input.AttemptID, pairingHash(pairingValue))
	if !found || expired || attempt.state != pairingNeedsProfile || !attempt.hasDigest {
		if expired {
			clearPairingCookie(w, r)
		}
		h.writeError(w, rid, http.StatusConflict, "profile_unavailable", "This Codex onboarding step is no longer available.")
		return
	}
	displayName := strings.TrimSpace(input.DisplayName)
	handle := strings.ToLower(strings.TrimSpace(input.Handle))
	if !validHumanProfileInput(displayName, handle) {
		h.writeError(w, rid, http.StatusBadRequest, "invalid_profile", "Choose a display name and a 3–64 character lowercase handle using letters, numbers, and internal hyphens.")
		return
	}
	if h.config.HumanBindingStore == nil {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Commons authentication is temporarily unavailable")
		return
	}
	binding, err := h.config.HumanBindingStore.BindHumanAccount(r.Context(), domain.BindHumanAccountRequest{ProviderSubjectDigest: append([]byte(nil), attempt.digest[:]...), DisplayName: displayName, Handle: handle})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			h.writeError(w, rid, http.StatusConflict, "profile_conflict", "This Commons installation is already bound to another account or handle.")
		} else if errors.Is(err, domain.ErrInvalid) {
			h.writeError(w, rid, http.StatusBadRequest, "invalid_profile", "Choose a valid display name and handle.")
		} else {
			h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Commons authentication is temporarily unavailable")
		}
		return
	}
	if !h.pairings.complete(input.AttemptID) {
		h.writeError(w, rid, http.StatusConflict, "profile_unavailable", "This Codex onboarding step is no longer available.")
		return
	}
	h.humanAuth.updateIdentity(binding.DisplayName, binding.Handle, binding.Revision)
	if h.config.OnHumanIdentityUpdated != nil {
		h.config.OnHumanIdentityUpdated(binding.DisplayName, binding.Handle, binding.Revision)
	}
	token, session, ok := h.humanAuth.create("codex", binding.Revision)
	if !ok {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Could not create a Commons session")
		return
	}
	setHumanCookie(w, r, token, session.expiresAt, int(h.humanAuth.ttl/time.Second))
	clearPairingCookie(w, r)
	h.write(w, http.StatusOK, envelope{OK: true, Data: h.humanAuth.result(&session), Meta: responseMeta{RequestID: rid}})
}

func validHumanProfileInput(displayName, handle string) bool {
	if len(displayName) < 1 || len(displayName) > 200 || strings.TrimSpace(displayName) == "" || len(handle) < 3 || len(handle) > 64 || handle != strings.ToLower(handle) || strings.HasPrefix(handle, "@") {
		return false
	}
	for index, char := range handle {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' || index == 0 && char == '-' || index == len(handle)-1 && char == '-' {
			return false
		}
	}
	return true
}

func (h *handler) codexCancel(w http.ResponseWriter, r *http.Request, rid string) {
	config, ok := h.codexConfig()
	if !ok {
		h.writeError(w, rid, http.StatusServiceUnavailable, "codex_unavailable", "Codex authentication is unavailable")
		return
	}
	if !sameOrigin(r) {
		h.writeError(w, rid, http.StatusForbidden, "origin_forbidden", "same-origin request required")
		return
	}
	var input codexCancelRequest
	if !h.decode(w, r, RequestMeta{RequestID: rid}, &input) {
		return
	}
	if input.AttemptID == "" {
		h.badBody(w, RequestMeta{RequestID: rid}, "attempt_id is required")
		return
	}
	pairingValue, ok := pairingCookieValue(r)
	if !ok {
		h.writeError(w, rid, http.StatusNotFound, "pairing_not_found", "The Codex sign-in attempt is no longer available.")
		return
	}
	attempt, found, _ := h.pairings.lookup(input.AttemptID, pairingHash(pairingValue))
	if !found {
		clearPairingCookie(w, r)
		h.write(w, http.StatusOK, envelope{OK: true, Data: authSessionResult{Authenticated: false}, Meta: responseMeta{RequestID: rid}})
		return
	}
	if attempt.loginID != "" {
		_ = config.Client.CancelLogin(r.Context(), attempt.loginID)
	}
	h.pairings.remove(input.AttemptID)
	clearPairingCookie(w, r)
	h.write(w, http.StatusOK, envelope{OK: true, Data: authSessionResult{Authenticated: false}, Meta: responseMeta{RequestID: rid}})
}

func (h *handler) updateProfile(w http.ResponseWriter, r *http.Request, rid string) {
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
	key := r.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) || key == "" {
		h.writeError(w, rid, http.StatusBadRequest, "bad_idempotency_key", "Idempotency-Key required")
		return
	}
	var input struct {
		DisplayName  string `json:"display_name"`
		Handle       string `json:"handle"`
		BaseRevision int64  `json:"base_revision"`
	}
	if !h.decode(w, r, RequestMeta{RequestID: rid}, &input) {
		return
	}
	displayName, handle := strings.TrimSpace(input.DisplayName), strings.ToLower(strings.TrimSpace(input.Handle))
	if !validHumanProfileInput(displayName, handle) || input.BaseRevision < 1 {
		h.writeError(w, rid, http.StatusBadRequest, "invalid_profile", "Choose a valid display name, handle, and profile revision.")
		return
	}
	if h.config.HumanBindingStore == nil {
		h.writeError(w, rid, http.StatusServiceUnavailable, "unavailable", "Human profile storage is unavailable")
		return
	}
	binding, err := h.config.HumanBindingStore.UpdateHumanProfile(r.Context(), domain.UpdateHumanProfileRequest{Principal: domain.HumanLocalPrincipal, DisplayName: displayName, Handle: handle, BaseRevision: input.BaseRevision, IdempotencyKey: key})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			h.writeError(w, rid, http.StatusConflict, "profile_conflict", "Your profile changed in another Commons tab. Review the current profile and try again.")
		} else if errors.Is(err, domain.ErrNotFound) {
			h.writeError(w, rid, http.StatusNotFound, "profile_unavailable", "Complete Codex onboarding before editing your profile.")
		} else {
			h.writeError(w, rid, http.StatusBadRequest, "invalid_profile", "Choose a valid profile.")
		}
		return
	}
	h.humanAuth.updateIdentity(binding.DisplayName, binding.Handle, binding.Revision)
	if h.config.OnHumanIdentityUpdated != nil {
		h.config.OnHumanIdentityUpdated(binding.DisplayName, binding.Handle, binding.Revision)
	}
	session := humanSession{csrf: principal.csrfToken, authMethod: principal.authMethod, bindingRevision: binding.Revision}
	h.write(w, http.StatusOK, envelope{OK: true, Data: h.humanAuth.result(&session), Meta: responseMeta{RequestID: rid}})
}
