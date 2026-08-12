package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
)

type archaeologyGrantBackend struct {
	fakeBackend
	claims int
}

func (b *archaeologyGrantBackend) ClaimProjectArchaeologyTask(_ context.Context, input application.ArchaeologyTaskClaimRequest) (application.ArchaeologyTaskClaimResponse, error) {
	b.claims++
	return application.ArchaeologyTaskClaimResponse{LaunchID: input.LaunchID, ProjectID: input.ProjectID, ThreadID: input.ThreadID, CodexSessionID: input.CodexSessionID, ReportToken: "scoped-report-token", ReportExpiresAt: time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)}, nil
}

func (*archaeologyGrantBackend) ReportProjectArchaeologyTask(context.Context, string, application.ArchaeologyTaskReportEnvelope) (application.ArchaeologySession, error) {
	return application.ArchaeologySession{}, nil
}

func TestArchaeologyTaskClaimUsesOnlyScopedGrantRouteAndStrictJSON(t *testing.T) {
	backend := &archaeologyGrantBackend{}
	handler := NewHandler(backend, Config{})
	body := `{"launch_id":"launch","project_id":"project","thread_id":"thread","session_id":"session","grant":"abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/project-archaeology/task/claim", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || backend.claims != 1 || !strings.Contains(rec.Body.String(), `"report_token":"scoped-report-token"`) {
		t.Fatalf("status=%d claims=%d body=%s", rec.Code, backend.claims, rec.Body.String())
	}

	bad := httptest.NewRequest(http.MethodPost, "/v1/project-archaeology/task/claim", strings.NewReader(strings.TrimSuffix(body, "}")+`,"credential":"broad"}`))
	bad.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest || backend.claims != 1 {
		t.Fatalf("unknown field status=%d claims=%d", badRec.Code, backend.claims)
	}
}

func TestArchaeologyTaskReportRequiresStableIdempotencyKey(t *testing.T) {
	handler := NewHandler(&archaeologyGrantBackend{}, Config{})
	req := httptest.NewRequest(http.MethodPost, "/v1/project-archaeology/task/report", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "bad_idempotency_key") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLegacyTaskPackClaimAndReportRoutesAreNotExposed(t *testing.T) {
	handler := NewHandler(&archaeologyGrantBackend{}, Config{})
	for _, path := range []string{
		"/v1/project-archaeology/handoff/claim",
		"/v1/project-archaeology/handoff/report",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("legacy route %s remained exposed: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
