package httpapi

import (
	"context"
	"time"

	"codex-commons/internal/application"
)

// Backend is the complete storage boundary for the Slice 2 transport. It is
// keeps the established transport types while GeneralHome aliases the application-owned read model.
type Backend interface {
	LegacyBackend
	GeneralHome(context.Context, GeneralHomeQuery, RequestMeta) (GeneralHomeResult, error)
	BrowseAttention(context.Context, AttentionBrowseQuery, RequestMeta) (AttentionBrowseResult, error)
	BrowseProjects(context.Context, ProjectsBrowseQuery, RequestMeta) (ProjectsBrowseResult, error)
	BrowsePeople(context.Context, PeopleBrowseQuery, RequestMeta) (PeopleBrowseResult, error)
	BrowseTopics(context.Context, TopicsQuery, RequestMeta) (TopicsResult, error)
	BrowsePosts(context.Context, PostFeedQuery, RequestMeta) (PostFeedResult, error)
	OpenPost(context.Context, PostOpenQuery, RequestMeta) (PostOpenResult, error)
	SetPostState(context.Context, PostStateWriteRequest, RequestMeta) (WriteResult, error)
	ProjectOverview(context.Context, ProjectOverviewQuery, RequestMeta) (ProjectOverviewResult, error)
}

type CommentReadBackend interface {
	OpenComment(context.Context, CommentOpenQuery, RequestMeta) (CommentOpenResult, error)
}

type NotificationBackend interface {
	Notifications(context.Context, NotificationListQuery, RequestMeta) (NotificationListResult, error)
	MarkNotificationRead(context.Context, NotificationReadRequest, RequestMeta) (WriteResult, error)
}

type InstallationStatusBackend interface {
	InstallationStatus(context.Context, RequestMeta) (InstallationStatusResult, error)
}

type InstallationStatusResult struct {
	Service struct {
		Version   string    `json:"version"`
		StartedAt time.Time `json:"started_at"`
	} `json:"service"`
	Database struct {
		SchemaVersion int `json:"schema_version"`
	} `json:"database"`
	Codex struct {
		Configured               bool       `json:"configured"`
		Available                bool       `json:"available"`
		Version                  string     `json:"version,omitempty"`
		AccountState             string     `json:"account_state"`
		CompatibilityStatus      string     `json:"compatibility_status"`
		CompatibilityCheckedAt   *time.Time `json:"compatibility_checked_at,omitempty"`
		SessionRevocationPending bool       `json:"session_revocation_pending"`
	} `json:"codex"`
	Archaeology struct {
		CatalogCompletedAt *time.Time `json:"catalog_completed_at,omitempty"`
		ActiveCount        int        `json:"active_count"`
		UncertainCount     int        `json:"uncertain_count"`
	} `json:"archaeology"`
	Backup struct {
		Status         string     `json:"status"`
		LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	} `json:"backup"`
	Reconciliation struct {
		Status string     `json:"status"`
		LastAt *time.Time `json:"last_at,omitempty"`
	} `json:"reconciliation"`
	Evidence struct {
		CompletedHistorians    int                  `json:"completed_historians"`
		FailedHistorians       int                  `json:"failed_historians"`
		UncertainHistorians    int                  `json:"uncertain_historians"`
		DistinctProjects       int                  `json:"distinct_projects"`
		ReportsReceived        int                  `json:"reports_received"`
		LostReports            int                  `json:"lost_reports"`
		ReviewedImports        int                  `json:"reviewed_imports"`
		Cancellations          int                  `json:"cancellations"`
		ReportRecovery         EvidenceVerification `json:"report_recovery"`
		DuplicateLaunchCheck   EvidenceVerification `json:"duplicate_launch_check"`
		RepositoryImmutability EvidenceVerification `json:"repository_immutability"`
		CanonicalImmutability  EvidenceVerification `json:"canonical_immutability"`
		BetaPrerequisitesMet   bool                 `json:"beta_prerequisites_met"`
		RestoreDrill           struct {
			Status         string     `json:"status"`
			LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
		} `json:"restore_drill"`
	} `json:"evidence"`
}

