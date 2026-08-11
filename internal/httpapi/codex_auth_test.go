package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
)

type authTestCodexClient struct {
	mu           sync.Mutex
	available    bool
	device       codexauth.DeviceCode
	pollResults  []codexauth.LoginResult
	pollIDs      []string
	cancelledIDs []string
	handler      func(codexauth.Event)
}

func (c *authTestCodexClient) Available() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.available
}

func (c *authTestCodexClient) StartDeviceCode(context.Context) (codexauth.DeviceCode, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.available {
		return codexauth.DeviceCode{}, codexauth.ErrUnavailable
	}
	return c.device, nil
}

func (c *authTestCodexClient) PollLogin(_ context.Context, loginID string) (codexauth.LoginResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pollIDs = append(c.pollIDs, loginID)
	if len(c.pollResults) == 0 {
		return codexauth.LoginResult{State: "pending"}, nil
	}
	result := c.pollResults[0]
	c.pollResults = c.pollResults[1:]
	return result, nil
}

func (c *authTestCodexClient) CancelLogin(_ context.Context, loginID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelledIDs = append(c.cancelledIDs, loginID)
	return nil
}

func (c *authTestCodexClient) AccountState(context.Context) (codexauth.AccountState, error) {
	if !c.Available() {
		return codexauth.AccountUnknown, codexauth.ErrUnavailable
	}
	return codexauth.AccountSignedOut, nil
}

func (c *authTestCodexClient) SetEventHandler(handler func(codexauth.Event)) {
	c.mu.Lock()
	c.handler = handler
	c.mu.Unlock()
}

func (c *authTestCodexClient) Close() error { return nil }

type authTestBindingStore struct {
	mu          sync.Mutex
	binding     *domain.HumanAccountBinding
	bindCalls   []domain.BindHumanAccountRequest
	updateCalls []domain.UpdateHumanProfileRequest
	updateKeys  map[string]bool
}

func (s *authTestBindingStore) GetHumanAccountBinding(context.Context) (domain.HumanAccountBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding == nil {
		return domain.HumanAccountBinding{}, domain.ErrNotFound
	}
	return cloneAuthTestBinding(*s.binding), nil
}

func (s *authTestBindingStore) BindHumanAccount(_ context.Context, request domain.BindHumanAccountRequest) (domain.HumanAccountBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindCalls = append(s.bindCalls, request)
	if s.binding != nil {
		if hmac.Equal(s.binding.ProviderSubjectDigest, request.ProviderSubjectDigest) {
			return cloneAuthTestBinding(*s.binding), nil
		}
		return domain.HumanAccountBinding{}, domain.ErrConflict
	}
	s.binding = &domain.HumanAccountBinding{
		Principal:             domain.HumanLocalPrincipal,
		Provider:              "chatgpt",
		ProviderSubjectDigest: append([]byte(nil), request.ProviderSubjectDigest...),
		DisplayName:           request.DisplayName,
		Handle:                request.Handle,
		Revision:              1,
	}
	return cloneAuthTestBinding(*s.binding), nil
}

func (s *authTestBindingStore) UpdateHumanProfile(_ context.Context, request domain.UpdateHumanProfileRequest) (domain.HumanAccountBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateCalls = append(s.updateCalls, request)
	if s.updateKeys == nil {
		s.updateKeys = make(map[string]bool)
	}
	if s.binding == nil {
		return domain.HumanAccountBinding{}, domain.ErrNotFound
	}
	if s.updateKeys[request.IdempotencyKey] {
		return cloneAuthTestBinding(*s.binding), nil
	}
	if request.BaseRevision != s.binding.Revision {
		return domain.HumanAccountBinding{}, domain.ErrConflict
	}
	s.updateKeys[request.IdempotencyKey] = true
	s.binding.DisplayName = request.DisplayName
	s.binding.Handle = request.Handle
	s.binding.Revision++
	return cloneAuthTestBinding(*s.binding), nil
}

type authTestEventStore struct {
	mu     sync.Mutex
	events []domain.HumanAuthEventRequest
}

