package appbackend

import (
	"context"
	"errors"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
)

func mapProjectCoreError(err error, resource string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrNotFound):
		return httpapi.NewError(httpapi.CodeNotFound, resource+" not found")
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrFutureRevision):
		return httpapi.NewError(httpapi.CodeConflict, resource+" conflict")
	case errors.Is(err, domain.ErrInvalid):
		return httpapi.NewError(httpapi.CodeInvalid, "invalid "+resource)
	case errors.Is(err, domain.ErrUnavailable):
		return httpapi.NewError(httpapi.CodeUnavailable, resource+" unavailable")
	default:
		return err
	}
}

func validateProjectCoreIdentity(meta httpapi.RequestMeta) error {
	if meta.Actor == "" || meta.Session == "" || meta.Host == "" ||
		(meta.PrincipalKind != "agent" && meta.PrincipalKind != "human") {
		return httpapi.NewError(httpapi.CodeInvalid, "server-attested identity required")
	}
	return nil
}

func validateCoreWrite(meta httpapi.RequestMeta, humanOnly bool) error {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return err
	}
	if humanOnly && meta.PrincipalKind != "human" {
		return httpapi.NewError(httpapi.CodeForbidden, "local human admin required")
	}
	if meta.IdempotencyKey == "" {
		return httpapi.NewError(httpapi.CodeInvalid, "Idempotency-Key required")
	}
	return nil
}

func coreActor(meta httpapi.RequestMeta) application.ProjectCoreActor {
	return application.ProjectCoreActor{Actor: meta.Actor, Session: meta.Session, RequestID: meta.IdempotencyKey}
}

func coreWriteResult(result domain.WriteResult, err error, resource string) (httpapi.WriteResult, error) {
	if err != nil {
		return httpapi.WriteResult{}, mapProjectCoreError(err, resource)
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}

func (a *Adapter) ProjectCoreDetail(ctx context.Context, project string, meta httpapi.RequestMeta) (httpapi.ProjectCoreDetailResult, error) {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return httpapi.ProjectCoreDetailResult{}, err
	}
	out, err := a.home.ProjectCoreDetail(ctx, project)
	return out, mapProjectCoreError(err, "project")
}

func (a *Adapter) ListProjectMilestones(ctx context.Context, query httpapi.ProjectMilestoneListQuery, meta httpapi.RequestMeta) (httpapi.ProjectMilestoneListResult, error) {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return httpapi.ProjectMilestoneListResult{}, err
	}
	out, err := a.home.ListMilestones(ctx, query)
	return out, mapProjectCoreError(err, "milestones")
}

func (a *Adapter) ListProjectTasks(ctx context.Context, query httpapi.ProjectTaskListQuery, meta httpapi.RequestMeta) (httpapi.ProjectTaskListResult, error) {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return httpapi.ProjectTaskListResult{}, err
	}
	out, err := a.home.ListCanonicalTasks(ctx, query)
	return out, mapProjectCoreError(err, "tasks")
}

func (a *Adapter) OpenProjectTask(ctx context.Context, query httpapi.ProjectTaskOpenQuery, meta httpapi.RequestMeta) (httpapi.ProjectTaskOpenResult, error) {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return httpapi.ProjectTaskOpenResult{}, err
	}
	out, err := a.home.OpenCanonicalTask(ctx, query)
	return out, mapProjectCoreError(err, "task")
}

func (a *Adapter) ListProjectTaskEvents(ctx context.Context, query httpapi.ProjectTaskEventListQuery, meta httpapi.RequestMeta) (httpapi.ProjectTaskEventListResult, error) {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return httpapi.ProjectTaskEventListResult{}, err
	}
	out, err := a.home.ListTaskEvents(ctx, query)
	return out, mapProjectCoreError(err, "task events")
}

func (a *Adapter) ListProjectWiki(ctx context.Context, query httpapi.ProjectWikiListQuery, meta httpapi.RequestMeta) (httpapi.ProjectWikiListResult, error) {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return httpapi.ProjectWikiListResult{}, err
	}
	out, err := a.home.ListWikiPages(ctx, query)
	return out, mapProjectCoreError(err, "wiki")
}

