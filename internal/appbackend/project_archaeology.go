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
