// Package apiclient is a small cancellation-aware client for the Slice 2 API.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"codex-commons/internal/httpapi"
)

const defaultMaxResponseBytes = int64(1 << 20)

type Config struct {
	BaseURL          string
	BearerToken      string
	HostCredential   string
	HTTPClient       *http.Client
	Timeout          time.Duration
	MaxResponseBytes int64
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

type Client struct {
	config Config
	base   *url.URL
}

type APIError struct {
	Status                   int
	Code, Message, RequestID string
}

func (e *APIError) Error() string { return fmt.Sprintf("commons API %s: %s", e.Code, e.Message) }

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Meta struct {
		RequestID string `json:"request_id"`
	} `json:"meta"`
}

func New(config Config) (*Client, error) {
	base, err := url.Parse(config.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, errors.New("valid absolute BaseURL required")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	if config.MaxResponseBytes <= 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	return &Client{config: config, base: base}, nil
}

func (c *Client) Health(ctx context.Context) (httpapi.HealthResult, error) {
	var out httpapi.HealthResult
	err := c.get(ctx, "/v1/health", nil, "", &out)
	return out, err
}
func (c *Client) Context(ctx context.Context, q httpapi.ContextQuery) (httpapi.ContextResult, error) {
	v := url.Values{}
	if q.Budget > 0 {
		v.Set("budget", strconv.Itoa(q.Budget))
	}
	if q.Since != nil {
		v.Set("since", strconv.FormatInt(*q.Since, 10))
	}
	var out httpapi.ContextResult
	err := c.get(ctx, "/v1/context/"+url.PathEscape(q.Project), v, "", &out)
	return out, err
}
func (c *Client) Who(ctx context.Context, q httpapi.WhoQuery) (httpapi.WhoResult, error) {
	v := url.Values{}
	if q.Project != "" {
		v.Set("project", q.Project)
	}
	if q.State != "" {
		v.Set("state", q.State)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	var out httpapi.WhoResult
	err := c.get(ctx, "/v1/who", v, "", &out)
	return out, err
}
func (c *Client) Inbox(ctx context.Context, q httpapi.InboxQuery) (httpapi.InboxResult, error) {
	v := url.Values{}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	var out httpapi.InboxResult
	err := c.get(ctx, "/v1/inbox/"+url.PathEscape(q.Project), v, "", &out)
	return out, err
}
func (c *Client) Search(ctx context.Context, q httpapi.SearchQuery) (httpapi.SearchResult, error) {
	v := url.Values{"q": {q.Query}}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	var out httpapi.SearchResult
	err := c.get(ctx, "/v1/search/"+url.PathEscape(q.Project), v, "", &out)
	return out, err
}
func (c *Client) Open(ctx context.Context, q httpapi.OpenQuery) (httpapi.OpenResult, error) {
	v := url.Values{"ref": {q.Ref}}
	if q.Budget > 0 {
		v.Set("budget", strconv.Itoa(q.Budget))
	}
	var out httpapi.OpenResult
	err := c.get(ctx, "/v1/open", v, "", &out)
	return out, err
}
func (c *Client) Next(ctx context.Context, q httpapi.NextQuery) (httpapi.NextResult, error) {
	v := url.Values{}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	var out httpapi.NextResult
	err := c.get(ctx, "/v1/next/"+url.PathEscape(q.Project), v, "", &out)
	return out, err
}
func (c *Client) Claim(ctx context.Context, in httpapi.ClaimRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, "/v1/claims", in, key, &out)
	return out, err
}
func (c *Client) Post(ctx context.Context, in httpapi.PostRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, "/v1/posts", in, key, &out)
	return out, err
}
func (c *Client) Comment(ctx context.Context, in httpapi.CommentRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, "/v1/comments", in, key, &out)
	return out, err
}
func (c *Client) SetStatus(ctx context.Context, in httpapi.StatusRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, "/v1/status", in, key, &out)
	return out, err
}
func (c *Client) RequestTopic(ctx context.Context, in httpapi.TopicRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, "/v1/topic-requests", in, key, &out)
	return out, err
}

func (c *Client) get(ctx context.Context, path string, query url.Values, requestID string, dst any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, requestID, "", dst)
}
func (c *Client) post(ctx context.Context, path string, body any, key string, dst any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, nil, bytes.NewReader(encoded), "", key, dst)
}
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, requestID, key string, dst any) error {
	if c.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.Timeout)
		defer cancel()
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.BearerToken)
	}
	if c.config.HostCredential != "" {
		req.Header.Set("X-Commons-Host-Credential", c.config.HostCredential)
	}
	if requestID == "" {
		requestID, _ = ctx.Value(requestIDKey{}).(string)
	}
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, c.config.MaxResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(payload)) > c.config.MaxResponseBytes {
		return errors.New("commons API response exceeds limit")
	}
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return errors.New("invalid commons API response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !env.OK {
		apiErr := &APIError{Status: resp.StatusCode, Code: "http_error", Message: http.StatusText(resp.StatusCode), RequestID: env.Meta.RequestID}
		if env.Error != nil {
			apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
		}
		return apiErr
	}
	if dst != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, dst); err != nil {
			return errors.New("invalid commons API data")
		}
	}
	return nil
}
