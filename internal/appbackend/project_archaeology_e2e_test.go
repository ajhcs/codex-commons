package appbackend

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	commonsstore "codex-commons/internal/store"
)

type unavailableNativeTransitionLauncher struct{}

func (unavailableNativeTransitionLauncher) Available(context.Context) error {
	return domain.ErrUnavailable
}
func (unavailableNativeTransitionLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, application.ArchaeologyNativeToolCall) application.ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	return domain.ArchaeologyLaunchResult{}, domain.ErrUnavailable
}
func (unavailableNativeTransitionLauncher) InterruptNative(context.Context, domain.ArchaeologyNativeJob) error {
	return domain.ErrUnavailable
}
func (unavailableNativeTransitionLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	return domain.ErrUnavailable
}

func TestArchaeologyHandoffClaimIsAgentOnlyAndExactSessionBound(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	discovered, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover"}, domain.ArchaeologyDiscovery{SourceRootsScanned: 1, Candidates: []domain.ArchaeologyCandidate{{ID: "p", Name: "Project", PathLabel: "configured-label", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	configured, err := repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "config", BaseRevision: discovered.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"p"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	started, err := repository.StartArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "start", BaseRevision: configured.Revision})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := New(legacyStub{}, application.New(repository, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	human := httpapi.RequestMeta{PrincipalKind: "human", Principal: domain.HumanLocalPrincipal, Actor: "human", Session: "human", Host: "browser", IdempotencyKey: "human-claim"}
	if _, err = backend.ClaimProjectArchaeology(ctx, application.ArchaeologyHandoffClaimRequest{HandoffID: started.Handoff.ID}, human); err == nil {
		t.Fatal("human claimed agent handoff")
	}
	agent := httpapi.RequestMeta{PrincipalKind: "agent", Actor: "historian", Session: "exact-session", Host: "codex", IdempotencyKey: "agent-claim"}
	claimed, err := backend.ClaimProjectArchaeology(ctx, application.ArchaeologyHandoffClaimRequest{HandoffID: started.Handoff.ID}, agent)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Handoff == nil || claimed.Handoff.ClaimedBy != "exact-session" {
		t.Fatalf("claimed=%+v", claimed)
	}
	other := agent
	other.Session = "other-session"
	other.IdempotencyKey = "wrong-report"
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	report := application.ArchaeologyHandoffReportEnvelope{HandoffID: started.Handoff.ID, Outcomes: []application.ArchaeologyOutcomeReportRequest{{Title: "Outcome", Summary: "Grounded", ProjectID: "p", SourceCount: 1, Provenance: []application.ArchaeologyProvenanceReportRequest{{SourceKind: "git", SourceLabel: "commit:abc", Digest: digest, RecordedAt: time.Now().UTC().Add(-time.Hour)}}, HistoricalImport: application.HistoricalImportRequest{SchemaVersion: 1}}}}
	if _, err = backend.ReportProjectArchaeology(ctx, report, other); err == nil {
		t.Fatal("non-claiming session reported result")
	}
}

