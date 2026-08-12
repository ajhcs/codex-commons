package server_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codex-commons/internal/demodata"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/server"
)

func testWeb(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>Commons</title><div id=app></div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("globalThis.commons=true"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func request(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = "127.0.0.1:8088"
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestAnonymousReadIsExplicitReadOnlyAndSPAFallbackIsBounded(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.WebDir = testWeb(t)
	config.AnonymousRead = true
	config.DemoSeed = true
	app, err := server.New(context.Background(), config, demodata.Seed)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	projects := request(t, app.Handler(), http.MethodGet, "/v1/projects?limit=10")
	if projects.Code != http.StatusOK || !strings.Contains(projects.Body.String(), `"total":4`) {
		t.Fatalf("anonymous real-data read failed: code=%d body=%s", projects.Code, projects.Body.String())
	}
	post := request(t, app.Handler(), http.MethodPost, "/v1/posts")
	if post.Code != http.StatusUnauthorized || !strings.Contains(post.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("anonymous write was not rejected: code=%d body=%s", post.Code, post.Body.String())
	}
	missingAPI := request(t, app.Handler(), http.MethodGet, "/v1/not-a-route")
	if missingAPI.Code != http.StatusNotFound || strings.Contains(missingAPI.Body.String(), "<title>Commons") {
		t.Fatalf("SPA swallowed API route: code=%d body=%s", missingAPI.Code, missingAPI.Body.String())
	}
	spa := request(t, app.Handler(), http.MethodGet, "/projects/billing-orchestrator")
	if spa.Code != http.StatusOK || !strings.Contains(spa.Body.String(), "<title>Commons") {
		t.Fatalf("SPA fallback failed: code=%d body=%s", spa.Code, spa.Body.String())
	}
	asset := request(t, app.Handler(), http.MethodGet, "/app.js")
	if asset.Code != http.StatusOK || asset.Body.String() != "globalThis.commons=true" {
		t.Fatalf("static asset failed: code=%d body=%s", asset.Code, asset.Body.String())
	}
	staticWrite := request(t, app.Handler(), http.MethodPost, "/projects")
	if staticWrite.Code != http.StatusMethodNotAllowed {
		t.Fatalf("non-GET static route was not rejected: %d", staticWrite.Code)
	}
}

func TestPersistentDataSurvivesRuntimeRestartButPresenceRequiresExplicitSeed(t *testing.T) {
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	config := server.DefaultConfig()
	config.DatabasePath = database
	config.WebDir = testWeb(t)
	config.AnonymousRead = true
	config.DemoSeed = true
	first, err := server.New(context.Background(), config, demodata.Seed)
	if err != nil {
		t.Fatal(err)
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
	projects := request(t, second.Handler(), http.MethodGet, "/v1/projects?limit=10")
	if projects.Code != http.StatusOK || !strings.Contains(projects.Body.String(), `"total":4`) {
		t.Fatalf("durable projects did not survive restart: code=%d body=%s", projects.Code, projects.Body.String())
	}
	people := request(t, second.Handler(), http.MethodGet, "/v1/people?limit=10")
	if people.Code != http.StatusOK || !strings.Contains(people.Body.String(), `"total":0`) {
		t.Fatalf("presence was incorrectly reconstructed: code=%d body=%s", people.Code, people.Body.String())
	}
}

func TestConfigFailsClosedForAnonymousLANAndSecrets(t *testing.T) {
	base := server.DefaultConfig()
	base.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	base.WebDir = testWeb(t)
	base.Listen = "192.168.1.60:8088"
	base.AnonymousRead = true
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "allow-anonymous-lan") {
		t.Fatalf("anonymous LAN did not require acknowledgement: %v", err)
	}
	base.AllowAnonymousLAN = true
	if err := base.Validate(); err != nil {
		t.Fatalf("acknowledged literal LAN address rejected: %v", err)
	}
	base.Listen = "0.0.0.0:8088"
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "wildcard") {
		t.Fatalf("wildcard bind accepted: %v", err)
	}

	credentials := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(credentials, []byte(`{"credentials":[{"bearer_token":"secret","actor":"agent","session":"S-1","host":"plumbob","project":"alpha","purpose":"Dogfood coordination"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"COMMONS_DB": filepath.Join(t.TempDir(), "parsed.sqlite")}
	config, err := server.ParseConfig([]string{"--credentials-file", credentials}, func(key string) string { return env[key] }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Credentials) != 1 || config.Credentials[0].BearerToken != "secret" ||
		config.Credentials[0].Project != "alpha" || config.Credentials[0].Purpose != "Dogfood coordination" {
		t.Fatalf("credential file not parsed: %+v", config.Credentials)
	}
	if _, err := server.ParseConfig([]string{"--bearer-token", "leak"}, func(key string) string { return env[key] }, io.Discard); err == nil {
		t.Fatal("secret-bearing command-line flag was unexpectedly accepted")
	}
}

func assertHeadMatchesGet(t *testing.T, handler http.Handler, target string) {
	t.Helper()
	get := request(t, handler, http.MethodGet, target)
	head := request(t, handler, http.MethodHead, target)
	if head.Code != get.Code {
		t.Fatalf("HEAD code=%d, GET code=%d", head.Code, get.Code)
	}
	if !reflect.DeepEqual(head.Header(), get.Header()) {
		t.Fatalf("HEAD headers=%v, GET headers=%v", head.Header(), get.Header())
	}
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD body=%q, want empty", head.Body.String())
	}
}

func TestAnonymousHeadMirrorsDynamicGetWithoutAllowingMutation(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.WebDir = testWeb(t)
	config.AnonymousRead = true
	config.DemoSeed = true
	app, err := server.New(context.Background(), config, demodata.Seed)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	for _, target := range []string{
		"/v1/health",
		"/v1/projects?limit=10",
		"/v1/posts?limit=10",
		"/v1/projects/demo-billing-orchestrator",
		"/v1/projects/demo-billing-orchestrator/overview",
		"/v1/projects/demo-billing-orchestrator/tasks?limit=25",
		"/v1/projects/demo-billing-orchestrator/wiki?limit=100",
	} {
		assertHeadMatchesGet(t, app.Handler(), target)
	}

	mutation := request(t, app.Handler(), http.MethodHead, "/v1/claims")
	if mutation.Code != http.StatusNotFound || mutation.Body.Len() != 0 {
		t.Fatalf("HEAD selected mutation route: code=%d body=%q", mutation.Code, mutation.Body.String())
	}
	post := request(t, app.Handler(), http.MethodPost, "/v1/claims")
	if post.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous mutation auth weakened: code=%d body=%q", post.Code, post.Body.String())
	}
}

func TestCredentialMetadataEnvironmentAndBounds(t *testing.T) {
	env := map[string]string{
		"COMMONS_DB":           filepath.Join(t.TempDir(), "parsed.sqlite"),
		"COMMONS_BEARER_TOKEN": "secret",
		"COMMONS_ACTOR":        "agent-alpha",
		"COMMONS_SESSION":      "session-alpha",
		"COMMONS_HOST":         "host-alpha",
		"COMMONS_PROJECT":      "alpha",
		"COMMONS_PURPOSE":      "Dogfood coordination",
	}
	config, err := server.ParseConfig(nil, func(key string) string { return env[key] }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Credentials) != 1 || config.Credentials[0].Project != "alpha" || config.Credentials[0].Purpose != "Dogfood coordination" {
		t.Fatalf("environment credential=%+v", config.Credentials)
	}
	base := server.DefaultConfig()
	base.DatabasePath = filepath.Join(t.TempDir(), "bounds.sqlite")
	base.Credentials = []httpapi.Credential{{BearerToken: "secret", Actor: "agent", Session: "session", Host: "host"}}
	if err := base.Validate(); err != nil {
		t.Fatalf("omitted optional metadata rejected: %v", err)
	}
	base.Credentials[0].Project = strings.Repeat("p", 101)
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "bounded") {
		t.Fatalf("oversized project accepted: %v", err)
	}
	base.Credentials[0].Project = ""
	base.Credentials[0].Purpose = strings.Repeat("p", 401)
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "bounded") {
		t.Fatalf("oversized purpose accepted: %v", err)
	}
}

func TestExperimentalHistorianRequiresManagedCodexAuthAndCanBeExplicitlyEnabled(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite3")
	config.WebDir = t.TempDir()
	config.EnableExperimentalHistorian = true
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "managed Codex auth") {
		t.Fatalf("experimental historian without auth err=%v", err)
	}
	config.CodexAuth = true
	config.CodexBin = "/usr/bin/codex"
	config.CodexBindingKeySet = true
	config.CodexBindingKey[0] = 1
	if err := config.Validate(); err != nil {
		t.Fatalf("explicit experimental historian config rejected: %v", err)
	}
}
