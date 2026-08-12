package httpapi

import (
	"context"
	"net/http"

	"codex-commons/internal/application"
)

type ProjectArchaeologyBackend interface {
	ProjectArchaeology(context.Context, RequestMeta) (application.ArchaeologySession, error)
	DiscoverProjectArchaeology(context.Context, RequestMeta) (application.ArchaeologySession, error)
	ConfigureProjectArchaeology(context.Context, application.ArchaeologyConfigRequest, RequestMeta) (application.ArchaeologySession, error)
	StartProjectArchaeology(context.Context, application.ArchaeologyTransitionRequest, RequestMeta) (application.ArchaeologySession, error)
	PauseProjectArchaeology(context.Context, application.ArchaeologyTransitionRequest, RequestMeta) (application.ArchaeologySession, error)
	ResumeProjectArchaeology(context.Context, application.ArchaeologyTransitionRequest, RequestMeta) (application.ArchaeologySession, error)
	CancelProjectArchaeology(context.Context, application.ArchaeologyTransitionRequest, RequestMeta) (application.ArchaeologySession, error)
	ClaimProjectArchaeology(context.Context, application.ArchaeologyHandoffClaimRequest, RequestMeta) (application.ArchaeologySession, error)
	ReportProjectArchaeology(context.Context, application.ArchaeologyHandoffReportEnvelope, RequestMeta) (application.ArchaeologySession, error)
	PreviewArchaeologyImport(context.Context, application.ArchaeologyImportPreviewRequest, RequestMeta) (application.ArchaeologyImportPreview, error)
}

func (h *handler) projectArchaeologyRoute(w http.ResponseWriter, r *http.Request, meta RequestMeta) bool {
	path := r.URL.Path
	known := path == "/v1/project-archaeology" || path == "/v1/project-archaeology/discover" || path == "/v1/project-archaeology/config" || path == "/v1/project-archaeology/start" || path == "/v1/project-archaeology/pause" || path == "/v1/project-archaeology/resume" || path == "/v1/project-archaeology/cancel" || path == "/v1/project-archaeology/handoff/claim" || path == "/v1/project-archaeology/handoff/report" || path == "/v1/project-archaeology/import-preview"
	if !known {
		return false
	}
	backend, ok := h.backend.(ProjectArchaeologyBackend)
	if !ok {
		h.finish(w, meta, nil, NewError(CodeUnavailable, "project archaeology unavailable"), false)
		return true
	}
	switch {
	case r.Method == http.MethodGet && path == "/v1/project-archaeology":
		out, err := backend.ProjectArchaeology(r.Context(), meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodPost && path == "/v1/project-archaeology/discover":
		out, err := backend.DiscoverProjectArchaeology(r.Context(), meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodPut && path == "/v1/project-archaeology/config":
		var input application.ArchaeologyConfigRequest
		if !h.decodeCoreWrite(w, r, meta, &input) {
			return true
		}
		out, err := backend.ConfigureProjectArchaeology(r.Context(), input, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodPost && (path == "/v1/project-archaeology/start" || path == "/v1/project-archaeology/pause" || path == "/v1/project-archaeology/resume" || path == "/v1/project-archaeology/cancel"):
		var input application.ArchaeologyTransitionRequest
		if !h.decodeCoreWrite(w, r, meta, &input) {
			return true
		}
		var out application.ArchaeologySession
		var err error
		switch path {
		case "/v1/project-archaeology/start":
			out, err = backend.StartProjectArchaeology(r.Context(), input, meta)
		case "/v1/project-archaeology/pause":
			out, err = backend.PauseProjectArchaeology(r.Context(), input, meta)
		case "/v1/project-archaeology/resume":
			out, err = backend.ResumeProjectArchaeology(r.Context(), input, meta)
		case "/v1/project-archaeology/cancel":
			out, err = backend.CancelProjectArchaeology(r.Context(), input, meta)
		}
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodPost && path == "/v1/project-archaeology/handoff/claim":
		var input application.ArchaeologyHandoffClaimRequest
		if !h.decodeCoreWrite(w, r, meta, &input) {
			return true
		}
		out, err := backend.ClaimProjectArchaeology(r.Context(), input, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodPost && path == "/v1/project-archaeology/handoff/report":
		var input application.ArchaeologyHandoffReportEnvelope
		if !h.decodeCoreWrite(w, r, meta, &input) {
			return true
		}
		out, err := backend.ReportProjectArchaeology(r.Context(), input, meta)
		h.finish(w, meta, out, err, true)
	case r.Method == http.MethodPost && path == "/v1/project-archaeology/import-preview":
		var input application.ArchaeologyImportPreviewRequest
		if !h.decodeCoreWrite(w, r, meta, &input) {
			return true
		}
		out, err := backend.PreviewArchaeologyImport(r.Context(), input, meta)
		h.finish(w, meta, out, err, true)
	default:
		h.notFound(w, meta)
	}
	return true
}
