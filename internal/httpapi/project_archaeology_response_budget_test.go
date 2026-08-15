package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
)

type archaeologyBudgetBackend struct {
	*fakeBackend
	batch   application.ArchaeologyBatchDetail
	outcome application.ArchaeologyOutcomePage
	preview application.ArchaeologySelectedPreview
	apply   application.ArchaeologySelectedApplyResult
	status  InstallationStatusResult
	session application.ArchaeologySession
}

func (b *archaeologyBudgetBackend) ProjectArchaeology(context.Context, RequestMeta) (application.ArchaeologySession, error) {
	return b.session, nil
}
func (b *archaeologyBudgetBackend) ProjectArchaeologyCatalog(context.Context, application.ArchaeologyCatalogRequest, RequestMeta) (application.ArchaeologyCatalogPage, error) {
	return application.ArchaeologyCatalogPage{}, nil
}
func (b *archaeologyBudgetBackend) ProjectArchaeologyBatchHistory(context.Context, string, int, RequestMeta) (application.ArchaeologyBatchHistoryPage, error) {
	return application.ArchaeologyBatchHistoryPage{}, nil
}
func (b *archaeologyBudgetBackend) ProjectArchaeologyBatch(context.Context, string, RequestMeta) (application.ArchaeologyBatchDetail, error) {
	return b.batch, nil
}
func (b *archaeologyBudgetBackend) ProjectArchaeologyBatchOutcomes(context.Context, string, string, RequestMeta) (application.ArchaeologyOutcomePage, error) {
	return b.outcome, nil
}
func (b *archaeologyBudgetBackend) PreviewSelectedArchaeologyImports(context.Context, string, application.ArchaeologySelectedPreviewRequest, RequestMeta) (application.ArchaeologySelectedPreview, error) {
	return b.preview, nil
}
func (b *archaeologyBudgetBackend) PreviewSelectedArchaeologyImportsPage(context.Context, string, string, application.ArchaeologySelectedPreviewRequest, RequestMeta) (application.ArchaeologySelectedPreview, error) {
	return b.preview, nil
}
func (b *archaeologyBudgetBackend) ApplySelectedArchaeologyImports(context.Context, string, application.ArchaeologySelectedApplyRequest, RequestMeta) (application.ArchaeologySelectedApplyResult, error) {
	return b.apply, nil
}
func (b *archaeologyBudgetBackend) DiscoverProjectArchaeology(context.Context, RequestMeta) (application.ArchaeologySession, error) {
	return application.ArchaeologySession{}, nil
}
func (b *archaeologyBudgetBackend) ConfigureProjectArchaeology(context.Context, application.ArchaeologyConfigRequest, RequestMeta) (application.ArchaeologySession, error) {
	return application.ArchaeologySession{}, nil
}
func (b *archaeologyBudgetBackend) StartProjectArchaeology(context.Context, application.ArchaeologyTransitionRequest, RequestMeta) (application.ArchaeologySession, error) {
	return application.ArchaeologySession{}, nil
}
func (b *archaeologyBudgetBackend) PauseProjectArchaeology(context.Context, application.ArchaeologyTransitionRequest, RequestMeta) (application.ArchaeologySession, error) {
	return application.ArchaeologySession{}, nil
}
func (b *archaeologyBudgetBackend) ResumeProjectArchaeology(context.Context, application.ArchaeologyTransitionRequest, RequestMeta) (application.ArchaeologySession, error) {
	return application.ArchaeologySession{}, nil
}
func (b *archaeologyBudgetBackend) CancelProjectArchaeology(context.Context, application.ArchaeologyTransitionRequest, RequestMeta) (application.ArchaeologySession, error) {
	return application.ArchaeologySession{}, nil
}
func (b *archaeologyBudgetBackend) PreviewArchaeologyImport(context.Context, application.ArchaeologyImportPreviewRequest, RequestMeta) (application.ArchaeologyImportPreview, error) {
	return application.ArchaeologyImportPreview{}, nil
}
func (b *archaeologyBudgetBackend) InstallationStatus(context.Context, RequestMeta) (InstallationStatusResult, error) {
	return b.status, nil
}

