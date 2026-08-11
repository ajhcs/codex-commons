package application

import (
	"context"
	"strconv"
	"time"

	"codex-commons/internal/domain"
)

type ProjectCoreRepository interface {
	ProjectCoreSnapshot(context.Context, domain.ProjectCoreReadQuery) (domain.ProjectCoreSnapshot, error)
	MilestoneListSnapshot(context.Context, domain.MilestoneListQuery) (domain.MilestoneListSnapshot, error)
	TaskListSnapshot(context.Context, domain.TaskListQuery) (domain.TaskListSnapshot, error)
	TaskOpenSnapshot(context.Context, domain.TaskOpenQuery) (domain.TaskOpenSnapshot, error)
	TaskEventListSnapshot(context.Context, domain.TaskEventListQuery) (domain.TaskEventListSnapshot, error)
	WikiListSnapshot(context.Context, domain.WikiListQuery) (domain.WikiListSnapshot, error)
	OpenWikiRevision(context.Context, string, string, int64) (domain.WikiRevision, error)
	WikiHistorySnapshot(context.Context, domain.WikiHistoryQuery) (domain.WikiHistorySnapshot, error)
	CreateCanonicalProject(context.Context, domain.CreateProjectCommand) (domain.WriteResult, error)
	UpdateCanonicalProject(context.Context, domain.UpdateProjectCommand) (domain.WriteResult, error)
	CreateMilestone(context.Context, domain.CreateMilestoneCommand) (domain.WriteResult, error)
	UpdateMilestone(context.Context, domain.UpdateMilestoneCommand) (domain.WriteResult, error)
	CreateCanonicalTask(context.Context, domain.CreateTaskCommand) (domain.WriteResult, error)
	UpdateCanonicalTask(context.Context, domain.UpdateTaskCommand) (domain.WriteResult, error)
	ChangeCanonicalTaskState(context.Context, domain.ChangeTaskStateCommand) (domain.WriteResult, error)
	AppendWikiRevision(context.Context, domain.AppendWikiRevisionCommand) (domain.WriteResult, error)
}

const (
	defaultCoreEventLimit = 20
	maxCoreEventLimit     = 50
)

func coreTaskListLimit(value int) (int, bool) {
	if value == 0 {
		return 25, true
	}
	return value, value >= 1 && value <= 25
}

func coreEventLimit(value int) (int, bool) {
	if value == 0 {
		return defaultCoreEventLimit, true
	}
	return value, value >= 1 && value <= maxCoreEventLimit
}

func coreCursorTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return value
}

func projectCoreRepository(service *Service) (ProjectCoreRepository, error) {
	if service == nil {
		return nil, domain.ErrUnavailable
	}
	repository, ok := service.repository.(ProjectCoreRepository)
	if !ok {
		return nil, domain.ErrUnavailable
	}
	return repository, nil
}

type ProjectCoreActor struct {
	Actor, Session, RequestID string
}

func coreMeta(actor ProjectCoreActor) domain.CoreWriteMeta {
	return domain.CoreWriteMeta{ActorID: actor.Actor, SessionID: actor.Session, RequestID: actor.RequestID}
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() || value.Equal(time.Unix(0, 0).UTC()) {
		return nil
	}
	copy := value.UTC()
	return &copy
}

