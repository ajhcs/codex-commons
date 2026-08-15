package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"codex-commons/internal/domain"
)

const HumanSessionCookieName = "commons_session"
const humanSessionCookie = HumanSessionCookieName

type HumanAuthConfig struct {
	AdminSecret     string
	DisplayName     string
	Handle          string
	Actor           string
	Principal       string
	Session         string
	Host            string
	SessionTTL      time.Duration
	RecoveryEnabled bool
	CodexEnabled    bool
}

type humanSession struct {
	csrf            string
	createdAt       time.Time
	expiresAt       time.Time
	authMethod      string
	bindingRevision int64
}

type humanAuth struct {
	mu              sync.Mutex
	secretDigest    [sha256.Size]byte
	displayName     string
	principalID     string
	handle          string
	actor           string
	session         string
	host            string
	ttl             time.Duration
	recoveryEnabled bool
	codexEnabled    bool
	sessions        map[string]humanSession
	failures        []time.Time
	now             func() time.Time
	store           HumanSessionStore
}

type HumanSessionStore interface {
	SaveHumanBrowserSession(context.Context, domain.HumanBrowserSession) error
	HumanBrowserSession(context.Context, []byte, time.Time) (domain.HumanBrowserSession, error)
	RevokeHumanBrowserSession(context.Context, []byte, time.Time) error
	RevokeHumanBrowserSessionsByMethod(context.Context, string, time.Time) error
	UpdateHumanBrowserSessionCSRF(context.Context, []byte, []byte) error
	UpdateHumanBrowserSessionRevisions(context.Context, string, int64) error
	SetCodexSessionRevocationPending(context.Context, bool) error
	CodexSessionRevocationPending(context.Context) (bool, error)
}

type authPrincipal struct {
	Credential
	Kind            string
	Principal       string
	csrfToken       string
	cookie          string
	authMethod      string
	bindingRevision int64
}

type authSessionResult struct {
	Authenticated   bool               `json:"authenticated"`
	Principal       *authPrincipalView `json:"principal,omitempty"`
	CSRFToken       string             `json:"csrf_token,omitempty"`
	AuthMethod      string             `json:"auth_method,omitempty"`
	ProfileRevision int64              `json:"profile_revision,omitempty"`
}