func (s *authTestEventStore) RecordHumanAuthEvent(_ context.Context, event domain.HumanAuthEventRequest) error {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	return nil
}

func cloneAuthTestBinding(binding domain.HumanAccountBinding) domain.HumanAccountBinding {
	binding.ProviderSubjectDigest = append([]byte(nil), binding.ProviderSubjectDigest...)
	return binding
}

func testCodexBindingKey() [32]byte {
	var key [32]byte
	for index := range key {
		key[index] = byte(index + 1)
	}
	return key
}

func testCodexDigest(key [32]byte, email string) []byte {
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(strings.ToLower(strings.TrimSpace(email))))
	return mac.Sum(nil)
}

func newCodexAuthTestHandler(recovery, allowLAN bool) (*handler, *authTestCodexClient, *authTestBindingStore, *authTestEventStore) {
	client := &authTestCodexClient{
		available: true,
		device: codexauth.DeviceCode{
			LoginID:         "login-test-1",
			VerificationURL: "https://auth.openai.com/codex/device",
			UserCode:        "ABCD-EFGH",
		},
	}
	binding := &authTestBindingStore{}
	events := &authTestEventStore{}
	h := NewHandler(&fakeBackend{}, Config{
		HumanAuth: &HumanAuthConfig{
			AdminSecret:     testAdminSecret,
			DisplayName:     "Local Admin",
			Handle:          "local-admin",
			Actor:           "local-admin",
			Principal:       domain.HumanLocalPrincipal,
			Session:         domain.HumanLegacySession,
			Host:            "browser",
			SessionTTL:      time.Hour,
			RecoveryEnabled: recovery,
			CodexEnabled:    true,
		},
		HumanBindingStore: binding,
		HumanAuthEvents:   events,
		CodexAuth: &CodexAuthConfig{
			Client:            client,
			BindingKey:        testCodexBindingKey(),
			AllowFirstBindLAN: allowLAN,
		},
	})
	return h.(*handler), client, binding, events
}