type EvidenceVerification struct {
	Status     string     `json:"status"`
	Violations int        `json:"violations"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
}

type AddressabilityBackend interface {
	LookupContributors(context.Context, ContributorLookupQuery, RequestMeta) (ContributorLookupResult, error)
	SetPerspectiveScope(context.Context, PerspectiveScopeWriteRequest, RequestMeta) (WriteResult, error)
}

type LegacyBackend interface {
	Health(context.Context, RequestMeta) (HealthResult, error)
	Context(context.Context, ContextQuery, RequestMeta) (ContextResult, error)
	Who(context.Context, WhoQuery, RequestMeta) (WhoResult, error)
	Inbox(context.Context, InboxQuery, RequestMeta) (InboxResult, error)
	Search(context.Context, SearchQuery, RequestMeta) (SearchResult, error)
	Open(context.Context, OpenQuery, RequestMeta) (OpenResult, error)
	Next(context.Context, NextQuery, RequestMeta) (NextResult, error)
	Claim(context.Context, ClaimRequest, RequestMeta) (WriteResult, error)
	Post(context.Context, PostRequest, RequestMeta) (WriteResult, error)
	Comment(context.Context, CommentRequest, RequestMeta) (WriteResult, error)
	SetStatus(context.Context, StatusRequest, RequestMeta) (WriteResult, error)
	RequestTopic(context.Context, TopicRequest, RequestMeta) (WriteResult, error)
}

type RequestMeta struct {
	PrincipalKind  string
	Principal      string
	Actor          string
	Session        string
	Host           string
	RequestID      string
	IdempotencyKey string
}

type GeneralHomeQuery = application.HomeQuery
type GeneralHomeResult = application.GeneralHome
type AttentionBrowseQuery = application.AttentionBrowseRequest
type AttentionBrowseResult = application.AttentionBrowseResult
type ProjectsBrowseQuery = application.ProjectsBrowseRequest
type ProjectsBrowseResult = application.ProjectsBrowseResult
type PeopleBrowseQuery = application.PeopleBrowseRequest
type PeopleBrowseResult = application.PeopleBrowseResult
type TopicsQuery = application.TopicsRequest
type TopicsResult = application.TopicsResult
type TopicItem = application.TopicItem
type PostFeedQuery = application.PostFeedRequest
type PostFeedResult = application.PostFeedResult
type PostOpenQuery = application.PostOpenRequest
type PostOpenResult = application.PostOpenResult
type ContributorLookupQuery = application.ContributorLookupRequest
type ContributorLookupResult = application.ContributorLookupResult
type PostAttachment = application.PostAttachment
type ProjectOverviewQuery = application.ProjectOverviewQuery
type ProjectOverviewResult = application.ProjectOverview
type NotificationListQuery = application.NotificationListRequest
type CommentOpenQuery = application.CommentOpenRequest
type CommentOpenResult = application.CommentOpenResult
type NotificationListResult = application.NotificationListResult

type HealthResult struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type Budget struct {
	Requested int    `json:"requested"`
	Used      int    `json:"used"`
	Unit      string `json:"unit"`
}

type ContextQuery struct {
	Project string
	Since   *int64
	Budget  int
}

type ContextResult struct {
	Project   string         `json:"project"`
	Revision  int64          `json:"revision"`
	Cursor    int64          `json:"cursor"`
	Unchanged bool           `json:"unchanged"`
	Budget    Budget         `json:"budget"`
	Packet    map[string]any `json:"packet,omitempty"`
}

type WhoQuery struct {
	Project string
	State   string
	Limit   int
}

type PresenceItem struct {
	Session       string `json:"session"`
	Actor         string `json:"actor"`
	Host          string `json:"host"`
	HostConnected bool   `json:"host_connected"`
	Execution     string `json:"execution"`
	LeaseExpires  string `json:"lease_expires,omitempty"`
	LastActivity  string `json:"last_activity"`
	LoadedFact    string `json:"loaded_fact,omitempty"`
	Project       string `json:"project,omitempty"`
}

type WhoResult struct {
	Sessions []PresenceItem `json:"sessions"`
}

type InboxQuery struct {
	Project string
	Limit   int
}

type InboxItem struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	From    string `json:"from"`
	Ref     string `json:"ref"`
	Unread  bool   `json:"unread"`
	Snippet string `json:"snippet"`
}

type InboxResult struct {
	Project  string      `json:"project"`
	Unread   int         `json:"unread"`
	Mentions int         `json:"mentions"`
	Replies  int         `json:"replies"`
	Items    []InboxItem `json:"items"`
}

type SearchQuery struct {
	Project string
	Query   string
	Limit   int
}

type SearchHit struct {
	Ref       string `json:"ref"`
	Revision  int64  `json:"revision"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Timestamp string `json:"timestamp"`
	Snippet   string `json:"snippet"`
}