func TestNativeArchaeologyPauseAndResumeRoutesFailClosedWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "native-route-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "native-route", Name: "Native route", PathLabel: "Native route", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "native-route-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"native-route"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "native-route-start", BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(repository, nil, nil)
	schedulerContext, cancelScheduler := context.WithCancel(ctx)
	cancelScheduler()
	if err = service.ConfigureNativeProjectArchaeology(schedulerContext, unavailableNativeTransitionLauncher{}, domain.HumanLocalPrincipal); err != nil {
		t.Fatal(err)
	}
	defer service.CloseProjectArchaeology()
	backend, err := New(legacyStub{}, service)
	if err != nil {
		t.Fatal(err)
	}
	meta := httpapi.RequestMeta{PrincipalKind: "human", Principal: domain.HumanLocalPrincipal, Actor: "human", Session: "human", Host: "browser", IdempotencyKey: "native-route-pause"}
	if _, err = backend.PauseProjectArchaeology(ctx, application.ArchaeologyTransitionRequest{BaseRevision: value.Revision}, meta); err == nil {
		t.Fatal("native pause route succeeded")
	}
	meta.IdempotencyKey = "native-route-resume"
	if _, err = backend.ResumeProjectArchaeology(ctx, application.ArchaeologyTransitionRequest{BaseRevision: value.Revision}, meta); err == nil {
		t.Fatal("native resume route succeeded")
	}
	after, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "running" || after.Revision != value.Revision || len(after.NativeBatches) != 1 || after.NativeBatches[0].State != "queued" || after.NativeBatches[0].Jobs[0].State != "queued" {
		t.Fatalf("after=%+v", after)
	}
}
func TestSelectedArchaeologyApplyHTTPReplaysExactAuditAfterReopen(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "selected-replay.sqlite3")
	repository, err := commonsstore.Open(ctx, dbPath, commonsstore.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateCanonicalProject(ctx, domain.CreateProjectCommand{ID: "replay-project", Name: "Replay project", Purpose: "Selected Apply replay", Meta: domain.CoreWriteMeta{ActorID: "local-admin", SessionID: "human-local-admin", RequestID: "create-replay-project"}})
	if err != nil {
		repository.Close()
		t.Fatal(err)
	}
	digest := func(character string) string { return "sha256:" + strings.Repeat(character, 64) }
	completed := now.Add(-time.Hour)
	proposal := application.HistoricalImportRequest{
		SchemaVersion: 1, BatchID: "native-replay-import", SourceDigest: digest("a"), CollisionPolicy: "current_wins",
		Tasks: []application.HistoricalTaskRequest{{
			Key: "replay-task", Title: "Durable replay task", State: "done",
			Source: application.HistoricalSourceRequest{Kind: "repository_document", StableID: "docs/replay", Digest: digest("b"), OccurredAt: completed},
			Attributions: []application.HistoricalAttributionRequest{{
				Session: "historical-replay", Role: "implementer", Confidence: "verified", Source: application.HistoricalSourceRequest{Kind: "codex_session_uuidv7", StableID: "historical-replay", Digest: digest("c"), OccurredAt: completed},
			}},
		}},
	}
	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		repository.Close()
		t.Fatal(err)
	}
	stamp := now.Format(time.RFC3339Nano)
	_, err = repository.DB().ExecContext(ctx, `
INSERT INTO archaeology_sessions(id,principal,state,discovery_state,created_at,updated_at) VALUES('replay-session','human:local-admin','completed','ready',?,?);
INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at) VALUES('replay-batch','replay-session','replay-run',zeroblob(32),'app_server_dynamic_tools','completed',2,?,?);
INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,canonical_project_id) VALUES('replay-session','replay-candidate','Replay','Replay',1,1,0,1,2,'low','','replay-project');
INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,project_name,mode,state,created_at,updated_at) VALUES('replay-job','replay-batch','replay-session','replay-candidate','replay-project','Replay','app_server_dynamic_tools','completed',?,?);`, stamp, stamp, stamp, stamp, stamp, stamp)
	if err == nil {
		_, err = repository.DB().ExecContext(ctx, `INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES('replay-outcome','replay-job','replay-project','Replay outcome','Exact source-backed proposal',1,?,?)`, string(proposalJSON), stamp)
	}
	if err != nil {
		repository.Close()
		t.Fatal(err)
	}
	newHandler := func(repo *commonsstore.Store) http.Handler {
		service := application.New(repo, nil, integrationClock{now: now})
		service.ConfigureNativeArchaeologyApply(true)
		backend, buildErr := New(legacyStub{}, service)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		return httpapi.NewHandler(backend, httpapi.Config{HumanAuth: &httpapi.HumanAuthConfig{AdminSecret: projectCoreAdminSecret, DisplayName: "Admin", Actor: "local-admin", Session: "human-local-admin", Host: "browser", SessionTTL: time.Hour, RecoveryEnabled: true}})
	}
	handler := newHandler(repository)
	cookie, csrf := loginProjectCoreHuman(t, handler)
	previewResponse := projectCoreRequest(handler, http.MethodPost, "/v1/project-archaeology/batches/replay-batch/import-preview-page", `{"outcome_ids":["replay-outcome"]}`, "", cookie, csrf, "selected-review-page")
	if previewResponse.Code != http.StatusOK {
		repository.Close()
		t.Fatalf("preview code=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}
	var previewEnvelope struct {
		Data application.ArchaeologySelectedPreview `json:"data"`
	}
	if err = json.Unmarshal(previewResponse.Body.Bytes(), &previewEnvelope); err != nil || previewEnvelope.Data.ReviewCompletionToken == "" {
		repository.Close()
		t.Fatalf("preview decode err=%v data=%+v", err, previewEnvelope.Data)
	}
	applyInput := application.ArchaeologySelectedApplyRequest{OutcomeIDs: []string{"replay-outcome"}, SelectionDigest: previewEnvelope.Data.SelectionDigest, ManifestDigest: previewEnvelope.Data.ManifestDigest, ReviewAcknowledged: true, ReviewCompletionToken: previewEnvelope.Data.ReviewCompletionToken}
	applyBody, _ := json.Marshal(applyInput)
	first := projectCoreRequest(handler, http.MethodPost, "/v1/project-archaeology/batches/replay-batch/import-apply", string(applyBody), "", cookie, csrf, "selected-apply-replay")
	if first.Code != http.StatusOK {
		repository.Close()
		t.Fatalf("first apply code=%d body=%s", first.Code, first.Body.String())
	}
	var firstEnvelope struct {
		Data application.ArchaeologySelectedApplyResult `json:"data"`
	}
	if err = json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil || !firstEnvelope.Data.Applied || firstEnvelope.Data.AuditID == "" {
		repository.Close()
		t.Fatalf("first apply decode err=%v data=%+v", err, firstEnvelope.Data)
	}
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err = commonsstore.Open(ctx, dbPath, commonsstore.WithClock(func() time.Time { return now.Add(time.Minute) }))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	handler = newHandler(repository)
	cookie, csrf = loginProjectCoreHuman(t, handler)
	replay := projectCoreRequest(handler, http.MethodPost, "/v1/project-archaeology/batches/replay-batch/import-apply", string(applyBody), "", cookie, csrf, "selected-apply-replay")
	if replay.Code != http.StatusOK {
		t.Fatalf("replay code=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayEnvelope struct {
		Data application.ArchaeologySelectedApplyResult `json:"data"`
	}
	err = json.Unmarshal(replay.Body.Bytes(), &replayEnvelope)
	firstData, replayData := firstEnvelope.Data, replayEnvelope.Data
	if err != nil || replayData.BatchID != firstData.BatchID || strings.Join(replayData.OutcomeIDs, "\x00") != strings.Join(firstData.OutcomeIDs, "\x00") || replayData.SelectionDigest != firstData.SelectionDigest || replayData.ManifestDigest != firstData.ManifestDigest || replayData.Applied != firstData.Applied || replayData.AuditID != firstData.AuditID {
		t.Fatalf("replay decode err=%v first=%+v replay=%+v", err, firstEnvelope.Data, replayEnvelope.Data)
	}
	mismatched := applyInput
	mismatched.ManifestDigest = digest("f")
	mismatchBody, _ := json.Marshal(mismatched)
	conflict := projectCoreRequest(handler, http.MethodPost, "/v1/project-archaeology/batches/replay-batch/import-apply", string(mismatchBody), "", cookie, csrf, "selected-apply-replay")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("mismatched replay code=%d body=%s", conflict.Code, conflict.Body.String())
	}
	var tasks, audits int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE project_id='replay-project'`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_imports WHERE request_key='selected-apply-replay'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || audits != 1 {
		t.Fatalf("tasks=%d audits=%d", tasks, audits)
	}
}
