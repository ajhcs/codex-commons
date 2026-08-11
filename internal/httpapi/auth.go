package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const humanSessionCookie = "commons_session"

type HumanAuthConfig struct {
	AdminSecret string
	DisplayName string
	Handle      string
	Actor       string
	Principal   string
	Session     string
	Host        string
	SessionTTL  time.Duration
}

type humanSession struct {
	csrf      string
	createdAt time.Time
	expiresAt time.Time
}

type humanAuth struct {
	mu           sync.Mutex
	secretDigest [sha256.Size]byte
	displayName  string
	principalID  string
	handle       string
	actor        string
	session      string
	host         string
	ttl          time.Duration
	sessions     map[string]humanSession
	failures     []time.Time
	now          func() time.Time
}

type authPrincipal struct {
	Credential
	Kind      string
	Principal string
	csrfToken string
	cookie    string
}

type authSessionResult struct {
	Authenticated bool               `json:"authenticated"`
	Principal     *authPrincipalView `json:"principal,omitempty"`
	CSRFToken     string             `json:"csrf_token,omitempty"`
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

func newHumanAuth(config *HumanAuthConfig) *humanAuth {
	if config == nil || config.AdminSecret == "" {
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
	return &humanAuth{
		secretDigest: sha256.Sum256([]byte(config.AdminSecret)),
		displayName:  config.DisplayName,
		actor:        config.Actor,
		principalID:  principal,
		handle:       handle,
		session:      config.Session,
		host:         config.Host,
		ttl:          ttl,
		sessions:     make(map[string]humanSession),
		now:          time.Now,
	}
}

func (a *humanAuth) verifySecret(value string) bool {
	if a == nil {
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

func (a *humanAuth) create() (string, humanSession, bool) {
	if a == nil {
		return "", humanSession{}, false
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
	session := humanSession{csrf: csrf, createdAt: now, expiresAt: now.Add(a.ttl)}
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
	if !ok {
		return humanSession{}, false
	}
	if !session.expiresAt.After(a.now().UTC()) {
		delete(a.sessions, token)
		return humanSession{}, false
	}
	return session, true
}

func (a *humanAuth) remove(token string) {
	if a == nil || token == "" {
		return
	}
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
}

func randomAuthToken() (string, bool) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", false
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), true
}

func (a *humanAuth) principal() authPrincipal {
	return authPrincipal{Kind: "human", Principal: a.principalID, Credential: Credential{
		Actor: a.actor, Session: a.session, Host: a.host,
	}}
}

func (a *humanAuth) result(session *humanSession) authSessionResult {
	if a == nil || session == nil {
		return authSessionResult{Authenticated: false}
	}
	return authSessionResult{
		Authenticated: true,
		Principal:     &authPrincipalView{Kind: "human", Principal: a.principalID, Handle: a.handle, DisplayName: a.displayName},
		CSRFToken:     session.csrf,
	}
}

func setHumanCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: humanSessionCookie, Value: token, Path: "/", Expires: expires,
		MaxAge: maxAge, HttpOnly: true, Secure: r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func sameOrigin(r *http.Request) bool {
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
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Host, r.Host)
}

func isHumanWritePath(path string) bool {
	if path == "/v1/projects" || strings.HasPrefix(path, "/v1/projects/") ||
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
