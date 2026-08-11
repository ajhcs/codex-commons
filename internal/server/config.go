package server

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
)

const (
	defaultListen  = "127.0.0.1:8088"
	defaultWebDir  = "web/dist/client"
	defaultVersion = "dev"
)

type Config struct {
	Listen                string
	DatabasePath          string
	WebDir                string
	Version               string
	Credentials           []httpapi.Credential
	HumanAuth             *httpapi.HumanAuthConfig
	AnonymousRead         bool
	AllowAnonymousLAN     bool
	AllowInsecureHumanLAN bool
	DemoSeed              bool
	ReadTimeout           time.Duration
	ReadHeaderTimeout     time.Duration
	WriteTimeout          time.Duration
	IdleTimeout           time.Duration
	ShutdownTimeout       time.Duration
}

func DefaultConfig() Config {
	return Config{
		Listen: defaultListen, WebDir: defaultWebDir, Version: defaultVersion,
		ReadTimeout: 15 * time.Second, ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		ShutdownTimeout: 10 * time.Second,
	}
}

// ParseConfig reads non-secret settings from flags with environment fallbacks.
// Credential secret values are accepted only from a mode-0600 JSON file or
// environment variables so they do not appear in process arguments.
func ParseConfig(args []string, getenv func(string) string, stderr io.Writer) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	config := DefaultConfig()
	config.Listen = envOr(getenv, "COMMONS_LISTEN", config.Listen)
	config.DatabasePath = strings.TrimSpace(getenv("COMMONS_DB"))
	config.WebDir = envOr(getenv, "COMMONS_WEB_DIR", config.WebDir)
	config.Version = envOr(getenv, "COMMONS_VERSION", config.Version)
	config.AnonymousRead = envBool(getenv, "COMMONS_ANONYMOUS_READ")
	config.AllowAnonymousLAN = envBool(getenv, "COMMONS_ALLOW_ANONYMOUS_LAN")
	config.AllowInsecureHumanLAN = envBool(getenv, "COMMONS_ALLOW_INSECURE_HUMAN_LAN")
	config.DemoSeed = envBool(getenv, "COMMONS_DEMO_SEED")

	credentialsFile := strings.TrimSpace(getenv("COMMONS_CREDENTIALS_FILE"))
	humanSecretFile := strings.TrimSpace(getenv("COMMONS_HUMAN_ADMIN_SECRET_FILE"))
	humanName := envOr(getenv, "COMMONS_HUMAN_ADMIN_NAME", "Local admin")
	humanHandle := envOr(getenv, "COMMONS_HUMAN_ADMIN_HANDLE", "local-admin")
	flags := flag.NewFlagSet("commons-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&config.Listen, "listen", config.Listen, "literal listen address (host:port)")
	flags.StringVar(&config.DatabasePath, "db", config.DatabasePath, "persistent SQLite database path")
	flags.StringVar(&config.WebDir, "web-dir", config.WebDir, "built frontend directory")
	flags.StringVar(&config.Version, "version", config.Version, "health response version")
	flags.StringVar(&credentialsFile, "credentials-file", credentialsFile, "mode-0600 JSON credentials file (secret values are not accepted as flags)")
	flags.StringVar(&humanSecretFile, "human-admin-secret-file", humanSecretFile, "mode-0600 human admin bootstrap-secret file")
	flags.BoolVar(&config.AnonymousRead, "anonymous-read", config.AnonymousRead, "allow unauthenticated API reads with a fixed server-attested prototype identity")
	flags.BoolVar(&config.AllowAnonymousLAN, "allow-anonymous-lan", config.AllowAnonymousLAN, "acknowledge anonymous-read on a non-loopback literal address")
	flags.BoolVar(&config.AllowInsecureHumanLAN, "allow-insecure-human-lan", config.AllowInsecureHumanLAN, "acknowledge plaintext human-session cookies on a non-loopback evaluation listener")
	flags.BoolVar(&config.DemoSeed, "demo-seed", config.DemoSeed, "idempotently seed explicit prototype data before listening")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	credentials, err := readCredentials(credentialsFile)
	if err != nil {
		return Config{}, err
	}
	envCredential, err := credentialFromEnv(getenv)
	if err != nil {
		return Config{}, err
	}
	if envCredential != nil {
		credentials = append(credentials, *envCredential)
	}
	config.Credentials = credentials
	humanSecret, err := readHumanSecret(getenv("COMMONS_HUMAN_ADMIN_SECRET"), humanSecretFile)
	if err != nil {
		return Config{}, err
	}
	if humanSecret != "" {
		config.HumanAuth = &httpapi.HumanAuthConfig{
			AdminSecret: humanSecret, DisplayName: humanName, Handle: humanHandle, Principal: domain.HumanLocalPrincipal,
			Actor: "local-admin", Session: "human-local-admin", Host: "browser",
			SessionTTL: 12 * time.Hour,
		}
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabasePath) == "" || c.DatabasePath == ":memory:" {
		return errors.New("persistent --db path is required; :memory: is not allowed by the server")
	}
	if strings.TrimSpace(c.WebDir) == "" {
		return errors.New("web directory required")
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || strings.TrimSpace(host) == "" || port == "" {
		return errors.New("listen must use an explicit host:port")
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip != nil && ip.IsUnspecified() {
		return errors.New("wildcard listen addresses are not allowed; use a literal loopback or LAN address")
	}
	if c.AnonymousRead && !isLoopbackHost(host) && !c.AllowAnonymousLAN {
		return errors.New("anonymous-read on a non-loopback address requires --allow-anonymous-lan")
	}
	if c.HumanAuth != nil {
		if !isLoopbackHost(host) && !c.AllowInsecureHumanLAN {
			return errors.New("human auth on a plaintext non-loopback listener requires --allow-insecure-human-lan")
		}
		minimumSecretBytes := 24
		if c.AllowInsecureHumanLAN {
			minimumSecretBytes = 8
		}
		if len(c.HumanAuth.AdminSecret) < minimumSecretBytes || len(c.HumanAuth.AdminSecret) > 1024 {
			return fmt.Errorf("human admin secret must be %d..1024 bytes", minimumSecretBytes)
		}
		if strings.TrimSpace(c.HumanAuth.DisplayName) == "" || len(c.HumanAuth.DisplayName) > 200 ||
			(c.HumanAuth.Handle != "" && (strings.TrimSpace(c.HumanAuth.Handle) == "" || len(c.HumanAuth.Handle) > 64)) ||
			(c.HumanAuth.Principal != "" && c.HumanAuth.Principal != domain.HumanLocalPrincipal) ||
			c.HumanAuth.Actor == "" || c.HumanAuth.Session == "" || c.HumanAuth.Host == "" || c.HumanAuth.SessionTTL <= 0 {
			return errors.New("valid human admin identity, display name, and session TTL required")
		}
	}
	for _, credential := range c.Credentials {
		if credential.Actor == "" || credential.Session == "" || credential.Host == "" || credential.BearerToken == "" && credential.HostCredential == "" {
			return errors.New("each credential requires a secret plus actor, session, and host")
		}
		if strings.TrimSpace(credential.Actor) != credential.Actor || len(credential.Actor) > 200 ||
			strings.TrimSpace(credential.Session) != credential.Session || len(credential.Session) > 200 ||
			strings.TrimSpace(credential.Host) != credential.Host || len(credential.Host) > 200 ||
			strings.TrimSpace(credential.Project) != credential.Project || len(credential.Project) > 100 ||
			strings.TrimSpace(credential.Purpose) != credential.Purpose || len(credential.Purpose) > 400 {
			return errors.New("credential actor, session, host, project, and purpose must be bounded and have no surrounding whitespace")
		}
	}
	if c.ReadTimeout <= 0 || c.ReadHeaderTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return errors.New("server timeouts must be positive")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envOr(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(getenv func(string) string, key string) bool {
	value, _ := strconv.ParseBool(strings.TrimSpace(getenv(key)))
	return value
}

func credentialFromEnv(getenv func(string) string) (*httpapi.Credential, error) {
	credential := httpapi.Credential{
		BearerToken:    strings.TrimSpace(getenv("COMMONS_BEARER_TOKEN")),
		HostCredential: strings.TrimSpace(getenv("COMMONS_HOST_CREDENTIAL")),
		Actor:          strings.TrimSpace(getenv("COMMONS_ACTOR")), Session: strings.TrimSpace(getenv("COMMONS_SESSION")),
		Host: strings.TrimSpace(getenv("COMMONS_HOST")), Project: strings.TrimSpace(getenv("COMMONS_PROJECT")),
		Purpose: strings.TrimSpace(getenv("COMMONS_PURPOSE")),
	}
	if credential.BearerToken == "" && credential.HostCredential == "" && credential.Actor == "" && credential.Session == "" && credential.Host == "" && credential.Project == "" && credential.Purpose == "" {
		return nil, nil
	}
	if credential.Actor == "" || credential.Session == "" || credential.Host == "" || credential.BearerToken == "" && credential.HostCredential == "" {
		return nil, errors.New("COMMONS credential environment requires a bearer/host secret plus COMMONS_ACTOR, COMMONS_SESSION, and COMMONS_HOST")
	}
	return &credential, nil
}

func readCredentials(path string) ([]httpapi.Credential, error) {
	if path == "" {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("credentials file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("credentials file must not be accessible by group or other users")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("credentials file: %w", err)
	}
	defer file.Close()
	type fileCredential struct {
		BearerToken    string `json:"bearer_token"`
		HostCredential string `json:"host_credential"`
		Actor          string `json:"actor"`
		Session        string `json:"session"`
		Host           string `json:"host"`
		Project        string `json:"project,omitempty"`
		Purpose        string `json:"purpose,omitempty"`
	}
	var payload struct {
		Credentials []fileCredential `json:"credentials"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("credentials file: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("credentials file must contain one JSON value")
	}
	credentials := make([]httpapi.Credential, 0, len(payload.Credentials))
	for _, item := range payload.Credentials {
		credentials = append(credentials, httpapi.Credential{
			BearerToken: item.BearerToken, HostCredential: item.HostCredential,
			Actor: item.Actor, Session: item.Session, Host: item.Host, Project: item.Project, Purpose: item.Purpose,
		})
	}
	return credentials, nil
}