func (a *Adapter) OpenProjectWiki(ctx context.Context, query httpapi.ProjectWikiOpenQuery, meta httpapi.RequestMeta) (httpapi.ProjectWikiOpenResult, error) {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return httpapi.ProjectWikiOpenResult{}, err
	}
	out, err := a.home.OpenWikiPage(ctx, query.Project, query.Slug, query.Revision)
	return out, mapProjectCoreError(err, "wiki page")
}

func (a *Adapter) ListProjectWikiRevisions(ctx context.Context, query httpapi.ProjectWikiHistoryQuery, meta httpapi.RequestMeta) (httpapi.ProjectWikiHistoryResult, error) {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return httpapi.ProjectWikiHistoryResult{}, err
	}
	out, err := a.home.WikiHistory(ctx, query)
	return out, mapProjectCoreError(err, "wiki history")
}

func (a *Adapter) CreateCoreProject(ctx context.Context, request httpapi.CreateCoreProjectRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.CreateProject(ctx, request, coreActor(meta))
	return coreWriteResult(result, err, "project")
}

func (a *Adapter) UpdateCoreProject(ctx context.Context, id string, request httpapi.UpdateCoreProjectRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.UpdateProject(ctx, id, request, coreActor(meta))
	return coreWriteResult(result, err, "project")
}

func (a *Adapter) CreateCoreMilestone(ctx context.Context, project string, request httpapi.CreateCoreMilestoneRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.CreateProjectMilestone(ctx, project, request, coreActor(meta))
	return coreWriteResult(result, err, "milestone")
}

func (a *Adapter) UpdateCoreMilestone(ctx context.Context, id string, request httpapi.UpdateCoreMilestoneRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.UpdateProjectMilestone(ctx, id, request, coreActor(meta))
	return coreWriteResult(result, err, "milestone")
}

func (a *Adapter) CreateCoreTask(ctx context.Context, project string, request httpapi.CreateCoreTaskRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateCoreWrite(meta, false); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.CreateProjectTask(ctx, project, request, coreActor(meta))
	return coreWriteResult(result, err, "task")
}

func (a *Adapter) UpdateCoreTask(ctx context.Context, id string, request httpapi.UpdateCoreTaskRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateCoreWrite(meta, false); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.UpdateProjectTask(ctx, id, request, coreActor(meta))
	return coreWriteResult(result, err, "task")
}

func (a *Adapter) ChangeCoreTaskState(ctx context.Context, id string, request httpapi.ChangeCoreTaskStateRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateCoreWrite(meta, false); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.ChangeProjectTaskState(ctx, id, request, coreActor(meta))
	return coreWriteResult(result, err, "task state")
}

func (a *Adapter) AppendCoreWikiRevision(ctx context.Context, project, slug string, request httpapi.AppendCoreWikiRevisionRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.AppendProjectWikiRevision(ctx, project, slug, request, coreActor(meta))
	return coreWriteResult(result, err, "wiki revision")
}

func (a *Adapter) PreviewHistoricalImport(ctx context.Context, project string, request httpapi.HistoricalImportRequest, meta httpapi.RequestMeta) (httpapi.HistoricalImportResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return httpapi.HistoricalImportResult{}, err
	}
	result, err := a.home.PreviewHistoricalTaskImport(ctx, project, request, coreActor(meta))
	return result, mapProjectCoreError(err, "historical import")
}

func (a *Adapter) ApplyHistoricalImport(ctx context.Context, project string, request httpapi.HistoricalImportRequest, meta httpapi.RequestMeta) (httpapi.HistoricalImportResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return httpapi.HistoricalImportResult{}, err
	}
	result, err := a.home.ApplyHistoricalTaskImport(ctx, project, request, coreActor(meta))
	return result, mapProjectCoreError(err, "historical import")
}

func (a *Adapter) SupersedeHistoricalImport(ctx context.Context, project, batch string, request httpapi.SupersedeHistoricalImportRequest, meta httpapi.RequestMeta) (httpapi.HistoricalImportResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return httpapi.HistoricalImportResult{}, err
	}
	result, err := a.home.SupersedeHistoricalTaskImport(ctx, project, batch, request, coreActor(meta))
	return result, mapProjectCoreError(err, "historical import")
}

var _ httpapi.ProjectCoreBackend = (*Adapter)(nil)
