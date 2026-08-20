package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/demodata"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/server"
	commonsstore "codex-commons/internal/store"
)

const runtimeAdminSecret = "slice-10-disposable-human-secret"

type continuationCodexClient struct {
	loginID string
	email   string
	handler func(codexauth.Event)
}

func (c *continuationCodexClient) Available() bool { return true }
func (c *continuationCodexClient) StartDeviceCode(context.Context) (codexauth.DeviceCode, error) {
	return codexauth.DeviceCode{LoginID: c.loginID, VerificationURL: "https://auth.openai.com/codex/device", UserCode: "TEST-CODE"}, nil
}
func (c *continuationCodexClient) PollLogin(context.Context, string) (codexauth.LoginResult, error) {
	email := c.email
	return codexauth.LoginResult{State: "success", Account: &codexauth.Account{Type: "chatgpt", Email: &email}}, nil
}
func (*continuationCodexClient) CancelLogin(context.Context, string) error { return nil }
func (*continuationCodexClient) AccountState(context.Context) (codexauth.AccountState, error) {
	return codexauth.AccountSignedIn, nil
}
func (c *continuationCodexClient) SetEventHandler(handler func(codexauth.Event)) { c.handler = handler }
func (*continuationCodexClient) Close() error                                    { return nil }

func TestShortHumanSecretRequiresExplicitInsecureLANMode(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.HumanAuth = &httpapi.HumanAuthConfig{
		AdminSecret: "shortkey", DisplayName: "Local admin", Actor: "local-admin",
		Session: "human-local-admin", Host: "browser", SessionTTL: time.Hour,
		RecoveryEnabled: true,
	}
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "24..1024") {
		t.Fatalf("short secret accepted without explicit insecure mode: %v", err)
	}
	config.AllowInsecureHumanLAN = true
	if err := config.Validate(); err != nil {
		t.Fatalf("explicit trusted-LAN evaluation secret rejected: %v", err)
	}
	config.HumanAuth.AdminSecret = "shorter"
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "8..1024") {
		t.Fatalf("sub-eight-byte secret accepted in insecure mode: %v", err)
	}
}

func TestCodexFirstBindLANRequiresDedicatedAcknowledgementAndLoadedKey(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.Listen = "192.168.1.60:8088"
	config.CodexAuth = true
	config.CodexBin = "/usr/bin/codex"
	config.CodexBindingKey[0] = 1
	config.CodexBindingKeySet = true
	config.AllowInsecureHumanLAN = true

	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "first-bind acknowledgement") {
		t.Fatalf("insecure-human-LAN acknowledgement bypassed first-bind policy: %v", err)
	}
	config.AllowFirstCodexBindLAN = true
	if err := config.Validate(); err != nil {
		t.Fatalf("dedicated first-bind acknowledgement rejected: %v", err)
	}
	config.CodexBindingKeySet = false
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "loaded, non-zero binding key") {
		t.Fatalf("unset binding key accepted: %v", err)
	}
}

