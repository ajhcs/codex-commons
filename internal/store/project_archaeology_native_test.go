package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/migrations"
	_ "modernc.org/sqlite"
)

func nativeTestOutcome(t *testing.T, project string) domain.ArchaeologyOutcome {
	t.Helper()
	occurred := time.Now().UTC().Add(-time.Hour)
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	stableID := "commit:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	proposal, err := json.Marshal(map[string]any{
		"schema_version": 1, "batch_id": "native-test", "source_digest": digest, "collision_policy": "current_wins", "project_thread_aliases": []any{},
		"tasks": []any{map[string]any{
			"key": "done", "title": "Historical task", "state": "done",
			"source":       map[string]any{"kind": "git", "stable_id": stableID, "digest": digest, "occurred_at": occurred},
			"attributions": []any{map[string]any{"session": "historian-session", "role": "implementer", "confidence": "verified", "source": map[string]any{"kind": "git", "stable_id": stableID, "digest": digest, "occurred_at": occurred}}},
			"events":       []any{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return domain.ArchaeologyOutcome{ID: "outcome", Title: "Report", Summary: "Grounded", ProjectID: project, SourceCount: 1, ProposalJSON: string(proposal), Provenance: []domain.ArchaeologyProvenance{{Kind: "git", StableID: stableID, Digest: digest, OccurredAt: occurred}}}
}

func nativeTestSession(t *testing.T, count int, maximum int) (*Store, domain.ArchaeologySession) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	candidates := make([]domain.ArchaeologyCandidate, 0, count)
	for i := 0; i < count; i++ {
		id := string(rune('a' + i))
		if i >= 26 {
			id = "project-" + strconv.Itoa(i)
		}
		candidates = append(candidates, domain.ArchaeologyCandidate{ID: id, Name: "Project " + id, PathLabel: "Project " + id, HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low", PrivacyNote: "Metadata only."})
	}
	value, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover"}, domain.ArchaeologyDiscovery{Candidates: candidates})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, count)
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: ids, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: maximum}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start-1", BaseRevision: value.Revision, AcknowledgeLargeBatch: count > 5})
	if err != nil {
		t.Fatal(err)
	}
	return s, value
}

func TestNativeLargeBatchRequiresImmutableServerAcknowledgement(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	candidates, ids := make([]domain.ArchaeologyCandidate, 0, 6), make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		ids = append(ids, id)
		candidates = append(candidates, domain.ArchaeologyCandidate{ID: id, Name: "Project " + id, PathLabel: "Project " + id, HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"})
	}
	value, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "large-discover"}, domain.ArchaeologyDiscovery{Candidates: candidates})
	if err != nil {
		t.Fatal(err)
	}
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "large-config", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: ids, Depth: "deep", Sources: domain.ArchaeologySources{Git: true, Docs: true, CodexHistory: true}, MaxConcurrency: 2}})
	if err != nil {
		t.Fatal(err)
	}
	command := domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "large-start", BaseRevision: value.Revision}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, command); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unacknowledged start err=%v", err)
	}
	var batches, jobs, projects int
	for query, target := range map[string]*int{"SELECT count(*) FROM archaeology_native_batches": &batches, "SELECT count(*) FROM archaeology_native_jobs": &jobs, "SELECT count(*) FROM projects": &projects} {
		if err = s.db.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if batches != 0 || jobs != 0 || projects != 0 {
		t.Fatalf("unacknowledged mutation batches=%d jobs=%d projects=%d", batches, jobs, projects)
	}
	command.AcknowledgeLargeBatch = true
	started, err := s.QueueArchaeologyNativeBatch(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	batch := started.NativeBatches[0]
	if len(batch.Jobs) != 6 || batch.MaxConcurrency != 2 || batch.Policy.Depth != "deep" || !batch.Policy.Sources.Git || !batch.Policy.Sources.Docs || !batch.Policy.Sources.CodexHistory || batch.LargeBatchAcknowledgedAt.IsZero() || batch.LargeBatchAcknowledgedBy != domain.HumanLocalPrincipal {
		t.Fatalf("batch=%+v", batch)
	}
	replay, err := s.QueueArchaeologyNativeBatch(ctx, command)
	if err != nil || replay.NativeBatches[0].ID != batch.ID || len(replay.NativeBatches) != 1 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	changed := command
	changed.AcknowledgeLargeBatch = false
	if _, err = s.QueueArchaeologyNativeBatch(ctx, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed acknowledgement err=%v", err)
	}
}

func TestNativeSchedulerSubmitsBeyondTwoAndUncertainGateStillCloses(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 2)
	otherPrincipal := "human:other"
	other, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: otherPrincipal, RequestID: "other-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{
		{ID: "other-a", Name: "Other A", PathLabel: "Other A", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low", PrivacyNote: "Metadata only."},
		{ID: "other-b", Name: "Other B", PathLabel: "Other B", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low", PrivacyNote: "Metadata only."},
		{ID: "other-c", Name: "Other C", PathLabel: "Other C", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low", PrivacyNote: "Metadata only."},
	}})
	if err != nil {
		t.Fatal(err)
	}
	other, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: otherPrincipal, RequestID: "other-configure", BaseRevision: other.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"other-a", "other-b", "other-c"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: otherPrincipal, RequestID: "other-start", BaseRevision: other.Revision}); err != nil {
		t.Fatal(err)
	}
	one, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, one.ID, "thread-1", "session-1", "turn-1"); err != nil {
		t.Fatal(err)
	}
	two, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, two.ID, "thread-2", "session-2", "turn-2"); err != nil {
		t.Fatal(err)
	}
	three, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, three.ID, "thread-3", "session-3", "turn-3"); err != nil {
		t.Fatal(err)
	}
	if err = s.LoseArchaeologyNativeTurn(ctx, two.ID, "thread-2", "turn-2"); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteArchaeologyNativeTurn(ctx, domain.ArchaeologyNativeTerminal{JobID: one.ID, ThreadID: "thread-1", TurnID: "turn-1", Status: "failed"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimArchaeologyNativeJob(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("uncertain gate err=%v", err)
	}
}

func TestNativeSchedulerClaimsAllThirtyManuallyConfirmedJobs(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 30, 2)
	seen := map[string]bool{}
	for index := 0; index < 30; index++ {
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatalf("claim %d: %v", index+1, err)
		}
		if seen[job.ID] {
			t.Fatalf("duplicate claim %q", job.ID)
		}
		seen[job.ID] = true
	}
	if _, err := s.ClaimArchaeologyNativeJob(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("claim after thirty err=%v", err)
	}
}

