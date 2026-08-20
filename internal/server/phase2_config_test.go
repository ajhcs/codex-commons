package server_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-commons/internal/server"
)

func phase2ConfigEnv(t *testing.T, require string) map[string]string {
	t.Helper()
	root := t.TempDir()
	keyPath := filepath.Join(root, "codex-binding.key")
	key := bytes.Repeat([]byte{0x2a}, 32)
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"COMMONS_DB":                     filepath.Join(root, "commons.sqlite3"),
		"COMMONS_REQUIRE_CODEX_READY":    require,
		"COMMONS_CODEX_AUTH":             "true",
		"COMMONS_CODEX_BIN":              "/usr/bin/codex",
		"COMMONS_CODEX_VERSION":          "0.147.0",
		"COMMONS_CODEX_BINDING_KEY_FILE": keyPath,
	}
}

func TestRequireCodexReadyDefaultsToOptional(t *testing.T) {
	env := map[string]string{"COMMONS_DB": filepath.Join(t.TempDir(), "commons.sqlite3")}
	config, err := server.ParseConfig(nil, func(key string) string { return env[key] }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if config.RequireCodexReady {
		t.Fatal("RequireCodexReady defaulted to required")
	}
}

func TestRequireCodexReadyEnvironmentValues(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want bool
	}{
		{name: "true", raw: "true", want: true},
		{name: "false", raw: "false", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := phase2ConfigEnv(t, test.raw)
			if !test.want {
				delete(env, "COMMONS_CODEX_AUTH")
				delete(env, "COMMONS_CODEX_BIN")
				delete(env, "COMMONS_CODEX_VERSION")
				delete(env, "COMMONS_CODEX_BINDING_KEY_FILE")
			}
			config, err := server.ParseConfig(nil, func(key string) string { return env[key] }, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if config.RequireCodexReady != test.want {
				t.Fatalf("RequireCodexReady=%v want %v", config.RequireCodexReady, test.want)
			}
		})
	}
}

func TestRequireCodexReadyEnvironmentRejectsInvalidBoolean(t *testing.T) {
	env := map[string]string{
		"COMMONS_DB":                  filepath.Join(t.TempDir(), "commons.sqlite3"),
		"COMMONS_REQUIRE_CODEX_READY": "sometimes",
	}
	_, err := server.ParseConfig(nil, func(key string) string { return env[key] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "COMMONS_REQUIRE_CODEX_READY") {
		t.Fatalf("invalid readiness boolean was accepted or poorly reported: %v", err)
	}
}

func TestRequireCodexReadyCLIOverridesEnvironment(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
		args []string
		want bool
	}{
		{name: "disable", env: "true", args: []string{"--require-codex-ready=false"}, want: false},
		{name: "enable", env: "false", args: []string{"--require-codex-ready"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := phase2ConfigEnv(t, test.env)
			config, err := server.ParseConfig(test.args, func(key string) string { return env[key] }, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if config.RequireCodexReady != test.want {
				t.Fatalf("RequireCodexReady=%v want %v", config.RequireCodexReady, test.want)
			}
		})
	}
}

func TestRequireCodexReadyHelpDescribesOperatorFailureMode(t *testing.T) {
	var stderr bytes.Buffer
	_, err := server.ParseConfig([]string{"--help"}, nil, &stderr)
	if err == nil || !strings.Contains(stderr.String(), "-require-codex-ready") ||
		!strings.Contains(stderr.String(), "delay Type=notify readiness") ||
		!strings.Contains(stderr.String(), "bounded required-Codex exhaustion") {
		t.Fatalf("readiness flag help missing or unclear: err=%v help=%q", err, stderr.String())
	}
}

func TestRequireCodexReadyRequiresManagedCodexAuth(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite3")
	config.WebDir = t.TempDir()
	config.RequireCodexReady = true
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "managed Codex auth") {
		t.Fatalf("required readiness without managed Codex auth was accepted: %v", err)
	}
}

func TestRequireCodexReadyAcceptsValidManagedCodexConfiguration(t *testing.T) {
	config := server.DefaultConfig()
	config.DatabasePath = filepath.Join(t.TempDir(), "commons.sqlite3")
	config.WebDir = t.TempDir()
	config.RequireCodexReady = true
	config.CodexAuth = true
	config.CodexBin = "/usr/bin/codex"
	config.CodexVersion = "0.147.0"
	config.CodexBindingKey[0] = 1
	config.CodexBindingKeySet = true
	if err := config.Validate(); err != nil {
		t.Fatalf("valid required Codex configuration rejected: %v", err)
	}
}
