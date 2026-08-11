package domain

import "time"

const (
	TopicGeneral = "general"
)

type Project struct {
	ID, Name, Status, Purpose, Milestone, Now string
	Revision                                  int64
}

type Topic struct {
	ID, ProjectID, Name string
	CreatedAt           time.Time
}

type WikiPage struct {
	ID, ProjectID, Slug, Title string
	Revision                   int64
	Summary, Body              string
}

type Task struct {
	ID, ProjectID, State, Title, OwnerSessionID, Accept string
	Priority                                            int
	Dependencies                                        []string
}

type Decision struct {
	ID, ProjectID, Title, Rationale string
	Revision                        int64
}

type Session struct {
	ID, Host, HostState, Turn, ProjectID, Purpose string
	LastActivity                                  time.Time
}

type InboxItem struct {
	ID, Kind, FromSessionID, Ref, Snippet string
	Unread                                bool
	CreatedAt                             time.Time
}

type Change struct {
	Revision  int64
	Kind, Ref string
	Summary   string
	CreatedAt time.Time
}

type ContextPacket struct {
	Project    Project
	Changes    []Change
	Tasks      []Task
	Decisions  []Decision
	Wiki       []WikiPage
	Sessions   []Session
	Unread     int
	Unchanged  bool
	FromCursor int64
}

type SearchHit struct {
	Ref, ProjectID, Kind, Title, Snippet string
	Revision                             int64
	CreatedAt                            time.Time
}

type Object struct {
	Ref, ProjectID, TopicID, Kind, Title, Summary, Body, Basis, RelatedRef, State, Accept, SessionID string
	Revision                                                                                         int64
	CreatedAt                                                                                        time.Time
}

type ClaimRequest struct {
	TaskID, ActorID, SessionID, RequestID string
	LeaseUntil                            *time.Time
}

type Claim struct {
	ID, TaskID, SessionID, RequestID string
	Revision                         int64
	ClaimedAt                        time.Time
	LeaseUntil                       *time.Time
}

type PostRequest struct {
	TopicID, Kind, Title, Body, Basis, Ref, ActorID, SessionID, RequestID string
	Attachments                                                           []PostAttachment
}

type Post struct {
	ID, TopicID, ProjectID, Kind, Title, Body, Basis, Ref, SessionID, RequestID string
	Revision                                                                    int64
	CreatedAt                                                                   time.Time
	Attachments                                                                 []PostAttachment
}

type CommentRequest struct {
	PostID, Body, Intent, ActorID, SessionID, RequestID string
}

type PostAttachment struct {
	Kind, URL, Title string
}

type PostStateRequest struct {
	PostID, State, SupersededBy, ActorID, SessionID, RequestID string
}

type StatusRequest struct {
	ProjectID, Ref, State, Detail, ActorID, SessionID, RequestID string
}

type WriteResult struct {
	ID       string
	Revision int64
}

type ChangesResult struct {
	ProjectID     string
	From, Current int64
	Changes       []Change
	Unchanged     bool
}

var PostKinds = map[string]bool{
	"finding": true, "question": true, "notice": true,
	"decision": true, "topic_request": true,
}

var CommentIntents = map[string]bool{
	"answer": true, "add_evidence": true, "challenge": true, "clarify": true,
}

const (
	AttentionOpen     = "open"
	AttentionResolved = "resolved"
)

var AttentionSeverities = map[string]bool{"high": true, "medium": true, "low": true}

var AttentionSourceKinds = map[string]bool{
	"task": true, "github_issue": true, "github_pull_request": true,
	"github_check": true, "host_connectivity": true, "forum_question": true,
}

// AttentionEvent is an explicit producer assertion. Severity and NextAction
// are never inferred from task priority, forum prose, or presence.
type AttentionEvent struct {
	EventID, AttentionID, State, Severity, Title, ProjectID, SourceRef string
	AccountableSessionID, NextAction, SourceKind                       string
	Untrusted                                                          bool
}

var ActivityKinds = map[string]bool{
	"project_updated": true, "task_claimed": true, "task_status_changed": true,
	"decision_recorded": true, "wiki_revised": true, "post_published": true,
	"comment_added": true, "github_issue_changed": true,
	"github_pull_request_changed": true, "github_check_changed": true,
	"github_commit_referenced": true, "host_connected": true,
	"host_disconnected": true, "wiki_proposal_created": true,
	"wiki_proposal_reviewed": true,
}

// ActivityEvent is deliberately restricted to action-changing kinds. Routine
// heartbeats and ordinary chatter have no valid kind and cannot enter the feed.
type ActivityEvent struct {
	ID, Kind, ProjectID, ActorID, ObjectRef, ObjectTitle, Outcome string
	Untrusted                                                     bool
	OccurredAt                                                    time.Time
}

type HomePageRequest struct{ Offset, Limit int }

type HomeReadQuery struct {
	Attention, Activity HomePageRequest
	SessionIDs          []string
}

type HomeAttention struct {
	ID, Severity, Title, ProjectID, ProjectName, SourceRef string
	AccountableSessionID, NextAction, SourceKind           string
	Untrusted                                              bool
	UpdatedAt                                              time.Time
	Destination                                            *BrowseDestination
}

type HomeActivity struct {
	ID, Kind, ProjectID, ProjectName, ActorID string
	ObjectRef, ObjectTitle, Outcome           string
	Untrusted                                 bool
	OccurredAt                                time.Time
}

type SessionFact struct {
	ID, Host, ProjectID, ProjectName, Purpose string
}

// HomeDurableSnapshot is read in one SQLite snapshot. Live process presence
// is captured separately by the application service, then joined by session ID.
type HomeDurableSnapshot struct {
	ProjectsTotal, AttentionTotal, ActivityTotal int
	Attention                                    []HomeAttention
	Activity                                     []HomeActivity
	Sessions                                     map[string]SessionFact
}