func TestNativeAmbiguousStartRetainsExactCodexIdentityAcrossCanonicalReads(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	launch := domain.ArchaeologyLaunchResult{ThreadID: "thread-visible", CodexSessionID: "session-visible", TurnID: "turn-visible"}
	if err = s.FailArchaeologyNativeStart(ctx, job.ID, launch, true); err != nil {
		t.Fatal(err)
	}
	for poll := 1; poll <= 3; poll++ {
		value, readErr := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
		if readErr != nil {
			t.Fatalf("poll %d: %v", poll, readErr)
		}
		got := value.NativeBatches[0].Jobs[0]
		if got.State != "uncertain" || got.ThreadID != launch.ThreadID || got.CodexSessionID != launch.CodexSessionID || got.TurnID != launch.TurnID || got.ErrorCode != "codex_acceptance_uncertain" {
			t.Fatalf("poll %d: job=%+v", poll, got)
		}
	}
}

func TestNativeIdentitylessUncertaintyRequiresRecoveredExactIdentityBeforeResolution(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FailArchaeologyNativeStart(ctx, job.ID, domain.ArchaeologyLaunchResult{}, true); err != nil {
		t.Fatal(err)
	}
	value, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveArchaeologyNativeUncertainty(ctx, domain.ArchaeologyNativeResolution{Principal: domain.HumanLocalPrincipal, RequestID: "identityless-resolve", BaseRevision: value.Revision, JobID: job.ID, Resolution: "confirmed_stopped"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("identity-less resolve err=%v", err)
	}
	view, err := application.New(s, nil, nil).ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil || view.Handoff == nil || len(view.Handoff.Tasks) != 1 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if len(view.Handoff.Tasks[0].AvailableActions) != 0 || len(view.Handoff.AllowedActions) != 0 || !strings.Contains(view.Handoff.Tasks[0].Error, "could not recover one exact task identity") {
		t.Fatalf("identity-less handoff=%+v", view.Handoff)
	}

	recovered := domain.ArchaeologyLaunchResult{ThreadID: "thread-recovered", CodexSessionID: "session-recovered", TurnID: "turn-recovered"}
	if err = s.BindArchaeologyNativeUncertainty(ctx, job.ID, recovered); err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeUncertainty(ctx, job.ID, recovered); err != nil {
		t.Fatalf("idempotent recovered identity replay: %v", err)
	}
	if err = s.BindArchaeologyNativeUncertainty(ctx, job.ID, domain.ArchaeologyLaunchResult{ThreadID: "other-thread", CodexSessionID: recovered.CodexSessionID, TurnID: recovered.TurnID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting recovered identity err=%v", err)
	}
	view, err = application.New(s, nil, nil).ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil || len(view.Handoff.Tasks[0].AvailableActions) != 1 || view.Handoff.Tasks[0].AvailableActions[0] != "resolve" || len(view.Handoff.AllowedActions) != 1 || view.Handoff.AllowedActions[0] != "resolve" {
		t.Fatalf("recovered handoff=%+v err=%v", view.Handoff, err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveArchaeologyNativeUncertainty(ctx, domain.ArchaeologyNativeResolution{Principal: domain.HumanLocalPrincipal, RequestID: "recovered-resolve", BaseRevision: value.Revision, JobID: job.ID, ThreadID: recovered.ThreadID, TurnID: recovered.TurnID, Resolution: "confirmed_stopped"}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCancelDuringStartKeepsCancellationAndLateCodexIdentity(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-during-start", value.Revision); err != nil {
		t.Fatal(err)
	}
	launch := domain.ArchaeologyLaunchResult{ThreadID: "late-thread", CodexSessionID: "late-session", TurnID: "late-turn"}
	if err = s.FailArchaeologyNativeStart(ctx, job.ID, launch, true); err != nil {
		t.Fatal(err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	got := value.NativeBatches[0].Jobs[0]
	if got.State != "uncertain" || got.ErrorCode != "canceled_during_ambiguous_start" || got.ThreadID != launch.ThreadID || got.TurnID != launch.TurnID {
		t.Fatalf("job=%+v", got)
	}
	wrong := domain.ArchaeologyNativeResolution{Principal: domain.HumanLocalPrincipal, RequestID: "resolve-wrong", BaseRevision: value.Revision, JobID: job.ID, ThreadID: "different-thread", TurnID: launch.TurnID, Resolution: "confirmed_stopped"}
	if _, err = s.ResolveArchaeologyNativeUncertainty(ctx, wrong); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("wrong identity resolution err=%v", err)
	}
	resolve := domain.ArchaeologyNativeResolution{Principal: domain.HumanLocalPrincipal, RequestID: "resolve-cancel-start", BaseRevision: value.Revision, JobID: job.ID, ThreadID: launch.ThreadID, TurnID: launch.TurnID, Resolution: "confirmed_stopped"}
	resolved, err := s.ResolveArchaeologyNativeUncertainty(ctx, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != "draft" || resolved.NativeBatches[0].State != "canceled" || resolved.NativeBatches[0].Jobs[0].State != "interrupted" || resolved.NativeBatches[0].Jobs[0].ErrorCode != "human_confirmed_stopped" {
		t.Fatalf("resolved=%+v", resolved)
	}
	if replayed, replayErr := s.ResolveArchaeologyNativeUncertainty(ctx, resolve); replayErr != nil || replayed.Revision != resolved.Revision {
		t.Fatalf("replay=%+v err=%v", replayed, replayErr)
	}
	var resolutions int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_resolutions WHERE job_id=?`, job.ID).Scan(&resolutions); err != nil || resolutions != 1 {
		t.Fatalf("resolutions=%d err=%v", resolutions, err)
	}
	resolved, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "resolve-reconfigure", BaseRevision: resolved.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"a"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "resolve-restart", BaseRevision: resolved.Revision}); err != nil {
		t.Fatal(err)
	}
	if next, claimErr := s.ClaimArchaeologyNativeJob(ctx); claimErr != nil || next.ID == job.ID {
		t.Fatalf("next=%+v err=%v", next, claimErr)
	}
}

func TestNativeCancelActiveThenRestartFinalizesDurableCancellation(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 2, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-cancel-loss", "session-cancel-loss", "turn-cancel-loss"); err != nil {
		t.Fatal(err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-before-loss", value.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileArchaeology(ctx); err != nil {
		t.Fatal(err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if value.State != "draft" || value.NativeBatches[0].State != "canceled" {
		t.Fatalf("after restart=%+v", value)
	}
	if value.NativeBatches[0].Jobs[0].State != "interrupted" || value.NativeBatches[0].Jobs[0].ErrorCode != "server_restarted_after_cancel_request" ||
		value.NativeBatches[0].Jobs[1].State != "interrupted" || value.NativeBatches[0].Jobs[1].ErrorCode != "canceled_before_start" {
		t.Fatalf("after restart jobs=%+v", value.NativeBatches[0].Jobs)
	}
}

func TestNativeSchedulerRestartMakesActiveUncertainWithoutRetry(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 2, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-r", "session-r", "turn-r"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileArchaeology(ctx); err != nil {
		t.Fatal(err)
	}
	value, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.NativeBatches) != 1 || value.NativeBatches[0].Jobs[0].State != "uncertain" {
		t.Fatalf("batches=%+v", value.NativeBatches)
	}
	if _, err = s.ClaimArchaeologyNativeJob(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("restart claim err=%v", err)
	}
}

func TestNativeOutcomesAllowRepeatRunsWithoutLegacyRewrite(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-a", "session-a", "turn-a"); err != nil {
		t.Fatal(err)
	}
	digest := [32]byte{1}
	outcome := nativeTestOutcome(t, "a")
	outcome.Title = "First report"
	if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: "thread-a", TurnID: "turn-a", Digest: digest, Outcomes: []domain.ArchaeologyOutcome{outcome}}); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteArchaeologyNativeTurn(ctx, domain.ArchaeologyNativeTerminal{JobID: job.ID, ThreadID: "thread-a", TurnID: "turn-a", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	firstBatchID := value.NativeBatches[0].ID
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure-2", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"a"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start-2", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	secondBatchID := value.NativeBatches[0].ID
	if secondBatchID == firstBatchID || value.NativeReviewBatchID != firstBatchID || len(value.Outcomes) != 1 || value.Outcomes[0].Title != "First report" {
		t.Fatalf("new batch hid prior review: review_batch=%q first=%q second=%q outcomes=%+v", value.NativeReviewBatchID, firstBatchID, secondBatchID, value.Outcomes)
	}
	view, err := application.New(s, nil, nil).ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil || view.Review == nil || view.Review.BatchID != firstBatchID || len(view.Review.ProposedOutcomes) != 1 {
		t.Fatalf("prior review view=%+v err=%v", view.Review, err)
	}
	second, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, second.ID, "thread-b", "session-b", "turn-b"); err != nil {
		t.Fatal(err)
	}
	secondDigest := [32]byte{2}
	secondOutcome := outcome
	secondOutcome.Title = "Second report"
	if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: second.ID, ThreadID: "thread-b", TurnID: "turn-b", Digest: secondDigest, Outcomes: []domain.ArchaeologyOutcome{secondOutcome}}); err != nil {
		t.Fatal(err)
	}
	var legacy int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_runs`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy != 0 {
		t.Fatalf("legacy runs=%d", legacy)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.NativeBatches) != 2 || value.NativeReviewBatchID != secondBatchID || len(value.Outcomes) != 1 || value.Outcomes[0].Title != "Second report" {
		t.Fatalf("batches=%d review_batch=%q outcomes=%+v", len(value.NativeBatches), value.NativeReviewBatchID, value.Outcomes)
	}
	var durableOutcomes int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_outcomes`).Scan(&durableOutcomes); err != nil || durableOutcomes != 2 {
		t.Fatalf("durable outcomes=%d err=%v", durableOutcomes, err)
	}
}

func TestNativeReportInvalidatesPriorOutcomeCursorWithoutDrift(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 2, 2)
	batchID := value.NativeBatches[0].ID
	first, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, first.ID, "cursor-thread-a", "cursor-session-a", "cursor-turn-a"); err != nil {
		t.Fatal(err)
	}
	outcomes := make([]domain.ArchaeologyOutcome, 2)
	for i := range outcomes {
		outcomes[i] = nativeTestOutcome(t, first.ProjectID)
		outcomes[i].ID = ""
		outcomes[i].Title = "Cursor report " + strconv.Itoa(i)
	}
	if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: first.ID, ThreadID: "cursor-thread-a", TurnID: "cursor-turn-a", Digest: [32]byte{31}, Outcomes: outcomes}); err != nil {
		t.Fatal(err)
	}
	page, err := s.ArchaeologyBatchOutcomes(ctx, domain.HumanLocalPrincipal, batchID, domain.ArchaeologyOutcomePageQuery{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	second, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, second.ID, "cursor-thread-b", "cursor-session-b", "cursor-turn-b"); err != nil {
		t.Fatal(err)
	}
	secondOutcome := nativeTestOutcome(t, second.ProjectID)
	secondOutcome.Title = "Later report"
	if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: second.ID, ThreadID: "cursor-thread-b", TurnID: "cursor-turn-b", Digest: [32]byte{32}, Outcomes: []domain.ArchaeologyOutcome{secondOutcome}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArchaeologyBatchOutcomes(ctx, domain.HumanLocalPrincipal, batchID, domain.ArchaeologyOutcomePageQuery{Limit: 1, Cursor: page.NextCursor}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale cursor err=%v", err)
	}
	fresh, err := s.ArchaeologyBatchOutcomes(ctx, domain.HumanLocalPrincipal, batchID, domain.ArchaeologyOutcomePageQuery{Limit: 5})
	if err != nil || len(fresh.Items) != 3 || fresh.NextCursor != "" {
		t.Fatalf("fresh=%+v err=%v", fresh, err)
	}
	seen := map[string]bool{}
	for _, item := range fresh.Items {
		if seen[item.ID] {
			t.Fatalf("duplicate outcome %s", item.ID)
		}
		seen[item.ID] = true
	}
}

func TestOrdinarySessionReadCapsOutcomeSummariesAndExcludesProposalBodies(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "bounded-thread", "bounded-session", "bounded-turn"); err != nil {
		t.Fatal(err)
	}
	outcomes := make([]domain.ArchaeologyOutcome, 2)
	for i := range outcomes {
		outcomes[i] = nativeTestOutcome(t, job.ProjectID)
		outcomes[i].Title = "Bounded " + strconv.Itoa(i)
		outcomes[i].ProposalJSON += strings.Repeat(" ", 20000)
	}
	if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: "bounded-thread", TurnID: "bounded-turn", Digest: [32]byte{41}, Outcomes: outcomes}); err != nil {
		t.Fatal(err)
	}
	for i := 2; i < 60; i++ {
		_, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES(?,?,?,?,?,1,?,?)`, "bounded-extra-"+strconv.Itoa(i), job.ID, job.ProjectID, "Bounded extra", "Summary", outcomes[0].ProposalJSON, stamp(testNow.Add(time.Duration(i)*time.Second)))
		if err != nil {
			t.Fatal(err)
		}
	}
	value, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Outcomes) != 5 {
		t.Fatalf("outcomes=%d", len(value.Outcomes))
	}
	for _, outcome := range value.Outcomes {
		if outcome.ProposalJSON != "" {
			t.Fatalf("ordinary session projected proposal bytes=%d", len(outcome.ProposalJSON))
		}
	}
}

func TestNativeReportDerivesDistinctIDsForDuplicateTitles(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-duplicate", "session-duplicate", "turn-duplicate"); err != nil {
		t.Fatal(err)
	}
	one := nativeTestOutcome(t, "a")
	two := one
	one.Title, two.Title = "Same useful title", "Same useful title"
	if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: "thread-duplicate", TurnID: "turn-duplicate", Digest: [32]byte{9}, Outcomes: []domain.ArchaeologyOutcome{one, two}}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB().QueryContext(ctx, `SELECT id FROM archaeology_native_outcomes ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("derived ids=%v", ids)
	}
}

func TestNativeProposalExactByteBoundaryAcceptsAndOneByteOverRejects(t *testing.T) {
	for _, test := range []struct {
		name    string
		extra   int
		wantErr error
	}{
		{name: "exact 32 KiB"},
		{name: "one byte over", extra: 1, wantErr: domain.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			s, _ := nativeTestSession(t, 1, 1)
			job, err := s.ClaimArchaeologyNativeJob(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-proposal-bound", "session-proposal-bound", "turn-proposal-bound"); err != nil {
				t.Fatal(err)
			}
			outcome := nativeTestOutcome(t, "a")
			padding := domain.ArchaeologyNativeProposalMaxBytes + test.extra - len(outcome.ProposalJSON)
			if padding < 0 {
				t.Fatalf("base proposal=%d bytes", len(outcome.ProposalJSON))
			}
			outcome.ProposalJSON += strings.Repeat(" ", padding)
			if len(outcome.ProposalJSON) != domain.ArchaeologyNativeProposalMaxBytes+test.extra || !json.Valid([]byte(outcome.ProposalJSON)) {
				t.Fatalf("proposal bytes=%d valid=%v", len(outcome.ProposalJSON), json.Valid([]byte(outcome.ProposalJSON)))
			}
			err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: "thread-proposal-bound", TurnID: "turn-proposal-bound", Digest: [32]byte{10}, Outcomes: []domain.ArchaeologyOutcome{outcome}})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("err=%v want=%v", err, test.wantErr)
			}
		})
	}
}

func TestNativeReportRejectsProposalEvidenceOmittedFromVisibleProvenance(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-hidden", "session-hidden", "turn-hidden"); err != nil {
		t.Fatal(err)
	}
	outcome := nativeTestOutcome(t, "a")
	visible := "commit:" + strings.Repeat("a", 40)
	hidden := "commit:" + strings.Repeat("b", 40)
	outcome.ProposalJSON = strings.Replace(outcome.ProposalJSON, `"stable_id":"`+visible+`"`, `"stable_id":"`+hidden+`"`, 1)
	err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: "thread-hidden", TurnID: "turn-hidden", Digest: [32]byte{8}, Outcomes: []domain.ArchaeologyOutcome{outcome}})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("hidden proposal evidence err=%v", err)
	}
}

func TestNativeCancelIsIdempotentAndReturnsOnlyExactActiveTurns(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 3, 2)
	one, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, one.ID, "thread-c1", "session-c1", "turn-c1"); err != nil {
		t.Fatal(err)
	}
	two, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, two.ID, "thread-c2", "session-c2", "turn-c2"); err != nil {
		t.Fatal(err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	jobs, _, err := s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel", value.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].ThreadID == "" || jobs[0].TurnID == "" {
		t.Fatalf("jobs=%+v", jobs)
	}
	jobs, _, err = s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel", value.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("replay dispatched interrupts=%+v", jobs)
	}
	var queued, uncertain int
	if err = s.DB().QueryRowContext(ctx, `SELECT sum(state='interrupted'),sum(state='uncertain') FROM archaeology_native_jobs`).Scan(&queued, &uncertain); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || uncertain != 0 {
		t.Fatalf("interrupted=%d uncertain=%d", queued, uncertain)
	}
	if err = s.ReconcileArchaeology(ctx); err != nil {
		t.Fatal(err)
	}
	var cancelInterrupted, cancelUncertain int
	if err = s.DB().QueryRowContext(ctx, `SELECT sum(state='interrupted'),sum(state='uncertain') FROM archaeology_native_jobs`).Scan(&cancelInterrupted, &cancelUncertain); err != nil {
		t.Fatal(err)
	}
	if cancelInterrupted != 3 || cancelUncertain != 0 {
		t.Fatalf("cancel restart interrupted=%d uncertain=%d", cancelInterrupted, cancelUncertain)
	}
	current, readErr := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if _, _, err = s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-again", current.Revision); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("new-key repeated cancel err=%v", err)
	}
	if _, _, err = s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel", value.Revision+1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed replay err=%v", err)
	}
}

func TestNativeQueuedOnlyCancelFinalizesSynchronouslyAndReplays(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 2, 2)
	beforeRevision := value.Revision
	jobs, canceled, err := s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-queued-only", beforeRevision)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 || canceled.State != "draft" || canceled.Revision != beforeRevision+1 || canceled.NativeBatches[0].State != "canceled" {
		t.Fatalf("jobs=%+v canceled=%+v", jobs, canceled)
	}
	for _, job := range canceled.NativeBatches[0].Jobs {
		if job.State != "interrupted" || job.ErrorCode != "canceled_before_start" || job.ThreadID != "" || job.TurnID != "" {
			t.Fatalf("job=%+v", job)
		}
	}
	jobs, replay, err := s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-queued-only", beforeRevision)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 || replay.Revision != canceled.Revision || replay.State != "draft" || replay.NativeBatches[0].State != "canceled" {
		t.Fatalf("replay jobs=%+v session=%+v", jobs, replay)
	}
	if _, _, err = s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-queued-only", beforeRevision+1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed replay err=%v", err)
	}
}

func TestNativeQueueCreatesAuditedShellsAtomicallyAndReplays(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 2, 2)
	var projects, topics, tasks, mappings, changes, activity int
	for query, target := range map[string]*int{
		`SELECT count(*) FROM projects`:                                     &projects,
		`SELECT count(*) FROM topics WHERE project_id IS NOT NULL`:          &topics,
		`SELECT count(*) FROM tasks`:                                        &tasks,
		`SELECT count(*) FROM archaeology_candidate_projects`:               &mappings,
		`SELECT count(*) FROM changes WHERE kind="project_created"`:         &changes,
		`SELECT count(*) FROM activity_events WHERE kind="project_updated"`: &activity,
	} {
		if err := s.DB().QueryRowContext(ctx, query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if projects != 2 || topics != 2 || tasks != 0 || mappings != 2 || changes != 2 || activity != 2 {
		t.Fatalf("projects=%d topics=%d tasks=%d mappings=%d changes=%d activity=%d", projects, topics, tasks, mappings, changes, activity)
	}
	if len(value.NativeBatches) != 1 || len(value.NativeBatches[0].Jobs) != 2 || value.NativeBatches[0].Jobs[0].CandidateID == "" || value.NativeBatches[0].Jobs[0].ProjectID == "" {
		t.Fatalf("native ledger=%+v", value.NativeBatches)
	}
	if _, err := s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start-1", BaseRevision: value.Revision - 1}); err != nil {
		t.Fatal(err)
	}
	var batches, jobs int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_batches`).Scan(&batches); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if batches != 1 || jobs != 2 {
		t.Fatalf("replay batches=%d jobs=%d", batches, jobs)
	}
}

func TestNativeQueuePreservesConfiguredCanonicalProject(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.CreateCanonicalProject(ctx, domain.CreateProjectCommand{ID: "codex-commons", Name: "Existing Commons", Status: "active", Purpose: "Existing purpose", Now: "Existing now", Meta: coreTestMeta("existing")}); err != nil {
		t.Fatal(err)
	}
	var beforeName, beforePurpose, beforeNow string
	var beforeRevision int64
	if err = s.DB().QueryRowContext(ctx, `SELECT name,purpose,now_text,revision FROM projects WHERE id="codex-commons"`).Scan(&beforeName, &beforePurpose, &beforeNow, &beforeRevision); err != nil {
		t.Fatal(err)
	}
	var beforeActivity int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM activity_events`).Scan(&beforeActivity); err != nil {
		t.Fatal(err)
	}
	value, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "catalog-codex", CanonicalProjectID: "codex-commons", Name: "Catalog name", PathLabel: "Catalog", FromConfiguredRoot: true, HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"catalog-codex"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start", BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var name, purpose, nowText string
	var revision int64
	if err = s.DB().QueryRowContext(ctx, `SELECT name,purpose,now_text,revision FROM projects WHERE id="codex-commons"`).Scan(&name, &purpose, &nowText, &revision); err != nil {
		t.Fatal(err)
	}
	var activity int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM activity_events`).Scan(&activity); err != nil {
		t.Fatal(err)
	}
	if name != beforeName || purpose != beforePurpose || nowText != beforeNow || revision != beforeRevision || activity != beforeActivity {
		t.Fatalf("existing project mutated: %q %q %q %d activity=%d", name, purpose, nowText, revision, activity)
	}
	job := value.NativeBatches[0].Jobs[0]
	if job.CandidateID != "catalog-codex" || job.ProjectID != "codex-commons" {
		t.Fatalf("job=%+v", job)
	}
}

func TestNativeQueueRejectsOpaqueCollisionAndRollsBackBatch(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.CreateCanonicalProject(ctx, domain.CreateProjectCommand{ID: "opaque", Name: "Existing", Purpose: "Existing", Meta: coreTestMeta("existing-opaque")}); err != nil {
		t.Fatal(err)
	}
	value, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "opaque", Name: "Catalog", PathLabel: "Catalog", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"opaque"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start", BaseRevision: value.Revision}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("collision err=%v", err)
	}
	var batches, mappings int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_batches`).Scan(&batches); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_candidate_projects`).Scan(&mappings); err != nil {
		t.Fatal(err)
	}
	if batches != 0 || mappings != 0 {
		t.Fatalf("rollback batches=%d mappings=%d", batches, mappings)
	}
}