func TestHumanConfigSecretSourcesAndLANAcknowledgement(t *testing.T) {
	env := map[string]string{
		"COMMONS_DB":                    filepath.Join(t.TempDir(), "commons.sqlite"),
		"COMMONS_LISTEN":                "192.168.1.60:8088",
		"COMMONS_HUMAN_ADMIN_SECRET":    runtimeAdminSecret,
		"COMMONS_ENABLE_RECOVERY_LOGIN": "true",
	}
	getenv := func(key string) string { return env[key] }
	if _, err := server.ParseConfig(nil, getenv, io.Discard); err == nil || !strings.Contains(err.Error(), "allow-insecure-human-lan") {
		t.Fatalf("plaintext LAN auth did not fail closed: %v", err)
	}
	env["COMMONS_ALLOW_INSECURE_HUMAN_LAN"] = "true"
	config, err := server.ParseConfig(nil, getenv, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if config.HumanAuth == nil || config.HumanAuth.DisplayName != "Local admin" || config.HumanAuth.AdminSecret != runtimeAdminSecret {
		t.Fatalf("human config=%+v", config.HumanAuth)
	}
	if _, err := server.ParseConfig([]string{"--human-admin-secret", "must-not-be-argv"}, getenv, io.Discard); err == nil {
		t.Fatal("secret-bearing command-line flag was accepted")
	}

	secretFile := filepath.Join(t.TempDir(), "admin-secret")
	if err := os.WriteFile(secretFile, []byte(runtimeAdminSecret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secretFile, 0o644); err != nil {
		t.Fatal(err)
	}
	fileEnv := map[string]string{"COMMONS_DB": filepath.Join(t.TempDir(), "file.sqlite"), "COMMONS_HUMAN_ADMIN_SECRET_FILE": secretFile, "COMMONS_ENABLE_RECOVERY_LOGIN": "true"}
	if _, err := server.ParseConfig(nil, func(key string) string { return fileEnv[key] }, io.Discard); err == nil || !strings.Contains(err.Error(), "mode 0600") {
		t.Fatalf("permissive secret file accepted: %v", err)
	}
	if err := os.Chmod(secretFile, 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := server.ParseConfig(nil, func(key string) string { return fileEnv[key] }, io.Discard)
	if err != nil || parsed.HumanAuth == nil || parsed.HumanAuth.AdminSecret != runtimeAdminSecret {
		t.Fatalf("mode-0600 secret file config=%+v err=%v", parsed.HumanAuth, err)
	}
	fileEnv["COMMONS_HUMAN_ADMIN_SECRET"] = runtimeAdminSecret
	if _, err := server.ParseConfig(nil, func(key string) string { return fileEnv[key] }, io.Discard); err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("dual secret sources accepted: %v", err)
	}
}

func TestCodexUnavailableDoesNotPreventCommonsStartup(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.WebDir = testWeb(t)
	config.CodexAuth = true
	config.CodexBin = "/definitely/missing/codex"
	config.CodexBindingKey[0] = 1
	config.CodexBindingKeySet = true
	app, err := server.New(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	status := runtimeRequest(app.Handler(), http.MethodGet, "http://commons.test/v1/auth/codex/status", "", nil, "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"available":false`) {
		t.Fatalf("unavailable Codex status code=%d body=%s", status.Code, status.Body.String())
	}
}

type authData struct {
	Authenticated bool   `json:"authenticated"`
	CSRFToken     string `json:"csrf_token"`
}

func TestServerRejectsUnexpectedHostBeforeAuthentication(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.WebDir = testWeb(t)
	config.HumanAuth = &httpapi.HumanAuthConfig{
		AdminSecret: runtimeAdminSecret, DisplayName: "Local admin", Actor: "local-admin",
		Session: "human-local-admin", Host: "browser", SessionTTL: time.Hour,
		RecoveryEnabled: true,
	}
	app, err := server.New(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	request := httptest.NewRequest(http.MethodPost, "http://attacker.example/v1/auth/login", strings.NewReader(`{"secret":"`+runtimeAdminSecret+`"}`))
	request.Host = "attacker.example"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://attacker.example")
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMisdirectedRequest {
		t.Fatalf("unexpected Host code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPublicOriginMuxKeepsInternalReadinessDirectOnly(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.WebDir = testWeb(t)
	config.PublicOrigin = "https://commons.plumbob.lan"
	app, err := server.New(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	request := func(host string, forwarded bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8088/v1/internal/readiness", nil)
		req.Host = host
		req.RemoteAddr = "127.0.0.1:43123"
		if forwarded {
			req.Header.Set("X-Forwarded-For", "127.0.0.1")
		}
		recorder := httptest.NewRecorder()
		app.Handler().ServeHTTP(recorder, req)
		return recorder
	}
	if direct := request("127.0.0.1:8088", false); direct.Code != http.StatusOK {
		t.Fatalf("direct readiness code=%d body=%s", direct.Code, direct.Body.String())
	}
	if public := request("commons.plumbob.lan", false); public.Code != http.StatusNotFound {
		t.Fatalf("public readiness code=%d body=%s", public.Code, public.Body.String())
	}
	if forwarded := request("127.0.0.1:8088", true); forwarded.Code != http.StatusNotFound {
		t.Fatalf("forwarded readiness code=%d body=%s", forwarded.Code, forwarded.Body.String())
	}
}

func runtimeRequest(handler http.Handler, method, target, body string, cookie *http.Cookie, csrf, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:8088"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		req.Header.Set("Origin", "http://127.0.0.1:8088")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-Commons-CSRF", csrf)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func runtimeLogin(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	recorder := runtimeRequest(handler, http.MethodPost, "http://commons.test/v1/auth/login", `{"secret":"`+runtimeAdminSecret+`"}`, nil, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("login code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data authData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || response.Data.CSRFToken == "" {
		t.Fatalf("login cookies=%v data=%+v", cookies, response.Data)
	}
	return cookies[0], response.Data.CSRFToken
}

func runtimeCodexRequest(handler http.Handler, method, target, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:8088"
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8088")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func runtimeCodexStart(t *testing.T, handler http.Handler) (string, *http.Cookie) {
	t.Helper()
	response := runtimeCodexRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/start", `{}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("Codex start code=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			AttemptID string `json:"attempt_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.AttemptID == "" {
		t.Fatalf("Codex start body=%s err=%v", response.Body.String(), err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Codex start cookie count=%d, want 1", len(cookies))
	}
	return envelope.Data.AttemptID, cookies[0]
}

func runtimeCodexPoll(t *testing.T, handler http.Handler, attemptID string, pairing *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return runtimeCodexRequest(handler, http.MethodPost, "http://commons.test/v1/auth/codex/poll", `{"attempt_id":"`+attemptID+`"}`, pairing)
}

func humanSessionCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == httpapi.HumanSessionCookieName && cookie.Value != "" && cookie.MaxAge >= 0 {
			return cookie
		}
	}
	t.Fatal("human session cookie missing")
	return nil
}

func assertArchaeologyContinuation(t *testing.T, handler http.Handler, cookie *http.Cookie) {
	t.Helper()
	response := runtimeRequest(handler, http.MethodGet, "http://commons.test/v1/project-archaeology", "", cookie, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("archaeology continuation code=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			ID        string `json:"id"`
			State     string `json:"state"`
			Discovery struct {
				State              string            `json:"state"`
				Candidates         []json.RawMessage `json:"candidates"`
				SourceRootsScanned int               `json:"source_roots_scanned"`
				MetadataOnly       bool              `json:"metadata_only"`
			} `json:"discovery"`
			Config struct {
				SelectedProjectIDs []string `json:"selected_project_ids"`
				Depth              string   `json:"depth"`
				MaxConcurrency     int      `json:"max_concurrency"`
			} `json:"config"`
			Runs         []json.RawMessage          `json:"runs"`
			Revision     int64                      `json:"revision"`
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode archaeology continuation: %v body=%s", err, response.Body.String())
	}
	data := envelope.Data
	if data.ID != "" || data.State != "draft" || data.Discovery.State != "idle" || !data.Discovery.MetadataOnly || data.Discovery.SourceRootsScanned != 0 || data.Config.Depth != "standard" || data.Config.MaxConcurrency != 2 || data.Revision != 0 {
		t.Fatalf("unexpected virtual draft: %+v", data)
	}
	if data.Discovery.Candidates == nil || data.Config.SelectedProjectIDs == nil || data.Runs == nil {
		t.Fatalf("bounded response collections must be JSON arrays: %s", response.Body.String())
	}
	for _, name := range []string{"discovery", "historian_handoff", "review", "canonical_apply"} {
		raw, ok := data.Capabilities[name]
		if !ok {
			t.Fatalf("missing capability %q: %s", name, response.Body.String())
		}
		var capability struct {
			Configured bool   `json:"configured"`
			Available  bool   `json:"available"`
			Mode       string `json:"mode"`
		}
		if err := json.Unmarshal(raw, &capability); err != nil || capability.Mode == "" {
			t.Fatalf("invalid capability %q: %s", name, raw)
		}
	}
}

func TestCodexFirstProfileAndExistingBindingReceiveValidArchaeologyContinuation(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	var bindingKey [32]byte
	for index := range bindingKey {
		bindingKey[index] = byte(index + 1)
	}
	newConfig := func(loginID string) server.Config {
		config := server.DefaultConfig()
		config.DatabasePath = database
		config.WebDir = testWeb(t)
		config.CodexAuth = true
		config.CodexBin = "/usr/bin/codex"
		config.CodexClient = &continuationCodexClient{loginID: loginID, email: "continuation@example.com"}
		config.CodexBindingKey = bindingKey
		config.CodexBindingKeySet = true
		return config
	}

	first, err := server.New(ctx, newConfig("first-login"), nil)
	if err != nil {
		t.Fatal(err)
	}
	anonymous := runtimeRequest(first.Handler(), http.MethodGet, "http://commons.test/v1/project-archaeology", "", nil, "", "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous archaeology read code=%d body=%s", anonymous.Code, anonymous.Body.String())
	}
	attemptID, pairing := runtimeCodexStart(t, first.Handler())
	poll := runtimeCodexPoll(t, first.Handler(), attemptID, pairing)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), `"state":"needs_profile"`) {
		t.Fatalf("first Codex poll code=%d body=%s", poll.Code, poll.Body.String())
	}
	profile := runtimeCodexRequest(first.Handler(), http.MethodPost, "http://commons.test/v1/auth/codex/profile", `{"attempt_id":"`+attemptID+`","display_name":"Continuation Admin","handle":"continuation-admin"}`, pairing)
	if profile.Code != http.StatusOK {
		t.Fatalf("Codex profile code=%d body=%s", profile.Code, profile.Body.String())
	}
	assertArchaeologyContinuation(t, first.Handler(), humanSessionCookie(t, profile))
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := server.New(ctx, newConfig("existing-login"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	attemptID, pairing = runtimeCodexStart(t, second.Handler())
	poll = runtimeCodexPoll(t, second.Handler(), attemptID, pairing)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), `"auth_method":"codex"`) {
		t.Fatalf("existing Codex poll code=%d body=%s", poll.Code, poll.Body.String())
	}
	assertArchaeologyContinuation(t, second.Handler(), humanSessionCookie(t, poll))
}

func TestHumanPostCommentStateDurabilityAndSessionRestart(t *testing.T) {
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	config := server.DefaultConfig()
	config.DatabasePath = database
	config.WebDir = testWeb(t)
	config.DemoSeed = true
	config.HumanAuth = &httpapi.HumanAuthConfig{
		AdminSecret: runtimeAdminSecret, DisplayName: "Test Admin", Actor: "local-admin",
		Session: "human-local-admin", Host: "browser", SessionTTL: time.Hour,
		RecoveryEnabled: true,
	}
	first, err := server.New(context.Background(), config, demodata.Seed)
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := runtimeLogin(t, first.Handler())
	post := runtimeRequest(first.Handler(), http.MethodPost, "http://commons.test/v1/posts",
		`{"topic":"demo-billing-orchestrator","kind":"finding","title":"Human durable write","body":"Post survives restart","basis":"Slice 10 E2E"}`,
		cookie, csrf, "human-e2e-post")
	if post.Code != http.StatusOK {
		t.Fatalf("post code=%d body=%s", post.Code, post.Body.String())
	}
	var postResponse struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(post.Body.Bytes(), &postResponse); err != nil || postResponse.Data.ID == "" {
		t.Fatalf("post response=%s err=%v", post.Body.String(), err)
	}
	postID := postResponse.Data.ID
	comment := runtimeRequest(first.Handler(), http.MethodPost, "http://commons.test/v1/comments",
		`{"ref":"`+postID+`","body":"This adds reproducible evidence.","intent":"add_evidence"}`,
		cookie, csrf, "human-e2e-comment")
	if comment.Code != http.StatusOK {
		t.Fatalf("comment code=%d body=%s", comment.Code, comment.Body.String())
	}
	state := runtimeRequest(first.Handler(), http.MethodPost, "http://commons.test/v1/post-states",
		`{"ref":"`+postID+`","state":"resolved"}`, cookie, csrf, "human-e2e-state")
	if state.Code != http.StatusOK {
		t.Fatalf("state code=%d body=%s", state.Code, state.Body.String())
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	config.DemoSeed = false
	second, err := server.New(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	oldStatus := runtimeRequest(second.Handler(), http.MethodGet, "http://commons.test/v1/auth/session", "", cookie, "", "")
	if oldStatus.Code != http.StatusOK || !strings.Contains(oldStatus.Body.String(), `"authenticated":true`) || !strings.Contains(oldStatus.Body.String(), `"csrf_token":"`) {
		t.Fatalf("durable browser session did not recover: %s", oldStatus.Body.String())
	}
	var recovered struct {
		Data authData `json:"data"`
	}
	if err = json.Unmarshal(oldStatus.Body.Bytes(), &recovered); err != nil || recovered.Data.CSRFToken == "" || recovered.Data.CSRFToken == csrf {
		t.Fatalf("recovered session did not rotate csrf: body=%s err=%v", oldStatus.Body.String(), err)
	}
	writeBody := `{"topic":"demo-billing-orchestrator","kind":"finding","title":"After restart","body":"Rotated CSRF only","basis":"Restart test"}`
	for name, token := range map[string]string{"missing": "", "old": csrf} {
		failed := runtimeRequest(second.Handler(), http.MethodPost, "http://commons.test/v1/posts", writeBody, cookie, token, "restart-csrf-"+name)
		if failed.Code != http.StatusForbidden {
			t.Fatalf("%s csrf code=%d body=%s", name, failed.Code, failed.Body.String())
		}
	}
	succeeded := runtimeRequest(second.Handler(), http.MethodPost, "http://commons.test/v1/posts", writeBody, cookie, recovered.Data.CSRFToken, "restart-csrf-new")
	if succeeded.Code != http.StatusOK {
		t.Fatalf("rotated csrf write code=%d body=%s", succeeded.Code, succeeded.Body.String())
	}
	opened := runtimeRequest(second.Handler(), http.MethodGet, "http://commons.test/v1/posts/"+postID+"?comments_limit=20", "", cookie, "", "")
	if opened.Code != http.StatusOK || !strings.Contains(opened.Body.String(), `"state":"resolved"`) ||
		!strings.Contains(opened.Body.String(), `"intent":"add_evidence"`) || !strings.Contains(opened.Body.String(), `"Human durable write"`) {
		t.Fatalf("durable thread code=%d body=%s", opened.Code, opened.Body.String())
	}
	logout := runtimeRequest(second.Handler(), http.MethodPost, "http://commons.test/v1/auth/logout", `{}`, cookie, recovered.Data.CSRFToken, "")
	if logout.Code != http.StatusOK {
		t.Fatalf("logout code=%d body=%s", logout.Code, logout.Body.String())
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := server.New(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	loggedOut := runtimeRequest(third.Handler(), http.MethodGet, "http://commons.test/v1/auth/session", "", cookie, "", "")
	if !strings.Contains(loggedOut.Body.String(), `"authenticated":false`) {
		t.Fatalf("logout did not survive restart: %s", loggedOut.Body.String())
	}
}

func TestAnonymousReadHonorsHumanSessionCookie(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.WebDir = testWeb(t)
	config.AnonymousRead = true
	config.HumanAuth = &httpapi.HumanAuthConfig{
		AdminSecret: runtimeAdminSecret, DisplayName: "Test Admin", Actor: "local-admin",
		Session: "human-local-admin", Host: "browser", SessionTTL: time.Hour,
		RecoveryEnabled: true,
	}
	app, err := server.New(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	anonymous := runtimeRequest(app.Handler(), http.MethodGet, "http://commons.test/v1/notifications", "", nil, "", "")
	if anonymous.Code != http.StatusForbidden {
		t.Fatalf("anonymous notifications code=%d body=%s", anonymous.Code, anonymous.Body.String())
	}
	cookie, _ := runtimeLogin(t, app.Handler())
	listed := runtimeRequest(app.Handler(), http.MethodGet, "http://commons.test/v1/notifications", "", cookie, "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"items":[]`) {
		t.Fatalf("human notifications code=%d body=%s", listed.Code, listed.Body.String())
	}
	stale := &http.Cookie{Name: httpapi.HumanSessionCookieName, Value: "invalid"}
	invalid := runtimeRequest(app.Handler(), http.MethodGet, "http://commons.test/v1/projects", "", stale, "", "")
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid human cookie did not fail closed: code=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func agentRequest(handler http.Handler, method, target, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer configured-agent-secret")
	req.Host = "127.0.0.1:8088"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestConfiguredAgentStartupIdentitySupportsWritesMentionsAndRestart(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	durable, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	if err = durable.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Status: "active", Purpose: "test"}); err != nil {
		t.Fatal(err)
	}
	if err = durable.CreateTopic(ctx, domain.Topic{ID: "alpha-posts", ProjectID: "alpha", Name: "Alpha posts"}); err != nil {
		t.Fatal(err)
	}
	if err = durable.Close(); err != nil {
		t.Fatal(err)
	}

	credential := httpapi.Credential{BearerToken: "configured-agent-secret", Actor: "agent-alpha", Session: "session-alpha", Host: "host-alpha", Project: "alpha", Purpose: "Dogfood coordination"}
	config := server.DefaultConfig()
	config.DatabasePath = database
	config.WebDir = testWeb(t)
	config.Credentials = []httpapi.Credential{credential}
	first, err := server.New(ctx, config, nil)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"topic":"alpha-posts","kind":"question","title":"Configured identity","body":"Can the registered agent write?","basis":"Runtime E2E","mentions":[{"principal":"human:local-admin"}]}`
	created := agentRequest(first.Handler(), http.MethodPost, "http://commons.test/v1/posts", body, "configured-agent-post")
	if created.Code != http.StatusOK {
		t.Fatalf("configured agent post code=%d body=%s", created.Code, created.Body.String())
	}
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil || response.Data.ID == "" {
		t.Fatalf("post response=%s err=%v", created.Body.String(), err)
	}
	contributors := agentRequest(first.Handler(), http.MethodGet, "http://commons.test/v1/contributors?project=alpha&limit=10", "", "")
	if contributors.Code != http.StatusOK || !strings.Contains(contributors.Body.String(), `"session":"session-alpha"`) || !strings.Contains(contributors.Body.String(), `"host":"host-alpha"`) || !strings.Contains(contributors.Body.String(), `"project":{"id":"alpha"`) || !strings.Contains(contributors.Body.String(), `"purpose":"Dogfood coordination"`) {
		t.Fatalf("contributors code=%d body=%s", contributors.Code, contributors.Body.String())
	}
	people := agentRequest(first.Handler(), http.MethodGet, "http://commons.test/v1/people?limit=10", "", "")
	if people.Code != http.StatusOK || !strings.Contains(people.Body.String(), `"total":0`) {
		t.Fatalf("configured identity fabricated presence: code=%d body=%s", people.Code, people.Body.String())
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}

	audit, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	var mentions, notifications int
	if err := audit.DB().QueryRowContext(ctx, `SELECT count(*) FROM content_mentions WHERE source_kind='post' AND source_id=? AND recipient_principal=?`, response.Data.ID, domain.HumanLocalPrincipal).Scan(&mentions); err != nil {
		t.Fatal(err)
	}
	if err := audit.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_notifications WHERE post_id=? AND recipient_principal=? AND actor_session_id='session-alpha'`, response.Data.ID, domain.HumanLocalPrincipal).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if mentions != 1 || notifications != 1 {
		t.Fatalf("mention=%d notification=%d", mentions, notifications)
	}
	if err = audit.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := server.New(ctx, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	replay := agentRequest(second.Handler(), http.MethodPost, "http://commons.test/v1/posts", body, "configured-agent-post")
	var replayResponse struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayResponse); err != nil || replay.Code != http.StatusOK || replayResponse.Data.ID != response.Data.ID {
		t.Fatalf("restart replay code=%d body=%s err=%v", replay.Code, replay.Body.String(), err)
	}
	contributors = agentRequest(second.Handler(), http.MethodGet, "http://commons.test/v1/contributors?project=alpha&limit=10", "", "")
	if contributors.Code != http.StatusOK || strings.Count(contributors.Body.String(), `"principal":"session-alpha"`) != 1 {
		t.Fatalf("restart contributors code=%d body=%s", contributors.Code, contributors.Body.String())
	}
	people = agentRequest(second.Handler(), http.MethodGet, "http://commons.test/v1/people?limit=10", "", "")
	if people.Code != http.StatusOK || !strings.Contains(people.Body.String(), `"total":0`) {
		t.Fatalf("restart fabricated presence: code=%d body=%s", people.Code, people.Body.String())
	}
}

func TestConfiguredAgentMissingProjectFailsStartup(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.WebDir = testWeb(t)
	config.Credentials = []httpapi.Credential{{
		BearerToken: "configured-agent-secret", Actor: "agent-alpha", Session: "session-alpha",
		Host: "host-alpha", Project: "missing-project", Purpose: "Bounded test",
	}}
	app, err := server.New(context.Background(), config, nil)
	if app != nil || err == nil || !strings.Contains(err.Error(), `references missing project "missing-project"`) {
		t.Fatalf("missing project app=%v err=%v", app, err)
	}
}
