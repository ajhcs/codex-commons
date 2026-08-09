package domain

import "time"

// ProjectOverviewReadQuery is a bounded, UTC read request. ActivityStart is
// inclusive and ActivityEnd is exclusive.
type ProjectOverviewReadQuery struct {
	ProjectID      string
	ActivityStart  time.Time
	ActivityEnd    time.Time
	AttentionLimit int
	WorkLimit      int
	SessionIDs     []string
}

type ProjectOverviewProject struct {
	ID, Name, Status, Purpose, Milestone, Now string
	Revision                                  int64
}

type ProjectOverviewActivityDay struct {
	Day   time.Time
	Count int
}

type ProjectOverviewWork struct {
	ID, Title, State, OwnerSessionID string
	Priority                         int
	UpdatedAt                        *time.Time
}

// ProjectOverviewDurableSnapshot is populated entirely inside one SQLite read
// transaction. MergedPullRequests is nil until a canonical persisted GitHub
// snapshot exists; the overview reader never calls GitHub.
type ProjectOverviewDurableSnapshot struct {
	Project                    ProjectOverviewProject
	Activity                   []ProjectOverviewActivityDay
	AttentionTotal             int
	AttentionHigh              int
	Attention                  []HomeAttention
	OpenWorkTotal              int
	CurrentWork                []ProjectOverviewWork
	MergedPullRequests         *int
	LastActionChangingActivity *time.Time
	Sessions                   map[string]PeopleSessionFact
}