func TestProjectArchaeologyMaximumHTTPEnvelopesFitBrowserBudget(t *testing.T) {
	now := time.Now().UTC()
	b := &archaeologyBudgetBackend{fakeBackend: &fakeBackend{}}
	b.session.ID, b.session.State, b.session.Revision = strings.Repeat("s", 120), "completed", 100
	for index := 0; index < 100; index++ {
		b.session.Discovery.Candidates = append(b.session.Discovery.Candidates, application.ArchaeologyCandidate{ID: fmt.Sprintf("candidate-%03d-%s", index, strings.Repeat("i", 90)), Name: strings.Repeat("N", 200), PathLabel: strings.Repeat("P", 300), RepositoryLabel: strings.Repeat("R", 300), Signals: application.ArchaeologySignals{Git: true, Docs: true, CodexHistory: true}, Estimate: application.ArchaeologyEstimate{DurationSecondsMin: 1, DurationSecondsMax: 600, RelativeCost: "high"}, PrivacyNote: strings.Repeat("V", 500), CodexThreadCount: 10000})
		b.session.Runs = append(b.session.Runs, application.ArchaeologyRun{ID: fmt.Sprintf("run-%03d", index), ProjectID: fmt.Sprintf("project-%03d", index), State: "completed", PhaseLabel: strings.Repeat("F", 120), Error: strings.Repeat("E", 500), UpdatedAt: &now})
	}
	b.batch.BatchID, b.batch.State, b.batch.Mode = strings.Repeat("b", 120), "completed", "app_server_dynamic_tools"
	b.batch.Depth, b.batch.Concurrency, b.batch.SelectedTotal = "deep", 2, 30
	for index := 0; index < 30; index++ {
		b.batch.Tasks = append(b.batch.Tasks, application.ArchaeologyTaskLaunch{JobID: fmt.Sprintf("job-%02d-%s", index, strings.Repeat("j", 90)), BatchID: b.batch.BatchID, ProjectID: fmt.Sprintf("project-%02d", index), ProjectName: strings.Repeat("N", 200), State: "completed", ThreadID: strings.Repeat("t", 120), TurnID: strings.Repeat("u", 120), PhaseLabel: strings.Repeat("p", 120), Error: strings.Repeat("e", 500), CreatedAt: &now, UpdatedAt: &now})
	}
	b.session.Handoff = &application.ArchaeologyHandoff{BatchID: b.batch.BatchID, ID: strings.Repeat("h", 120), State: "completed", Depth: "deep", Concurrency: 2, Tasks: append([]application.ArchaeologyTaskLaunch(nil), b.batch.Tasks...), CandidateIDs: make([]string, 30), AllowedActions: []string{"resolve"}}
	for index := 0; index < 5; index++ {
		outcomeID := fmt.Sprintf("outcome-%d", index)
		b.outcome.Items = append(b.outcome.Items, application.ArchaeologyOutcome{ID: outcomeID, ProjectID: fmt.Sprintf("project-%d", index), Title: strings.Repeat("T", 200), Summary: strings.Repeat("S", 300), SourceCount: 1000, Provenance: []application.ArchaeologyProvenance{{SourceKind: "docs", SourceLabel: strings.Repeat("L", 300), Digest: "sha256:" + strings.Repeat("a", 64), RecordedAt: &now}}, MemberSessions: []application.ArchaeologyMemberSession{{SessionID: strings.Repeat("s", 120), DisplayName: strings.Repeat("D", 200), Reachability: "historical", Execution: "not_running", Authority: "observed", DemonstratedStrengths: []string{strings.Repeat("g", 120)}, Uncertainties: []string{strings.Repeat("u", 200)}}}})
		request := application.HistoricalImportRequest{SchemaVersion: 1, BatchID: fmt.Sprintf("native-%024d", index), SourceDigest: "sha256:" + strings.Repeat("a", 64), CollisionPolicy: "current_wins", Tasks: []application.HistoricalTaskRequest{{Key: fmt.Sprintf("task-%d", index), Title: strings.Repeat("Q", 200), Description: strings.Repeat("d", 30000), State: "done"}}}
		b.preview.OutcomeIDs = append(b.preview.OutcomeIDs, outcomeID)
		b.preview.Projects = append(b.preview.Projects, application.ArchaeologySelectedProjectPreview{OutcomeID: outcomeID, ProjectID: fmt.Sprintf("project-%d", index), Request: request, Preview: application.HistoricalImportResult{BatchID: request.BatchID, SourceDigest: request.SourceDigest, ManifestDigest: "sha256:" + strings.Repeat("b", 64), State: "preview", Tasks: []application.HistoricalImportTaskReceipt{{Key: fmt.Sprintf("task-%d", index), ID: strings.Repeat("i", 120), Disposition: "create"}}}})
	}
	b.session.Review = &application.ArchaeologyReview{BatchID: b.batch.BatchID, ProvenanceSummary: strings.Repeat("p", 1000), RequiresExplicitApproval: true}
	for index := 0; index < 60; index++ {
		template := b.outcome.Items[index%len(b.outcome.Items)]
		template.ID, template.ProjectID = fmt.Sprintf("session-outcome-%02d", index), fmt.Sprintf("project-%02d", index/2)
		template.Provenance = nil
		for source := 0; source < 4; source++ {
			template.Provenance = append(template.Provenance, application.ArchaeologyProvenance{SourceKind: "docs", SourceLabel: fmt.Sprintf("source-%02d-%d-%s", index, source, strings.Repeat("L", 270)), Digest: "sha256:" + strings.Repeat(string(rune('a'+source)), 64), RecordedAt: &now})
		}
		template.MemberSessions[0].SessionID = fmt.Sprintf("session-%02d-%s", index, strings.Repeat("s", 90))
		b.session.Review.ProposedOutcomes = append(b.session.Review.ProposedOutcomes, template)
		b.session.Review.MemberSessions = append(b.session.Review.MemberSessions, template.MemberSessions[0])
	}
	b.preview.BatchID, b.preview.SelectionDigest, b.preview.ManifestDigest = b.batch.BatchID, strings.Repeat("c", 64), strings.Repeat("d", 64)
	b.preview.ReviewSessionToken, b.preview.ReviewCompletionToken, b.preview.ReviewExpiresAt = strings.Repeat("r", 200), strings.Repeat("f", 200), now
	b.apply = application.ArchaeologySelectedApplyResult{BatchID: b.batch.BatchID, OutcomeIDs: append([]string(nil), b.preview.OutcomeIDs...), SelectionDigest: b.preview.SelectionDigest, ManifestDigest: b.preview.ManifestDigest, Applied: true, AuditID: strings.Repeat("a", 120)}
	b.status.Service.Version, b.status.Database.SchemaVersion = "release-test", 15
	b.status.Codex.Configured, b.status.Codex.Available, b.status.Codex.Version = true, true, "0.147.0"
	b.status.Codex.AccountState, b.status.Codex.CompatibilityStatus, b.status.Reconciliation.Status = "signed_in", "compatible", "healthy"
	h := testHandler(b, 0)
	tests := []struct{ method, target, body string }{
		{http.MethodGet, "/v1/project-archaeology", ""},
		{http.MethodGet, "/v1/project-archaeology/batches/" + b.batch.BatchID, ""},
		{http.MethodGet, "/v1/project-archaeology/batches/" + b.batch.BatchID + "/outcomes", ""},
		{http.MethodPost, "/v1/project-archaeology/batches/" + b.batch.BatchID + "/import-preview-page", `{"outcome_ids":["outcome-0"]}`},
		{http.MethodPost, "/v1/project-archaeology/batches/" + b.batch.BatchID + "/import-apply", `{"outcome_ids":["outcome-0"],"selection_digest":"x","manifest_digest":"y","review_acknowledged":true,"review_completion_token":"z"}`},
		{http.MethodGet, "/v1/installation-status", ""},
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
		req.Header.Set("Authorization", "Bearer bearer-secret")
		if test.body != "" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "budget-test")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.Len() >= 1<<20 || strings.Contains(test.target, "import-preview") && rec.Body.Len() < 150000 {
			t.Fatalf("%s %s code=%d bytes=%d body=%s", test.method, test.target, rec.Code, rec.Body.Len(), rec.Body.String())
		}
		t.Logf("%s %s serialized_bytes=%d", test.method, test.target, rec.Body.Len())
		decodeEnvelope(t, rec)
	}
}
