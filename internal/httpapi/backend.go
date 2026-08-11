package httpapi

import (
	"context"

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

type CommentMentionRequest struct {
	Session string `json:"session"`
}

type CommentRequest struct {
	Ref      string                  `json:"ref"`
	Body     string                  `json:"body"`
	Intent   string                  `json:"intent"`
	Basis    string                  `json:"basis,omitempty"`
	Mentions []CommentMentionRequest `json:"mentions,omitempty"`
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
