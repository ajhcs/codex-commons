package githubsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPerPage       = 50
	defaultMaxPages      = 5
	defaultMaxBodyBytes  = int64(1 << 20)
	defaultMaxReferences = 100
	apiVersion           = "2022-11-28"
)

type Config struct {
	BaseURL       string
	Token         string
	HTTPClient    *http.Client
	UserAgent     string
	PerPage       int
	MaxPages      int
	MaxBodyBytes  int64
	MaxReferences int
}

type Client struct {
	base          *url.URL
	token         string
	http          *http.Client
	userAgent     string
	perPage       int
	maxPages      int
	maxBodyBytes  int64
	maxReferences int
}

func New(config Config) (*Client, error) {
	base, err := url.Parse(config.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("%w: absolute BaseURL without query or fragment required", ErrInvalid)
	}
	if base.Scheme != "https" && !(base.Scheme == "http" && isLoopback(base.Hostname())) {
		return nil, fmt.Errorf("%w: BaseURL must use HTTPS (HTTP is allowed only for loopback tests)", ErrInvalid)
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	if config.UserAgent == "" {
		config.UserAgent = "codex-commons-githubsync/1"
	}
	if config.PerPage == 0 {
		config.PerPage = defaultPerPage
	}
	if config.MaxPages == 0 {
		config.MaxPages = defaultMaxPages
	}
	if config.MaxBodyBytes == 0 {
		config.MaxBodyBytes = defaultMaxBodyBytes
	}
	if config.MaxReferences == 0 {
		config.MaxReferences = defaultMaxReferences
	}
	if config.PerPage < 1 || config.PerPage > 100 || config.MaxPages < 1 || config.MaxPages > 20 || config.MaxBodyBytes < 1 || config.MaxReferences < 1 || config.MaxReferences > 1000 {
		return nil, fmt.Errorf("%w: invalid bounds", ErrInvalid)
	}
	base.Path = strings.TrimRight(base.Path, "/")
	return &Client{base: base, token: config.Token, http: config.HTTPClient, userAgent: config.UserAgent, perPage: config.PerPage, maxPages: config.MaxPages, maxBodyBytes: config.MaxBodyBytes, maxReferences: config.MaxReferences}, nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Sync performs only GET requests. It does not retry, mutate GitHub, follow
// webhooks, or start background work. ReferencedCommits is the complete allow
// list for commit metadata; pull-request heads are never fetched as commits.
func (c *Client) Sync(ctx context.Context, in Request) (Result, error) {
	if !validName(in.Owner) || !validName(in.Repository) {
		return Result{}, fmt.Errorf("%w: valid owner and repository required", ErrInvalid)
	}
	commits, err := boundedSHAs(in.ReferencedCommits, c.maxReferences)
	if err != nil {
		return Result{}, err
	}
	checks, err := boundedSHAs(in.CheckSHAs, c.maxReferences)
	if err != nil {
		return Result{}, err
	}
	r := Result{Untrusted: true, Validators: make(map[string]Validator)}
	root := "/repos/" + in.Owner + "/" + in.Repository

	var repo Repository
	changed, _, err := c.get(ctx, root, "repository", in.Validators, &repo, &r)
	if err != nil {
		return Result{}, err
	}
	if changed {
		r.Repository = &repo
	}
	if err := c.syncIssues(ctx, root+"/issues", in.Validators, &r); err != nil {
		return Result{}, err
	}
	if err := c.syncPulls(ctx, root+"/pulls", in.Validators, &r); err != nil {
		return Result{}, err
	}
	for _, sha := range checks {
		if err := c.syncChecks(ctx, root+"/commits/"+sha+"/check-runs", sha, in.Validators, &r); err != nil {
			return Result{}, err
		}
	}
	for _, sha := range commits {
		var wire commitWire
		key := "commit:" + sha
		changed, _, err := c.get(ctx, root+"/commits/"+sha, key, in.Validators, &wire, &r)
		if err != nil {
			return Result{}, err
		}
		if changed {
			r.Commits = append(r.Commits, Page[Commit]{Key: key, Items: []Commit{wire.model()}})
		}
	}
	r.Receipt.Unchanged = r.Receipt.Requests > 0 && r.Receipt.Requests == r.Receipt.NotModified
	if r.Receipt.Unchanged {
		return Result{Receipt: r.Receipt}, nil
	}
	if len(r.Validators) == 0 {
		r.Validators = nil
	}
	return r, nil
}

func validName(s string) bool {
	if s == "" || len(s) > 100 || s == "." || s == ".." {
		return false
	}
	for _, ch := range s {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.') {
			return false
		}
	}
	return true
}

func boundedSHAs(values []string, limit int) ([]string, error) {
	if len(values) > limit {
		return nil, fmt.Errorf("%w: too many commit references", ErrInvalid)
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) < 7 || len(value) > 64 {
			return nil, fmt.Errorf("%w: invalid commit SHA", ErrInvalid)
		}
		for _, ch := range value {
			if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
				return nil, fmt.Errorf("%w: invalid commit SHA", ErrInvalid)
			}
		}
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func (c *Client) collectionPath(path string, page int, extra string) string {
	separator := "?"
	if extra != "" {
		separator = "&"
		path += "?" + extra
	}
	return path + separator + "per_page=" + strconv.Itoa(c.perPage) + "&page=" + strconv.Itoa(page)
}

func (c *Client) syncIssues(ctx context.Context, path string, validators map[string]Validator, out *Result) error {
	for page := 1; page <= c.maxPages; page++ {
		requestPath := c.collectionPath(path, page, "state=open&sort=updated&direction=desc")
		key := "issues:" + strconv.Itoa(page)
		var wire []issueWire
		changed, hasNext, err := c.get(ctx, requestPath, key, validators, &wire, out)
		if err != nil {
			return err
		}
		if changed {
			items := make([]Issue, 0, len(wire))
			for _, item := range wire {
				if item.PullRequest == nil {
					items = append(items, item.model())
				}
			}
			out.Issues = append(out.Issues, Page[Issue]{Key: key, Items: items})
		}
		if !hasNext {
			return nil
		}
	}
	out.Receipt.Truncated = true
	return nil
}

func (c *Client) syncPulls(ctx context.Context, path string, validators map[string]Validator, out *Result) error {
	for page := 1; page <= c.maxPages; page++ {
		requestPath := c.collectionPath(path, page, "state=open&sort=updated&direction=desc")
		key := "pulls:" + strconv.Itoa(page)
		var wire []pullWire
		changed, hasNext, err := c.get(ctx, requestPath, key, validators, &wire, out)
		if err != nil {
			return err
		}
		if changed {
			items := make([]PullRequest, 0, len(wire))
			for _, item := range wire {
				items = append(items, item.model())
			}
			out.PullRequests = append(out.PullRequests, Page[PullRequest]{Key: key, Items: items})
		}
		if !hasNext {
			return nil
		}
	}
	out.Receipt.Truncated = true
	return nil
}

func (c *Client) syncChecks(ctx context.Context, path, sha string, validators map[string]Validator, out *Result) error {
	for page := 1; page <= c.maxPages; page++ {
		requestPath := c.collectionPath(path, page, "filter=latest")
		key := "checks:" + sha + ":" + strconv.Itoa(page)
		var wire checksWire
		changed, hasNext, err := c.get(ctx, requestPath, key, validators, &wire, out)
		if err != nil {
			return err
		}
		if changed {
			items := make([]CheckRun, 0, len(wire.CheckRuns))
			for _, item := range wire.CheckRuns {
				items = append(items, item.model())
			}
			out.Checks = append(out.Checks, Page[CheckRun]{Key: key, Items: items})
		}
		if !hasNext {
			return nil
		}
	}
	out.Receipt.Truncated = true
	return nil
}

func (c *Client) get(ctx context.Context, path, key string, validators map[string]Validator, dst any, result *Result) (bool, bool, error) {
	u := *c.base
	parts := strings.SplitN(path, "?", 2)
	u.Path = c.base.Path + parts[0]
	if len(parts) == 2 {
		u.RawQuery = parts[1]
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	prior := validators[key]
	if prior.ETag != "" {
		req.Header.Set("If-None-Match", prior.ETag)
	}
	if prior.LastModified != "" {
		req.Header.Set("If-Modified-Since", prior.LastModified)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()
	result.Receipt.Requests++
	if resp.StatusCode == http.StatusNotModified {
		result.Receipt.NotModified++
		return false, prior.HasNext, nil
	}
	if err := responseError(resp, req.URL.EscapedPath()); err != nil {
		return false, false, err
	}

	reader := io.LimitReader(resp.Body, c.maxBodyBytes+1)
	payload, err := io.ReadAll(reader)
	if err != nil {
		return false, false, err
	}
	result.Receipt.BodyBytes += int64(len(payload))
	if int64(len(payload)) > c.maxBodyBytes {
		return false, false, ErrTooLarge
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	if err := decoder.Decode(dst); err != nil {
		return false, false, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return false, false, ErrMalformed
	}
	hasNext := collectionLength(dst) >= c.perPage
	result.Validators[key] = Validator{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified"), HasNext: hasNext}
	return true, hasNext, nil
}

func collectionLength(value any) int {
	switch v := value.(type) {
	case *[]issueWire:
		return len(*v)
	case *[]pullWire:
		return len(*v)
	case *checksWire:
		return len(v.CheckRuns)
	default:
		return 0
	}
}

func responseError(resp *http.Response, path string) error {
	remaining, _ := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")) {
		resetUnix, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
		retrySeconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return &RateLimitError{Status: resp.StatusCode, Remaining: remaining, Reset: time.Unix(resetUnix, 0).UTC(), RetryAfter: time.Duration(retrySeconds) * time.Second}
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return &HTTPError{Status: resp.StatusCode, Method: http.MethodGet, Path: path}
	}
}

type issueWire struct {
	ID        int64     `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	PullRequest *json.RawMessage `json:"pull_request"`
}

func (w issueWire) model() Issue {
	return Issue{ID: w.ID, Number: w.Number, Title: w.Title, State: w.State, Author: w.User.Login, UpdatedAt: w.UpdatedAt, HTMLURL: w.HTMLURL}
}

type pullWire struct {
	ID        int64     `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
	Head      struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (w pullWire) model() PullRequest {
	return PullRequest{ID: w.ID, Number: w.Number, Title: w.Title, State: w.State, Draft: w.Draft, HeadSHA: w.Head.SHA, BaseRef: w.Base.Ref, UpdatedAt: w.UpdatedAt, HTMLURL: w.HTMLURL}
}

type checksWire struct {
	CheckRuns []checkWire `json:"check_runs"`
}

type checkWire struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	HTMLURL     string    `json:"html_url"`
}

func (w checkWire) model() CheckRun {
	return CheckRun{ID: w.ID, Name: w.Name, Status: w.Status, Conclusion: w.Conclusion, StartedAt: w.StartedAt, CompletedAt: w.CompletedAt, HTMLURL: w.HTMLURL}
}

type commitWire struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name  string    `json:"name"`
			Email string    `json:"email"`
			Date  time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

func (w commitWire) model() Commit {
	return Commit{SHA: w.SHA, Message: w.Commit.Message, AuthorName: w.Commit.Author.Name, AuthorEmail: w.Commit.Author.Email, AuthoredAt: w.Commit.Author.Date, HTMLURL: w.HTMLURL}
}
