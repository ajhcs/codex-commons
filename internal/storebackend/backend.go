// Package storebackend translates the durable SQLite and process-local
// presence contracts into the storage-neutral HTTP API contract.
package storebackend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

type Backend struct {
	store         *commonsstore.Store
	presence      *presence.Registry
	version       string
	now           func() time.Time
	startedAt     time.Time
	codex         codexauth.Client
	codexVersion  string
	runtimeHealth httpapi.RuntimeHealthProvider
	// runtimeHealthConfigured distinguishes the production snapshot seam from
	// legacy callers that construct the backend without a supervisor. The
	// former must remain pure; the latter retains the established DB-gated
	// liveness behavior instead of silently accepting a synthetic green state.
	runtimeHealthConfigured bool
}

func (b *Backend) ConfigureCodex(client codexauth.Client, version string) {
	b.codex, b.codexVersion = client, version
}

// ConfigureRuntimeHealth wires the process-local health snapshot published by
// the runtime supervisor. The provider is intentionally a small interface so
// the storage backend does not depend on the supervisor's concrete package.
// Passing nil restores the conservative legacy mode used by callers that do
// not own a runtime supervisor.
func (b *Backend) ConfigureRuntimeHealth(provider httpapi.RuntimeHealthProvider) {
	if provider == nil {
		b.runtimeHealth = defaultRuntimeHealthProvider()
		b.runtimeHealthConfigured = false
		return
	}
	b.runtimeHealth = provider
	b.runtimeHealthConfigured = true
}

const presenceIdleWindow = time.Hour

func New(store *commonsstore.Store, live *presence.Registry, version string) (*Backend, error) {
	if store == nil || live == nil {
		return nil, errors.New("store and presence registry required")
	}
	return &Backend{store: store, presence: live, version: version, now: time.Now, startedAt: time.Now().UTC(), runtimeHealth: defaultRuntimeHealthProvider()}, nil
}

// NewWithRuntimeHealth is the explicit wiring constructor for the server. New
// remains the compatibility path for tests and small callers that do not own a
// supervisor yet. A nil provider deliberately keeps that legacy path DB-gated.
func NewWithRuntimeHealth(store *commonsstore.Store, live *presence.Registry, version string, provider httpapi.RuntimeHealthProvider) (*Backend, error) {
	backend, err := New(store, live, version)
	if err != nil {
		return nil, err
	}
	backend.ConfigureRuntimeHealth(provider)
	return backend, nil
}

// RuntimeHealth returns a defensive copy of the last supervisor snapshot. An
// unconfigured backend exposes a conservative unknown snapshot; it must never
// claim that a real database is live merely because no supervisor was wired.
// Providers are expected to publish immutable values, but copying the map and
// time pointers here prevents a caller from mutating a provider-owned value
// through the response projection.
func (b *Backend) RuntimeHealth() httpapi.RuntimeHealthSnapshot {
	if b == nil || !b.runtimeHealthConfigured || b.runtimeHealth == nil {
		return defaultRuntimeHealthSnapshot()
	}
	return cloneRuntimeHealthSnapshot(b.runtimeHealth.Snapshot())
}

func defaultRuntimeHealthProvider() httpapi.RuntimeHealthProvider {
	return httpapi.RuntimeHealthProviderFunc(defaultRuntimeHealthSnapshot)
}

func defaultRuntimeHealthSnapshot() httpapi.RuntimeHealthSnapshot {
	return httpapi.RuntimeHealthSnapshot{
		Mode: "optional", Required: false, State: "starting", Ready: false, Live: false, Liveness: false, Status: "starting", Reason: "runtime_health_unconfigured",
		Components: map[string]httpapi.RuntimeComponentSnapshot{
			"database": {State: "unknown", Ready: false, Required: true, Status: "unknown", Reason: "database_unknown"},
			"codex":    {State: "unknown", Ready: false, Required: false, Status: "unknown", Reason: "runtime_health_unconfigured"},
		},
	}
}

func cloneRuntimeHealthSnapshot(in httpapi.RuntimeHealthSnapshot) httpapi.RuntimeHealthSnapshot {
	return httpapi.CloneRuntimeHealthSnapshot(in)
}

func (b *Backend) InstallationIdentityHex(ctx context.Context) (string, error) {
	if b == nil || b.store == nil {
		return "", httpapi.NewError(httpapi.CodeUnavailable, "installation identity unavailable")
	}
	return b.store.InstallationIdentityHex(ctx)
}

