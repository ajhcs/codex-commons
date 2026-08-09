package commons

type Project struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Revision   int        `json:"revision"`
	Status     string     `json:"status"`
	Purpose    string     `json:"purpose"`
	Milestone  string     `json:"milestone"`
	Now        string     `json:"now"`
	Principles []string   `json:"principles"`
	Decisions  []Decision `json:"decisions"`
	Wiki       []WikiPage `json:"wiki"`
	Tasks      []Task     `json:"tasks"`
	Changes    []Change   `json:"changes"`
}

type Decision struct {
	ID        string `json:"id"`
	Revision  int    `json:"revision"`
	Title     string `json:"title"`
	Rationale string `json:"rationale"`
}

type WikiPage struct {
	Slug     string `json:"slug"`
	Revision int    `json:"revision"`
	Summary  string `json:"summary"`
}

type Task struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Title     string `json:"title"`
	Owner     string `json:"owner,omitempty"`
	DependsOn string `json:"depends_on,omitempty"`
	Priority  int    `json:"priority"`
	Accept    string `json:"accept,omitempty"`
}

type Message struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	From    string `json:"from"`
	Ref     string `json:"ref"`
	Unread  bool   `json:"unread"`
	Age     string `json:"age"`
	Snippet string `json:"snippet"`
}

type Change struct {
	Revision int    `json:"revision"`
	Kind     string `json:"kind"`
	Summary  string `json:"summary"`
}

type Session struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	HostState string `json:"host_state"`
	Turn      string `json:"turn"`
	Last      string `json:"last_activity"`
	Project   string `json:"project"`
	Purpose   string `json:"purpose"`
}

type SearchItem struct {
	Ref      string `json:"ref"`
	Project  string `json:"project"`
	Revision int    `json:"revision"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

var DemoProject = Project{
	ID:        "commons-lab",
	Name:      "Commons Lab",
	Revision:  42,
	Status:    "slice-0",
	Purpose:   "Build a durable, token-efficient coordination commons for Codex tasks and humans.",
	Milestone: "Slice 0: agent contract and blind usability benchmark.",
	Now:       "Test whether a fresh agent can orient, retrieve evidence, and claim work through a tiny CLI contract.",
	Principles: []string{
		"agent-first and token-bounded",
		"public append-only history with human redaction",
		"event-triggered use; no ambient LLM polling",
	},
	Decisions: []Decision{
		{ID: "D-7", Revision: 36, Title: "Use a Go monolith with bundled SQLite", Rationale: "Bundle a current SQLite release to keep context budgets and storage portable. PostgreSQL remains the measured escape hatch."},
	},
	Wiki: []WikiPage{
		{Slug: "W-home", Revision: 41, Summary: "Commons Lab overview: scope, durable decisions, current milestone, and next work."},
		{Slug: "W-policy", Revision: 38, Summary: "Use the commons only when shared prior work, reusable evidence, or coordination value is plausible."},
	},
	Tasks: []Task{
		{ID: "T-101", State: "in_progress", Priority: 1, Title: "Implement fixture CLI", Owner: "S-PLUM-7"},
		{ID: "T-102", State: "ready", Priority: 1, Title: "Run blind orientation benchmark", Accept: "4 blind runs >=95%; workflow <=800 estimated tokens; cold p95 <=50 ms."},
		{ID: "T-103", State: "blocked", Priority: 2, Title: "Compare bundled SQLite with PostgreSQL", DependsOn: "T-101"},
	},
	Changes: []Change{
		{Revision: 40, Kind: "decision", Summary: "Slice 0 uses fixed responses and no storage."},
		{Revision: 41, Kind: "wiki", Summary: "Project home includes the rehydration contract."},
		{Revision: 42, Kind: "task", Summary: "Blind orientation benchmark is ready."},
	},
}

var Sessions = []Session{
	{ID: "S-PLUM-7", Host: "plumbob", HostState: "connected", Turn: "live", Last: "2026-08-09T15:04:05Z", Project: "commons-lab", Purpose: "Implement fixture CLI."},
	{ID: "S-DESK-2", Host: "studio", HostState: "connected", Turn: "idle", Last: "2026-08-09T14:46:00Z", Project: "commons-lab", Purpose: "Benchmark context packets."},
	{ID: "cx-ajhcs-04", Host: "workstation", HostState: "disconnected", Turn: "not-running", Last: "2026-08-08T21:10:00Z", Project: "ajhcs", Purpose: "Review an unrelated production change."},
}

var Messages = []Message{
	{ID: "M-3", Kind: "reply", From: "S-DESK-2", Ref: "P-21", Unread: true, Age: "7m", Snippet: "Can the delta cursor replace activity summaries?"},
}

func SearchCorpus() []SearchItem {
	p := DemoProject
	items := make([]SearchItem, 0, len(p.Decisions)+len(p.Wiki)+len(p.Tasks)+1)
	for _, d := range p.Decisions {
		items = append(items, SearchItem{Ref: "decision/" + d.ID, Project: p.ID, Revision: d.Revision, Kind: "decision", Title: d.Title, Body: d.Rationale})
	}
	for _, w := range p.Wiki {
		items = append(items, SearchItem{Ref: w.Slug, Project: p.ID, Revision: w.Revision, Kind: "wiki", Title: w.Slug, Body: w.Summary})
	}
	for _, task := range p.Tasks {
		items = append(items, SearchItem{Ref: "task/" + task.ID, Project: p.ID, Revision: p.Revision, Kind: "task", Title: task.Title, Body: task.State + " " + task.Owner + " " + task.DependsOn})
	}
	items = append(items, SearchItem{Ref: "P-21", Project: p.ID, Revision: 39, Kind: "finding", Title: "Full activity history bloats context packets", Body: "Use revision deltas and explicit open. Context budget tests showed that bounded packets preserve orientation."})
	return items
}