type ProjectCoreProject struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Purpose   string     `json:"purpose"`
	Now       string     `json:"now,omitempty"`
	Revision  int64      `json:"revision"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type ProjectCoreCounts struct {
	Tasks      int `json:"tasks"`
	Milestones int `json:"milestones"`
	WikiPages  int `json:"wiki_pages"`
}

type MilestoneItem struct {
	ID         string     `json:"id"`
	Project    string     `json:"project"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	Position   int        `json:"position"`
	TargetDate string     `json:"target_date,omitempty"`
	Revision   int64      `json:"revision"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

func milestoneView(item domain.Milestone) MilestoneItem {
	return MilestoneItem{ID: item.ID, Project: item.ProjectID, Title: item.Title, Status: item.Status,
		Position: item.Position, TargetDate: item.TargetDate, Revision: item.Revision,
		CreatedAt: optionalTime(item.CreatedAt), UpdatedAt: optionalTime(item.UpdatedAt)}
}

type CoreActivityWindow struct {
	Timezone     string        `json:"timezone"`
	Start        string        `json:"start"`
	EndExclusive string        `json:"end_exclusive"`
	Days         []ActivityDay `json:"days"`
}

type ProjectCoreDetailResult struct {
	Project         ProjectCoreProject `json:"project"`
	Counts          ProjectCoreCounts  `json:"counts"`
	ActiveMilestone *MilestoneItem     `json:"active_milestone,omitempty"`
	Activity        CoreActivityWindow `json:"activity"`
	SnapshotAt      time.Time          `json:"snapshot_at"`
}

func (s *Service) ProjectCoreDetail(ctx context.Context, projectID string) (ProjectCoreDetailResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return ProjectCoreDetailResult{}, err
	}
	if projectID == "" {
		return ProjectCoreDetailResult{}, domain.ErrInvalid
	}
	now := s.clock.Now().UTC()
	start := utcDay(now).AddDate(0, 0, -(projectOverviewDays - 1))
	end := utcDay(now).AddDate(0, 0, 1)
	snapshot, err := repository.ProjectCoreSnapshot(ctx, domain.ProjectCoreReadQuery{ProjectID: projectID, ActivityStart: start, ActivityEnd: end})
	if err != nil {
		return ProjectCoreDetailResult{}, err
	}
	dayCounts := make(map[string]int, len(snapshot.Activity))
	for _, day := range snapshot.Activity {
		dayCounts[day.Day.UTC().Format(time.DateOnly)] = day.Count
	}
	days := make([]ActivityDay, projectOverviewDays)
	for i := range days {
		day := start.AddDate(0, 0, i).Format(time.DateOnly)
		days[i] = ActivityDay{Day: day, Count: dayCounts[day]}
	}
	out := ProjectCoreDetailResult{
		Project: ProjectCoreProject{ID: snapshot.Project.ID, Name: snapshot.Project.Name, Status: snapshot.Project.Status,
			Purpose: snapshot.Project.Purpose, Now: snapshot.Project.Now, Revision: snapshot.Project.Revision,
			CreatedAt: optionalTime(snapshot.Project.CreatedAt), UpdatedAt: optionalTime(snapshot.Project.UpdatedAt)},
		Counts:   ProjectCoreCounts{Tasks: snapshot.Counts.Tasks, Milestones: snapshot.Counts.Milestones, WikiPages: snapshot.Counts.WikiPages},
		Activity: CoreActivityWindow{Timezone: "UTC", Start: start.Format(time.DateOnly), EndExclusive: end.Format(time.DateOnly), Days: days}, SnapshotAt: now,
	}
	if snapshot.ActiveMilestone != nil {
		value := milestoneView(*snapshot.ActiveMilestone)
		out.ActiveMilestone = &value
	}
	return out, nil
}

type MilestoneListRequest struct {
	Project string
	Limit   int
}

type MilestoneListResult struct {
	Total     int             `json:"total"`
	Limit     int             `json:"limit"`
	Truncated bool            `json:"truncated"`
	Items     []MilestoneItem `json:"items"`
}

func (s *Service) ListMilestones(ctx context.Context, request MilestoneListRequest) (MilestoneListResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return MilestoneListResult{}, err
	}
	limit, valid := browseLimit(request.Limit)
	if !valid || request.Project == "" {
		return MilestoneListResult{}, domain.ErrInvalid
	}
	snapshot, err := repository.MilestoneListSnapshot(ctx, domain.MilestoneListQuery{ProjectID: request.Project, Limit: limit})
	if err != nil {
		return MilestoneListResult{}, err
	}
	truncated := len(snapshot.Items) > limit
	if truncated {
		snapshot.Items = snapshot.Items[:limit]
	}
	out := MilestoneListResult{Total: snapshot.Total, Limit: limit, Truncated: truncated, Items: make([]MilestoneItem, 0, len(snapshot.Items))}
	for _, item := range snapshot.Items {
		out.Items = append(out.Items, milestoneView(item))
	}
	return out, nil
}

type TaskDependencyItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

type TaskMilestoneItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type HistoricalTaskImportItem struct {
	BatchID           string            `json:"batch_id"`
	SourceKey         string            `json:"source_key"`
	State             string            `json:"state"`
	SourceCompletedAt *time.Time        `json:"source_completed_at,omitempty"`
	RecordedAt        *time.Time        `json:"recorded_at,omitempty"`
	Source            *ProvenanceSource `json:"source,omitempty"`
}

type CanonicalTaskItem struct {
	ID                    string                    `json:"id"`
	Project               string                    `json:"project"`
	Title                 string                    `json:"title"`
	Description           string                    `json:"description,omitempty"`
	Acceptance            string                    `json:"acceptance,omitempty"`
	State                 string                    `json:"state"`
	Priority              int                       `json:"priority"`
	MilestoneID           string                    `json:"milestone_id,omitempty"`
	Milestone             *TaskMilestoneItem        `json:"milestone,omitempty"`
	OwnerSession          string                    `json:"owner_session,omitempty"`
	OwnerProvenance       *Provenance               `json:"owner_provenance,omitempty"`
	HistoricalImport      *HistoricalTaskImportItem `json:"historical_import,omitempty"`
	Contributors          []*Provenance             `json:"contributors,omitempty"`
	ContributorsTruncated bool                      `json:"contributors_truncated,omitempty"`
	Dependencies          []TaskDependencyItem      `json:"dependencies"`
	DependenciesTruncated bool                      `json:"dependencies_truncated"`
	Revision              int64                     `json:"revision"`
	CreatedAt             *time.Time                `json:"created_at,omitempty"`
	UpdatedAt             *time.Time                `json:"updated_at,omitempty"`
}

func taskView(task domain.CanonicalTask) CanonicalTaskItem {
	out := CanonicalTaskItem{ID: task.ID, Project: task.ProjectID, Title: task.Title, Description: task.Description,
		Acceptance: task.Acceptance, State: task.State, Priority: task.Priority, MilestoneID: task.MilestoneID,
		OwnerSession: task.OwnerSessionID, OwnerProvenance: attestedProvenance("", task.OwnerSessionID, task.OwnerPurpose),
		Dependencies:          make([]TaskDependencyItem, 0, len(task.Dependencies)),
		DependenciesTruncated: task.DependenciesTruncated, Revision: task.Revision,
		CreatedAt: optionalTime(task.CreatedAt), UpdatedAt: optionalTime(task.UpdatedAt)}
	if task.Milestone != nil {
		out.Milestone = &TaskMilestoneItem{ID: task.Milestone.ID, Title: task.Milestone.Title, Status: task.Milestone.Status}
	}
	if task.HistoricalImport != nil {
		out.HistoricalImport = &HistoricalTaskImportItem{
			BatchID: task.HistoricalImport.BatchID, SourceKey: task.HistoricalImport.SourceKey,
			State: task.HistoricalImport.State, SourceCompletedAt: optionalTime(task.HistoricalImport.SourceCompletedAt),
			RecordedAt: optionalTime(task.HistoricalImport.RecordedAt),
		}
		if source := task.HistoricalImport.Source; source != nil {
			out.HistoricalImport.Source = &ProvenanceSource{
				Kind: source.Kind, StableID: source.StableID, Digest: source.Digest,
				OccurredAt: optionalTime(source.OccurredAt),
			}
		}
	}
	out.ContributorsTruncated = task.ContributorsTruncated
	if task.Contributors != nil {
		out.Contributors = make([]*Provenance, 0, len(task.Contributors))
		for _, contributor := range task.Contributors {
			if view := provenanceView(contributor); view != nil {
				out.Contributors = append(out.Contributors, view)
			}
		}
	}
	for _, dependency := range task.Dependencies {
		out.Dependencies = append(out.Dependencies, TaskDependencyItem{ID: dependency.ID, Title: dependency.Title, State: dependency.State})
	}
	return out
}

type TaskStateCounts struct {
	Ready      int `json:"ready"`
	InProgress int `json:"in_progress"`
	Blocked    int `json:"blocked"`
	Done       int `json:"done"`
	Cancelled  int `json:"cancelled"`
	Total      int `json:"total"`
}

func taskCountsView(value domain.TaskStateCounts) TaskStateCounts {
	return TaskStateCounts{Ready: value.Ready, InProgress: value.InProgress, Blocked: value.Blocked,
		Done: value.Done, Cancelled: value.Cancelled, Total: value.Total}
}

type TaskListRequest struct {
	Project, Cursor, State, Milestone string
	Limit                             int
}

type TaskListResult struct {
	Total       int                 `json:"total"`
	Limit       int                 `json:"limit"`
	NextCursor  string              `json:"next_cursor,omitempty"`
	StateCounts TaskStateCounts     `json:"state_counts"`
	Items       []CanonicalTaskItem `json:"items"`
}

func (s *Service) ListCanonicalTasks(ctx context.Context, request TaskListRequest) (TaskListResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return TaskListResult{}, err
	}
	limit, valid := coreTaskListLimit(request.Limit)
	if !valid || request.Project == "" {
		return TaskListResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("project-tasks", request.Cursor)
	if err != nil || after != nil && after.Time.IsZero() {
		return TaskListResult{}, domain.ErrInvalid
	}
	snapshot, err := repository.TaskListSnapshot(ctx, domain.TaskListQuery{ProjectID: request.Project, State: request.State,
		MilestoneID: request.Milestone, After: after, Limit: limit})
	if err != nil {
		return TaskListResult{}, err
	}
	hasMore := len(snapshot.Items) > limit
	if hasMore {
		snapshot.Items = snapshot.Items[:limit]
	}
	out := TaskListResult{Total: snapshot.Total, Limit: limit, StateCounts: taskCountsView(snapshot.StateCounts), Items: make([]CanonicalTaskItem, 0, len(snapshot.Items))}
	for _, item := range snapshot.Items {
		out.Items = append(out.Items, taskView(item))
	}
	if hasMore && len(snapshot.Items) > 0 {
		last := snapshot.Items[len(snapshot.Items)-1]
		out.NextCursor = encodeCursor("project-tasks", domain.BrowseCursor{Time: coreCursorTime(last.UpdatedAt), ID: last.ID})
	}
	return out, nil
}

type TaskEventItem struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Summary    string      `json:"summary"`
	FromState  string      `json:"from_state,omitempty"`
	ToState    string      `json:"to_state,omitempty"`
	Actor      string      `json:"actor,omitempty"`
	Session    string      `json:"session,omitempty"`
	Revision   int64       `json:"revision"`
	CreatedAt  *time.Time  `json:"created_at,omitempty"`
	Provenance *Provenance `json:"provenance,omitempty"`
}

func taskEventView(event domain.TaskEvent) TaskEventItem {
	return TaskEventItem{ID: event.ID, Kind: event.Kind, Summary: event.Summary, FromState: event.FromState,
		ToState: event.ToState, Actor: event.ActorID, Session: event.SessionID, Revision: event.Revision,
		CreatedAt: optionalTime(event.CreatedAt), Provenance: provenanceView(event.Provenance)}
}

type TaskOpenRequest struct {
	Task        string
	EventsLimit int
}

type TaskOpenResult struct {
	Task             CanonicalTaskItem `json:"task"`
	Events           []TaskEventItem   `json:"events"`
	EventsNextCursor string            `json:"events_next_cursor,omitempty"`
}

func (s *Service) OpenCanonicalTask(ctx context.Context, request TaskOpenRequest) (TaskOpenResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return TaskOpenResult{}, err
	}
	limit, valid := coreEventLimit(request.EventsLimit)
	if !valid || request.Task == "" {
		return TaskOpenResult{}, domain.ErrInvalid
	}
	snapshot, err := repository.TaskOpenSnapshot(ctx, domain.TaskOpenQuery{TaskID: request.Task, EventsLimit: limit})
	if err != nil {
		return TaskOpenResult{}, err
	}
	hasMore := len(snapshot.Events) > limit
	if hasMore {
		snapshot.Events = snapshot.Events[:limit]
	}
	out := TaskOpenResult{Task: taskView(snapshot.Task), Events: make([]TaskEventItem, 0, len(snapshot.Events))}
	for _, event := range snapshot.Events {
		out.Events = append(out.Events, taskEventView(event))
	}
	if hasMore && len(snapshot.Events) > 0 {
		last := snapshot.Events[len(snapshot.Events)-1]
		out.EventsNextCursor = encodeCursor("task-events", domain.BrowseCursor{Time: coreCursorTime(last.CreatedAt), ID: last.ID})
	}
	return out, nil
}

type TaskEventListRequest struct {
	Task, Cursor string
	Limit        int
}

type TaskEventListResult struct {
	Limit      int             `json:"limit"`
	NextCursor string          `json:"next_cursor,omitempty"`
	Items      []TaskEventItem `json:"items"`
}

func (s *Service) ListTaskEvents(ctx context.Context, request TaskEventListRequest) (TaskEventListResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return TaskEventListResult{}, err
	}
	limit, valid := coreEventLimit(request.Limit)
	if !valid || request.Task == "" {
		return TaskEventListResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("task-events", request.Cursor)
	if err != nil || after != nil && after.Time.IsZero() {
		return TaskEventListResult{}, domain.ErrInvalid
	}
	snapshot, err := repository.TaskEventListSnapshot(ctx, domain.TaskEventListQuery{TaskID: request.Task, After: after, Limit: limit})
	if err != nil {
		return TaskEventListResult{}, err
	}
	hasMore := len(snapshot.Items) > limit
	if hasMore {
		snapshot.Items = snapshot.Items[:limit]
	}
	out := TaskEventListResult{Limit: limit, Items: make([]TaskEventItem, 0, len(snapshot.Items))}
	for _, event := range snapshot.Items {
		out.Items = append(out.Items, taskEventView(event))
	}
	if hasMore && len(snapshot.Items) > 0 {
		last := snapshot.Items[len(snapshot.Items)-1]
		out.NextCursor = encodeCursor("task-events", domain.BrowseCursor{Time: coreCursorTime(last.CreatedAt), ID: last.ID})
	}
	return out, nil
}

type WikiPageListItem struct {
	ID              string     `json:"id"`
	Project         string     `json:"project"`
	Slug            string     `json:"slug"`
	Title           string     `json:"title"`
	CurrentRevision int64      `json:"current_revision"`
	Summary         string     `json:"summary"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

