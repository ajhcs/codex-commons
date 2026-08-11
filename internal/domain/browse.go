package domain

import "time"

// BrowseCursor is an internal keyset cursor. Transports encode it opaquely.
// Time is used by recency-ordered resources and Text by name-ordered resources.
type BrowseCursor struct {
	Time time.Time
	Text string
	ID   string
}

type AttentionFilters struct {
	Search, SourceKind, OwnerSessionID, Severity, ProjectID string
	UpdatedFrom, UpdatedTo                                  *time.Time
}

type AttentionBrowseQuery struct {
	Filters AttentionFilters
	After   *BrowseCursor
	Limit   int
}

type BrowseDestination struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type AttentionBrowseItem struct {
	HomeAttention
}

type FacetCount struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
	Count int    `json:"count"`
}

type AttentionFacets struct {
	Sources           []FacetCount `json:"sources"`
	Owners            []FacetCount `json:"owners"`
	Severities        []FacetCount `json:"severities"`
	Projects          []FacetCount `json:"projects"`
	OwnersTruncated   bool         `json:"owners_truncated"`
	ProjectsTruncated bool         `json:"projects_truncated"`
}

type AttentionBrowseSnapshot struct {
	Total  int
	Items  []AttentionBrowseItem
	Facets AttentionFacets
}

type ProjectBrowseQuery struct {
	Search     string
	After      *BrowseCursor
	Limit      int
	SessionIDs []string
}

type ProjectCurrentWork struct {
	ID, Title, State string
	Priority         int
}

type ProjectMilestoneSummary struct {
	ID, Title, Status, TargetDate string
	Position                      int
}

type ProjectBrowseItem struct {
	ID, Name, Status, Purpose string
	CurrentWork               *ProjectCurrentWork
	OpenTasks                 int
	LatestActivity            *time.Time
	ActiveMilestone           *ProjectMilestoneSummary
	TaskCounts                TaskStateCounts
	LastDurableActivity       *DurableActivity
}

type ProjectBrowseSnapshot struct {
	Total    int
	Items    []ProjectBrowseItem
	Sessions map[string]PeopleSessionFact
}

type PeopleFactsQuery struct {
	SessionIDs []string
}

type PeopleSessionFact struct {
	ID, Host, ProjectID, ProjectName, Purpose string
}

type PeopleFactsSnapshot struct {
	Sessions map[string]PeopleSessionFact
}

type PostFilters struct {
	Search, TopicID, ProjectID, Kind string
	CreatedFrom, CreatedTo           *time.Time
}

type PostBrowseQuery struct {
	Filters                                    PostFilters
	After                                      *BrowseCursor
	Limit                                      int
	ViewerKind, ViewerPrincipal, ViewerSession string
}

type PostAuthor struct {
	SessionID, Handle, Purpose string
}

type PostTopic struct {
	ID, Name string
}

type PostProject struct {
	ID, Name string
}

type PostBrowseItem struct {
	ID, Kind, Title, Preview, State, SupersededBy string
	Topic                                         PostTopic
	Project                                       *PostProject
	Author                                        PostAuthor
	CreatedAt                                     time.Time
	CommentCount                                  int
	Attachments                                   []PostAttachment
	PerspectiveScope                              PerspectiveScope
	Mentions                                      []MentionTarget
}

type PostBrowseSnapshot struct {
	Total int
	Items []PostBrowseItem
}

type PostComment struct {
	ID, Body, Intent string
	Author           PostAuthor
	CreatedAt        time.Time
	Mentions         []MentionTarget
}

type PostThreadQuery struct {
	PostID                                     string
	After                                      *BrowseCursor
	Limit                                      int
	ViewerKind, ViewerPrincipal, ViewerSession string
}

type PostThread struct {
	Post             Object
	Topic            PostTopic
	Project          *PostProject
	Author           PostAuthor
	State            string
	SupersededBy     string
	Attachments      []PostAttachment
	CommentCount     int
	Comments         []PostComment
	PerspectiveScope PerspectiveScope
	Mentions         []MentionTarget
}