func codexAuthRequest(handler http.Handler, method, target, body, origin, remote string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.RemoteAddr = remote
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		if cookie != nil {
			request.AddCookie(cookie)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func startCodexAttempt(t *testing.T, handler http.Handler) (codexStartResult, *http.Cookie) {
	t.Helper()
	pairingCookie := &http.Cookie{
		Name:     pairingCookieName,
		Value:    "pairing-test-cookie",
		Path:     pairingCookiePath,
		MaxAge:   int(pairingTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	recorder := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/start", `{}`, "http://commons.test", "127.0.0.1:4321", pairingCookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("start code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data codexStartResult `json:"data"`
	}
	if err := decodeJSONResponse(recorder, &response); err != nil {
		t.Fatal(err)
	}
	return response.Data, pairingCookie
}

func decodeJSONResponse(recorder *httptest.ResponseRecorder, target any) error {
	if recorder == nil {
		return errors.New("nil response")
	}
	return json.Unmarshal(recorder.Body.Bytes(), target)
}

func TestCodexStartCookieOriginAndAttemptBinding(t *testing.T) {
	handler, client, _, _ := newCodexAuthTestHandler(false, false)

	for name, origin := range map[string]string{"missing origin": "", "wrong origin": "http://evil.test"} {
		recorder := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/start", `{}`, origin, "127.0.0.1:4321")
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"origin_forbidden"`) {
			t.Errorf("%s code=%d body=%s", name, recorder.Code, recorder.Body.String())
		}
	}

	generated := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/start", `{}`, "http://commons.test", "127.0.0.1:4321")
	if generated.Code != http.StatusOK {
		t.Fatalf("generated-cookie start code=%d body=%s", generated.Code, generated.Body.String())
	}
	generatedCookies := generated.Result().Cookies()
	if len(generatedCookies) != 1 {
		t.Errorf("start did not issue a pairing cookie: cookies=%v headers=%v", generatedCookies, generated.Header())
	} else if generatedCookies[0].Name != pairingCookieName || generatedCookies[0].Path != pairingCookiePath || !generatedCookies[0].HttpOnly || generatedCookies[0].SameSite != http.SameSiteStrictMode || generatedCookies[0].Secure || generatedCookies[0].MaxAge <= 0 {
		t.Errorf("unsafe pairing cookie=%+v", generatedCookies[0])
	}

	start, pairingCookie := startCodexAttempt(t, handler)
	if start.AttemptID == "" || start.VerificationURL != "https://auth.openai.com/codex/device" || start.UserCode != "ABCD-EFGH" || start.PollAfterMS <= 0 {
		t.Fatalf("start response=%+v", start)
	}
	duplicate := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/start", `{}`, "http://commons.test", "127.0.0.1:4321", pairingCookie)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), `"pairing_attempt_active"`) {
		t.Fatalf("duplicate start code=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	wrongCookie := &http.Cookie{Name: pairingCookieName, Value: "different-browser"}
	boundToOtherBrowser := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/poll", `{"attempt_id":"`+start.AttemptID+`"}`, "http://commons.test", "127.0.0.1:4321", wrongCookie)
	if boundToOtherBrowser.Code != http.StatusNotFound || !strings.Contains(boundToOtherBrowser.Body.String(), `"pairing_not_found"`) {
		t.Fatalf("cross-browser poll code=%d body=%s", boundToOtherBrowser.Code, boundToOtherBrowser.Body.String())
	}
	client.mu.Lock()
	if len(client.pollIDs) != 0 {
		t.Fatalf("cross-browser poll reached Codex client: %v", client.pollIDs)
	}
	client.mu.Unlock()
}

func TestCodexAccountUpdateWithoutHumanAuthIsSafe(t *testing.T) {
	client := &authTestCodexClient{available: true}
	NewHandler(&fakeBackend{}, Config{CodexAuth: &CodexAuthConfig{Client: client}})
	client.mu.Lock()
	handler := client.handler
	client.mu.Unlock()
	if handler == nil {
		t.Fatal("Codex event handler was not registered")
	}
	handler(codexauth.Event{Kind: "account_updated", AuthMode: "apiKey"})
}

func TestCodexAccountUpdateRevokesOnlyCodexSessions(t *testing.T) {
	handler, client, _, _ := newCodexAuthTestHandler(true, false)
	codexToken, _, ok := handler.humanAuth.create("codex", 1)
	if !ok {
		t.Fatal("failed to create Codex session")
	}
	recoveryToken, _, ok := handler.humanAuth.create("recovery", 1)
	if !ok {
		t.Fatal("failed to create recovery session")
	}
	client.mu.Lock()
	eventHandler := client.handler
	client.mu.Unlock()
	eventHandler(codexauth.Event{Kind: "account_updated", AuthMode: "chatgpt"})
	if _, ok := handler.humanAuth.lookup(codexToken); ok {
		t.Fatal("authoritative ChatGPT account update left a stale Codex session active")
	}
	if _, ok := handler.humanAuth.lookup(recoveryToken); !ok {
		t.Fatal("account update revoked the independent recovery session")
	}
}

func TestCodexPairingExpiryPollRateAndAttemptBinding(t *testing.T) {
	manager := newPairingManager()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }

	attempt, err := manager.reserve("cookie-a", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !manager.attachLogin(attempt.id, "login-a") {
		t.Fatal("failed to attach login")
	}
	if _, found, expired := manager.lookup(attempt.id, "cookie-b"); found || expired {
		t.Fatal("pairing was usable with another browser cookie")
	}

	active, loginID, err := manager.beginPoll(attempt.id)
	if err != nil || active.state != pairingWaiting || loginID != "login-a" {
		t.Fatalf("first poll active=%+v login=%q err=%v", active, loginID, err)
	}
	manager.finishPoll(attempt.id)
	if _, _, err := manager.beginPoll(attempt.id); err == nil || !strings.Contains(err.Error(), "too soon") {
		t.Fatalf("rapid second poll error=%v, want poll rate limit", err)
	}

	now = now.Add(minPollInterval)
	if _, _, err := manager.beginPoll(attempt.id); err != nil {
		t.Fatalf("poll after interval error=%v", err)
	}
	manager.finishPoll(attempt.id)
	now = now.Add(pairingTTL)
	if expired, found, isExpired := manager.lookup(attempt.id, "cookie-a"); !found || !isExpired || expired.state != pairingExpired {
		t.Fatalf("expired lookup=%+v found=%v expired=%v", expired, found, isExpired)
	}
	if _, found, _ := manager.lookup(attempt.id, "cookie-a"); found {
		t.Fatal("expired pairing remained available")
	}
}

func TestCodexIdentityUnavailableAndAccountMismatch(t *testing.T) {
	handler, client, _, _ := newCodexAuthTestHandler(false, false)
	client.pollResults = []codexauth.LoginResult{{State: "success", Account: nil}}
	start, cookie := startCodexAttempt(t, handler)
	recorder := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/poll", `{"attempt_id":"`+start.AttemptID+`"}`, "http://commons.test", "127.0.0.1:4321", cookie)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"codex_identity_unavailable"`) || !strings.Contains(recorder.Body.String(), `"state":"failed"`) {
		t.Fatalf("identity-unavailable code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != pairingCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("identity-unavailable did not clear pairing cookie: %v", cookies)
	}

	key := testCodexBindingKey()
	handler, client, binding, _ := newCodexAuthTestHandler(false, false)
	binding.binding = &domain.HumanAccountBinding{
		Principal:             domain.HumanLocalPrincipal,
		Provider:              "chatgpt",
		ProviderSubjectDigest: testCodexDigest(key, "bound@example.com"),
		DisplayName:           "Bound Admin",
		Handle:                "bound-admin",
		Revision:              4,
	}
	codexSession, _, ok := handler.humanAuth.create("codex", binding.binding.Revision)
	if !ok {
		t.Fatal("failed to create existing Codex session")
	}
	recoverySession, _, ok := handler.humanAuth.create("recovery", binding.binding.Revision)
	if !ok {
		t.Fatal("failed to create existing recovery session")
	}
	email := "other@example.com"
	client.pollResults = []codexauth.LoginResult{{State: "success", Account: &codexauth.Account{Type: "chatgpt", Email: &email}}}
	start, cookie = startCodexAttempt(t, handler)
	recorder = codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/poll", `{"attempt_id":"`+start.AttemptID+`"}`, "http://commons.test", "127.0.0.1:4321", cookie)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, `"account_mismatch"`) || !strings.Contains(body, `"state":"failed"`) {
		t.Fatalf("account-mismatch code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(body, email) || strings.Contains(body, "bound@example.com") {
		t.Fatalf("account-mismatch response disclosed an account identity: %s", body)
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != pairingCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("account-mismatch did not clear pairing cookie: %v", cookies)
	}
	binding.mu.Lock()
	if len(binding.bindCalls) != 0 {
		t.Fatalf("mismatch attempted a new bind: %+v", binding.bindCalls)
	}
	binding.mu.Unlock()
	if _, ok := handler.humanAuth.lookup(codexSession); !ok {
		t.Fatal("an unauthenticated mismatched pairing revoked an existing Codex session")
	}
	if _, ok := handler.humanAuth.lookup(recoverySession); !ok {
		t.Fatal("account mismatch revoked a recovery session")
	}
}

func TestRecoveryAuditKeyIsServerGenerated(t *testing.T) {
	handler, _, _, events := newCodexAuthTestHandler(true, false)
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "http://commons.test/v1/auth/login", strings.NewReader(`{"secret":"`+testAdminSecret+`"}`))
		request.Host = "commons.test"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://commons.test")
		request.Header.Set("X-Request-ID", "caller-reused-request-id")
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("recovery login %d code=%d body=%s", i, recorder.Code, recorder.Body.String())
		}
	}
	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.events) != 2 {
		t.Fatalf("recovery audit events=%d, want 2", len(events.events))
	}
	if events.events[0].IdempotencyKey == events.events[1].IdempotencyKey {
		t.Fatalf("recovery audit keys reused caller input: %q", events.events[0].IdempotencyKey)
	}
}