type WikiListRequest struct {
	Project, Cursor, Search string
	Limit                   int
}

type WikiListResult struct {
	Total      int                `json:"total"`
	Limit      int                `json:"limit"`
	NextCursor string             `json:"next_cursor,omitempty"`
	Items      []WikiPageListItem `json:"items"`
}

func (s *Service) ListWikiPages(ctx context.Context, request WikiListRequest) (WikiListResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return WikiListResult{}, err
	}
	limit, valid := browseLimit(request.Limit)
	if !valid || request.Project == "" {
		return WikiListResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("project-wiki", request.Cursor)
	if err != nil || after != nil && after.Text == "" {
		return WikiListResult{}, domain.ErrInvalid
	}
	snapshot, err := repository.WikiListSnapshot(ctx, domain.WikiListQuery{ProjectID: request.Project, Search: request.Search, After: after, Limit: limit})
	if err != nil {
		return WikiListResult{}, err
	}
	hasMore := len(snapshot.Items) > limit
	if hasMore {
		snapshot.Items = snapshot.Items[:limit]
	}
	out := WikiListResult{Total: snapshot.Total, Limit: limit, Items: make([]WikiPageListItem, 0, len(snapshot.Items))}
	for _, item := range snapshot.Items {
		out.Items = append(out.Items, WikiPageListItem{ID: item.ID, Project: item.ProjectID, Slug: item.Slug,
			Title: item.Title, CurrentRevision: item.CurrentRevision, Summary: item.Summary, UpdatedAt: optionalTime(item.UpdatedAt)})
	}
	if hasMore && len(snapshot.Items) > 0 {
		last := snapshot.Items[len(snapshot.Items)-1]
		out.NextCursor = encodeCursor("project-wiki", domain.BrowseCursor{Text: last.Title, ID: last.ID})
	}
	return out, nil
}

