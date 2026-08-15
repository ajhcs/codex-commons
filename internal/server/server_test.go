package server_test

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/demodata"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/server"
	commonsstore "codex-commons/internal/store"
)

type startupRecoveryCodexClient struct {
	continuationCodexClient
	launches atomic.Int32
}

func (*startupRecoveryCodexClient) ListWorkspaces(context.Context) ([]codexauth.Workspace, error) {
	return nil, nil
}
func (*startupRecoveryCodexClient) SupportsModel(context.Context, string, string) (bool, error) {
	return true, nil
}
func (*startupRecoveryCodexClient) LaunchTask(context.Context, string, string, string, string, string) (codexauth.TaskLaunch, error) {
	return codexauth.TaskLaunch{}, codexauth.ErrUnavailable
}
func (*startupRecoveryCodexClient) ExperimentalDynamicTools() bool { return true }
func (c *startupRecoveryCodexClient) LaunchHistorianTask(context.Context, string, string, string, string, string, string, codexauth.HistorianPolicy, codexauth.DynamicToolHandler, codexauth.TurnTerminalHandler) (codexauth.TaskLaunch, error) {
	c.launches.Add(1)
	return codexauth.TaskLaunch{ThreadID: "unexpected-thread", SessionID: "unexpected-session", TurnID: "unexpected-turn"}, nil
}
func (*startupRecoveryCodexClient) InterruptTurn(context.Context, string, string) error { return nil }

func testWeb(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>Commons</title><script>globalThis.theme=true</script><div id=app></div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-QA123.js"), []byte("globalThis.hashed=true"), 0o600); err != nil {
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
	security := request(t, app.Handler(), http.MethodGet, "/v1/health")
	for name, want := range map[string]string{
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Referrer-Policy":              "no-referrer",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
	} {
		if got := security.Header().Get(name); got != want {
			t.Fatalf("%s=%q want %q", name, got, want)
		}
	}
	if policy := security.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "script-src 'self' 'sha256-") || !strings.Contains(policy, "frame-ancestors 'none'") || strings.Contains(policy, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("unexpected Content-Security-Policy: %q", policy)
	}

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
	hashedAsset := request(t, app.Handler(), http.MethodGet, "/assets/index-QA123.js")
	if hashedAsset.Code != http.StatusOK || hashedAsset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("hashed asset cache contract failed: code=%d cache=%q", hashedAsset.Code, hashedAsset.Header().Get("Cache-Control"))
	}
	gzipRequest := httptest.NewRequest(http.MethodGet, "/assets/index-QA123.js", nil)
	gzipRequest.Host = config.Listen
	gzipRequest.Header.Set("Accept-Encoding", "br, gzip")
	gzipRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(gzipRecorder, gzipRequest)
	if gzipRecorder.Code != http.StatusOK || gzipRecorder.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(gzipRecorder.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("gzip static asset contract failed: code=%d encoding=%q vary=%q", gzipRecorder.Code, gzipRecorder.Header().Get("Content-Encoding"), gzipRecorder.Header().Get("Vary"))
	}
	reader, err := gzip.NewReader(gzipRecorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	uncompressed, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil || string(uncompressed) != "globalThis.hashed=true" {
		t.Fatalf("gzip asset body=%q err=%v", uncompressed, err)
	}
	noGzip := httptest.NewRequest(http.MethodGet, "/assets/index-QA123.js", nil)
	noGzip.Host = config.Listen
	noGzip.Header.Set("Accept-Encoding", "gzip;q=0, br")
	noGzipRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(noGzipRecorder, noGzip)
	if noGzipRecorder.Header().Get("Content-Encoding") != "" || noGzipRecorder.Body.String() != "globalThis.hashed=true" {
		t.Fatalf("gzip q=0 was not honored: encoding=%q body=%q", noGzipRecorder.Header().Get("Content-Encoding"), noGzipRecorder.Body.String())
	}
	explicitNoGzip := httptest.NewRequest(http.MethodGet, "/assets/index-QA123.js", nil)
	explicitNoGzip.Host = config.Listen
	explicitNoGzip.Header.Set("Accept-Encoding", "*;q=1, gzip;q=0")
	explicitNoGzipRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(explicitNoGzipRecorder, explicitNoGzip)
	if explicitNoGzipRecorder.Header().Get("Content-Encoding") != "" {
		t.Fatalf("explicit gzip q=0 did not override wildcard: encoding=%q", explicitNoGzipRecorder.Header().Get("Content-Encoding"))
	}
	missingAsset := request(t, app.Handler(), http.MethodGet, "/assets/missing-QA123.js")
	if missingAsset.Code != http.StatusNotFound || strings.Contains(missingAsset.Body.String(), "<title>Commons") || missingAsset.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("missing asset fell through to SPA: code=%d cache=%q body=%s", missingAsset.Code, missingAsset.Header().Get("Cache-Control"), missingAsset.Body.String())
	}
	staticWrite := request(t, app.Handler(), http.MethodPost, "/projects")
	if staticWrite.Code != http.StatusMethodNotAllowed {
		t.Fatalf("non-GET static route was not rejected: %d", staticWrite.Code)
	}
}