func TestCodexFirstBindProfileCreatesCodexSession(t *testing.T) {
	handler, client, binding, _ := newCodexAuthTestHandler(false, false)
	email := "first@example.com"
	client.pollResults = []codexauth.LoginResult{{State: "success", Account: &codexauth.Account{Type: "chatgpt", Email: &email}}}
	start, pairingCookie := startCodexAttempt(t, handler)
	poll := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/poll", `{"attempt_id":"`+start.AttemptID+`"}`, "http://commons.test", "127.0.0.1:4321", pairingCookie)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), `"state":"needs_profile"`) {
		t.Fatalf("needs-profile poll code=%d body=%s", poll.Code, poll.Body.String())
	}

	wrongOrigin := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/profile", `{"attempt_id":"`+start.AttemptID+`","display_name":"First Admin","handle":"first-admin"}`, "http://evil.test", "127.0.0.1:4321", pairingCookie)
	if wrongOrigin.Code != http.StatusForbidden || !strings.Contains(wrongOrigin.Body.String(), `"origin_forbidden"`) {
		t.Fatalf("profile origin code=%d body=%s", wrongOrigin.Code, wrongOrigin.Body.String())
	}

	profile := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/profile", `{"attempt_id":"`+start.AttemptID+`","display_name":"First Admin","handle":"first-admin"}`, "http://commons.test", "127.0.0.1:4321", pairingCookie)
	if profile.Code != http.StatusOK || !strings.Contains(profile.Body.String(), `"auth_method":"codex"`) || !strings.Contains(profile.Body.String(), `"profile_revision":1`) || !strings.Contains(profile.Body.String(), `"handle":"first-admin"`) {
		t.Fatalf("profile code=%d body=%s", profile.Code, profile.Body.String())
	}
	cookies := profile.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("profile cookies=%v", cookies)
	}
	var sessionCookie, clearedPairing *http.Cookie
	for _, cookie := range cookies {
		switch cookie.Name {
		case humanSessionCookie:
			sessionCookie = cookie
		case pairingCookieName:
			clearedPairing = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode || sessionCookie.Path != "/" || sessionCookie.Value == "" {
		t.Fatalf("unsafe human session cookie=%+v", sessionCookie)
	}
	if clearedPairing == nil || clearedPairing.Path != pairingCookiePath || clearedPairing.MaxAge >= 0 || !clearedPairing.HttpOnly || clearedPairing.SameSite != http.SameSiteStrictMode {
		t.Fatalf("pairing cookie was not cleared safely=%+v", clearedPairing)
	}
	binding.mu.Lock()
	if len(binding.bindCalls) != 1 || !hmac.Equal(binding.bindCalls[0].ProviderSubjectDigest, testCodexDigest(testCodexBindingKey(), email)) {
		t.Fatalf("bind calls=%+v", binding.bindCalls)
	}
	binding.mu.Unlock()
}

func TestRecoveryLoginCompatibilityAndExplicitDisablement(t *testing.T) {
	handler, _, _, events := newCodexAuthTestHandler(true, false)
	wrongOrigin := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/login", `{"secret":"`+testAdminSecret+`"}`, "http://evil.test", "127.0.0.1:4321")
	if wrongOrigin.Code != http.StatusForbidden || !strings.Contains(wrongOrigin.Body.String(), `"origin_forbidden"`) {
		t.Fatalf("wrong-origin recovery login code=%d body=%s", wrongOrigin.Code, wrongOrigin.Body.String())
	}
	login := codexAuthRequest(handler, http.MethodPost, "http://commons.test/v1/auth/login", `{"secret":"`+testAdminSecret+`"}`, "http://commons.test", "127.0.0.1:4321")
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"auth_method":"recovery"`) || !strings.Contains(login.Body.String(), `"principal"`) {
		t.Fatalf("recovery login code=%d body=%s", login.Code, login.Body.String())
	}
	var loginResponse struct {
		Data authSessionResult `json:"data"`
	}
	if err := decodeJSONResponse(login, &loginResponse); err != nil {
		t.Fatal(err)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != humanSessionCookie || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("recovery cookie=%v", cookies)
	}
	wrongCSRF := authRequest(handler, http.MethodPost, "http://commons.test/v1/auth/logout", `{}`, "http://commons.test", cookies[0], "wrong-csrf", "")
	if wrongCSRF.Code != http.StatusForbidden || !strings.Contains(wrongCSRF.Body.String(), `"csrf_failed"`) {
		t.Fatalf("wrong-CSRF logout code=%d body=%s", wrongCSRF.Code, wrongCSRF.Body.String())
	}
	wrongLogoutOrigin := authRequest(handler, http.MethodPost, "http://commons.test/v1/auth/logout", `{}`, "http://evil.test", cookies[0], loginResponse.Data.CSRFToken, "")
	if wrongLogoutOrigin.Code != http.StatusForbidden || !strings.Contains(wrongLogoutOrigin.Body.String(), `"origin_forbidden"`) {
		t.Fatalf("wrong-origin logout code=%d body=%s", wrongLogoutOrigin.Code, wrongLogoutOrigin.Body.String())
	}
	logout := authRequest(handler, http.MethodPost, "http://commons.test/v1/auth/logout", `{}`, "http://commons.test", cookies[0], loginResponse.Data.CSRFToken, "")
	if logout.Code != http.StatusOK || !strings.Contains(logout.Body.String(), `"authenticated":false`) {
		t.Fatalf("logout code=%d body=%s", logout.Code, logout.Body.String())
	}
	events.mu.Lock()
	if len(events.events) != 1 || events.events[0].EventType != "recovery_login" || events.events[0].IdempotencyKey == "" {
		t.Fatalf("recovery events=%+v", events.events)
	}
	events.mu.Unlock()

	disabled, _, _, _ := newCodexAuthTestHandler(false, false)
	recorder := codexAuthRequest(disabled, http.MethodPost, "http://commons.test/v1/auth/login", `{"secret":"`+testAdminSecret+`"}`, "http://commons.test", "127.0.0.1:4321")
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"recovery_disabled"`) {
		t.Fatalf("disabled recovery code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAuthOriginAndCSRFTokenBoundaries(t *testing.T) {
	for name, request := range map[string]*http.Request{
		"same origin":    httptest.NewRequest(http.MethodPost, "http://commons.test/v1/auth/logout", nil),
		"wrong host":     httptest.NewRequest(http.MethodPost, "http://commons.test/v1/auth/logout", nil),
		"credentialed":   httptest.NewRequest(http.MethodPost, "http://commons.test/v1/auth/logout", nil),
		"null origin":    httptest.NewRequest(http.MethodPost, "http://commons.test/v1/auth/logout", nil),
		"missing origin": httptest.NewRequest(http.MethodPost, "http://commons.test/v1/auth/logout", nil),
	} {
		switch name {
		case "same origin":
			request.Header.Set("Origin", "http://commons.test")
		case "wrong host":
			request.Header.Set("Origin", "http://evil.test")
		case "credentialed":
			request.Header.Set("Origin", "http://commons.test")
			request.Header.Set("Referer", "http://commons.test/v1/auth/logout")
		case "null origin":
			request.Header.Set("Origin", "null")
		}
		want := name == "same origin" || name == "credentialed"
		if got := sameOrigin(request); got != want {
			t.Errorf("%s sameOrigin=%v, want %v", name, got, want)
		}
	}

	for _, test := range []struct {
		left, right string
		want        bool
	}{
		{left: "csrf-token", right: "csrf-token", want: true},
		{left: "csrf-token", right: "other-token", want: false},
		{left: "", right: "", want: true},
		{left: "csrf-token", right: "", want: false},
	} {
		if got := constantTimeTokenEqual(test.left, test.right); got != test.want {
			t.Errorf("constantTimeTokenEqual(%q,%q)=%v, want %v", test.left, test.right, got, test.want)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "http://commons.test/v1/auth/session", nil)
	response := httptest.NewRecorder()
	setHumanCookie(response, request, "opaque-session", time.Now().Add(time.Hour), 3600)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != humanSessionCookie || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" || cookies[0].Value != "opaque-session" {
		t.Fatalf("session cookie=%v", cookies)
	}
}
