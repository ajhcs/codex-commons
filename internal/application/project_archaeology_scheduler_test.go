package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

func TestHistorianReportRejectsAgentSuppliedBoundProjectIdentity(t *testing.T) {
	s := &ArchaeologyScheduler{}
	body := []byte(`{"outcomes":[{"project_id":"other","title":"x"}]}`)
	response := s.handleTool(context.Background(), domain.ArchaeologyNativeJob{ID: "job", ProjectID: "bound"}, ArchaeologyNativeToolCall{ThreadID: "thread", TurnID: "turn", Tool: "commons_project_history_report", Arguments: body})
	if response.Success {
		t.Fatal("agent-supplied project_id was accepted")
	}
}

func TestHistorianReportRejectsUnknownEnvelopeFields(t *testing.T) {
	s := &ArchaeologyScheduler{}
	body := []byte(`{"outcomes":[],"extra":"not allowed"}`)
	response := s.handleTool(context.Background(), domain.ArchaeologyNativeJob{ID: "job", ProjectID: "bound"}, ArchaeologyNativeToolCall{ThreadID: "thread", TurnID: "turn", Tool: "commons_project_history_report", Arguments: body})
	if response.Success {
		t.Fatal("unknown report field was accepted")
	}
}

func TestNativeShellSupportsCanonicalPreviewWithoutImportingTasks(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "catalog-project", Name: "Catalog Project", PathLabel: "Catalog Project", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"catalog-project"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	occurred := time.Now().UTC().Add(-time.Hour)
	service := New(repository, nil, nil)
	request := HistoricalImportRequest{
		SchemaVersion: 1, BatchID: "native-preview", SourceDigest: digest, CollisionPolicy: "current_wins",
		Tasks: []HistoricalTaskRequest{{
			Key: "done", Title: "Historical task", State: "done",
			Source: HistoricalSourceRequest{Kind: "repository_document", StableID: "doc", Digest: digest, OccurredAt: occurred},
			Attributions: []HistoricalAttributionRequest{{
				Session: "historical", Role: "implementer", Confidence: "verified",
				Source: HistoricalSourceRequest{Kind: "codex_session_uuidv7", StableID: "historical", Digest: digest, OccurredAt: occurred},
			}},
		}},
	}
	preview, err := service.PreviewHistoricalTaskImport(ctx, "catalog-project", request, ProjectCoreActor{})
	if err != nil || preview.Applied {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	var tasks int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 {
		t.Fatalf("preview imported tasks=%d", tasks)
	}
}

func TestApplicationQueuedOnlyCancelReturnsRestartableCanonicalView(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "queued-cancel-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "queued-cancel", Name: "Queued cancel", PathLabel: "Queued cancel", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "queued-cancel-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"queued-cancel"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "queued-cancel-start", BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	service := New(repository, nil, nil)
	scheduler := &ArchaeologyScheduler{service: service, repository: repository, launcher: capabilityNativeLauncher{}, principal: domain.HumanLocalPrincipal, ctx: ctx, wake: make(chan struct{}, 1)}
	service.archaeologyScheduler = scheduler
	service.archaeologyLauncher = scheduler
	canceled, err := service.CancelProjectArchaeology(ctx, domain.HumanLocalPrincipal, "queued-cancel", ArchaeologyTransitionRequest{BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if canceled.State != "draft" || canceled.Handoff == nil || canceled.Handoff.State != "canceled" || canceled.Handoff.Progress.FailedCount != 1 || !canceled.Controls.CanStart || canceled.Controls.CanCancel {
		t.Fatalf("canceled=%+v", canceled)
	}
	replay, err := service.CancelProjectArchaeology(ctx, domain.HumanLocalPrincipal, "queued-cancel", ArchaeologyTransitionRequest{BaseRevision: value.Revision})
	if err != nil || replay.Revision != canceled.Revision || replay.Handoff == nil || replay.Handoff.State != "canceled" {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}

func TestApplicationNativePauseAndResumeFailClosedWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "native-transition-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "native-transition", Name: "Native transition", PathLabel: "Native transition", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "native-transition-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"native-transition"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "native-transition-start", BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	service := New(repository, nil, nil)
	service.archaeologyScheduler = &ArchaeologyScheduler{}
	beforeRevision := value.Revision
	if _, err = service.PauseProjectArchaeology(ctx, domain.HumanLocalPrincipal, "native-pause", ArchaeologyTransitionRequest{BaseRevision: beforeRevision}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("pause err=%v", err)
	}
	if _, err = service.ResumeProjectArchaeology(ctx, domain.HumanLocalPrincipal, "native-resume", ArchaeologyTransitionRequest{BaseRevision: beforeRevision}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("resume err=%v", err)
	}
	after, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "running" || after.Revision != beforeRevision || len(after.NativeBatches) != 1 || after.NativeBatches[0].State != "queued" || after.NativeBatches[0].Jobs[0].State != "queued" {
		t.Fatalf("after=%+v", after)
	}
}

func TestPersistedNativeBatchBlocksLegacyControlPlaneAfterFeatureToggle(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "toggle-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "toggle", Name: "Toggle", PathLabel: "Toggle", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "toggle-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"toggle"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "toggle-native-start", BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	service := New(repository, nil, nil) // native scheduler disabled after restart
	for name, transition := range map[string]func() (ArchaeologySession, error){
		"start": func() (ArchaeologySession, error) {
			return service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "toggle-legacy-start", ArchaeologyTransitionRequest{BaseRevision: value.Revision})
		},
		"pause": func() (ArchaeologySession, error) {
			return service.PauseProjectArchaeology(ctx, domain.HumanLocalPrincipal, "toggle-legacy-pause", ArchaeologyTransitionRequest{BaseRevision: value.Revision})
		},
		"resume": func() (ArchaeologySession, error) {
			return service.ResumeProjectArchaeology(ctx, domain.HumanLocalPrincipal, "toggle-legacy-resume", ArchaeologyTransitionRequest{BaseRevision: value.Revision})
		},
		"cancel": func() (ArchaeologySession, error) {
			return service.CancelProjectArchaeology(ctx, domain.HumanLocalPrincipal, "toggle-legacy-cancel", ArchaeologyTransitionRequest{BaseRevision: value.Revision})
		},
	} {
		if _, transitionErr := transition(); !errors.Is(transitionErr, domain.ErrUnavailable) {
			t.Fatalf("%s err=%v", name, transitionErr)
		}
	}
	after, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "running" || after.Revision != value.Revision || len(after.NativeBatches) != 1 || after.NativeBatches[0].State != "queued" || after.NativeBatches[0].Jobs[0].State != "queued" || after.Handoff != nil || len(after.Runs) != 0 || len(after.TaskLaunches) != 0 {
		t.Fatalf("after=%+v", after)
	}
	view, err := service.ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if view.Controls.CanStart || view.Controls.CanPause || view.Controls.CanResume || view.Controls.CanCancel {
		t.Fatalf("feature-disabled controls=%+v", view.Controls)
	}
}

type identityRecoveryLauncher struct {
	launch domain.ArchaeologyLaunchResult
	exact  bool
	err    error
	calls  int
}

func (*identityRecoveryLauncher) Available(context.Context) error { return nil }
func (*identityRecoveryLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	panic("identity recovery must not launch a task")
}
func (*identityRecoveryLauncher) InterruptNative(context.Context, domain.ArchaeologyNativeJob) error {
	panic("identity recovery must not interrupt a task")
}
func (*identityRecoveryLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	panic("identity recovery must not finalize a task")
}
func (l *identityRecoveryLauncher) RecoverNativeIdentity(_ context.Context, _ domain.ArchaeologyNativeJob, _ domain.ArchaeologyCandidate) (domain.ArchaeologyLaunchResult, bool, error) {
	l.calls++
	return l.launch, l.exact, l.err
}

func TestNativeStartupRecoveryBindsOnlyUniqueExactIdentityWithoutRelaunch(t *testing.T) {
	for _, test := range []struct {
		name  string
		exact bool
		err   error
	}{
		{name: "unique exact match", exact: true},
		{name: "zero or multiple matches remain blocked"},
		{name: "lookup unavailable remains blocked", err: errors.New("lookup unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repository, err := commonsstore.Open(ctx, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "recover-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "recover", Name: "Recover", PathLabel: "Recover", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
			if err != nil {
				t.Fatal(err)
			}
			value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "recover-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"recover"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "recover-start", BaseRevision: value.Revision}); err != nil {
				t.Fatal(err)
			}
			job, err := repository.ClaimArchaeologyNativeJob(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = repository.FailArchaeologyNativeStart(ctx, job.ID, domain.ArchaeologyLaunchResult{}, true); err != nil {
				t.Fatal(err)
			}
			launcher := &identityRecoveryLauncher{launch: domain.ArchaeologyLaunchResult{ThreadID: "thread-recovered", CodexSessionID: "session-recovered", TurnID: "turn-recovered"}, exact: test.exact, err: test.err}
			if err = reconcileArchaeologyNativeIdentities(ctx, repository, launcher, domain.HumanLocalPrincipal); err != nil {
				t.Fatal(err)
			}
			if launcher.calls != 1 {
				t.Fatalf("lookup calls=%d", launcher.calls)
			}
			got, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			recovered := got.NativeBatches[0].Jobs[0]
			if test.exact && test.err == nil {
				if recovered.ThreadID != "thread-recovered" || recovered.CodexSessionID != "session-recovered" || recovered.TurnID != "turn-recovered" || recovered.State != "uncertain" {
					t.Fatalf("recovered=%+v", recovered)
				}
				if err = reconcileArchaeologyNativeIdentities(ctx, repository, launcher, domain.HumanLocalPrincipal); err != nil || launcher.calls != 1 {
					t.Fatalf("replay err=%v calls=%d", err, launcher.calls)
				}
			} else if recovered.ThreadID != "" || recovered.CodexSessionID != "" || recovered.TurnID != "" || recovered.State != "uncertain" {
				t.Fatalf("non-exact recovery mutated identity: %+v", recovered)
			}
		})
	}
}

func TestHistorianMinimalToolPayloadRoundTripsCanonicalPreviewAndNativeStore(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "roundtrip-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "roundtrip", Name: "Roundtrip", PathLabel: "Roundtrip", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "roundtrip-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"roundtrip"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "roundtrip-start", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	job, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.BindArchaeologyNativeJob(ctx, job.ID, "thread-roundtrip", "session-roundtrip", "turn-roundtrip"); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	stable := "commit:" + strings.Repeat("a", 40)
	occurred := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	body := []byte(`{"outcomes":[{"title":"Verified history","summary":"Source-grounded","source_count":1,"provenance":[{"source_kind":"git","source_label":"` + stable + `","digest":"` + digest + `","recorded_at":"` + occurred + `"}],"contributors":[{"session_id":"session-roundtrip","contribution":"Implemented","confidence":"verified"}],"historical_import":{"schema_version":1,"source_digest":"` + digest + `","collision_policy":"current_wins","project_thread_aliases":[],"tasks":[{"key":"done","title":"Historical task","state":"done","source":{"kind":"git","stable_id":"` + stable + `","digest":"` + digest + `","occurred_at":"` + occurred + `"},"attributions":[{"session":"session-roundtrip","role":"implementer","confidence":"verified","source":{"kind":"git","stable_id":"` + stable + `","digest":"` + digest + `","occurred_at":"` + occurred + `"}}],"events":[]}]}}]}`)
	service := New(repository, nil, nil)
	scheduler := &ArchaeologyScheduler{service: service, repository: repository}
	response := scheduler.handleTool(ctx, job, ArchaeologyNativeToolCall{ThreadID: "thread-roundtrip", TurnID: "turn-roundtrip", Tool: "commons_project_history_report", Arguments: body})
	if !response.Success {
		t.Fatal("minimal generated report was rejected")
	}
	canonical, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil || len(canonical.Outcomes) != 1 {
		t.Fatalf("outcomes=%+v err=%v", canonical.Outcomes, err)
	}
	if canonical.Outcomes[0].ProposalJSON != "" {
		t.Fatal("global session read exposed proposal body")
	}
	var proposal string
	if err = repository.DB().QueryRowContext(ctx, `SELECT proposal_json FROM archaeology_native_outcomes WHERE job_id=?`, job.ID).Scan(&proposal); err != nil || !strings.Contains(proposal, `"batch_id":"native-`) || strings.Contains(proposal, job.BatchID) {
		t.Fatalf("proposal batch identity was not safely server-bound: %s err=%v", proposal, err)
	}
	var taskCount int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks`).Scan(&taskCount); err != nil || taskCount != 0 {
		t.Fatalf("canonical tasks=%d err=%v", taskCount, err)
	}
}

func maximalHistorianReport(t *testing.T) []byte {
	t.Helper()
	occurred := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	source := func(index int) map[string]any {
		stable := fmt.Sprintf("docs/%s-%02d", strings.Repeat("a", 291), index)
		return map[string]any{"kind": "docs", "stable_id": stable, "digest": fmt.Sprintf("sha256:%064x", index+1), "occurred_at": occurred}
	}
	outcomes := make([]any, 0, 2)
	for outcomeIndex := 0; outcomeIndex < 2; outcomeIndex++ {
		sources := []map[string]any{source(outcomeIndex*4 + 0), source(outcomeIndex*4 + 1), source(outcomeIndex*4 + 2), source(outcomeIndex*4 + 3)}
		provenance := make([]any, 0, len(sources))
		for _, item := range sources {
			provenance = append(provenance, map[string]any{"source_kind": item["kind"], "source_label": item["stable_id"], "digest": item["digest"], "recorded_at": item["occurred_at"]})
		}
		aliases := make([]any, 0, 3)
		for index := 0; index < 3; index++ {
			aliases = append(aliases, map[string]any{"alias": fmt.Sprintf("alias-%d-%d", outcomeIndex, index), "session": fmt.Sprintf("alias-session-%d-%d-%s", outcomeIndex, index, strings.Repeat("a", 30)), "source": sources[index]})
		}
		contributor := fmt.Sprintf("contributor-%d-%s", outcomeIndex, strings.Repeat("c", 35))
		tasks := make([]any, 0, 4)
		for taskIndex := 0; taskIndex < 4; taskIndex++ {
			primary := sources[taskIndex%len(sources)]
			secondary := sources[(taskIndex+1)%len(sources)]
			tasks = append(tasks, map[string]any{
				"key": fmt.Sprintf("task-%d-%d", outcomeIndex, taskIndex), "title": strings.Repeat("T", 75),
				"description": strings.Repeat("D", 200), "acceptance": strings.Repeat("A", 200), "state": "done", "source": primary,
				"attributions": []any{
					map[string]any{"session": contributor, "role": "implementer", "confidence": "verified", "source": primary},
					map[string]any{"session": contributor, "role": "reviewer", "confidence": "supported", "source": secondary},
				},
				"events": []any{map[string]any{"key": fmt.Sprintf("event-%d-%d", outcomeIndex, taskIndex), "kind": "reviewed", "summary": strings.Repeat("E", 200), "session": contributor, "confidence": "supported", "source": secondary}},
			})
		}
		outcomes = append(outcomes, map[string]any{
			"title": fmt.Sprintf("%s-%d", strings.Repeat("H", 73), outcomeIndex), "summary": strings.Repeat("S", 300), "source_count": 1000,
			"provenance":        provenance,
			"contributors":      []any{map[string]any{"session_id": contributor, "contribution": strings.Repeat("C", 250), "demonstrated_strength": strings.Repeat("G", 75), "uncertainty": strings.Repeat("U", 125), "confidence": "verified"}},
			"historical_import": map[string]any{"schema_version": 1, "source_digest": sources[0]["digest"], "collision_policy": "current_wins", "project_thread_aliases": aliases, "tasks": tasks},
		})
	}
	body, err := json.Marshal(map[string]any{"outcomes": outcomes})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestHistorianMaximumSchemaReportRoundTripsAndBoundariesReject(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "max-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "maximum", Name: "Maximum", PathLabel: "Maximum", HasDocs: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "max-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"maximum"}, Depth: "deep", Sources: domain.ArchaeologySources{Docs: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "max-start", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	job, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.BindArchaeologyNativeJob(ctx, job.ID, "thread-max", "session-max", "turn-max"); err != nil {
		t.Fatal(err)
	}
	body := maximalHistorianReport(t)
	if len(body) >= domain.ArchaeologyNativeReportMaxBytes || len(body) < 24<<10 {
		t.Fatalf("maximum schema-shaped report=%d bytes", len(body))
	}
	service := New(repository, nil, nil)
	scheduler := &ArchaeologyScheduler{service: service, repository: repository}
	response := scheduler.handleTool(ctx, job, ArchaeologyNativeToolCall{ThreadID: "thread-max", TurnID: "turn-max", Tool: "commons_project_history_report", Arguments: body})
	if !response.Success {
		t.Fatal("maximum schema-shaped report was rejected")
	}
	canonical, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil || len(canonical.Outcomes) != 2 {
		t.Fatalf("outcomes=%d err=%v", len(canonical.Outcomes), err)
	}
	for _, outcome := range canonical.Outcomes {
		if len(outcome.ProposalJSON) > domain.ArchaeologyNativeProposalMaxBytes {
			t.Fatalf("proposal=%d bytes", len(outcome.ProposalJSON))
		}
	}
	view, err := service.ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil || view.Review == nil || view.Review.CanApply || view.Capabilities.CanonicalApply.Available {
		t.Fatalf("native review must be review-only: %+v err=%v", view.Review, err)
	}
	overBytes := append(append([]byte(nil), body...), make([]byte, domain.ArchaeologyNativeReportMaxBytes-len(body)+1)...)
	if scheduler.handleTool(ctx, job, ArchaeologyNativeToolCall{ThreadID: "thread-max", TurnID: "turn-max", Tool: "commons_project_history_report", Arguments: overBytes}).Success {
		t.Fatal("one-byte-over report was accepted")
	}
	var decoded map[string]any
	if err = json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	outcomes := decoded["outcomes"].([]any)
	decoded["outcomes"] = append(outcomes, outcomes[0])
	overItems, _ := json.Marshal(decoded)
	if scheduler.handleTool(ctx, job, ArchaeologyNativeToolCall{ThreadID: "thread-max", TurnID: "turn-max", Tool: "commons_project_history_report", Arguments: overItems}).Success {
		t.Fatal("one-item-over report was accepted")
	}
}

type immediateEarlyReportLauncher struct {
	body     []byte
	response chan bool
}

func (*immediateEarlyReportLauncher) Available(context.Context) error { return nil }
func (*immediateEarlyReportLauncher) InterruptNative(context.Context, domain.ArchaeologyNativeJob) error {
	return nil
}
func (*immediateEarlyReportLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	return nil
}
func (l *immediateEarlyReportLauncher) LaunchNative(_ context.Context, job domain.ArchaeologyNativeJob, _ domain.ArchaeologySession, _ domain.ArchaeologyCandidate, onTool func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, onTerminal func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	go func() {
		response := onTool(context.Background(), ArchaeologyNativeToolCall{ThreadID: "thread-early-report", TurnID: "turn-early-report", Tool: "commons_project_history_report", Arguments: l.body})
		l.response <- response.Success
		onTerminal(domain.ArchaeologyNativeTerminal{ThreadID: "thread-early-report", TurnID: "turn-early-report", Status: "completed"})
	}()
	return domain.ArchaeologyLaunchResult{LaunchID: job.ID, ProjectID: job.ProjectID, ThreadID: "thread-early-report", CodexSessionID: "session-early-report", TurnID: "turn-early-report"}, nil
}

func TestImmediatePreBindReportBindsPersistsAndCompletesWithoutLoss(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "early-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "early", Name: "Early", PathLabel: "Early", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "early-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"early"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "early-start", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	job, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("b", 64)
	stable := "commit:" + strings.Repeat("b", 40)
	occurred := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	body := []byte(`{"outcomes":[{"title":"Early report","summary":"Bound first","source_count":1,"provenance":[{"source_kind":"git","source_label":"` + stable + `","digest":"` + digest + `","recorded_at":"` + occurred + `"}],"contributors":[],"historical_import":{"schema_version":1,"source_digest":"` + digest + `","collision_policy":"current_wins","project_thread_aliases":[],"tasks":[{"key":"early","title":"Early task","state":"done","source":{"kind":"git","stable_id":"` + stable + `","digest":"` + digest + `","occurred_at":"` + occurred + `"},"attributions":[{"session":"session-early-report","role":"implementer","confidence":"verified","source":{"kind":"git","stable_id":"` + stable + `","digest":"` + digest + `","occurred_at":"` + occurred + `"}}],"events":[]}]}}]}`)
	launcher := &immediateEarlyReportLauncher{body: body, response: make(chan bool, 1)}
	service := New(repository, nil, nil)
	scheduler := &ArchaeologyScheduler{service: service, repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: ctx, wake: make(chan struct{}, 1)}
	scheduler.launch(job)
	select {
	case accepted := <-launcher.response:
		if !accepted {
			t.Fatal("early report was rejected after bind")
		}
	case <-time.After(time.Second):
		t.Fatal("early report stalled behind bind")
	}
	deadline := time.Now().Add(time.Second)
	for {
		canonical, readErr := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(canonical.NativeBatches) == 1 && canonical.NativeBatches[0].Jobs[0].State == "completed" {
			if len(canonical.Outcomes) != 1 || canonical.NativeBatches[0].Jobs[0].ThreadID != "thread-early-report" {
				t.Fatalf("canonical=%+v", canonical)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("early terminal/report did not reconcile: %+v", canonical.NativeBatches)
		}
		time.Sleep(time.Millisecond)
	}
	scheduler.callbackWG.Wait()
}

func TestNativeTaskIdentityPersistsInFirstAndSubsequentCanonicalViews(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "poll-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "poll-project", Name: "Poll Project", PathLabel: "Poll Project", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "poll-configure", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"poll-project"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "poll-start", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	job, err := repository.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.BindArchaeologyNativeJob(ctx, job.ID, "thread-persisted", "session-private", "turn-persisted"); err != nil {
		t.Fatal(err)
	}
	service := New(repository, nil, nil)
	for poll := 1; poll <= 3; poll++ {
		view, readErr := service.ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
		if readErr != nil {
			t.Fatalf("poll %d: %v", poll, readErr)
		}
		if view.Handoff == nil || view.Handoff.Concurrency != 1 || len(view.Handoff.Tasks) != 1 {
			t.Fatalf("poll %d: handoff=%+v", poll, view.Handoff)
		}
		task := view.Handoff.Tasks[0]
		if task.State != "active" || task.ThreadID != "thread-persisted" || task.TurnID != "turn-persisted" || task.PhaseLabel != "Visible in Codex" {
			t.Fatalf("poll %d: task=%+v", poll, task)
		}
	}
}

func TestArchaeologyViewHidesStartUntilLegacyCatalogIsRefreshed(t *testing.T) {
	value := domain.ArchaeologySession{State: "draft", Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"legacy"}}, Candidates: []domain.ArchaeologyCandidate{{ID: "legacy"}}}
	if archaeologyView(value).Controls.CanStart {
		t.Fatal("legacy unmapped candidate exposed Start")
	}
	value.Candidates[0].CanonicalProjectID = "legacy"
	if !archaeologyView(value).Controls.CanStart {
		t.Fatal("refreshed candidate did not expose Start")
	}
}

func TestNativeProjectionRetriesOnlyTerminalAttentionWithoutUncertainty(t *testing.T) {
	base := domain.ArchaeologySession{State: "draft", Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"mapped"}}, Candidates: []domain.ArchaeologyCandidate{{ID: "mapped", CanonicalProjectID: "mapped"}}, NativeBatches: []domain.ArchaeologyNativeBatch{{ID: "batch", State: "attention", MaxConcurrency: 1, PolicyAttested: true, Policy: domain.ArchaeologyExecutionPolicy{Depth: "quick", Sources: domain.ArchaeologySources{Git: true}}, Jobs: []domain.ArchaeologyNativeJob{{ID: "job", CandidateID: "mapped", ProjectID: "mapped", State: "failed"}}}}}
	if !archaeologyView(base).Controls.CanStart {
		t.Fatal("proven terminal start failure stranded the next run")
	}
	base.State = "running"
	base.NativeBatches[0].Jobs[0].State = "uncertain"
	if view := archaeologyView(base); view.Controls.CanStart || view.Handoff == nil || len(view.Handoff.AllowedActions) != 0 || len(view.Handoff.Tasks) != 1 || len(view.Handoff.Tasks[0].AvailableActions) != 0 {
		t.Fatalf("uncertain work was not fail-closed: %+v", view)
	}
}

type bindFailureRepository struct {
	ArchaeologyNativeRepository
	failed    bool
	uncertain bool
	launch    domain.ArchaeologyLaunchResult
}

func (r *bindFailureRepository) ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error) {
	return domain.ArchaeologySession{Candidates: []domain.ArchaeologyCandidate{{ID: "candidate", Name: "Candidate"}}}, nil
}

func (r *bindFailureRepository) BindArchaeologyNativeJob(context.Context, string, string, string, string) error {
	return domain.ErrConflict
}
func (r *bindFailureRepository) BindArchaeologyNativeIdentity(context.Context, string, string, string, string) error {
	return domain.ErrConflict
}

func (r *bindFailureRepository) FailArchaeologyNativeStart(_ context.Context, _ string, launch domain.ArchaeologyLaunchResult, uncertain bool) error {
	r.failed = true
	r.uncertain = uncertain
	r.launch = launch
	return nil
}

type acceptedThenBindFailureLauncher struct {
	ArchaeologyNativeLauncher
	interrupted bool
}

func (acceptedThenBindFailureLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	return domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}, nil
}

func (l *acceptedThenBindFailureLauncher) InterruptNative(_ context.Context, job domain.ArchaeologyNativeJob) error {
	l.interrupted = job.ThreadID == "thread" && job.TurnID == "turn"
	return nil
}
func (*acceptedThenBindFailureLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	return nil
}

func TestNativeAcceptedTaskBecomesUncertainWhenDurableBindFails(t *testing.T) {
	repository := &bindFailureRepository{}
	launcher := &acceptedThenBindFailureLauncher{}
	scheduler := &ArchaeologyScheduler{
		service:    &Service{},
		repository: repository,
		launcher:   launcher,
		principal:  domain.HumanLocalPrincipal,
		ctx:        context.Background(),
	}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	if !repository.failed || !repository.uncertain || repository.launch.ThreadID != "thread" || repository.launch.TurnID != "turn" || !launcher.interrupted {
		t.Fatalf("failed=%v uncertain=%v launch=%+v interrupted=%v", repository.failed, repository.uncertain, repository.launch, launcher.interrupted)
	}
}

type orderedNativeRepository struct {
	ArchaeologyNativeRepository
	calls             []string
	failed, uncertain bool
}

func (r *orderedNativeRepository) ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error) {
	return domain.ArchaeologySession{Candidates: []domain.ArchaeologyCandidate{{ID: "candidate", Name: "Commons"}}}, nil
}
func (r *orderedNativeRepository) BindArchaeologyNativeIdentity(context.Context, string, string, string, string) error {
	r.calls = append(r.calls, "bind")
	return nil
}
func (r *orderedNativeRepository) ActivateArchaeologyNativeJob(context.Context, string, string, string) error {
	r.calls = append(r.calls, "activate")
	return nil
}
func (r *orderedNativeRepository) FailArchaeologyNativeStart(_ context.Context, _ string, _ domain.ArchaeologyLaunchResult, uncertain bool) error {
	r.calls = append(r.calls, "uncertain")
	r.failed = true
	r.uncertain = uncertain
	return nil
}

type orderedNativeLauncher struct {
	ArchaeologyNativeLauncher
	calls                *[]string
	finalizeErr          error
	launches, interrupts int
}

func (l *orderedNativeLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	l.launches++
	*l.calls = append(*l.calls, "launch")
	return domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}, nil
}
func (l *orderedNativeLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	*l.calls = append(*l.calls, "finalize")
	return l.finalizeErr
}
func (l *orderedNativeLauncher) InterruptNative(context.Context, domain.ArchaeologyNativeJob) error {
	l.interrupts++
	*l.calls = append(*l.calls, "interrupt")
	return nil
}
func TestNativeLaunchBindsThenFinalizesThenActivates(t *testing.T) {
	r := &orderedNativeRepository{}
	l := &orderedNativeLauncher{calls: &r.calls}
	s := &ArchaeologyScheduler{service: &Service{}, repository: r, launcher: l, principal: domain.HumanLocalPrincipal, ctx: context.Background()}
	s.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	if got := strings.Join(r.calls, ","); got != "launch,bind,finalize,activate" {
		t.Fatalf("order=%s", got)
	}
}
func TestNativeFinalizeFailureIsDurablyUncertainAndInterruptedOnce(t *testing.T) {
	r := &orderedNativeRepository{}
	l := &orderedNativeLauncher{calls: &r.calls, finalizeErr: errors.New("readback mismatch")}
	s := &ArchaeologyScheduler{service: &Service{}, repository: r, launcher: l, principal: domain.HumanLocalPrincipal, ctx: context.Background()}
	s.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	if got := strings.Join(r.calls, ","); got != "launch,bind,finalize,uncertain,interrupt" {
		t.Fatalf("order=%s", got)
	}
	if !r.failed || !r.uncertain || l.launches != 1 || l.interrupts != 1 {
		t.Fatalf("failed=%v uncertain=%v launches=%d interrupts=%d", r.failed, r.uncertain, l.launches, l.interrupts)
	}
}

type acceptedRequestLostResponseLauncher struct {
	ArchaeologyNativeLauncher
}

func (acceptedRequestLostResponseLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	return domain.ArchaeologyLaunchResult{State: "uncertain"}, errors.New("response lost")
}

func TestNativeAcceptedRequestWithoutResponseBecomesUncertain(t *testing.T) {
	repository := &bindFailureRepository{}
	scheduler := &ArchaeologyScheduler{
		service:    &Service{},
		repository: repository,
		launcher:   acceptedRequestLostResponseLauncher{},
		principal:  domain.HumanLocalPrincipal,
		ctx:        context.Background(),
	}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	if !repository.failed || !repository.uncertain {
		t.Fatalf("failed=%v uncertain=%v", repository.failed, repository.uncertain)
	}
}

type postTurnLaunchFailureLauncher struct {
	ArchaeologyNativeLauncher
	launches     int
	interrupts   int
	interruptErr error
	interrupted  domain.ArchaeologyNativeJob
}

func (l *postTurnLaunchFailureLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	l.launches++
	return domain.ArchaeologyLaunchResult{ThreadID: "thread-post-turn", CodexSessionID: "session-post-turn", TurnID: "turn-post-turn", State: "uncertain", Error: "visibility"}, errors.New("post-turn visibility failed")
}

func (l *postTurnLaunchFailureLauncher) InterruptNative(_ context.Context, job domain.ArchaeologyNativeJob) error {
	l.interrupts++
	l.interrupted = job
	return l.interruptErr
}

func TestNativePostTurnLaunchFailurePersistsIdentityAndInterruptsExactlyOnce(t *testing.T) {
	for _, test := range []struct {
		name         string
		interruptErr error
	}{
		{name: "interrupt succeeds"},
		{name: "interrupt failure remains durable uncertain", interruptErr: errors.New("transport unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &bindFailureRepository{}
			launcher := &postTurnLaunchFailureLauncher{interruptErr: test.interruptErr}
			scheduler := &ArchaeologyScheduler{service: &Service{}, repository: repository, launcher: launcher, principal: domain.HumanLocalPrincipal, ctx: context.Background()}
			scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
			if !repository.failed || !repository.uncertain || repository.launch.ThreadID != "thread-post-turn" || repository.launch.CodexSessionID != "session-post-turn" || repository.launch.TurnID != "turn-post-turn" {
				t.Fatalf("failed=%v uncertain=%v launch=%+v", repository.failed, repository.uncertain, repository.launch)
			}
			if launcher.launches != 1 || launcher.interrupts != 1 {
				t.Fatalf("non-idempotent replay: launches=%d interrupts=%d", launcher.launches, launcher.interrupts)
			}
			if launcher.interrupted.ThreadID != "thread-post-turn" || launcher.interrupted.CodexSessionID != "session-post-turn" || launcher.interrupted.TurnID != "turn-post-turn" {
				t.Fatalf("interrupt identity=%+v", launcher.interrupted)
			}
		})
	}
}

func TestArchaeologySchedulerCloseJoinsAcceptedCallbacks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := &ArchaeologyScheduler{ctx: ctx, cancel: cancel}
	if !scheduler.beginCallback() {
		t.Fatal("callback was rejected before close")
	}
	release := make(chan struct{})
	go func() {
		<-release
		scheduler.callbackWG.Done()
	}()
	closed := make(chan struct{})
	go func() {
		scheduler.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("close returned before the accepted callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not join the callback")
	}
	if scheduler.beginCallback() {
		t.Fatal("callback accepted after close")
	}
}

type cancelAllNativeRepository struct {
	ArchaeologyNativeRepository
	jobs  []domain.ArchaeologyNativeJob
	value domain.ArchaeologySession
}

func (r *cancelAllNativeRepository) CancelArchaeologyNativeBatch(context.Context, string, string, int64) ([]domain.ArchaeologyNativeJob, domain.ArchaeologySession, error) {
	return append([]domain.ArchaeologyNativeJob(nil), r.jobs...), r.value, nil
}

func (r *cancelAllNativeRepository) ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error) {
	return r.value, nil
}

type cancelAllNativeLauncher struct {
	ArchaeologyNativeLauncher
	identities []string
}

func (l *cancelAllNativeLauncher) InterruptNative(_ context.Context, job domain.ArchaeologyNativeJob) error {
	l.identities = append(l.identities, job.ThreadID+"|"+job.TurnID)
	return nil
}

func TestNativeSchedulerCancelInterruptsEveryReturnedExactIdentity(t *testing.T) {
	jobs := make([]domain.ArchaeologyNativeJob, 9)
	for index := range jobs {
		jobs[index] = domain.ArchaeologyNativeJob{
			ID:       fmt.Sprintf("job-%d", index),
			ThreadID: fmt.Sprintf("thread-%d", index),
			TurnID:   fmt.Sprintf("turn-%d", index),
		}
	}
	repository := &cancelAllNativeRepository{jobs: jobs, value: domain.ArchaeologySession{ID: "session", State: "cancel_requested"}}
	launcher := &cancelAllNativeLauncher{}
	scheduler := &ArchaeologyScheduler{repository: repository, launcher: launcher, wake: make(chan struct{}, 1)}
	value, err := scheduler.Cancel(context.Background(), domain.HumanLocalPrincipal, "cancel-nine", 4)
	if err != nil {
		t.Fatal(err)
	}
	if value.ID != "session" || len(launcher.identities) != 9 {
		t.Fatalf("value=%+v identities=%v", value, launcher.identities)
	}
	for index, identity := range launcher.identities {
		if identity != fmt.Sprintf("thread-%d|turn-%d", index, index) {
			t.Fatalf("identity[%d]=%q", index, identity)
		}
	}
}
