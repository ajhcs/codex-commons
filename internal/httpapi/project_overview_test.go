package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
)

func (f *fakeBackend) ProjectOverview(_ context.Context, query ProjectOverviewQuery, meta RequestMeta) (ProjectOverviewResult, error) {
	f.seen("project_overview", meta)
	count := 4
	return ProjectOverviewResult{
		Project:        ProjectSummaryFixture(query.Project),
		Metrics:        ProjectMetricsFixture(count),
		NeedsAttention: AttentionPreviewFixture(query.AttentionLimit),
		CurrentWork:    CurrentWorkPreviewFixture(query.WorkLimit),
	}, nil
}

// Small helpers keep the transport fixture focused on routing and bounds.
func ProjectSummaryFixture(project string) application.ProjectSummary {
	return application.ProjectSummary{ID: project, Name: "Alpha", Purpose: "test overview"}
}

func ProjectMetricsFixture(count int) application.ProjectMetrics {
	return application.ProjectMetrics{AttentionTotal: 2, AttentionHigh: 1, OpenWork: 3,
		MergedPullRequests: application.CountMetric{Available: true, Count: &count}, ActiveSessions: 1}
}

func AttentionPreviewFixture(limit int) application.AttentionPreview {
	return application.AttentionPreview{Total: 2, Limit: limit, Items: []application.AttentionItem{{
		ID: "A-1", Severity: "high", Title: "Check failed", Project: "alpha",
		SourceRef: "check/1", NextAction: "Inspect", SourceKind: "github_check",
		UpdatedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC), Untrusted: true,
	}}}
}

func CurrentWorkPreviewFixture(limit int) application.CurrentWorkPreview {
	return application.CurrentWorkPreview{Total: 3, Limit: limit, Items: []application.CurrentWorkItem{{
		ID: "T-1", Title: "Build overview", State: "in_progress",
		Target: application.NavigationRef{Kind: "task", Ref: "T-1"},
	}}}
}

func TestProjectOverviewRouteIsAuthenticatedBoundedAndUntrusted(t *testing.T) {
	backend := &fakeBackend{}
	handler := testHandler(backend, 0)
	if rec := request(handler, http.MethodGet, "/v1/projects/alpha/overview", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated code=%d", rec.Code)
	}
	rec := request(handler, http.MethodGet, "/v1/projects/alpha/overview?attention_limit=4&work_limit=7", "", "bearer-secret")
	body := rec.Body.String()
	for _, want := range []string{`"id":"alpha"`, `"attention_total":2`, `"limit":4`, `"kind":"task"`, `"untrusted":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s: code=%d body=%s", want, rec.Code, body)
		}
	}
	if rec.Code != http.StatusOK || backend.last.Actor != "agent-7" || backend.last.Session != "S-7" || backend.calls[len(backend.calls)-1] != "project_overview" {
		t.Fatalf("route/identity code=%d meta=%+v calls=%v body=%s", rec.Code, backend.last, backend.calls, body)
	}
}

func TestProjectOverviewRouteRejectsMalformedPathAndLimits(t *testing.T) {
	handler := testHandler(&fakeBackend{}, 0)
	for _, target := range []string{
		"/v1/projects/alpha/overview/extra",
		"/v1/projects//overview",
		"/v1/projects/alpha/overview?attention_limit=21",
		"/v1/projects/alpha/overview?work_limit=0",
	} {
		rec := request(handler, http.MethodGet, target, "", "bearer-secret")
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
			t.Fatalf("target=%s code=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}