func TestMigration13RequiresRefreshBeforeLegacyCandidateCanQueue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v12.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{"001_core.sql", "002_general_home.sql", "003_posts_feed.sql", "004_comment_intent.sql", "005_project_core.sql", "006_continuity_provenance.sql", "007_addressable_contributors.sql", "008_single_plane_attention.sql", "009_codex_human_auth.sql", "010_project_archaeology.sql", "011_archaeology_handoff.sql", "012_codex_archaeology_launch.sql"} {
		body, readErr := migrations.FS.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, index+1, name, "2026-08-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	legacySessionID := archaeologySessionID(domain.HumanLocalPrincipal)
	if _, err = db.Exec(`INSERT INTO archaeology_sessions(id,principal,state,discovery_state,depth,source_git,source_docs,source_codex_history,max_concurrency,revision,created_at,updated_at) VALUES(?,"human:local-admin","draft","ready","standard",1,0,0,1,2,"2026-08-01T00:00:00Z","2026-08-01T00:00:00Z")`, legacySessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,selected,from_codex_metadata,from_configured_root,codex_thread_count) VALUES(?,"legacy","Legacy","Legacy",1,0,0,1,2,"low","metadata",1,1,0,1)`, legacySessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO archaeology_runs(id,session_id,candidate_id,state,created_at,updated_at) VALUES("legacy-run",?,"legacy","queued","2026-08-01T00:00:00Z","2026-08-01T00:00:00Z")`, legacySessionID); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	legacy, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Candidates[0].CanonicalProjectID != "" {
		t.Fatalf("legacy mapping=%q", legacy.Candidates[0].CanonicalProjectID)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "before-refresh", BaseRevision: legacy.Revision}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("before refresh err=%v", err)
	}
	var batchCount, jobCount int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_batches`).Scan(&batchCount); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_jobs`).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if batchCount != 0 || jobCount != 0 {
		t.Fatalf("invalid queue persisted batch=%d jobs=%d", batchCount, jobCount)
	}
	refreshed, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "refresh"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "legacy", Name: "Legacy refreshed", PathLabel: "Legacy", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}, {ID: "new-a", Name: "New A", PathLabel: "New A", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}, {ID: "new-b", Name: "New B", PathLabel: "New B", HasDocs: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.Candidates) != 3 || len(refreshed.Config.SelectedProjectIDs) != 0 {
		t.Fatalf("refreshed candidates=%d selected=%v", len(refreshed.Candidates), refreshed.Config.SelectedProjectIDs)
	}
	refreshed, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure-refreshed", BaseRevision: refreshed.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"new-a", "new-b"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true, Docs: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "after-refresh", BaseRevision: refreshed.Revision}); err != nil {
		t.Fatalf("queue after refresh: %v", err)
	}
	var legacyRuns int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_runs WHERE id="legacy-run" AND session_id=? AND candidate_id="legacy" AND state="queued"`, legacySessionID).Scan(&legacyRuns); err != nil {
		t.Fatal(err)
	}
	if legacyRuns != 1 {
		t.Fatalf("legacy run changed=%d", legacyRuns)
	}
	var integrity string
	if err = s.DB().QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check=%q err=%v", integrity, err)
	}
	foreignKeys, err := s.DB().QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer foreignKeys.Close()
	if foreignKeys.Next() {
		t.Fatal("foreign_key_check reported a violation")
	}
	if err = foreignKeys.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMigration14QuarantinesUnattestedNativeWorkWithoutInventingPolicy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v13-native.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	names := []string{"001_core.sql", "002_general_home.sql", "003_posts_feed.sql", "004_comment_intent.sql", "005_project_core.sql", "006_continuity_provenance.sql", "007_addressable_contributors.sql", "008_single_plane_attention.sql", "009_codex_human_auth.sql", "010_project_archaeology.sql", "011_archaeology_handoff.sql", "012_codex_archaeology_launch.sql", "013_archaeology_native_scheduler.sql"}
	for index, name := range names {
		body, readErr := migrations.FS.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = db.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err = db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, index+1, name, "2026-08-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	type legacyCase struct {
		suffix, principal, sessionState, batchState, jobState, thread, turn string
	}
	cases := []legacyCase{
		{"queued", "human:upgrade-queued", "running", "queued", "queued", "", ""},
		{"active", "human:upgrade-active", "running", "running", "active", "thread-upgrade", "turn-upgrade"},
		{"completed", "human:upgrade-completed", "draft", "completed", "completed", "thread-completed", "turn-completed"},
		{"canceled", "human:upgrade-canceled", "draft", "canceled", "interrupted", "thread-canceled", "turn-canceled"},
	}
	for _, item := range cases {
		sessionID, candidateID, projectID := archaeologySessionID(item.principal), "candidate-"+item.suffix, "project-"+item.suffix
		batchID, jobID := "ARB-upgrade-"+item.suffix, "ARJ-upgrade-"+item.suffix
		at := "2026-08-01T00:00:00Z"
		if _, err = db.Exec(`INSERT INTO archaeology_sessions(id,principal,state,discovery_state,depth,source_git,source_docs,source_codex_history,max_concurrency,revision,created_at,updated_at) VALUES(?,?,?,'ready','deep',0,1,1,1,5,?,?)`, sessionID, item.principal, item.sessionState, at, at); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO projects(id,name,status,purpose,milestone,now_text,revision,created_at,updated_at) VALUES(?,?,'active','legacy','','',1,?,?)`, projectID, "Project "+item.suffix, at, at); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO topics(id,project_id,name,created_at) VALUES(?,?,?,?)`, projectID, projectID, "Project "+item.suffix, at); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,selected,from_codex_metadata,from_configured_root,codex_thread_count,canonical_project_id) VALUES(?,?,?, ?,1,1,1,1,2,'low','metadata',1,1,0,1,?)`, sessionID, candidateID, "Candidate "+item.suffix, "Candidate "+item.suffix, projectID); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO archaeology_candidate_projects(session_id,candidate_id,project_id,mapped_by_principal,purpose,created_project,mapped_at) VALUES(?,?,?,?,?,0,?)`, sessionID, candidateID, projectID, item.principal, "legacy mapping", at); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at) VALUES(?,?,?,zeroblob(32),'app_server_dynamic_tools',?,1,?,?)`, batchID, sessionID, "legacy-start-"+item.suffix, item.batchState, at, at); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,mode,state,thread_id,codex_session_id,turn_id,created_at,updated_at) VALUES(?,?,?,?,?,'app_server_dynamic_tools',?,?,?, ?,?,?)`, jobID, batchID, sessionID, candidateID, projectID, item.jobState, item.thread, func() string {
			if item.thread != "" {
				return "session-" + item.suffix
			}
			return ""
		}(), item.turn, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	expect := map[string]struct{ batch, job, code string }{
		"human:upgrade-queued":    {"canceled", "interrupted", "execution_policy_unattested_never_launched"},
		"human:upgrade-active":    {"attention", "uncertain", "execution_policy_unattested"},
		"human:upgrade-completed": {"completed", "completed", ""},
		"human:upgrade-canceled":  {"canceled", "interrupted", ""},
	}
	service := application.New(s, nil, nil)
	for principal, want := range expect {
		value, readErr := s.ArchaeologySession(ctx, principal)
		if readErr != nil || len(value.NativeBatches) != 1 || value.NativeBatches[0].PolicyAttested || value.NativeBatches[0].Policy.Depth != "" || value.NativeBatches[0].Policy.Sources != (domain.ArchaeologySources{}) || value.NativeBatches[0].State != want.batch || value.NativeBatches[0].Jobs[0].State != want.job || value.NativeBatches[0].Jobs[0].ErrorCode != want.code {
			t.Fatalf("principal=%s value=%+v err=%v", principal, value, readErr)
		}
		view, viewErr := service.ProjectArchaeology(ctx, principal)
		if viewErr != nil || view.Handoff == nil || view.Handoff.PolicyAttested || view.Handoff.Depth != "" || view.Handoff.Sources != (application.ArchaeologySources{}) {
			t.Fatalf("principal=%s view=%+v err=%v", principal, view, viewErr)
		}
		// This service intentionally has no native scheduler. Persisted native
		// history must never fall back to the legacy control plane after a
		// feature-disabled restart, even when the latest durable batch is safely
		// restartable. The direct store queue below proves storage restartability;
		// a scheduler-enabled service owns the user-facing native start control.
		if view.Controls.CanStart || view.Controls.CanPause || view.Controls.CanResume || view.Controls.CanCancel {
			t.Fatalf("principal=%s feature-disabled controls=%+v", principal, view.Controls)
		}
	}
	active, err := s.ArchaeologySession(ctx, "human:upgrade-active")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveArchaeologyNativeUncertainty(ctx, domain.ArchaeologyNativeResolution{Principal: "human:upgrade-active", RequestID: "upgrade-resolve", BaseRevision: active.Revision, JobID: "ARJ-upgrade-active", ThreadID: "thread-upgrade", TurnID: "turn-upgrade", Resolution: "confirmed_stopped"}); err != nil {
		t.Fatal(err)
	}
	queued, err := s.ArchaeologySession(ctx, "human:upgrade-queued")
	if err != nil {
		t.Fatal(err)
	}
	queued, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: "human:upgrade-queued", RequestID: "upgrade-reconfigure", BaseRevision: queued.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"candidate-queued"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: "human:upgrade-queued", RequestID: "upgrade-restart", BaseRevision: queued.Revision}); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil || claimed.Policy.Depth != "standard" || !claimed.Policy.Sources.Git {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	rows, err := s.DB().QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration 14 foreign key violation")
	}
}

func TestNativeStartFailureTerminatesBatchWithAttention(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FailArchaeologyNativeStart(ctx, job.ID, domain.ArchaeologyLaunchResult{}, false); err != nil {
		t.Fatal(err)
	}
	value, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if value.State != "draft" || len(value.NativeBatches) != 1 || value.NativeBatches[0].State != "attention" || value.NativeBatches[0].Jobs[0].State != "failed" {
		t.Fatalf("session=%s batches=%+v", value.State, value.NativeBatches)
	}
}

func TestNativeLateProgressCannotEraseTerminalOrCancelState(t *testing.T) {
	ctx := context.Background()
	for _, target := range []string{"report_ready", "cancel_requested"} {
		t.Run(target, func(t *testing.T) {
			s, value := nativeTestSession(t, 1, 1)
			job, err := s.ClaimArchaeologyNativeJob(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-"+target, "session-"+target, "turn-"+target); err != nil {
				t.Fatal(err)
			}
			if target == "report_ready" {
				outcome := nativeTestOutcome(t, "a")
				if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: "thread-" + target, TurnID: "turn-" + target, Digest: [32]byte{1}, Outcomes: []domain.ArchaeologyOutcome{outcome}}); err != nil {
					t.Fatal(err)
				}
			} else {
				value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-late-progress", value.Revision); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.UpdateArchaeologyNativeProgress(ctx, domain.ArchaeologyNativeProgress{JobID: job.ID, ThreadID: "thread-" + target, TurnID: "turn-" + target, PhaseLabel: "Late update", SourcesExamined: 2}); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("late progress err=%v", err)
			}
			var state string
			if err = s.DB().QueryRowContext(ctx, `SELECT state FROM archaeology_native_jobs WHERE id=?`, job.ID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != target {
				t.Fatalf("state=%s want=%s", state, target)
			}
		})
	}
}

func TestNativeFailedAndInterruptedTurnsLeaveBatchNeedingAttention(t *testing.T) {
	ctx := context.Background()
	for _, status := range []string{"failed", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			s, _ := nativeTestSession(t, 1, 1)
			job, err := s.ClaimArchaeologyNativeJob(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-"+status, "session-"+status, "turn-"+status); err != nil {
				t.Fatal(err)
			}
			if err = s.CompleteArchaeologyNativeTurn(ctx, domain.ArchaeologyNativeTerminal{JobID: job.ID, ThreadID: "thread-" + status, TurnID: "turn-" + status, Status: status}); err != nil {
				t.Fatal(err)
			}
			value, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			if value.State != "draft" || value.NativeBatches[0].State != "attention" || value.NativeBatches[0].Jobs[0].State != status {
				t.Fatalf("session=%s batches=%+v", value.State, value.NativeBatches)
			}
		})
	}
}

func TestNativeCancelReturnsEveryExactActiveTurnUpToSelectionBound(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 9, 2)
	for index := 0; index < 9; index++ {
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.BindArchaeologyNativeJob(ctx, job.ID, fmt.Sprintf("thread-cancel-%d", index), fmt.Sprintf("session-cancel-%d", index), fmt.Sprintf("turn-cancel-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	value, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	jobs, canceled, err := s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-all-nine", value.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 9 || canceled.State != "cancel_requested" || canceled.NativeBatches[0].State != "cancel_requested" {
		t.Fatalf("jobs=%d canceled=%+v", len(jobs), canceled)
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		if job.ThreadID == "" || job.CodexSessionID == "" || job.TurnID == "" || seen[job.ID] {
			t.Fatalf("non-exact cancellation identity: %+v", job)
		}
		seen[job.ID] = true
		if err = s.CompleteArchaeologyNativeTurn(ctx, domain.ArchaeologyNativeTerminal{JobID: job.ID, ThreadID: job.ThreadID, TurnID: job.TurnID, Status: "interrupted"}); err != nil {
			t.Fatal(err)
		}
	}
	final, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != "draft" || final.NativeBatches[0].State != "canceled" {
		t.Fatalf("final=%+v", final)
	}
	for _, job := range final.NativeBatches[0].Jobs {
		if job.State != "interrupted" || job.ErrorCode != "codex_turn_interrupted" {
			t.Fatalf("job=%+v", job)
		}
	}
}

func TestNativeRestartFinalizesEveryDurableCancelRequestWithoutUncertainty(t *testing.T) {
	ctx := context.Background()
	s, value := nativeTestSession(t, 9, 2)
	for index := 0; index < 9; index++ {
		job, err := s.ClaimArchaeologyNativeJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.BindArchaeologyNativeJob(ctx, job.ID, fmt.Sprintf("thread-restart-%d", index), fmt.Sprintf("session-restart-%d", index), fmt.Sprintf("turn-restart-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	value, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	jobs, _, err := s.CancelArchaeologyNativeBatch(ctx, domain.HumanLocalPrincipal, "cancel-before-restart", value.Revision)
	if err != nil || len(jobs) != 9 {
		t.Fatalf("jobs=%d err=%v", len(jobs), err)
	}
	if err = s.ReconcileArchaeology(ctx); err != nil {
		t.Fatal(err)
	}
	final, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != "draft" || final.NativeBatches[0].State != "canceled" {
		t.Fatalf("final=%+v", final)
	}
	for _, job := range final.NativeBatches[0].Jobs {
		if job.State != "interrupted" || job.ErrorCode != "server_restarted_after_cancel_request" || job.TerminalAt.IsZero() {
			t.Fatalf("job=%+v", job)
		}
	}
	var uncertain int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_jobs WHERE state='uncertain'`).Scan(&uncertain); err != nil || uncertain != 0 {
		t.Fatalf("uncertain=%d err=%v", uncertain, err)
	}
}
