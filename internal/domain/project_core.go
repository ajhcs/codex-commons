package domain

import "time"

// Project Core deliberately stops at durable continuity primitives. These
// records do not model teams, sprints, estimates, scheduling, or assignments.

var ProjectWriteStatuses = map[string]bool{
	"active": true, "paused": true, "completed": true, "archived": true,
}

var MilestoneStatuses = map[string]bool{
	"planned": true, "active": true, "completed": true, "cancelled": true,
}

var TaskStates = map[string]bool{
	"ready": true, "in_progress": true, "blocked": true,
	"done": true, "cancelled": true,
}

type CanonicalProject struct {
	ID, Name, Status, Purpose, Now string
	Revision                       int64
	CreatedAt, UpdatedAt           time.Time
}

type ProjectCounts struct {
	Tasks, Milestones, WikiPages int
}

type DurableActivity struct {
	Kind, Ref, Title string
	OccurredAt       time.Time
}

type Milestone struct {
	ID, ProjectID, Title, Status, TargetDate string
	Position                                 int
	Revision                                 int64
	CreatedAt, UpdatedAt                     time.Time
}

type ProjectCoreReadQuery struct {
	ProjectID                  string
	ActivityStart, ActivityEnd time.Time
}

type ProjectCoreSnapshot struct {
	Project         CanonicalProject
	Counts          ProjectCounts
	ActiveMilestone *Milestone
	Activity        []ProjectOverviewActivityDay
}

type TaskDependency struct {
	ID, Title, State string
}

type TaskMilestoneSummary struct {
	ID, Title, Status string
}

type CanonicalTask struct {
	ID, ProjectID, Title, Description, Acceptance, State string
	Priority                                             int
	MilestoneID, OwnerSessionID, OwnerPurpose            string
	Milestone                                            *TaskMilestoneSummary
	Dependencies                                         []TaskDependency
	DependenciesTruncated                                bool
	HistoricalImport                                     *HistoricalTaskImport
	Contributors                                         []Provenance
	ContributorsTruncated                                bool
	Revision                                             int64
	CreatedAt, UpdatedAt                                 time.Time
}

type TaskStateCounts struct {
	Ready, InProgress, Blocked, Done, Cancelled, Total int
}

type TaskEvent struct {
	ID, TaskID, ProjectID, Kind, Summary            string
	FromState, ToState, ActorID, SessionID, Purpose string
	Revision                                        int64
	CreatedAt                                       time.Time
	Provenance                                      Provenance
}

type WikiPageSummary struct {
	ID, ProjectID, Slug, Title, Summary string
	CurrentRevision                     int64
	UpdatedAt                           time.Time
}

type WikiRevision struct {
	PageID, ProjectID, Slug, Title, Summary, Body, AuthorSessionID, AuthorPurpose string
	Revision                                                                      int64
	CreatedAt                                                                     time.Time
}

type WikiRevisionSummary struct {
	Revision                                int64
	Summary, AuthorSessionID, AuthorPurpose string
	CreatedAt                               time.Time
}

type MilestoneListQuery struct {
	ProjectID string
	Limit     int
}

type MilestoneListSnapshot struct {
	Total int
	Items []Milestone
}

type TaskListQuery struct {
	ProjectID, State, MilestoneID string
	After                         *BrowseCursor
	Limit                         int
}

type TaskListSnapshot struct {
	Total       int
	StateCounts TaskStateCounts
	Items       []CanonicalTask
}

type TaskOpenQuery struct {
	TaskID      string
	EventsLimit int
}

type TaskOpenSnapshot struct {
	Task   CanonicalTask
	Events []TaskEvent
}

type TaskEventListQuery struct {
	TaskID string
	After  *BrowseCursor
	Limit  int
}

type TaskEventListSnapshot struct {
	ProjectID string
	Items     []TaskEvent
}

type WikiListQuery struct {
	ProjectID, Search string
	After             *BrowseCursor
	Limit             int
}

type WikiListSnapshot struct {
	Total int
	Items []WikiPageSummary
}

type WikiHistoryQuery struct {
	ProjectID, Slug string
	BeforeRevision  int64
	Limit           int
}

type WikiHistorySnapshot struct {
	CurrentRevision int64
	Items           []WikiRevisionSummary
}

type CoreWriteMeta struct {
	ActorID, SessionID, RequestID string
}

type CreateProjectCommand struct {
	ID, Name, Status, Purpose, Now string
	Meta                           CoreWriteMeta
}

type UpdateProjectCommand struct {
	ID, Name, Status, Purpose, Now string
	BaseRevision                   int64
	Meta                           CoreWriteMeta
}

type CreateMilestoneCommand struct {
	ProjectID, Title, Status, TargetDate string
	Position                             int
	Meta                                 CoreWriteMeta
}

type UpdateMilestoneCommand struct {
	ID, Title, Status, TargetDate string
	Position                      int
	BaseRevision                  int64
	Meta                          CoreWriteMeta
}

type CreateTaskCommand struct {
	ProjectID, Title, Description, Acceptance, State, MilestoneID string
	Priority                                                      int
	DependencyIDs                                                 []string
	Meta                                                          CoreWriteMeta
}

type UpdateTaskCommand struct {
	ID, Title, Description, Acceptance, MilestoneID string
	Priority                                        int
	DependencyIDs                                   []string
	BaseRevision                                    int64
	Meta                                            CoreWriteMeta
}

type ChangeTaskStateCommand struct {
	ID, State, Basis string
	BaseRevision     int64
	Meta             CoreWriteMeta
}

type AppendWikiRevisionCommand struct {
	ProjectID, Slug, Title, Summary, Body string
	BaseRevision                          int64
	Meta                                  CoreWriteMeta
}
