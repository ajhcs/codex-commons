// Package githubsync provides a bounded, read-only GitHub synchronization
// client. Remote strings are returned as inert, explicitly untrusted data.
package githubsync

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	ResourceRepository   = "repository"
	ResourceIssues       = "issues"
	ResourcePullRequests = "pull_requests"
	ResourceChecks       = "checks"
	ResourceCommit       = "commit"
)

var (
	ErrInvalid      = errors.New("invalid github sync request")
	ErrTooLarge     = errors.New("github response exceeds limit")
	ErrMalformed    = errors.New("malformed github response")
	ErrUnauthorized = errors.New("github authentication failed")
	ErrNotFound     = errors.New("github resource not found")
)

// Validator is persisted by the application under the stable request key.
// HasNext is needed to continue a conditionally unchanged collection page.
type Validator struct {
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	HasNext      bool   `json:"has_next,omitempty"`
}

type Request struct {
	Owner             string
	Repository        string
	Validators        map[string]Validator
	ReferencedCommits []string
	CheckSHAs         []string
}

type Result struct {
	Untrusted    bool                 `json:"untrusted,omitempty"`
	Repository   *Repository          `json:"repository,omitempty"`
	Issues       []Page[Issue]        `json:"issue_pages,omitempty"`
	PullRequests []Page[PullRequest]  `json:"pull_request_pages,omitempty"`
	Checks       []Page[CheckRun]     `json:"check_pages,omitempty"`
	Commits      []Page[Commit]       `json:"commits,omitempty"`
	Validators   map[string]Validator `json:"validator_updates,omitempty"`
	Receipt      Receipt              `json:"receipt"`
}

type Receipt struct {
	Unchanged   bool  `json:"unchanged"`
	Requests    int   `json:"requests"`
	NotModified int   `json:"not_modified"`
	BodyBytes   int64 `json:"body_bytes"`
	Truncated   bool  `json:"truncated,omitempty"`
}

// Page is a changed page only. An all-304 Sync result contains just Receipt;
// callers retain their previously persisted data and validators.
type Page[T any] struct {
	Key   string `json:"key"`
	Items []T    `json:"items"`
}

type Repository struct {
	ID            int64     `json:"id"`
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	Private       bool      `json:"private"`
	Archived      bool      `json:"archived"`
	UpdatedAt     time.Time `json:"updated_at"`
	HTMLURL       string    `json:"html_url"`
}

type Issue struct {
	ID        int64     `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Author    string    `json:"author"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
}

type PullRequest struct {
	ID        int64     `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	HeadSHA   string    `json:"head_sha"`
	BaseRef   string    `json:"base_ref"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
}

type CheckRun struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	HTMLURL     string    `json:"html_url"`
}

type Commit struct {
	SHA         string    `json:"sha"`
	Message     string    `json:"message"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	AuthoredAt  time.Time `json:"authored_at"`
	HTMLURL     string    `json:"html_url"`
}

type RateLimitError struct {
	Status     int
	Remaining  int
	Reset      time.Time
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github rate limited (status %d, remaining %d)", e.Status, e.Remaining)
}

// HTTPError reports a sanitized non-success response. It never includes a
// response body or configured credential.
type HTTPError struct {
	Status int
	Method string
	Path   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github %s %s returned %s", e.Method, e.Path, http.StatusText(e.Status))
}