func TestOuterMuxRejectsMisdirectedStaticAndSPARequests(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite")
	config.WebDir = testWeb(t)
	app, err := server.New(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, target := range []string{"/", "/projects/example", "/assets/index-QA123.js"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Host = "evil.test"
		req.Header.Set("Forwarded", "host=127.0.0.1:8088;proto=https")
		req.Header.Set("X-Forwarded-Host", "127.0.0.1:8088")
		req.Header.Set("X-Forwarded-Proto", "https")
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, req)
		if response.Code != http.StatusMisdirectedRequest || strings.Contains(response.Body.String(), "<title>Commons") {
			t.Fatalf("target=%s code=%d body=%q", target, response.Code, response.Body.String())
		}
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

func TestServerStartupReconcilesNativeWorkBeforeSchedulerWake(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "commons.sqlite")
	repository, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	discovery := domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{
		{ID: "restart-a", Name: "Restart A", PathLabel: "Restart A", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"},
		{ID: "restart-b", Name: "Restart B", PathLabel: "Restart B", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"},
	}}
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "startup-recovery-discover"}, discovery)
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "startup-recovery-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"restart-a", "restart-b"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "startup-recovery-start", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	job, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.BindArchaeologyNativeJob(ctx, job.ID, "thread-before-restart", "session-before-restart", "turn-before-restart"); err != nil {
		t.Fatal(err)
	}
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}

	client := &startupRecoveryCodexClient{continuationCodexClient: continuationCodexClient{loginID: "startup", email: "startup@example.com"}}
	config := server.DefaultConfig()
	config.DatabasePath = database
	config.WebDir = testWeb(t)
	config.CodexAuth = true
	config.CodexBin = "/usr/bin/codex"
	config.CodexBindingKeySet = true
	config.CodexBindingKey[0] = 1
	config.CodexClient = client
	config.EnableExperimentalHistorian = true
	app, err := server.New(ctx, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	verifier, err := commonsstore.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer verifier.Close()
	recovered, err := verifier.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.NativeBatches) != 1 || recovered.NativeBatches[0].State != "attention" || len(recovered.NativeBatches[0].Jobs) != 2 {
		t.Fatalf("recovered=%+v", recovered.NativeBatches)
	}
	states := map[string]int{}
	for _, recoveredJob := range recovered.NativeBatches[0].Jobs {
		states[recoveredJob.State]++
	}
	if states["uncertain"] != 1 || states["queued"] != 1 || client.launches.Load() != 0 {
		t.Fatalf("states=%v launches=%d", states, client.launches.Load())
	}
	if recovered.NativeBatches[0].Jobs[0].ThreadID == "" && recovered.NativeBatches[0].Jobs[1].ThreadID == "" {
		t.Fatal("exact accepted task identity was lost during startup reconciliation")
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

func TestReleaseIdentityFollowsCurrentOnlyToMatchingImmutableDirectory(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "release-test")
	if err := os.Mkdir(release, 0o755); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(release, "VERSION")
	if err := os.WriteFile(identity, []byte("release-test\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("release-test", filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"COMMONS_DB": filepath.Join(root, "commons.sqlite3"), "COMMONS_RELEASE_IDENTITY_FILE": filepath.Join(root, "current", "VERSION")}
	config, err := server.ParseConfig(nil, func(key string) string { return env[key] }, io.Discard)
	if err != nil || config.Version != "release-test" {
		t.Fatalf("matching current identity config=%+v err=%v", config, err)
	}
	if err = os.Chmod(identity, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = server.ParseConfig(nil, func(key string) string { return env[key] }, io.Discard); err == nil {
		t.Fatal("writable release identity accepted")
	}
	if err = os.WriteFile(identity, []byte("wrong\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(identity, 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err = server.ParseConfig(nil, func(key string) string { return env[key] }, io.Discard); err == nil {
		t.Fatal("identity differing from canonical release directory accepted")
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