// Health is the public liveness projection. Production wiring supplies the
// immutable runtime provider, in which case this method performs no I/O and
// gates only on its cached Live value. A backend without an explicit provider
// retains the old DB ping for compatibility with standalone callers/tests;
// that path is not used by the server's production health endpoints.
func (b *Backend) Health(ctx context.Context, _ httpapi.RequestMeta) (httpapi.HealthResult, error) {
	if b == nil || !b.runtimeHealthConfigured {
		if b == nil || b.store == nil || b.store.DB().PingContext(ctx) != nil {
			return httpapi.HealthResult{}, httpapi.NewError(httpapi.CodeUnavailable, "database unavailable")
		}
		return httpapi.HealthResult{Status: "ok", Version: b.version}, nil
	}
	result := httpapi.HealthResult{Status: "ok", Version: b.version}
	snapshot := b.RuntimeHealth()
	if !snapshot.Live {
		result.Status = "degraded"
		return result, httpapi.NewError(httpapi.CodeUnavailable, "service unavailable")
	}
	return result, nil
}

func (b *Backend) InstallationStatus(ctx context.Context, _ httpapi.RequestMeta) (httpapi.InstallationStatusResult, error) {
	var out httpapi.InstallationStatusResult
	out.Service.Version = b.version
	out.Service.StartedAt = b.startedAt
	out.Runtime = b.RuntimeHealth()
	out.Codex.AccountState = "unknown"
	out.Codex.Configured = b.codex != nil
	out.Codex.Version = b.codexVersion
	if b.codex != nil {
		out.Codex.Available = b.codex.Available()
		checkCtx, cancel := context.WithTimeout(ctx, time.Second)
		state, err := b.codex.AccountState(checkCtx)
		cancel()
		if err == nil {
			out.Codex.AccountState = string(state)
		}
	}
	var backupAt, reconcileAt, compatibilityAt, restoreAt, recoveryAt, duplicateAt, repositoryAt, canonicalAt sql.NullString
	var recoveryDigest, duplicateDigest, repositoryDigest, canonicalDigest string
	var installationID []byte
	if err := b.store.DB().QueryRowContext(ctx, `SELECT (SELECT max(version) FROM schema_migrations),installation_id,backup_status,backup_verified_at,reconciliation_status,reconciliation_checked_at,compatibility_status,compatibility_checked_at,restore_status,restore_verified_at,report_recovery_status,report_recovery_violations,report_recovery_checked_at,report_recovery_receipt_digest,duplicate_launch_status,duplicate_launch_violations,duplicate_launch_checked_at,duplicate_launch_receipt_digest,repository_immutability_status,repository_immutability_violations,repository_immutability_checked_at,repository_immutability_receipt_digest,canonical_immutability_status,canonical_immutability_violations,canonical_immutability_checked_at,canonical_immutability_receipt_digest,codex_session_revocation_pending FROM installation_status WHERE id=1`).Scan(&out.Database.SchemaVersion, &installationID, &out.Backup.Status, &backupAt, &out.Reconciliation.Status, &reconcileAt, &out.Codex.CompatibilityStatus, &compatibilityAt, &out.Evidence.RestoreDrill.Status, &restoreAt, &out.Evidence.ReportRecovery.Status, &out.Evidence.ReportRecovery.Violations, &recoveryAt, &recoveryDigest, &out.Evidence.DuplicateLaunchCheck.Status, &out.Evidence.DuplicateLaunchCheck.Violations, &duplicateAt, &duplicateDigest, &out.Evidence.RepositoryImmutability.Status, &out.Evidence.RepositoryImmutability.Violations, &repositoryAt, &repositoryDigest, &out.Evidence.CanonicalImmutability.Status, &out.Evidence.CanonicalImmutability.Violations, &canonicalAt, &canonicalDigest, &out.Codex.SessionRevocationPending); err != nil {
		return out, httpapi.NewError(httpapi.CodeUnavailable, "installation status unavailable")
	}
	encoded, err := commonsstore.EncodeInstallationIdentity(installationID)
	if err != nil {
		return out, httpapi.NewError(httpapi.CodeUnavailable, "installation status unavailable")
	}
	out.Database.InstallationID = encoded
	var discovered sql.NullString
	if err := b.store.DB().QueryRowContext(ctx, `SELECT max(discovered_at),(SELECT count(*) FROM archaeology_native_jobs WHERE state IN ('starting','active','report_ready','cancel_requested')),(SELECT count(*) FROM archaeology_native_jobs WHERE state='uncertain') FROM archaeology_sessions`).Scan(&discovered, &out.Archaeology.ActiveCount, &out.Archaeology.UncertainCount); err != nil {
		return out, httpapi.NewError(httpapi.CodeUnavailable, "installation status unavailable")
	}
	parse := func(value sql.NullString) *time.Time {
		if !value.Valid {
			return nil
		}
		at, err := time.Parse(time.RFC3339Nano, value.String)
		if err != nil {
			return nil
		}
		return &at
	}
	out.Backup.LastVerifiedAt = parse(backupAt)
	out.Reconciliation.LastAt = parse(reconcileAt)
	out.Codex.CompatibilityCheckedAt = parse(compatibilityAt)
	out.Archaeology.CatalogCompletedAt = parse(discovered)
	out.Evidence.RestoreDrill.LastVerifiedAt = parse(restoreAt)
	out.Evidence.ReportRecovery.CheckedAt = parse(recoveryAt)
	out.Evidence.DuplicateLaunchCheck.CheckedAt = parse(duplicateAt)
	out.Evidence.RepositoryImmutability.CheckedAt = parse(repositoryAt)
	out.Evidence.CanonicalImmutability.CheckedAt = parse(canonicalAt)
	verifyReceipt := func(kind, digest string, value *httpapi.EvidenceVerification) {
		if len(digest) != 64 || value.CheckedAt == nil {
			*value = httpapi.EvidenceVerification{Status: "unknown"}
			return
		}
		var count int
		err := b.store.DB().QueryRowContext(ctx, `SELECT count(*) FROM installation_evidence_receipts WHERE kind=? AND status=? AND violations=? AND checked_at=? AND receipt_digest=?`, kind, value.Status, value.Violations, value.CheckedAt.UTC().Format(time.RFC3339Nano), digest).Scan(&count)
		if err != nil || count != 1 {
			*value = httpapi.EvidenceVerification{Status: "unknown"}
		}
	}
	verifyReceipt("report_recovery", recoveryDigest, &out.Evidence.ReportRecovery)
	verifyReceipt("duplicate_launch", duplicateDigest, &out.Evidence.DuplicateLaunchCheck)
	verifyReceipt("repository_immutability", repositoryDigest, &out.Evidence.RepositoryImmutability)
	verifyReceipt("canonical_immutability", canonicalDigest, &out.Evidence.CanonicalImmutability)
	if err := b.store.DB().QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM archaeology_native_jobs WHERE state='completed'),
		(SELECT count(*) FROM archaeology_native_jobs WHERE state IN ('failed','interrupted')),
		(SELECT count(*) FROM archaeology_native_jobs WHERE state IN ('uncertain','attention')),
		(SELECT count(DISTINCT project_id) FROM archaeology_native_jobs),
		(SELECT count(DISTINCT job_id) FROM archaeology_native_outcomes),
		(SELECT count(*) FROM archaeology_native_jobs WHERE error_code='completed_without_report'),
		(SELECT count(*) FROM archaeology_selected_imports),
		(SELECT count(*) FROM archaeology_native_batches WHERE state='canceled')`).Scan(&out.Evidence.CompletedHistorians, &out.Evidence.FailedHistorians, &out.Evidence.UncertainHistorians, &out.Evidence.DistinctProjects, &out.Evidence.ReportsReceived, &out.Evidence.LostReports, &out.Evidence.ReviewedImports, &out.Evidence.Cancellations); err != nil {
		return out, httpapi.NewError(httpapi.CodeUnavailable, "installation evidence unavailable")
	}
	out.Evidence.BetaPrerequisitesMet = out.Codex.Configured && out.Codex.Available && out.Codex.AccountState == "signed_in" && !out.Codex.SessionRevocationPending && out.Evidence.LostReports == 0 && out.Evidence.UncertainHistorians == 0 && out.Backup.Status == "verified" && out.Evidence.RestoreDrill.Status == "verified" && out.Codex.CompatibilityStatus == "compatible" && out.Reconciliation.Status == "healthy" && out.Evidence.ReportRecovery.Status == "verified" && out.Evidence.DuplicateLaunchCheck.Status == "verified" && out.Evidence.RepositoryImmutability.Status == "verified" && out.Evidence.CanonicalImmutability.Status == "verified"
	return out, nil
}

func (b *Backend) Context(ctx context.Context, query httpapi.ContextQuery, meta httpapi.RequestMeta) (httpapi.ContextResult, error) {
	since := int64(0)
	if query.Since != nil {
		since = *query.Since
	}
	packet, err := b.store.Context(ctx, query.Project, meta.Session, since)
	if err != nil {
		return httpapi.ContextResult{}, mapError(err, "context")
	}
	filtered, filterErr := b.filterPostChanges(ctx, packet.Changes, meta)
	if filterErr != nil {
		return httpapi.ContextResult{}, filterErr
	}
	packet.Changes = filtered
	result := httpapi.ContextResult{
		Project: query.Project, Revision: packet.Project.Revision, Cursor: packet.Project.Revision,
		Unchanged: packet.Unchanged,
		Budget:    httpapi.Budget{Requested: query.Budget, Unit: "estimated_tokens"},
	}
	if !packet.Unchanged {
		result.Packet = contextMap(packet)
	}
	result.Budget.Used = estimatedTokens(result.Packet)
	if result.Budget.Used > query.Budget {
		return httpapi.ContextResult{}, httpapi.NewError(httpapi.CodeInvalid, "context exceeds requested budget; use since cursor")
	}
	return result, nil
}

func (b *Backend) filterPostChanges(ctx context.Context, changes []domain.Change, meta httpapi.RequestMeta) ([]domain.Change, error) {
	out := make([]domain.Change, 0, len(changes))
	for _, change := range changes {
		postID := ""
		switch change.Kind {
		case "post":
			postID = change.Ref
		case "comment":
			var err error
			postID, err = b.store.PostForComment(ctx, change.Ref)
			if err != nil {
				return nil, mapError(err, "context")
			}
		}
		if postID != "" {
			allowed, err := b.store.CanDiscoverPost(ctx, meta.PrincipalKind, meta.Session, postID)
			if err != nil {
				return nil, mapError(err, "context")
			}
			if !allowed {
				continue
			}
		}
		out = append(out, change)
	}
	return out, nil
}

func contextMap(packet domain.ContextPacket) map[string]any {
	return map[string]any{
		"project": packet.Project, "changes": packet.Changes, "tasks": packet.Tasks,
		"decisions": packet.Decisions, "wiki": packet.Wiki, "sessions": packet.Sessions,
		"unread": packet.Unread,
	}
}

func estimatedTokens(value any) int {
	if value == nil {
		return 0
	}
	body, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return (len(body) + 3) / 4
}

func (b *Backend) Who(_ context.Context, query httpapi.WhoQuery, _ httpapi.RequestMeta) (httpapi.WhoResult, error) {
	items := b.presence.List(query.Project)
	out := httpapi.WhoResult{Sessions: make([]httpapi.PresenceItem, 0, min(query.Limit, len(items)))}
	now := b.now().UTC()
	for _, item := range items {
		if !matchesPresenceState(query.State, item, now) {
			continue
		}
		lease := ""
		if item.LeaseExpires != nil {
			lease = item.LeaseExpires.UTC().Format(time.RFC3339Nano)
		}
		loaded := ""
		if item.LoadedFact != nil {
			loaded = *item.LoadedFact
		}
		out.Sessions = append(out.Sessions, httpapi.PresenceItem{
			Session: item.Session, Actor: item.Actor, Host: item.Host, Project: item.Project,
			HostConnected: item.HostConnected, Execution: item.Execution, LeaseExpires: lease,
			LastActivity: item.LastActivity.UTC().Format(time.RFC3339Nano), LoadedFact: loaded,
		})
		if len(out.Sessions) == query.Limit {
			break
		}
	}
	return out, nil
}

func matchesPresenceState(state string, item presence.Snapshot, now time.Time) bool {
	if state == "all" {
		return true
	}
	if item.Execution == "executing" {
		return state == "active" || state == "live"
	}
	idle := now.Sub(item.LastActivity) <= presenceIdleWindow
	switch state {
	case "active":
		return idle
	case "idle":
		return idle
	case "inactive":
		return !idle
	default:
		return false
	}
}

func (b *Backend) Inbox(ctx context.Context, query httpapi.InboxQuery, meta httpapi.RequestMeta) (httpapi.InboxResult, error) {
	items, err := b.store.Inbox(ctx, query.Project, meta.Session, query.Limit)
	if err != nil {
		return httpapi.InboxResult{}, mapError(err, "inbox")
	}
	out := httpapi.InboxResult{Project: query.Project, Items: make([]httpapi.InboxItem, 0, len(items))}
	for _, item := range items {
		if item.Unread {
			out.Unread++
		}
		switch item.Kind {
		case "mention":
			out.Mentions++
		case "reply":
			out.Replies++
		}
		out.Items = append(out.Items, httpapi.InboxItem{ID: item.ID, Kind: item.Kind, From: item.FromSessionID, Ref: item.Ref, Unread: item.Unread, Snippet: item.Snippet})
	}
	return out, nil
}

func (b *Backend) Search(ctx context.Context, query httpapi.SearchQuery, meta httpapi.RequestMeta) (httpapi.SearchResult, error) {
	items, err := b.store.Search(ctx, query.Project, query.Query, query.Limit)
	if err != nil {
		return httpapi.SearchResult{}, mapError(err, "search")
	}
	out := httpapi.SearchResult{Project: query.Project, Hits: make([]httpapi.SearchHit, 0, len(items))}
	for _, item := range items {
		if domain.PostKinds[item.Kind] {
			allowed, checkErr := b.store.CanDiscoverPost(ctx, meta.PrincipalKind, meta.Session, item.Ref)
			if checkErr != nil {
				return httpapi.SearchResult{}, mapError(checkErr, "search")
			}
			if !allowed {
				continue
			}
		}
		out.Hits = append(out.Hits, httpapi.SearchHit{Ref: item.Ref, Revision: item.Revision, Kind: item.Kind, Title: item.Title, Timestamp: item.CreatedAt.UTC().Format(time.RFC3339Nano), Snippet: item.Snippet})
	}
	return out, nil
}

func (b *Backend) Open(ctx context.Context, query httpapi.OpenQuery, meta httpapi.RequestMeta) (httpapi.OpenResult, error) {
	object, err := b.store.Open(ctx, query.Ref)
	if err != nil {
		return httpapi.OpenResult{}, mapError(err, "object")
	}
	if domain.PostKinds[object.Kind] {
		allowed, checkErr := b.store.CanDiscoverPost(ctx, meta.PrincipalKind, meta.Session, object.Ref)
		if checkErr != nil {
			return httpapi.OpenResult{}, mapError(checkErr, "object")
		}
		if !allowed {
			return httpapi.OpenResult{}, httpapi.NewError(httpapi.CodeNotFound, "object not found")
		}
	}
	value := objectMap(object)
	used := estimatedTokens(value)
	if used > query.Budget {
		return httpapi.OpenResult{}, httpapi.NewError(httpapi.CodeInvalid, "object exceeds requested budget")
	}
	return httpapi.OpenResult{Ref: object.Ref, Kind: object.Kind, Revision: object.Revision, Object: value,
		Budget: httpapi.Budget{Requested: query.Budget, Used: used, Unit: "estimated_tokens"}}, nil
}

func objectMap(object domain.Object) map[string]any {
	return map[string]any{
		"ref": object.Ref, "project": object.ProjectID, "topic": object.TopicID,
		"kind": object.Kind, "title": object.Title, "summary": object.Summary,
		"body": object.Body, "basis": object.Basis, "related_ref": object.RelatedRef,
		"state": object.State, "accept": object.Accept, "session": object.SessionID,
		"created_at": object.CreatedAt,
	}
}

func (b *Backend) Next(ctx context.Context, query httpapi.NextQuery, _ httpapi.RequestMeta) (httpapi.NextResult, error) {
	items, err := b.store.Next(ctx, query.Project, query.Limit)
	if err != nil {
		return httpapi.NextResult{}, mapError(err, "tasks")
	}
	out := httpapi.NextResult{Project: query.Project, Tasks: make([]httpapi.TaskItem, 0, len(items))}
	for _, item := range items {
		out.Tasks = append(out.Tasks, httpapi.TaskItem{ID: item.ID, State: item.State, Priority: item.Priority, Title: item.Title, Accept: item.Accept})
	}
	return out, nil
}

func (b *Backend) Claim(ctx context.Context, request httpapi.ClaimRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if meta.IdempotencyKey == "" {
		return httpapi.WriteResult{}, httpapi.NewError(httpapi.CodeInvalid, "Idempotency-Key required")
	}
	var leaseUntil *time.Time
	if request.Lease != "" {
		duration, err := time.ParseDuration(request.Lease)
		if err != nil || duration <= 0 {
			return httpapi.WriteResult{}, httpapi.NewError(httpapi.CodeInvalid, "lease must be a positive duration")
		}
		value := b.now().UTC().Add(duration)
		leaseUntil = &value
	}
	result, err := b.store.Claim(ctx, domain.ClaimRequest{TaskID: request.Task, ActorID: meta.Actor, SessionID: meta.Session, RequestID: meta.IdempotencyKey, LeaseUntil: leaseUntil})
	if err != nil {
		return httpapi.WriteResult{}, mapError(err, "claim")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}

func (b *Backend) Post(ctx context.Context, request httpapi.PostRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	mentions := make([]string, 0, len(request.Mentions))
	for _, item := range request.Mentions {
		principal := item.Principal
		if principal == "" {
			principal = item.Session
		}
		mentions = append(mentions, principal)
	}
	attachments := make([]domain.PostAttachment, 0, len(request.Attachments))
	for _, item := range request.Attachments {
		attachments = append(attachments, domain.PostAttachment{Kind: item.Kind, URL: item.URL, Title: item.Title})
	}
	result, err := b.store.Post(ctx, domain.PostRequest{TopicID: request.Topic, Kind: request.Kind, Title: request.Title,
		Body: request.Body, Basis: request.Basis, Ref: request.Ref, Attachments: attachments,
		ActorID: meta.Actor, ActorKind: meta.PrincipalKind, ActorPrincipal: meta.Principal, SessionID: meta.Session, RequestID: meta.IdempotencyKey, MentionPrincipals: mentions})
	if err != nil {
		return httpapi.WriteResult{}, mapError(err, "post")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}

func (b *Backend) Comment(ctx context.Context, request httpapi.CommentRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	mentions := make([]string, 0, len(request.Mentions))
	for _, m := range request.Mentions {
		principal := m.Principal
		if principal == "" {
			principal = m.Session
		}
		mentions = append(mentions, principal)
	}
	result, err := b.store.Comment(ctx, domain.CommentRequest{PostID: request.Ref, Body: request.Body, Intent: request.Intent, ActorID: meta.Actor, ActorKind: meta.PrincipalKind, ActorPrincipal: meta.Principal, SessionID: meta.Session, RequestID: meta.IdempotencyKey, MentionPrincipals: mentions})
	if err != nil {
		return httpapi.WriteResult{}, mapError(err, "comment")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}

func (b *Backend) SetStatus(ctx context.Context, request httpapi.StatusRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	object, err := b.store.Open(ctx, request.Ref)
	if err != nil {
		return httpapi.WriteResult{}, mapError(err, "status target")
	}
	if object.ProjectID == "" {
		return httpapi.WriteResult{}, httpapi.NewError(httpapi.CodeInvalid, "status target must be project scoped")
	}
	result, err := b.store.Status(ctx, domain.StatusRequest{ProjectID: object.ProjectID, Ref: request.Ref, State: request.Status, Detail: request.Basis, ActorID: meta.Actor, SessionID: meta.Session, RequestID: meta.IdempotencyKey})
	if err != nil {
		return httpapi.WriteResult{}, mapError(err, "status")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}

func (b *Backend) RequestTopic(ctx context.Context, request httpapi.TopicRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	return b.Post(ctx, httpapi.PostRequest{Topic: domain.TopicGeneral, Kind: "topic_request", Title: request.Title, Body: request.Body, Basis: request.Basis}, meta)
}

func mapError(err error, resource string) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return httpapi.NewError(httpapi.CodeNotFound, resource+" not found")
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrFutureRevision):
		return httpapi.NewError(httpapi.CodeConflict, resource+" conflict")
	case errors.Is(err, domain.ErrInvalid):
		return httpapi.NewError(httpapi.CodeInvalid, "invalid "+resource)
	case errors.Is(err, domain.ErrUnavailable):
		return httpapi.NewError(httpapi.CodeUnavailable, resource+" unavailable")
	case err == nil:
		return nil
	default:
		return fmt.Errorf("%s: %w", strings.ReplaceAll(resource, " ", "_"), err)
	}
}

var _ httpapi.LegacyBackend = (*Backend)(nil)