type WikiPage struct {
	ID              string      `json:"id"`
	Project         string      `json:"project"`
	Slug            string      `json:"slug"`
	Title           string      `json:"title"`
	Revision        int64       `json:"revision"`
	Summary         string      `json:"summary"`
	Body            string      `json:"body"`
	AuthorSessionID string      `json:"author_session_id"`
	Provenance      *Provenance `json:"provenance,omitempty"`
	CreatedAt       *time.Time  `json:"created_at,omitempty"`
}

type WikiOpenResult struct {
	Page WikiPage `json:"page"`
}

func wikiPageView(page domain.WikiRevision) WikiPage {
	return WikiPage{ID: page.PageID, Project: page.ProjectID, Slug: page.Slug, Title: page.Title,
		Revision: page.Revision, Summary: page.Summary, Body: page.Body, AuthorSessionID: page.AuthorSessionID,
		Provenance: attestedProvenance("", page.AuthorSessionID, page.AuthorPurpose), CreatedAt: optionalTime(page.CreatedAt)}
}

func (s *Service) OpenWikiPage(ctx context.Context, project, slug string, revision int64) (WikiOpenResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return WikiOpenResult{}, err
	}
	if project == "" || slug == "" || revision < 0 {
		return WikiOpenResult{}, domain.ErrInvalid
	}
	page, err := repository.OpenWikiRevision(ctx, project, slug, revision)
	if err != nil {
		return WikiOpenResult{}, err
	}
	return WikiOpenResult{Page: wikiPageView(page)}, nil
}

