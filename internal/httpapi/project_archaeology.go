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
	PreviewArchaeologyImport(context.Context, application.ArchaeologyImportPreviewRequest, RequestMeta) (application.ArchaeologyImportPreview, error)
}

type ProjectArchaeologyGrantBackend interface {
	ClaimProjectArchaeologyTask(context.Context, application.ArchaeologyTaskClaimRequest) (application.ArchaeologyTaskClaimResponse, error)
	ReportProjectArchaeologyTask(context.Context, string, application.ArchaeologyTaskReportEnvelope) (application.ArchaeologySession, error)
}

func (h *handler) projectArchaeologyGrantRoute(w http.ResponseWriter, r *http.Request, requestID string) bool {
	if r.Method != http.MethodPost || (r.URL.Path != "/v1/project-archaeology/task/claim" && r.URL.Path != "/v1/project-archaeology/task/report") {
		return false
	}
	backend, ok := h.backend.(ProjectArchaeologyGrantBackend)
	meta := RequestMeta{RequestID: requestID}
	if !ok {
		h.finish(w, meta, nil, NewError(CodeUnavailable, "project archaeology task grants unavailable"), false)
		return true
	}
	if r.URL.Path == "/v1/project-archaeology/task/claim" {
		var input application.ArchaeologyTaskClaimRequest
		if !h.decode(w, r, meta, &input) {
			return true
		}
		out, err := backend.ClaimProjectArchaeologyTask(r.Context(), input)
		h.finish(w, meta, out, err, false)
		return true
	}
	requestKey := r.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(requestKey) || requestKey == "" {
		h.writeError(w, requestID, http.StatusBadRequest, "bad_idempotency_key", "Idempotency-Key required")
		return true
	}
	var input application.ArchaeologyTaskReportEnvelope
	if !h.decode(w, r, meta, &input) {
		return true
	}
	out, err := backend.ReportProjectArchaeologyTask(r.Context(), requestKey, input)
	h.finish(w, meta, out, err, false)
	return true
}

func (h *handler) projectArchaeologyRoute(w http.ResponseWriter, r *http.Request, meta RequestMeta) bool {
	path := r.URL.Path
	known := path == "/v1/project-archaeology" || path == "/v1/project-archaeology/discover" || path == "/v1/project-archaeology/config" || path == "/v1/project-archaeology/start" || path == "/v1/project-archaeology/pause" || path == "/v1/project-archaeology/resume" || path == "/v1/project-archaeology/cancel" || path == "/v1/project-archaeology/import-preview"
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
