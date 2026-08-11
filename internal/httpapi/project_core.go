package httpapi

import (
	"context"

	"codex-commons/internal/application"
)

// ProjectCoreBackend is deliberately optional. The canonical project
// workspace is additive to the established agent transport, so existing
// Backend implementations and narrow test doubles remain valid.
type ProjectCoreBackend interface {
	ProjectCoreDetail(context.Context, string, RequestMeta) (ProjectCoreDetailResult, error)
	ListProjectMilestones(context.Context, ProjectMilestoneListQuery, RequestMeta) (ProjectMilestoneListResult, error)
	ListProjectTasks(context.Context, ProjectTaskListQuery, RequestMeta) (ProjectTaskListResult, error)
	OpenProjectTask(context.Context, ProjectTaskOpenQuery, RequestMeta) (ProjectTaskOpenResult, error)
	ListProjectTaskEvents(context.Context, ProjectTaskEventListQuery, RequestMeta) (ProjectTaskEventListResult, error)
	ListProjectWiki(context.Context, ProjectWikiListQuery, RequestMeta) (ProjectWikiListResult, error)
	OpenProjectWiki(context.Context, ProjectWikiOpenQuery, RequestMeta) (ProjectWikiOpenResult, error)
	ListProjectWikiRevisions(context.Context, ProjectWikiHistoryQuery, RequestMeta) (ProjectWikiHistoryResult, error)

	CreateCoreProject(context.Context, CreateCoreProjectRequest, RequestMeta) (WriteResult, error)
	UpdateCoreProject(context.Context, string, UpdateCoreProjectRequest, RequestMeta) (WriteResult, error)
	CreateCoreMilestone(context.Context, string, CreateCoreMilestoneRequest, RequestMeta) (WriteResult, error)
	UpdateCoreMilestone(context.Context, string, UpdateCoreMilestoneRequest, RequestMeta) (WriteResult, error)
	CreateCoreTask(context.Context, string, CreateCoreTaskRequest, RequestMeta) (WriteResult, error)
	UpdateCoreTask(context.Context, string, UpdateCoreTaskRequest, RequestMeta) (WriteResult, error)
	ChangeCoreTaskState(context.Context, string, ChangeCoreTaskStateRequest, RequestMeta) (WriteResult, error)
	AppendCoreWikiRevision(context.Context, string, string, AppendCoreWikiRevisionRequest, RequestMeta) (WriteResult, error)
	PreviewHistoricalImport(context.Context, string, HistoricalImportRequest, RequestMeta) (HistoricalImportResult, error)
	ApplyHistoricalImport(context.Context, string, HistoricalImportRequest, RequestMeta) (HistoricalImportResult, error)
	SupersedeHistoricalImport(context.Context, string, string, SupersedeHistoricalImportRequest, RequestMeta) (HistoricalImportResult, error)
}

type ProjectCoreDetailResult = application.ProjectCoreDetailResult
type ProjectMilestoneListQuery = application.MilestoneListRequest
type ProjectMilestoneListResult = application.MilestoneListResult
type ProjectTaskListQuery = application.TaskListRequest
type ProjectTaskListResult = application.TaskListResult
type ProjectTaskOpenQuery = application.TaskOpenRequest
type ProjectTaskOpenResult = application.TaskOpenResult
type ProjectTaskEventListQuery = application.TaskEventListRequest
type ProjectTaskEventListResult = application.TaskEventListResult
type ProjectWikiListQuery = application.WikiListRequest
type ProjectWikiListResult = application.WikiListResult
type ProjectWikiOpenQuery struct {
	Project  string
	Slug     string
	Revision int64
}
type ProjectWikiOpenResult = application.WikiOpenResult
type ProjectWikiHistoryQuery = application.WikiHistoryRequest
type ProjectWikiHistoryResult = application.WikiHistoryResult

type CreateCoreProjectRequest = application.CreateProjectRequest
type UpdateCoreProjectRequest = application.UpdateProjectRequest
type CreateCoreMilestoneRequest = application.CreateMilestoneRequest
type UpdateCoreMilestoneRequest = application.UpdateMilestoneRequest
type CreateCoreTaskRequest = application.CreateTaskRequest
type UpdateCoreTaskRequest = application.UpdateTaskRequest
type ChangeCoreTaskStateRequest = application.ChangeTaskStateRequest
type AppendCoreWikiRevisionRequest = application.AppendWikiRevisionRequest
type HistoricalImportRequest = application.HistoricalImportRequest
type HistoricalImportResult = application.HistoricalImportResult
type SupersedeHistoricalImportRequest = application.SupersedeHistoricalImportRequest