type WikiRevisionItem struct {
	Revision        int64       `json:"revision"`
	Summary         string      `json:"summary"`
	AuthorSessionID string      `json:"author_session_id"`
	Provenance      *Provenance `json:"provenance,omitempty"`
	CreatedAt       *time.Time  `json:"created_at,omitempty"`
}

type WikiHistoryRequest struct {
	Project, Slug, Cursor string
	Limit                 int
}

type WikiHistoryResult struct {
	Limit      int                `json:"limit"`
	NextCursor string             `json:"next_cursor,omitempty"`
	Items      []WikiRevisionItem `json:"items"`
}

func (s *Service) WikiHistory(ctx context.Context, request WikiHistoryRequest) (WikiHistoryResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return WikiHistoryResult{}, err
	}
	limit, valid := browseLimit(request.Limit)
	if !valid || request.Project == "" || request.Slug == "" {
		return WikiHistoryResult{}, domain.ErrInvalid
	}
	before := int64(0)
	if request.Cursor != "" {
		cursor, err := decodeCursor("wiki-history", request.Cursor)
		if err != nil || cursor == nil {
			return WikiHistoryResult{}, domain.ErrInvalid
		}
		before, err = strconv.ParseInt(cursor.Text, 10, 64)
		if err != nil || before < 1 {
			return WikiHistoryResult{}, domain.ErrInvalid
		}
	}
	snapshot, err := repository.WikiHistorySnapshot(ctx, domain.WikiHistoryQuery{ProjectID: request.Project, Slug: request.Slug, BeforeRevision: before, Limit: limit})
	if err != nil {
		return WikiHistoryResult{}, err
	}
	hasMore := len(snapshot.Items) > limit
	if hasMore {
		snapshot.Items = snapshot.Items[:limit]
	}
	out := WikiHistoryResult{Limit: limit, Items: make([]WikiRevisionItem, 0, len(snapshot.Items))}
	for _, item := range snapshot.Items {
		out.Items = append(out.Items, WikiRevisionItem{Revision: item.Revision, Summary: item.Summary,
			AuthorSessionID: item.AuthorSessionID, Provenance: attestedProvenance("", item.AuthorSessionID, item.AuthorPurpose),
			CreatedAt: optionalTime(item.CreatedAt)})
	}
	if hasMore && len(snapshot.Items) > 0 {
		last := snapshot.Items[len(snapshot.Items)-1]
		out.NextCursor = encodeCursor("wiki-history", domain.BrowseCursor{Text: strconv.FormatInt(last.Revision, 10), ID: request.Slug})
	}
	return out, nil
}

type CreateProjectRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status,omitempty"`
	Purpose string `json:"purpose"`
	Now     string `json:"now,omitempty"`
}

type UpdateProjectRequest struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	Purpose      string `json:"purpose"`
	Now          string `json:"now,omitempty"`
	BaseRevision *int64 `json:"base_revision"`
}

type CreateMilestoneRequest struct {
	Title      string `json:"title"`
	Status     string `json:"status"`
	Position   int    `json:"position"`
	TargetDate string `json:"target_date,omitempty"`
}

type UpdateMilestoneRequest struct {
	Title        string `json:"title"`
	Status       string `json:"status"`
	Position     int    `json:"position"`
	TargetDate   string `json:"target_date,omitempty"`
	BaseRevision *int64 `json:"base_revision"`
}

type CreateTaskRequest struct {
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	Acceptance    string   `json:"acceptance,omitempty"`
	State         string   `json:"state,omitempty"`
	Priority      int      `json:"priority"`
	MilestoneID   string   `json:"milestone_id,omitempty"`
	DependencyIDs []string `json:"dependency_ids,omitempty"`
}

type UpdateTaskRequest struct {
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	Acceptance    string   `json:"acceptance,omitempty"`
	Priority      int      `json:"priority"`
	MilestoneID   string   `json:"milestone_id,omitempty"`
	DependencyIDs []string `json:"dependency_ids,omitempty"`
	BaseRevision  *int64   `json:"base_revision"`
}

