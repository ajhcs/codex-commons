// Package storebackend translates the durable SQLite and process-local
// presence contracts into the storage-neutral HTTP API contract.
package storebackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

type Backend struct {
	store    *commonsstore.Store
	presence *presence.Registry
	version  string
	now      func() time.Time
}

const presenceIdleWindow = time.Hour

func New(store *commonsstore.Store, live *presence.Registry, version string) (*Backend, error) {
	if store == nil || live == nil {
		return nil, errors.New("store and presence registry required")
	}
	return &Backend{store: store, presence: live, version: version, now: time.Now}, nil
}

func (b *Backend) Health(ctx context.Context, _ httpapi.RequestMeta) (httpapi.HealthResult, error) {
	if err := b.store.DB().PingContext(ctx); err != nil {
		return httpapi.HealthResult{}, httpapi.NewError(httpapi.CodeUnavailable, "database unavailable")
	}
	return httpapi.HealthResult{Status: "ok", Version: b.version}, nil
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

func (b *Backend) Search(ctx context.Context, query httpapi.SearchQuery, _ httpapi.RequestMeta) (httpapi.SearchResult, error) {
	items, err := b.store.Search(ctx, query.Project, query.Query, query.Limit)
	if err != nil {
		return httpapi.SearchResult{}, mapError(err, "search")
	}
	out := httpapi.SearchResult{Project: query.Project, Hits: make([]httpapi.SearchHit, 0, len(items))}
	for _, item := range items {
		out.Hits = append(out.Hits, httpapi.SearchHit{Ref: item.Ref, Revision: item.Revision, Kind: item.Kind, Title: item.Title, Timestamp: item.CreatedAt.UTC().Format(time.RFC3339Nano), Snippet: item.Snippet})
	}
	return out, nil
}

func (b *Backend) Open(ctx context.Context, query httpapi.OpenQuery, _ httpapi.RequestMeta) (httpapi.OpenResult, error) {
	object, err := b.store.Open(ctx, query.Ref)
	if err != nil {
		return httpapi.OpenResult{}, mapError(err, "object")
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
	attachments := make([]domain.PostAttachment, 0, len(request.Attachments))
	for _, item := range request.Attachments {
		attachments = append(attachments, domain.PostAttachment{Kind: item.Kind, URL: item.URL, Title: item.Title})
	}
	result, err := b.store.Post(ctx, domain.PostRequest{TopicID: request.Topic, Kind: request.Kind, Title: request.Title,
		Body: request.Body, Basis: request.Basis, Ref: request.Ref, Attachments: attachments,
		ActorID: meta.Actor, SessionID: meta.Session, RequestID: meta.IdempotencyKey})
	if err != nil {
		return httpapi.WriteResult{}, mapError(err, "post")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}

func (b *Backend) Comment(ctx context.Context, request httpapi.CommentRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	result, err := b.store.Comment(ctx, domain.CommentRequest{PostID: request.Ref, Body: request.Body, Intent: request.Intent, ActorID: meta.Actor, SessionID: meta.Session, RequestID: meta.IdempotencyKey})
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
