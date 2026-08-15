package appbackend

import (
	"context"

	"codex-commons/internal/application"
	"codex-commons/internal/httpapi"
)

func validateArchaeologyRead(meta httpapi.RequestMeta) error {
	if err := validateProjectCoreIdentity(meta); err != nil {
		return err
	}
	if meta.PrincipalKind != "human" {
		return httpapi.NewError(httpapi.CodeForbidden, "local human admin required")
	}
	return nil
}
func (a *Adapter) ProjectArchaeology(ctx context.Context, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateArchaeologyRead(meta); err != nil {
		return application.ArchaeologySession{}, err
	}
	out, err := a.home.ProjectArchaeology(ctx, meta.Principal)
	return out, mapProjectCoreError(err, "project archaeology")
}
func (a *Adapter) ProjectArchaeologyCatalog(ctx context.Context, input application.ArchaeologyCatalogRequest, meta httpapi.RequestMeta) (application.ArchaeologyCatalogPage, error) {
	if err := validateArchaeologyRead(meta); err != nil {
		return application.ArchaeologyCatalogPage{}, err
	}
	out, err := a.home.ProjectArchaeologyCatalog(ctx, meta.Principal, input)
	return out, mapProjectCoreError(err, "project archaeology catalog")
}
func (a *Adapter) ProjectArchaeologyBatchHistory(ctx context.Context, cursor string, limit int, meta httpapi.RequestMeta) (application.ArchaeologyBatchHistoryPage, error) {
	if err := validateArchaeologyRead(meta); err != nil {
		return application.ArchaeologyBatchHistoryPage{}, err
	}
	out, err := a.home.ProjectArchaeologyBatchHistory(ctx, meta.Principal, cursor, limit)
	return out, mapProjectCoreError(err, "project archaeology history")
}
func (a *Adapter) ProjectArchaeologyBatch(ctx context.Context, batchID string, meta httpapi.RequestMeta) (application.ArchaeologyBatchDetail, error) {
	if err := validateArchaeologyRead(meta); err != nil {
		return application.ArchaeologyBatchDetail{}, err
	}
	out, err := a.home.ProjectArchaeologyBatch(ctx, meta.Principal, batchID)
	return out, mapProjectCoreError(err, "project archaeology batch")
}
func (a *Adapter) ProjectArchaeologyBatchOutcomes(ctx context.Context, batchID, cursor string, meta httpapi.RequestMeta) (application.ArchaeologyOutcomePage, error) {
	if err := validateArchaeologyRead(meta); err != nil {
		return application.ArchaeologyOutcomePage{}, err
	}
	out, err := a.home.ProjectArchaeologyBatchOutcomes(ctx, meta.Principal, batchID, cursor)
	return out, mapProjectCoreError(err, "project archaeology batch outcomes")
}
func (a *Adapter) PreviewSelectedArchaeologyImports(ctx context.Context, batchID string, input application.ArchaeologySelectedPreviewRequest, meta httpapi.RequestMeta) (application.ArchaeologySelectedPreview, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySelectedPreview{}, err
	}
	out, err := a.home.PreviewSelectedArchaeologyImports(ctx, meta.Principal, batchID, input, coreActor(meta))
	return out, mapProjectCoreError(err, "selected archaeology preview")
}
func (a *Adapter) PreviewSelectedArchaeologyImportsPage(ctx context.Context, batchID, cursor string, input application.ArchaeologySelectedPreviewRequest, meta httpapi.RequestMeta) (application.ArchaeologySelectedPreview, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySelectedPreview{}, err
	}
	input.ReviewRequestID = meta.IdempotencyKey
	out, err := a.home.PreviewSelectedArchaeologyImportsPage(ctx, meta.Principal, batchID, cursor, input, coreActor(meta))
	return out, mapProjectCoreError(err, "selected archaeology preview page")
}
func (a *Adapter) ApplySelectedArchaeologyImports(ctx context.Context, batchID string, input application.ArchaeologySelectedApplyRequest, meta httpapi.RequestMeta) (application.ArchaeologySelectedApplyResult, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySelectedApplyResult{}, err
	}
	out, err := a.home.ApplySelectedArchaeologyImports(ctx, meta.Principal, meta.IdempotencyKey, batchID, input, coreActor(meta))
	return out, mapProjectCoreError(err, "selected archaeology apply")
}
func (a *Adapter) DiscoverProjectArchaeology(ctx context.Context, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySession{}, err
	}
	out, err := a.home.DiscoverProjectArchaeology(ctx, meta.Principal, meta.IdempotencyKey)
	return out, mapProjectCoreError(err, "project archaeology")
}
func (a *Adapter) ConfigureProjectArchaeology(ctx context.Context, input application.ArchaeologyConfigRequest, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySession{}, err
	}
	out, err := a.home.ConfigureArchaeologySession(ctx, meta.Principal, meta.IdempotencyKey, input)
	return out, mapProjectCoreError(err, "project archaeology")
}
func (a *Adapter) StartProjectArchaeology(ctx context.Context, input application.ArchaeologyTransitionRequest, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySession{}, err
	}
	out, err := a.home.StartProjectArchaeology(ctx, meta.Principal, meta.IdempotencyKey, input)
	return out, mapProjectCoreError(err, "project archaeology")
}
func (a *Adapter) PauseProjectArchaeology(ctx context.Context, input application.ArchaeologyTransitionRequest, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySession{}, err
	}
	out, err := a.home.PauseProjectArchaeology(ctx, meta.Principal, meta.IdempotencyKey, input)
	return out, mapProjectCoreError(err, "project archaeology")
}
func (a *Adapter) ResumeProjectArchaeology(ctx context.Context, input application.ArchaeologyTransitionRequest, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySession{}, err
	}
	out, err := a.home.ResumeProjectArchaeology(ctx, meta.Principal, meta.IdempotencyKey, input)
	return out, mapProjectCoreError(err, "project archaeology")
}
func (a *Adapter) CancelProjectArchaeology(ctx context.Context, input application.ArchaeologyTransitionRequest, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySession{}, err
	}
	out, err := a.home.CancelProjectArchaeology(ctx, meta.Principal, meta.IdempotencyKey, input)
	return out, mapProjectCoreError(err, "project archaeology")
}