type ChangeTaskStateRequest struct {
	State        string `json:"state"`
	Basis        string `json:"basis"`
	BaseRevision *int64 `json:"base_revision"`
}

type AppendWikiRevisionRequest struct {
	Title        string `json:"title"`
	Summary      string `json:"summary"`
	Body         string `json:"body"`
	BaseRevision *int64 `json:"base_revision"`
}

func (s *Service) CreateProject(ctx context.Context, request CreateProjectRequest, actor ProjectCoreActor) (domain.WriteResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return repository.CreateCanonicalProject(ctx, domain.CreateProjectCommand{ID: request.ID, Name: request.Name,
		Status: request.Status, Purpose: request.Purpose, Now: request.Now, Meta: coreMeta(actor)})
}

func (s *Service) UpdateProject(ctx context.Context, id string, request UpdateProjectRequest, actor ProjectCoreActor) (domain.WriteResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if request.BaseRevision == nil {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.UpdateCanonicalProject(ctx, domain.UpdateProjectCommand{ID: id, Name: request.Name, Status: request.Status,
		Purpose: request.Purpose, Now: request.Now, BaseRevision: *request.BaseRevision, Meta: coreMeta(actor)})
}

func (s *Service) CreateProjectMilestone(ctx context.Context, project string, request CreateMilestoneRequest, actor ProjectCoreActor) (domain.WriteResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return repository.CreateMilestone(ctx, domain.CreateMilestoneCommand{ProjectID: project, Title: request.Title, Status: request.Status,
		Position: request.Position, TargetDate: request.TargetDate, Meta: coreMeta(actor)})
}

func (s *Service) UpdateProjectMilestone(ctx context.Context, id string, request UpdateMilestoneRequest, actor ProjectCoreActor) (domain.WriteResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if request.BaseRevision == nil {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.UpdateMilestone(ctx, domain.UpdateMilestoneCommand{ID: id, Title: request.Title, Status: request.Status,
		Position: request.Position, TargetDate: request.TargetDate, BaseRevision: *request.BaseRevision, Meta: coreMeta(actor)})
}

func (s *Service) CreateProjectTask(ctx context.Context, project string, request CreateTaskRequest, actor ProjectCoreActor) (domain.WriteResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return repository.CreateCanonicalTask(ctx, domain.CreateTaskCommand{ProjectID: project, Title: request.Title,
		Description: request.Description, Acceptance: request.Acceptance, State: request.State, Priority: request.Priority,
		MilestoneID: request.MilestoneID, DependencyIDs: request.DependencyIDs, Meta: coreMeta(actor)})
}

func (s *Service) UpdateProjectTask(ctx context.Context, id string, request UpdateTaskRequest, actor ProjectCoreActor) (domain.WriteResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if request.BaseRevision == nil {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{ID: id, Title: request.Title, Description: request.Description,
		Acceptance: request.Acceptance, Priority: request.Priority, MilestoneID: request.MilestoneID,
		DependencyIDs: request.DependencyIDs, BaseRevision: *request.BaseRevision, Meta: coreMeta(actor)})
}

func (s *Service) ChangeProjectTaskState(ctx context.Context, id string, request ChangeTaskStateRequest, actor ProjectCoreActor) (domain.WriteResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if request.BaseRevision == nil {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.ChangeCanonicalTaskState(ctx, domain.ChangeTaskStateCommand{ID: id, State: request.State,
		Basis: request.Basis, BaseRevision: *request.BaseRevision, Meta: coreMeta(actor)})
}

func (s *Service) AppendProjectWikiRevision(ctx context.Context, project, slug string, request AppendWikiRevisionRequest, actor ProjectCoreActor) (domain.WriteResult, error) {
	repository, err := projectCoreRepository(s)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if request.BaseRevision == nil {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.AppendWikiRevision(ctx, domain.AppendWikiRevisionCommand{ProjectID: project, Slug: slug,
		Title: request.Title, Summary: request.Summary, Body: request.Body, BaseRevision: *request.BaseRevision, Meta: coreMeta(actor)})
}
