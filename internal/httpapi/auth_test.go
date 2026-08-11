package httpapi

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAdminSecret = "correct-horse-battery-staple-commons"

func humanTestHandler(backend Backend) http.Handler {
	return NewHandler(backend, Config{
		Credentials: []Credential{{BearerToken: "agent-secret", Actor: "agent", Session: "S-agent", Host: "plumbob"}},
		HumanAuth: &HumanAuthConfig{
			AdminSecret: testAdminSecret, DisplayName: "Cole", Actor: "local-admin",
			Session: "human-local-admin", Host: "browser", SessionTTL: time.Hour,
		},
	})
}

func authRequest(handler http.Handler, method, target, body, origin string, cookie *http.Cookie, csrf, idempotency string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-Commons-CSRF", csrf)
	}
	if idempotency != "" {
		req.Header.Set("Idempotency-Key", idempotency)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func loginHuman(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	recorder := authRequest(handler, http.MethodPost, "http://commons.test/v1/auth/login",
		`{"secret":"`+testAdminSecret+`"}`, "http://commons.test", nil, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("login code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Secure {
		t.Fatalf("unsafe plaintext-evaluation cookie=%+v", cookie)
	}
	var response struct {
		Data authSessionResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Data.Authenticated || response.Data.Principal == nil || response.Data.Principal.Kind != "human" ||
		response.Data.Principal.DisplayName != "Cole" || response.Data.CSRFToken == "" {
		t.Fatalf("login response=%+v", response.Data)
	}
	return cookie, response.Data.CSRFToken
}

func TestHumanLoginSessionCookieWriteAndLogout(t *testing.T) {
	backend := &fakeBackend{}
	handler := humanTestHandler(backend)
	cookie, csrf := loginHuman(t, handler)

	status := authRequest(handler, http.MethodGet, "http://commons.test/v1/auth/session", "", "", cookie, "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"authenticated":true`) ||
		!strings.Contains(status.Body.String(), `"csrf_token"`) {
		t.Fatalf("status code=%d body=%s", status.Code, status.Body.String())
	}

	body := `{"topic":"general","kind":"finding","title":"Human post","body":"Durable body","basis":"Observed"}`
	write := authRequest(handler, http.MethodPost, "http://commons.test/v1/posts", body, "http://commons.test", cookie, csrf, "human-post-1")
	if write.Code != http.StatusOK || backend.last.Actor != "local-admin" || backend.last.Session != "human-local-admin" ||
		backend.last.Host != "browser" || backend.last.IdempotencyKey != "human-post-1" {
		t.Fatalf("write code=%d meta=%+v body=%s", write.Code, backend.last, write.Body.String())
	}

	logout := authRequest(handler, http.MethodPost, "http://commons.test/v1/auth/logout", `{}`, "http://commons.test", cookie, csrf, "")
	if logout.Code != http.StatusOK || !strings.Contains(logout.Body.String(), `"authenticated":false`) {
		t.Fatalf("logout code=%d body=%s", logout.Code, logout.Body.String())
	}
	expired := logout.Result().Cookies()
	if len(expired) != 1 || expired[0].MaxAge >= 0 {
		t.Fatalf("logout cookie=%v", expired)
	}
	after := authRequest(handler, http.MethodGet, "http://commons.test/v1/auth/session", "", "", cookie, "", "")
	if !strings.Contains(after.Body.String(), `"authenticated":false`) {
		t.Fatalf("session survived logout: %s", after.Body.String())
	}
}

func TestHumanAuthOriginCSRFAndAgentOnlyBoundaries(t *testing.T) {
	backend := &fakeBackend{}
	handler := humanTestHandler(backend)
	noOrigin := authRequest(handler, http.MethodPost, "http://commons.test/v1/auth/login", `{"secret":"`+testAdminSecret+`"}`, "", nil, "", "")
	if noOrigin.Code != http.StatusForbidden || !strings.Contains(noOrigin.Body.String(), `"origin_forbidden"`) {
		t.Fatalf("origin code=%d body=%s", noOrigin.Code, noOrigin.Body.String())
	}
	cookie, csrf := loginHuman(t, handler)
	body := `{"ref":"P-1","body":"Evidence","intent":"add_evidence"}`
	for name, test := range map[string]struct {
		origin, token, key string
		want               int
	}{
		"missing origin": {"", csrf, "comment-1", http.StatusForbidden},
		"wrong origin":   {"http://evil.test", csrf, "comment-1", http.StatusForbidden},
		"missing csrf":   {"http://commons.test", "", "comment-1", http.StatusForbidden},
		"wrong csrf":     {"http://commons.test", "wrong", "comment-1", http.StatusForbidden},
		"missing key":    {"http://commons.test", csrf, "", http.StatusBadRequest},
	} {
		recorder := authRequest(handler, http.MethodPost, "http://commons.test/v1/comments", body, test.origin, cookie, test.token, test.key)
		if recorder.Code != test.want {
			t.Errorf("%s code=%d body=%s", name, recorder.Code, recorder.Body.String())
		}
	}
	claim := authRequest(handler, http.MethodPost, "http://commons.test/v1/claims", `{"task":"T-1"}`, "http://commons.test", cookie, csrf, "claim-1")
	if claim.Code != http.StatusForbidden || !strings.Contains(claim.Body.String(), `"forbidden"`) {
		t.Fatalf("human claim code=%d body=%s", claim.Code, claim.Body.String())
	}
}

func TestHumanLoginRateLimitAndSecureTLSCookie(t *testing.T) {
	handler := humanTestHandler(&fakeBackend{})
	for i := 0; i < 5; i++ {
		recorder := authRequest(handler, http.MethodPost, "http://commons.test/v1/auth/login", `{"secret":"wrong"}`, "http://commons.test", nil, "", "")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d code=%d body=%s", i+1, recorder.Code, recorder.Body.String())
		}
	}
	limited := authRequest(handler, http.MethodPost, "http://commons.test/v1/auth/login", `{"secret":"wrong"}`, "http://commons.test", nil, "", "")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" ||
		!strings.Contains(limited.Body.String(), `"rate_limited"`) {
		t.Fatalf("limit code=%d retry=%q body=%s", limited.Code, limited.Header().Get("Retry-After"), limited.Body.String())
	}

	tlsHandler := humanTestHandler(&fakeBackend{})
	req := httptest.NewRequest(http.MethodPost, "https://commons.test/v1/auth/login", strings.NewReader(`{"secret":"`+testAdminSecret+`"}`))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://commons.test")
	recorder := httptest.NewRecorder()
	tlsHandler.ServeHTTP(recorder, req)
	cookies := recorder.Result().Cookies()
	if recorder.Code != http.StatusOK || len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("TLS cookie code=%d cookies=%v body=%s", recorder.Code, cookies, recorder.Body.String())
	}
}

func TestHumanSessionsAreBoundedWithoutSingleDeviceLogout(t *testing.T) {
	auth := newHumanAuth(&HumanAuthConfig{AdminSecret: testAdminSecret, DisplayName: "Cole", Actor: "admin", Session: "human", Host: "browser", SessionTTL: time.Hour})
	now := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }
	first, _, ok := auth.create()
	if !ok {
		t.Fatal("first session failed")
	}
	now = now.Add(time.Second)
	second, _, _ := auth.create()
	if _, ok := auth.lookup(first); !ok {
		t.Fatal("second device login invalidated first")
	}
	if _, ok := auth.lookup(second); !ok {
		t.Fatal("second device session missing")
	}
	for i := 0; i < 7; i++ {
		now = now.Add(time.Second)
		_, _, _ = auth.create()
	}
	if len(auth.sessions) != 8 {
		t.Fatalf("sessions=%d", len(auth.sessions))
	}
	if _, ok := auth.lookup(first); ok {
		t.Fatal("oldest session was not evicted at hard cap")
	}
}
