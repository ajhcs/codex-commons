package httpapi

import (
	"context"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/runtimehealth"
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
	// Runtime is the process-local health projection. It is populated from the
	// immutable snapshot maintained by the runtime supervisor; transport
	// handlers must never derive it by probing a dependency. Keeping this
	// section additive preserves the established installation-status contract.
	Runtime  RuntimeHealthSnapshot `json:"runtime"`
	Database struct {
		SchemaVersion  int    `json:"schema_version"`
		InstallationID string `json:"installation_id,omitempty"`
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

// RuntimeHealthSnapshot is the storage-neutral projection of the shared
// runtime supervisor snapshot. The runtimehealth package owns the mutable
// state; this value is deliberately copied before it reaches a handler so
// JSON projection cannot observe a partially-updated supervisor state.
//
// Mode is "required" or "optional". Required describes the deployment's
// policy, while Ready is the supervisor's final readiness decision. In
// particular, an unavailable optional Codex component must not be treated as
// a failed Commons readiness result.
type RuntimeHealthSnapshot struct {
	Mode              string                              `json:"mode"`
	Required          bool                                `json:"required"`
	State             string                              `json:"state"`
	Ready             bool                                `json:"ready"`
	Live              bool                                `json:"live"`
	Liveness          bool                                `json:"liveness"`
	WatchdogEligible  bool                                `json:"watchdog_eligible"`
	SchedulerEligible bool                                `json:"scheduler_eligible"`
	Status            string                              `json:"status"`
	Reason            string                              `json:"reason,omitempty"`
	ObservedAt        *time.Time                          `json:"observed_at,omitempty"`
	LastSuccessAt     *time.Time                          `json:"last_success_at,omitempty"`
	LastFailureAt     *time.Time                          `json:"last_failure_at,omitempty"`
	Generation        uint64                              `json:"generation"`
	Supervisor        RuntimeSupervisorSnapshot           `json:"supervisor"`
	Components        map[string]RuntimeComponentSnapshot `json:"components"`
}

// RuntimeSupervisorSnapshot contains bounded, non-secret supervisor
// metadata. LastError is intentionally absent: error payloads can contain
// command paths, environment details, or other values that are not safe for
// an HTTP response. Runtimehealth should publish a safe reason/status pair.
type RuntimeSupervisorSnapshot struct {
	Generation        uint64     `json:"generation"`
	State             string     `json:"state"`
	RetryCount        int        `json:"retry_count"`
	RetryAt           *time.Time `json:"retry_at,omitempty"`
	LastHealthy       *time.Time `json:"last_healthy_at,omitempty"`
	RecoveryActive    bool       `json:"recovery_active"`
	RecoveryExhausted bool       `json:"recovery_exhausted"`
	RecoverySince     *time.Time `json:"recovery_since,omitempty"`
}

// RuntimeComponentSnapshot is a bounded component-level readiness state. A
// component may be degraded while the overall snapshot remains ready when
// the component is optional.
type RuntimeComponentSnapshot struct {
	State    string `json:"state"`
	Ready    bool   `json:"ready"`
	Required bool   `json:"required"`
	Status   string `json:"status,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// RuntimeHealthProvider supplies the last immutable health snapshot. Snapshot
// must be a pure, non-blocking accessor: implementations must not ping the
// database, query Codex, or perform other I/O in response to an HTTP request.
type RuntimeHealthProvider interface {
	Snapshot() RuntimeHealthSnapshot
}

// RuntimeHealthProviderFunc adapts a closure to RuntimeHealthProvider. It is
// useful to wire the runtimehealth package without making httpapi depend on
// the supervisor's concrete implementation.
type RuntimeHealthProviderFunc func() RuntimeHealthSnapshot

func (f RuntimeHealthProviderFunc) Snapshot() RuntimeHealthSnapshot {
	if f == nil {
		return RuntimeHealthSnapshot{}
	}
	return f()
}

// CloneRuntimeHealthSnapshot returns a value-safe copy for transport owners
// that receive a snapshot directly from a provider. The copy is deliberately
// limited to the DTO's map and pointer members; all scalar status fields are
// already value types.
func CloneRuntimeHealthSnapshot(in RuntimeHealthSnapshot) RuntimeHealthSnapshot {
	out := in
	if out.Generation == 0 {
		out.Generation = out.Supervisor.Generation
	}
	if out.Supervisor.Generation == 0 {
		out.Supervisor.Generation = out.Generation
	}
	if in.ObservedAt != nil {
		value := *in.ObservedAt
		out.ObservedAt = &value
	}
	if in.LastSuccessAt != nil {
		value := *in.LastSuccessAt
		out.LastSuccessAt = &value
	}
	if in.LastFailureAt != nil {
		value := *in.LastFailureAt
		out.LastFailureAt = &value
	}
	if in.Supervisor.RetryAt != nil {
		value := *in.Supervisor.RetryAt
		out.Supervisor.RetryAt = &value
	}
	if in.Supervisor.LastHealthy != nil {
		value := *in.Supervisor.LastHealthy
		out.Supervisor.LastHealthy = &value
	}
	if in.Supervisor.RecoverySince != nil {
		value := *in.Supervisor.RecoverySince
		out.Supervisor.RecoverySince = &value
	}
	if in.Components != nil {
		out.Components = make(map[string]RuntimeComponentSnapshot, len(in.Components))
		for name, component := range in.Components {
			out.Components[name] = component
		}
	}
	return out
}

// RuntimeHealthBackend exposes the pure runtime snapshot projection used by
// the loopback readiness endpoint. It intentionally has no context argument:
// a readiness request is not permission to perform dependency I/O.
type RuntimeHealthBackend interface {
	RuntimeHealth() RuntimeHealthSnapshot
}

// ProjectRuntimeHealth adapts the storage-neutral runtimehealth evaluator
// result to the additive HTTP installation/readiness projection. It performs
// no I/O and copies only bounded status codes, booleans, generations, and
// timestamps. The required flag comes from deployment policy rather than a
// request, so an optional Codex degradation remains service-ready.
func ProjectRuntimeHealth(snapshot runtimehealth.Snapshot, required bool) RuntimeHealthSnapshot {
	mode := "optional"
	if required {
		mode = "required"
	}
	supervisorState := string(snapshot.Components.Supervisor.Status)
	switch snapshot.Components.Supervisor.Status {
	case runtimehealth.ComponentHealthy:
		supervisorState = string(runtimehealth.SupervisorRunning)
	case runtimehealth.ComponentStarting:
		supervisorState = string(runtimehealth.SupervisorStarting)
	case runtimehealth.ComponentRecovering:
		supervisorState = string(runtimehealth.SupervisorRecovering)
	case runtimehealth.ComponentExhausted:
		supervisorState = string(runtimehealth.SupervisorExhausted)
	case runtimehealth.ComponentStopping:
		supervisorState = string(runtimehealth.SupervisorStopping)
	}
	out := RuntimeHealthSnapshot{
		Mode:              mode,
		Required:          required,
		State:             string(snapshot.State),
		Ready:             snapshot.Ready,
		Live:              snapshot.Live,
		Liveness:          snapshot.Liveness,
		WatchdogEligible:  snapshot.WatchdogEligible,
		SchedulerEligible: snapshot.SchedulerEligible,
		Status:            string(snapshot.Status),
		Reason:            string(snapshot.Reason),
		Generation:        snapshot.Generation,
		Supervisor: RuntimeSupervisorSnapshot{
			Generation: snapshot.Generation,
			State:      supervisorState,
		},
		Components: make(map[string]RuntimeComponentSnapshot, 7),
	}
	if !snapshot.ObservedAt.IsZero() {
		value := snapshot.ObservedAt.UTC()
		out.ObservedAt = &value
	}
	if !snapshot.LastSuccessAt.IsZero() {
		value := snapshot.LastSuccessAt.UTC()
		out.LastSuccessAt = &value
	}
	if !snapshot.LastFailureAt.IsZero() {
		value := snapshot.LastFailureAt.UTC()
		out.LastFailureAt = &value
	}
	add := func(name string, status runtimehealth.ComponentStatus, componentRequired bool) {
		out.Components[name] = RuntimeComponentSnapshot{
			State:    string(status.Status),
			Ready:    status.Status == runtimehealth.ComponentHealthy,
			Required: componentRequired,
			Status:   string(status.Status),
			Reason:   string(status.Reason),
		}
	}
	add("database", snapshot.Components.Database, true)
	add("codex", snapshot.Components.Codex, required)
	add("supervisor", snapshot.Components.Supervisor, required)
	add("account", snapshot.Components.Account, required)
	add("model", snapshot.Components.Model, required)
	add("reconciliation", snapshot.Components.Reconciliation, true)
	add("persistence", snapshot.Components.Persistence, true)
	return out
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