type authPrincipalView struct {
	Kind        string `json:"kind"`
	Principal   string `json:"principal"`
	Handle      string `json:"handle,omitempty"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Secret string `json:"secret"`
}

func newHumanAuth(config *HumanAuthConfig, stores ...HumanSessionStore) *humanAuth {
	if config == nil || !config.RecoveryEnabled && !config.CodexEnabled {
		return nil
	}
	principal, handle := config.Principal, config.Handle
	if principal == "" {
		principal = "human:local-admin"
	}
	if handle == "" {
		handle = "local-admin"
	}
	ttl := config.SessionTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	a := &humanAuth{
		secretDigest:    sha256.Sum256([]byte(config.AdminSecret)),
		displayName:     config.DisplayName,
		actor:           config.Actor,
		principalID:     principal,
		handle:          handle,
		session:         config.Session,
		host:            config.Host,
		ttl:             ttl,
		recoveryEnabled: config.RecoveryEnabled,
		codexEnabled:    config.CodexEnabled,
		sessions:        make(map[string]humanSession),
		now:             time.Now,
	}
	if len(stores) > 0 {
		a.store = stores[0]
	}
	return a
}

func (a *humanAuth) verifySecret(value string) bool {
	if a == nil || !a.recoveryEnabled || a.secretDigest == [sha256.Size]byte{} {
		return false
	}
	digest := sha256.Sum256([]byte(value))
	return subtle.ConstantTimeCompare(digest[:], a.secretDigest[:]) == 1
}

func (a *humanAuth) rateLimited() (bool, time.Duration) {
	if a == nil {
		return false, 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now().UTC()
	cutoff := now.Add(-time.Minute)
	kept := a.failures[:0]
	for _, failure := range a.failures {
		if failure.After(cutoff) {
			kept = append(kept, failure)
		}
	}
	a.failures = kept
	if len(a.failures) < 5 {
		return false, 0
	}
	retry := a.failures[0].Add(time.Minute).Sub(now)
	if retry < time.Second {
		retry = time.Second
	}
	return true, retry
}

func (a *humanAuth) recordFailure() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures = append(a.failures, a.now().UTC())
	if len(a.failures) > 5 {
		a.failures = append([]time.Time(nil), a.failures[len(a.failures)-5:]...)
	}
}

func (a *humanAuth) create(methodAndRevision ...any) (string, humanSession, bool) {
	if a == nil {
		return "", humanSession{}, false
	}
	method := "recovery"
	revision := int64(0)
	if len(methodAndRevision) > 0 {
		if value, ok := methodAndRevision[0].(string); ok && value != "" {
			method = value
		}
	}
	if len(methodAndRevision) > 1 {
		if value, ok := methodAndRevision[1].(int64); ok && value >= 0 {
			revision = value
		}
	}
	token, ok := randomAuthToken()
	if !ok {
		return "", humanSession{}, false
	}
	csrf, ok := randomAuthToken()
	if !ok {
		return "", humanSession{}, false
	}
	now := a.now().UTC()
	session := humanSession{csrf: csrf, createdAt: now, expiresAt: now.Add(a.ttl), authMethod: method, bindingRevision: revision}
	if a.store != nil {
		tokenDigest, csrfDigest := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(csrf))
		if err := a.store.SaveHumanBrowserSession(context.Background(), domain.HumanBrowserSession{TokenDigest: tokenDigest[:], CSRFDigest: csrfDigest[:], Principal: a.principalID, AuthMethod: method, BindingRevision: revision, CreatedAt: now, ExpiresAt: session.expiresAt}); err != nil {
			return "", humanSession{}, false
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures = nil
	for key, existing := range a.sessions {
		if !existing.expiresAt.After(now) {
			delete(a.sessions, key)
		}
	}
	if len(a.sessions) >= 8 {
		oldestKey := ""
		var oldest time.Time
		for key, existing := range a.sessions {
			if oldestKey == "" || existing.createdAt.Before(oldest) || existing.createdAt.Equal(oldest) && key < oldestKey {
				oldestKey, oldest = key, existing.createdAt
			}
		}
		delete(a.sessions, oldestKey)
	}
	a.sessions[token] = session
	return token, session, true
}

func (a *humanAuth) lookup(token string) (humanSession, bool) {
	if a == nil || token == "" {
		return humanSession{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	session, ok := a.sessions[token]
	if !ok && a.store != nil {
		digest := sha256.Sum256([]byte(token))
		stored, err := a.store.HumanBrowserSession(context.Background(), digest[:], a.now().UTC())
		if err == nil && stored.Principal == a.principalID {
			session = humanSession{createdAt: stored.CreatedAt, expiresAt: stored.ExpiresAt, authMethod: stored.AuthMethod, bindingRevision: stored.BindingRevision}
			a.sessions[token] = session
			ok = true
		}
	}
	if !ok {
		return humanSession{}, false
	}
	if !session.expiresAt.After(a.now().UTC()) {
		delete(a.sessions, token)
		return humanSession{}, false
	}
	return session, true
}

func (a *humanAuth) remove(token string) error {
	if a == nil || token == "" {
		return nil
	}
	if a.store != nil {
		digest := sha256.Sum256([]byte(token))
		if err := a.store.RevokeHumanBrowserSession(context.Background(), digest[:], a.now().UTC()); err != nil {
			return err
		}
	}
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
	return nil
}

func (a *humanAuth) revokeCodexSessions() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	for token, session := range a.sessions {
		if session.authMethod == "codex" {
			delete(a.sessions, token)
		}
	}
	a.mu.Unlock()
	if a.store != nil {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if err = a.store.RevokeHumanBrowserSessionsByMethod(context.Background(), "codex", a.now().UTC()); err == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

func (a *humanAuth) setCodexRevocationPending(pending bool) error {
	if a == nil || a.store == nil {
		return nil
	}
	return a.store.SetCodexSessionRevocationPending(context.Background(), pending)
}

func (a *humanAuth) codexRevocationPending() bool {
	if a == nil || a.store == nil {
		return false
	}
	pending, err := a.store.CodexSessionRevocationPending(context.Background())
	return err != nil || pending
}

func (a *humanAuth) rotateCSRF(token string) (humanSession, bool) {
	session, ok := a.lookup(token)
	if !ok {
		return humanSession{}, false
	}
	csrf, ok := randomAuthToken()
	if !ok {
		return humanSession{}, false
	}
	if a.store != nil {
		tokenDigest, csrfDigest := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(csrf))
		if a.store.UpdateHumanBrowserSessionCSRF(context.Background(), tokenDigest[:], csrfDigest[:]) != nil {
			return humanSession{}, false
		}
	}
	a.mu.Lock()
	session.csrf = csrf
	a.sessions[token] = session
	a.mu.Unlock()
	return session, true
}

func (a *humanAuth) updateIdentity(displayName, handle string, revision int64) error {
	if a == nil || strings.TrimSpace(displayName) == "" || handle == "" {
		return domain.ErrInvalid
	}
	if a.store != nil {
		if err := a.store.UpdateHumanBrowserSessionRevisions(context.Background(), a.principalID, revision); err != nil {
			return err
		}
	}
	a.mu.Lock()
	a.displayName = displayName
	a.handle = handle
	for token, session := range a.sessions {
		if revision > 0 && (session.authMethod == "codex" || session.authMethod == "recovery") {
			session.bindingRevision = revision
			a.sessions[token] = session
		}
	}
	a.mu.Unlock()
	return nil
}

func randomAuthToken() (string, bool) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", false
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), true
}

func (a *humanAuth) principal() authPrincipal {
	if a == nil {
		return authPrincipal{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return authPrincipal{Kind: "human", Principal: a.principalID, Credential: Credential{
		Actor: a.actor, Session: a.session, Host: a.host,
	}}
}

func (a *humanAuth) result(session *humanSession) authSessionResult {
	if a == nil || session == nil {
		return authSessionResult{Authenticated: false}
	}
	method := session.authMethod
	if method == "" {
		method = "recovery"
	}
	a.mu.Lock()
	principal := a.principalID
	handle := a.handle
	displayName := a.displayName
	a.mu.Unlock()
	return authSessionResult{
		Authenticated:   true,
		Principal:       &authPrincipalView{Kind: "human", Principal: principal, Handle: handle, DisplayName: displayName},
		CSRFToken:       session.csrf,
		AuthMethod:      method,
		ProfileRevision: session.bindingRevision,
	}
}

func secureRequest(r *http.Request, publicOrigin string) bool {
	if publicOrigin != "" {
		parsed, err := url.Parse(publicOrigin)
		return err == nil && parsed.Scheme == "https"
	}
	return r != nil && r.TLS != nil
}

func setHumanCookie(w http.ResponseWriter, r *http.Request, publicOrigin, token string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: humanSessionCookie, Value: token, Path: "/", Expires: expires,
		MaxAge: maxAge, HttpOnly: true, Secure: secureRequest(r, publicOrigin),
		SameSite: http.SameSiteStrictMode,
	})
}

func sameOrigin(r *http.Request, publicOrigin string) bool {
	source := strings.TrimSpace(r.Header.Get("Origin"))
	if source == "" {
		source = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if source == "" || source == "null" {
		return false
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.User != nil {
		return false
	}
	expected := &url.URL{Scheme: "http", Host: r.Host}
	if r.TLS != nil {
		expected.Scheme = "https"
	}
	if publicOrigin != "" {
		configured, parseErr := url.Parse(publicOrigin)
		if parseErr != nil || configured.User != nil || configured.Path != "" || configured.RawQuery != "" || configured.Fragment != "" {
			return false
		}
		expected = configured
	}
	return strings.EqualFold(parsed.Scheme, expected.Scheme) && strings.EqualFold(parsed.Host, expected.Host)
}

func isHumanWritePath(path string) bool {
	if path == "/v1/projects" || strings.HasPrefix(path, "/v1/projects/") ||
		strings.HasPrefix(path, "/v1/project-archaeology/") ||
		strings.HasPrefix(path, "/v1/milestones/") || strings.HasPrefix(path, "/v1/tasks/") {
		return true
	}
	switch path {
	case "/v1/notification-reads", "/v1/posts", "/v1/comments", "/v1/post-states", "/v1/post-perspective-scopes", "/v1/topic-requests":
		return true
	default:
		return false
	}
}
