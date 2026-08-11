package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"codex-commons/internal/apiclient"
)

const (
	agentConfigEnv          = "COMMONS_AGENT_CONFIG_FILE"
	defaultTimeout          = 5 * time.Second
	defaultMaxResponseBytes = int64(64 << 10)
	maxAgentConfigBytes     = int64(64 << 10)
	maxCLIResponseBytes     = int64(256 << 10)
)

// agentConfig intentionally contains no actor, session, or host fields. The
// server binds the configured secret to those identity facts; accepting them
// here would turn attestation back into caller self-assertion.
type agentConfig struct {
	BaseURL          string `json:"base_url"`
	BearerToken      string `json:"bearer_token,omitempty"`
	HostCredential   string `json:"host_credential,omitempty"`
	DefaultProject   string `json:"default_project,omitempty"`
	Timeout          string `json:"timeout,omitempty"`
	MaxResponseBytes int64  `json:"max_response_bytes,omitempty"`
}

type runtimeDeps struct {
	getenv        func(string) string
	userConfigDir func() (string, error)
	httpClient    *http.Client
}

func (d runtimeDeps) withDefaults() runtimeDeps {
	if d.getenv == nil {
		d.getenv = os.Getenv
	}
	if d.userConfigDir == nil {
		d.userConfigDir = os.UserConfigDir
	}
	return d
}

func loadAgentClient(deps runtimeDeps) (*apiclient.Client, agentConfig, error) {
	deps = deps.withDefaults()
	path := strings.TrimSpace(deps.getenv(agentConfigEnv))
	if path == "" {
		root, err := deps.userConfigDir()
		if err != nil {
			return nil, agentConfig{}, fmt.Errorf("locate agent config: %w", err)
		}
		path = filepath.Join(root, "codex-commons", "agent.json")
	}
	config, err := readAgentConfig(path)
	if err != nil {
		return nil, agentConfig{}, err
	}
	timeout := defaultTimeout
	if config.Timeout != "" {
		timeout, err = time.ParseDuration(config.Timeout)
		if err != nil || timeout <= 0 || timeout > time.Minute {
			return nil, agentConfig{}, errors.New("agent config timeout must be a positive duration no greater than 1m")
		}
	}
	maxResponse := config.MaxResponseBytes
	if maxResponse == 0 {
		maxResponse = defaultMaxResponseBytes
	}
	if maxResponse < 1024 || maxResponse > maxCLIResponseBytes {
		return nil, agentConfig{}, fmt.Errorf("agent config max_response_bytes must be in 1024..%d", maxCLIResponseBytes)
	}
	httpClient := deps.httpClient
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		// A Commons credential is host-scoped. Do not allow ambient proxy
		// configuration or redirects to forward it to another endpoint.
		transport.Proxy = nil
		httpClient = &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	client, err := apiclient.New(apiclient.Config{
		BaseURL: config.BaseURL, BearerToken: config.BearerToken,
		HostCredential: config.HostCredential, HTTPClient: httpClient,
		Timeout: timeout, MaxResponseBytes: maxResponse,
	})
	if err != nil {
		return nil, agentConfig{}, fmt.Errorf("agent config: %w", err)
	}
	return client, config, nil
}

func readAgentConfig(path string) (agentConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return agentConfig{}, fmt.Errorf("agent config: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return agentConfig{}, fmt.Errorf("agent config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return agentConfig{}, errors.New("agent config must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return agentConfig{}, errors.New("agent config must not be accessible by group or other users")
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxAgentConfigBytes+1))
	if err != nil {
		return agentConfig{}, fmt.Errorf("agent config: %w", err)
	}
	if int64(len(payload)) > maxAgentConfigBytes {
		return agentConfig{}, errors.New("agent config exceeds 65536 bytes")
	}
	var config agentConfig
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return agentConfig{}, fmt.Errorf("agent config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return agentConfig{}, errors.New("agent config must contain one JSON value")
	}
	if err := validateAgentConfig(config); err != nil {
		return agentConfig{}, err
	}
	return config, nil
}

func validateAgentConfig(config agentConfig) error {
	base, err := url.Parse(config.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return errors.New("agent config base_url must be an absolute HTTP(S) URL")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("agent config base_url must not contain user info, a query, or a fragment")
	}
	if (config.BearerToken == "") == (config.HostCredential == "") {
		return errors.New("agent config requires exactly one bearer_token or host_credential")
	}
	secret := config.BearerToken
	if secret == "" {
		secret = config.HostCredential
	}
	if strings.TrimSpace(secret) != secret || strings.ContainsAny(secret, "\r\n\x00") {
		return errors.New("agent config credential contains invalid whitespace or control characters")
	}
	if len(secret) > 4096 {
		return errors.New("agent config credential exceeds 4096 bytes")
	}
	if len(config.DefaultProject) > 200 || strings.ContainsAny(config.DefaultProject, "\r\n\x00") {
		return errors.New("agent config default_project is invalid")
	}
	return nil
}