type SearchResult struct {
	Project string      `json:"project"`
	Hits    []SearchHit `json:"hits"`
}

type OpenQuery struct {
	Ref    string
	Budget int
}

type OpenResult struct {
	Ref      string         `json:"ref"`
	Kind     string         `json:"kind"`
	Revision int64          `json:"revision"`
	Object   map[string]any `json:"object"`
	Budget   Budget         `json:"budget"`
}

type NextQuery struct {
	Project string
	Limit   int
}

type TaskItem struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Priority int    `json:"priority"`
	Title    string `json:"title"`
	Accept   string `json:"accept,omitempty"`
}

type NextResult struct {
	Project string     `json:"project"`
	Tasks   []TaskItem `json:"tasks"`
}

// Write request types deliberately contain no author, actor, session, or host.
type ClaimRequest struct {
	Task  string `json:"task"`
	Lease string `json:"lease,omitempty"`
}

type PostRequest struct {
	Topic       string           `json:"topic"`
	Kind        string           `json:"kind"`
	Title       string           `json:"title"`
	Body        string           `json:"body"`
	Basis       string           `json:"basis"`
	Ref         string           `json:"ref,omitempty"`
	Attachments []PostAttachment `json:"attachments,omitempty"`
	Mentions    []MentionRequest `json:"mentions,omitempty"`
}

type PostStateWriteRequest struct {
	Ref          string `json:"ref"`
	State        string `json:"state"`
	SupersededBy string `json:"superseded_by,omitempty"`
}

type PerspectiveScopeWriteRequest struct {
	Ref          string `json:"ref"`
	Scope        string `json:"scope"`
	BaseRevision int64  `json:"base_revision"`
}

type MentionRequest struct {
	Principal string `json:"principal"`
	Session   string `json:"session,omitempty"`
}

type CommentRequest struct {
	Ref      string           `json:"ref"`
	Body     string           `json:"body"`
	Intent   string           `json:"intent"`
	Basis    string           `json:"basis,omitempty"`
	Mentions []MentionRequest `json:"mentions,omitempty"`
}

type StatusRequest struct {
	Ref    string `json:"ref"`
	Status string `json:"status"`
	Basis  string `json:"basis"`
}

type TopicRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Basis string `json:"basis"`
}

// WriteResult acknowledges a committed write. Revision is positive for
// project-scoped writes; zero is reserved for any committed post on the global
// General topic, which has no project change cursor.
type NotificationReadRequest struct {
	ID string `json:"id"`
}

type WriteResult struct {
	ID        string `json:"id"`
	Revision  int64  `json:"revision"`
	Persisted bool   `json:"persisted"`
}

// Error is the only backend error that crosses the transport boundary.
const (
	CodeForbidden   = "forbidden"
	CodeNotFound    = "not_found"
	CodeConflict    = "conflict"
	CodeInvalid     = "invalid"
	CodeUnavailable = "unavailable"
)

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func NewError(code, message string) *Error { return &Error{Code: code, Message: message} }
