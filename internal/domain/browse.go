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

type ProjectBrowseItem struct {
	ID, Name, Status, Purpose string
	CurrentWork               *ProjectCurrentWork
	OpenTasks                 int
	LatestActivity            *time.Time
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
