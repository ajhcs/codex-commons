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
}

type Post struct {
	ID, TopicID, ProjectID, Kind, Title, Body, Basis, Ref, SessionID, RequestID string
	Revision                                                                    int64
	CreatedAt                                                                   time.Time
}

type CommentRequest struct {
	PostID, Body, ActorID, SessionID, RequestID string
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