func (a *Adapter) ResolveProjectArchaeologyUncertainty(ctx context.Context, input application.ArchaeologyResolutionRequest, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologySession{}, err
	}
	out, err := a.home.ResolveProjectArchaeologyUncertainty(ctx, meta.Principal, meta.IdempotencyKey, input)
	return out, mapProjectCoreError(err, "project archaeology resolution")
}

func (a *Adapter) ClaimProjectArchaeology(ctx context.Context, input application.ArchaeologyHandoffClaimRequest, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, false); err != nil {
		return application.ArchaeologySession{}, err
	}
	if meta.PrincipalKind != "agent" {
		return application.ArchaeologySession{}, httpapi.NewError(httpapi.CodeForbidden, "Codex agent session required")
	}
	out, err := a.home.ClaimProjectArchaeology(ctx, input.HandoffID, meta.IdempotencyKey, meta.Session)
	return out, mapProjectCoreError(err, "project archaeology handoff")
}
func (a *Adapter) ReportProjectArchaeology(ctx context.Context, input application.ArchaeologyHandoffReportEnvelope, meta httpapi.RequestMeta) (application.ArchaeologySession, error) {
	if err := validateCoreWrite(meta, false); err != nil {
		return application.ArchaeologySession{}, err
	}
	if meta.PrincipalKind != "agent" {
		return application.ArchaeologySession{}, httpapi.NewError(httpapi.CodeForbidden, "Codex agent session required")
	}
	out, err := a.home.ReportProjectArchaeology(ctx, input.HandoffID, meta.IdempotencyKey, meta.Session, application.ArchaeologyHandoffReportRequest{Outcomes: input.Outcomes})
	return out, mapProjectCoreError(err, "project archaeology report")
}
func (a *Adapter) PreviewArchaeologyImport(ctx context.Context, input application.ArchaeologyImportPreviewRequest, meta httpapi.RequestMeta) (application.ArchaeologyImportPreview, error) {
	if err := validateCoreWrite(meta, true); err != nil {
		return application.ArchaeologyImportPreview{}, err
	}
	out, err := a.home.PreviewArchaeologyImport(ctx, meta.Principal, input, coreActor(meta))
	return out, mapProjectCoreError(err, "project archaeology import preview")
}

func (a *Adapter) ClaimProjectArchaeologyTask(ctx context.Context, input application.ArchaeologyTaskClaimRequest) (application.ArchaeologyTaskClaimResponse, error) {
	out, err := a.home.ClaimProjectArchaeologyTask(ctx, input)
	return out, mapProjectCoreError(err, "project archaeology task claim")
}

func (a *Adapter) ReportProjectArchaeologyTask(ctx context.Context, requestID string, input application.ArchaeologyTaskReportEnvelope) (application.ArchaeologySession, error) {
	out, err := a.home.ReportProjectArchaeologyTask(ctx, requestID, input)
	return out, mapProjectCoreError(err, "project archaeology task report")
}

var _ httpapi.ProjectArchaeologyBackend = (*Adapter)(nil)
var _ httpapi.ProjectArchaeologyGrantBackend = (*Adapter)(nil)
