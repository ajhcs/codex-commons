package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type Config struct {
	BaseURL          string
	Secret           string
	Apply            bool
	AllowInsecureLAN bool
	Client           *http.Client
}

type Receipt struct {
	Mode       string            `json:"mode"`
	Namespace  string            `json:"namespace"`
	Project    string            `json:"project,omitempty"`
	Phase      string            `json:"completed_phase"`
	Verified   bool              `json:"verified"`
	Milestones map[string]string `json:"milestones,omitempty"`
	Tasks      map[string]string `json:"tasks,omitempty"`
	WikiPages  map[string]string `json:"wiki_pages,omitempty"`
	Posts      map[string]string `json:"posts,omitempty"`
	Counts     PlanCounts        `json:"counts"`
}

type PlanCounts struct {
	Milestones int `json:"milestones"`
	Tasks      int `json:"tasks"`
	WikiPages  int `json:"wiki_pages"`
	Posts      int `json:"posts"`
}
type writeResult struct {
	ID        string `json:"id"`
	Revision  int64  `json:"revision"`
	Persisted bool   `json:"persisted"`
}
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *apiError       `json:"error,omitempty"`
}

func Run(ctx context.Context, manifest Manifest, cfg Config) (Receipt, error) {
	receipt := Receipt{Mode: "dry-run", Namespace: manifest.Namespace, Phase: "validated", Counts: PlanCounts{len(manifest.Milestones), len(manifest.Tasks), len(manifest.WikiPages), len(manifest.Posts)}}
	if err := Validate(manifest); err != nil {
		return receipt, err
	}
	if !cfg.Apply {
		return receipt, nil
	}
	receipt.Mode = "apply"
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return receipt, errors.New("--base-url is required with --apply")
	}
	if cfg.Secret == "" {
		return receipt, errors.New("COMMONS_BOOTSTRAP_ADMIN_SECRET is required with --apply")
	}
	client, err := newAPIClient(cfg)
	if err != nil {
		return receipt, err
	}
	if err = client.login(ctx, cfg.Secret); err != nil {
		return receipt, fmt.Errorf("login: %w", err)
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = client.logout(logoutCtx)
	}()
	receipt.Phase = "authenticated"
	projectReq := map[string]any{"id": manifest.Project.ID, "name": manifest.Project.Name, "status": manifest.Project.Status, "purpose": manifest.Project.Purpose, "now": manifest.Project.Now}
	result, err := client.write(ctx, http.MethodPost, "/v1/projects", projectReq, requestKey(manifest, "project", manifest.Project.Key))
	if err != nil {
		return receipt, phaseError("project", manifest.Project.Key, err)
	}
	receipt.Project, receipt.Phase = result.ID, "project"
	receipt.Milestones = map[string]string{}
	for _, item := range manifest.Milestones {
		body := map[string]any{"title": item.Title, "status": item.Status, "position": item.Position, "target_date": item.TargetDate}
		result, err = client.write(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(manifest.Project.ID)+"/milestones", body, requestKey(manifest, "milestone", item.Key))
		if err != nil {
			return receipt, phaseError("milestone", item.Key, err)
		}
		receipt.Milestones[item.Key] = result.ID
	}
	receipt.Phase = "milestones"
	receipt.Tasks = map[string]string{}
	ordered, _ := taskOrder(manifest.Tasks)
	for _, item := range ordered {
		state := item.State
		if state == "" {
			state = "ready"
		}
		deps := make([]string, len(item.DependencyKeys))
		for i, key := range item.DependencyKeys {
			deps[i] = receipt.Tasks[key]
		}
		body := map[string]any{"title": item.Title, "description": item.Description, "acceptance": item.Acceptance, "state": state, "priority": item.Priority, "dependency_ids": deps}
		if item.MilestoneKey != "" {
			body["milestone_id"] = receipt.Milestones[item.MilestoneKey]
		}
		result, err = client.write(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(manifest.Project.ID)+"/tasks", body, requestKey(manifest, "task", item.Key))
		if err != nil {
			return receipt, phaseError("task", item.Key, err)
		}
		receipt.Tasks[item.Key] = result.ID
	}
	receipt.Phase = "tasks"
	receipt.WikiPages = map[string]string{}
	zero := int64(0)
	for _, item := range manifest.WikiPages {
		body := map[string]any{"title": item.Title, "summary": item.Summary, "body": item.Body, "base_revision": &zero}
		path := "/v1/projects/" + url.PathEscape(manifest.Project.ID) + "/wiki/" + url.PathEscape(item.Slug) + "/revisions"
		result, err = client.write(ctx, http.MethodPost, path, body, requestKey(manifest, "wiki", item.Key))
		if err != nil {
			return receipt, phaseError("wiki", item.Key, err)
		}
		receipt.WikiPages[item.Key] = result.ID
	}
	receipt.Phase = "wiki"
	receipt.Posts = map[string]string{}
	for _, item := range manifest.Posts {
		body := map[string]any{"topic": manifest.Project.ID, "kind": item.Kind, "title": item.Title, "body": item.Body, "basis": item.Basis, "attachments": item.Attachments}
		if item.TaskKey != "" {
			body["ref"] = receipt.Tasks[item.TaskKey]
		}
		result, err = client.write(ctx, http.MethodPost, "/v1/posts", body, requestKey(manifest, "post", item.Key))
		if err != nil {
			return receipt, phaseError("post", item.Key, err)
		}
		receipt.Posts[item.Key] = result.ID
	}
	receipt.Phase = "posts"
	if err := verifyComplete(ctx, client, manifest, receipt); err != nil {
		return receipt, fmt.Errorf("verification: %w", err)
	}
	receipt.Phase, receipt.Verified = "verified", true
	return receipt, nil
}

func requestKey(m Manifest, kind, key string) string {
	return "bootstrap:" + m.Namespace + ":" + kind + ":" + key
}
func phaseError(kind, key string, err error) error { return fmt.Errorf("%s %q: %w", kind, key, err) }

type apiClient struct {
	base         *url.URL
	origin, csrf string
	http         *http.Client
}

func newAPIClient(cfg Config) (*apiClient, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("--base-url must be an HTTP(S) origin without credentials, path, query, or fragment")
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, errors.New("--base-url scheme must be http or https")
	}
	host := base.Hostname()
	loopback := strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	if base.Scheme == "http" && !loopback && !cfg.AllowInsecureLAN {
		return nil, errors.New("non-loopback HTTP requires --allow-insecure-http; prefer TLS")
	}
	jar, _ := cookiejar.New(nil)
	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second, Jar: jar}
	} else if httpClient.Jar == nil {
		httpClient.Jar = jar
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	base.Path = ""
	return &apiClient{base: base, origin: base.Scheme + "://" + base.Host, http: httpClient}, nil
}

func (c *apiClient) login(ctx context.Context, secret string) error {
	var data struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/auth/login", map[string]string{"secret": secret}, "", &data); err != nil {
		return err
	}
	if !data.Authenticated || data.CSRFToken == "" {
		return errors.New("server returned an invalid authenticated session")
	}
	c.csrf = data.CSRFToken
	return nil
}

func (c *apiClient) logout(ctx context.Context) error {
	var data struct {
		Authenticated bool `json:"authenticated"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/auth/logout", map[string]any{}, "", &data)
	c.csrf = ""
	return err
}
func (c *apiClient) write(ctx context.Context, method, path string, body any, key string) (writeResult, error) {
	var result writeResult
	if err := c.do(ctx, method, path, body, key, &result); err != nil {
		return result, err
	}
	if result.ID == "" || !result.Persisted || result.Revision < 0 {
		return result, errors.New("server returned an invalid write acknowledgement")
	}
	return result, nil
}

func (c *apiClient) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, "", out)
}

func (c *apiClient) do(ctx context.Context, method, path string, body any, key string, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", c.origin)
		if c.csrf != "" {
			req.Header.Set("X-Commons-CSRF", c.csrf)
		}
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxResponseBytes {
		return errors.New("server response exceeds 1 MiB")
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("HTTP %d returned invalid JSON", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if env.Error != nil {
			return fmt.Errorf("HTTP %d %s: %s", resp.StatusCode, env.Error.Code, env.Error.Message)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if len(env.Data) == 0 {
		return errors.New("server response omitted data")
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}
