package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

type fakeArchaeologyDiscoverer struct{}

func (fakeArchaeologyDiscoverer) DiscoverMetadata(context.Context) (domain.ArchaeologyDiscovery, error) {
	return domain.ArchaeologyDiscovery{SourceRootsScanned: 1, Candidates: []domain.ArchaeologyCandidate{{ID: "p", Name: "Project", PathLabel: "Project", HasGit: true, DurationMinSeconds: 60, DurationMaxSeconds: 120, RelativeCost: "low", PrivacyNote: "Metadata only."}}}, nil
}

type countingArchaeologyDiscoverer struct{ calls int }

func (d *countingArchaeologyDiscoverer) DiscoverMetadata(context.Context) (domain.ArchaeologyDiscovery, error) {
	d.calls++
	return fakeArchaeologyDiscoverer{}.DiscoverMetadata(context.Background())
}

func TestProjectArchaeologyRefreshNeverCallsInventoryDuringLiveNativeWork(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "p", Name: "Project", PathLabel: "Project", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure", Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"p"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}, BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start", BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	jobID := value.NativeBatches[0].Jobs[0].ID
	discoverer := &countingArchaeologyDiscoverer{}
	service := New(repository, nil, nil)
	service.ConfigureProjectArchaeology(discoverer, nil)
	for _, state := range []string{"queued", "starting", "active", "report_ready", "cancel_requested"} {
		if _, err = repository.DB().ExecContext(ctx, `UPDATE archaeology_native_jobs SET state=? WHERE id=?`, state, jobID); err != nil {
			t.Fatal(err)
		}
		before, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "refresh-"+state); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("state=%s err=%v", state, err)
		}
		after, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if discoverer.calls != 0 || after.Revision != before.Revision || len(after.Config.SelectedProjectIDs) != len(before.Config.SelectedProjectIDs) {
			t.Fatalf("state=%s calls=%d before_revision=%d after=%+v", state, discoverer.calls, before.Revision, after)
		}
	}
}

func TestSelectedPreviewTraversesMoreThanFiveAndRejectsCursorBypass(t *testing.T) {
	value := ArchaeologySelectedPreview{Projects: make([]ArchaeologySelectedProjectPreview, 6)}
	for index := range value.Projects {
		value.Projects[index].OutcomeID = fmt.Sprintf("outcome-%d", index)
	}
	first, err := selectedPreviewPage(value, "")
	if err != nil || len(first.Projects) != 5 || first.NextCursor != "5" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := selectedPreviewPage(value, first.NextCursor)
	if err != nil || len(second.Projects) != 1 || second.Projects[0].OutcomeID != "outcome-5" || second.NextCursor != "" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	for _, cursor := range []string{"-1", "1", "6", "9", "5junk", "05"} {
		if _, err = selectedPreviewPage(value, cursor); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("cursor=%q err=%v", cursor, err)
		}
	}
}

func TestArchaeologyReportBridgesToCanonicalNonMutatingPreview(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	_, err = repository.CreateCanonicalProject(ctx, domain.CreateProjectCommand{ID: "p", Name: "Project", Purpose: "Dogfood", Meta: domain.CoreWriteMeta{ActorID: "human", SessionID: "human", RequestID: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	service := New(repository, nil, nil)
	service.ConfigureProjectArchaeology(fakeArchaeologyDiscoverer{}, nil)
	session, err := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "discover")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.ConfigureArchaeologySession(ctx, domain.HumanLocalPrincipal, "config", ArchaeologyConfigRequest{SelectedProjectIDs: []string{"p"}, Depth: "quick", Sources: ArchaeologySources{Git: true}, MaxConcurrency: 1, BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "start", ArchaeologyTransitionRequest{BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.ClaimProjectArchaeology(ctx, session.Handoff.ID, "claim", "exact-session")
	if err != nil {
		t.Fatal(err)
	}
	digest := func(letter string) string { return "sha256:" + strings.Repeat(letter, 64) }
	occurred := time.Now().UTC().Add(-time.Hour)
	historical := HistoricalImportRequest{
		SchemaVersion: 1, BatchID: "archaeology-cutover", SourceDigest: digest("a"), CollisionPolicy: "current_wins",
		Tasks: []HistoricalTaskRequest{{
			Key: "outcome", Title: "Verified outcome", State: "done",
			Source: HistoricalSourceRequest{Kind: "repository_document", StableID: "review", Digest: digest("b"), OccurredAt: occurred},
			Attributions: []HistoricalAttributionRequest{{
				Session: "historical-session", Role: "implementer", Confidence: "verified",
				Source: HistoricalSourceRequest{Kind: "codex_session_uuidv7", StableID: "historical-session", Digest: digest("c"), OccurredAt: occurred},
			}},
		}},
	}
	_, err = service.ReportProjectArchaeology(ctx, session.Handoff.ID, "invalid-report", "exact-session", ArchaeologyHandoffReportRequest{Outcomes: []ArchaeologyOutcomeReportRequest{{Title: "Invalid", Summary: "Missing canonical tasks", ProjectID: "p", SourceCount: 1, Provenance: []ArchaeologyProvenanceReportRequest{{SourceKind: "git", SourceLabel: "commit:invalid", Digest: digest("e"), RecordedAt: occurred}}, HistoricalImport: HistoricalImportRequest{SchemaVersion: 1, BatchID: "invalid", SourceDigest: digest("f"), CollisionPolicy: "current_wins"}}}})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid canonical proposal err=%v", err)
	}
	reported, err := service.ReportProjectArchaeology(ctx, session.Handoff.ID, "report", "exact-session", ArchaeologyHandoffReportRequest{Outcomes: []ArchaeologyOutcomeReportRequest{{Title: "Verified outcome", Summary: "Source-grounded", ProjectID: "p", SourceCount: 1, Provenance: []ArchaeologyProvenanceReportRequest{{SourceKind: "git", SourceLabel: "commit:abc", Digest: digest("d"), RecordedAt: occurred}}, Contributors: []ArchaeologyContributorReportRequest{{SessionID: "historical-session", Contribution: "Implemented", Confidence: "verified"}}, HistoricalImport: historical}}})
	if err != nil {
		t.Fatal(err)
	}
	if reported.Review == nil || !reported.Review.CanApply || reported.Capabilities.CanonicalApply.Available != true {
		t.Fatalf("reported=%+v", reported)
	}
	preview, err := service.PreviewArchaeologyImport(ctx, domain.HumanLocalPrincipal, ArchaeologyImportPreviewRequest{OutcomeID: reported.Review.ProposedOutcomes[0].ID}, ProjectCoreActor{Actor: "human", Session: "human", RequestID: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ProjectID != "p" || preview.Request.ConfirmSourceDigest != "" || preview.Preview.State != "preview" || preview.Preview.Applied {
		t.Fatalf("preview=%+v", preview)
	}
	var batches int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM historical_import_batches`).Scan(&batches); err != nil || batches != 0 {
		t.Fatalf("preview mutated batches=%d err=%v", batches, err)
	}
}

type fakeArchaeologyLauncher struct{ launches int }

func (*fakeArchaeologyLauncher) Available(context.Context) error { return nil }
func (f *fakeArchaeologyLauncher) Launch(_ context.Context, _ domain.ArchaeologySession) error {
	f.launches++
	return nil
}

type capabilityNativeLauncher struct{ err error }

func (f capabilityNativeLauncher) Available(context.Context) error { return f.err }
func (capabilityNativeLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	return domain.ArchaeologyLaunchResult{}, domain.ErrUnavailable
}
func (capabilityNativeLauncher) InterruptNative(context.Context, domain.ArchaeologyNativeJob) error {
	return domain.ErrUnavailable
}
func (capabilityNativeLauncher) FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error {
	return domain.ErrUnavailable
}

func TestProjectArchaeologyNativeSchedulerCapabilityUsesRuntimeAvailability(t *testing.T) {
	for _, test := range []struct {
		name          string
		availability  error
		wantAvailable bool
	}{
		{name: "healthy scheduler", wantAvailable: true},
		{name: "unavailable scheduler", availability: domain.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheduler := &ArchaeologyScheduler{launcher: capabilityNativeLauncher{err: test.availability}}
			service := &Service{archaeologyScheduler: scheduler, archaeologyLauncher: scheduler}
			view := service.archaeologySessionView(domain.ArchaeologySession{
				State:      "draft",
				Config:     domain.ArchaeologyConfig{Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 2},
				Candidates: []domain.ArchaeologyCandidate{{ID: "project", CanonicalProjectID: "project"}},
			})
			if !view.Capabilities.TaskLaunch.Configured || view.Capabilities.TaskLaunch.Available != test.wantAvailable {
				t.Fatalf("task launch capability=%+v", view.Capabilities.TaskLaunch)
			}
			if view.Controls.CanStart {
				t.Fatalf("empty persisted selection exposed Start: %+v", view.Controls)
			}
		})
	}
}

func TestNativeArchaeologyMaximumCanonicalResponseFitsBrowserBudget(t *testing.T) {
	now := time.Now().UTC()
	value := domain.ArchaeologySession{
		ID: "AR-max", State: "running", DiscoveryState: "ready", Revision: 100, UpdatedAt: now,
		Config: domain.ArchaeologyConfig{Depth: "deep", Sources: domain.ArchaeologySources{Docs: true}, MaxConcurrency: 2},
	}
	for index := 0; index < 100; index++ {
		candidateID := fmt.Sprintf("candidate-%03d-%s", index, strings.Repeat("i", 100))
		value.Candidates = append(value.Candidates, domain.ArchaeologyCandidate{
			ID: candidateID, CanonicalProjectID: fmt.Sprintf("project-%03d", index), Name: strings.Repeat("N", 200), PathLabel: strings.Repeat("P", 300), RepositoryLabel: strings.Repeat("R", 300),
			LastActivityAt: now, HasGit: true, HasDocs: true, HasCodexHistory: true, FromCodexMetadata: true, FromConfiguredRoot: true,
			DurationMinSeconds: 1, DurationMaxSeconds: 600, RelativeCost: "high", PrivacyNote: strings.Repeat("V", 500), CodexThreadCount: 10000,
		})
		if index < domain.ArchaeologyNativeMaxProjects {
			value.Config.SelectedProjectIDs = append(value.Config.SelectedProjectIDs, candidateID)
		}
	}
	for index := 0; index < 100; index++ {
		value.Runs = append(value.Runs, domain.ArchaeologyRun{ID: fmt.Sprintf("run-%03d", index), ProjectID: fmt.Sprintf("project-%03d", index), State: "completed", PhaseLabel: strings.Repeat("F", 120), Error: strings.Repeat("E", 500), UpdatedAt: now})
	}
	batch := domain.ArchaeologyNativeBatch{ID: strings.Repeat("b", 120), State: "completed", Mode: "app_server_dynamic_tools", MaxConcurrency: 2, PolicyAttested: true, Policy: domain.ArchaeologyExecutionPolicy{Depth: "deep", Sources: domain.ArchaeologySources{Docs: true}}, CreatedAt: now, UpdatedAt: now}
	for index := 0; index < domain.ArchaeologyNativeMaxProjects; index++ {
		batch.Jobs = append(batch.Jobs, domain.ArchaeologyNativeJob{
			ID: fmt.Sprintf("job-%03d-%s", index, strings.Repeat("j", 100)), BatchID: batch.ID, CandidateID: value.Config.SelectedProjectIDs[index], ProjectID: fmt.Sprintf("project-%03d", index), Mode: "app_server_dynamic_tools", State: "completed",
			ThreadID: strings.Repeat("t", 120), TurnID: strings.Repeat("u", 120), PhaseLabel: strings.Repeat("L", 120), SourcesExamined: 1000, CreatedAt: now, UpdatedAt: now,
		})
	}
	value.NativeBatches = []domain.ArchaeologyNativeBatch{batch}
	for index := 0; index < domain.ArchaeologyNativeMaxProjects*2; index++ {
		outcome := domain.ArchaeologyOutcome{ID: fmt.Sprintf("outcome-%03d", index), Title: strings.Repeat("T", 200), Summary: strings.Repeat("S", 300), ProjectID: fmt.Sprintf("project-%03d", index/2), SourceCount: 1000, ProposalJSON: `{}`}
		for sourceIndex := 0; sourceIndex < 4; sourceIndex++ {
			outcome.Provenance = append(outcome.Provenance, domain.ArchaeologyProvenance{Kind: "docs", StableID: strings.Repeat("d", 300), Digest: "sha256:" + strings.Repeat("a", 64), OccurredAt: now})
		}
		outcome.Contributors = []domain.ArchaeologyContributor{{SessionID: fmt.Sprintf("session-%03d-%s", index, strings.Repeat("s", 100)), Contribution: strings.Repeat("C", 300), DemonstratedStrength: strings.Repeat("G", 120), Uncertainty: strings.Repeat("U", 200), Confidence: "verified"}}
		value.Outcomes = append(value.Outcomes, outcome)
	}
	view := (&Service{}).archaeologySessionView(value)
	encoded, err := json.Marshal(struct {
		Data ArchaeologySession `json:"data"`
	}{Data: view})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) >= 1<<20 {
		t.Fatalf("maximum archaeology response=%d bytes", len(encoded))
	}
	if len(view.Discovery.Candidates) != 100 || len(view.Runs) != 100 || view.Handoff == nil || len(view.Handoff.Tasks) != 30 || view.Review == nil || len(view.Review.ProposedOutcomes) != 60 || len(view.Review.MemberSessions) != 60 {
		t.Fatalf("bounded cardinality candidates=%d runs=%d handoff=%+v review=%+v", len(view.Discovery.Candidates), len(view.Runs), view.Handoff, view.Review)
	}
}

func TestLegacyArchaeologyMaximumBoundedCanonicalResponseFitsBrowserBudget(t *testing.T) {
	now := time.Now().UTC()
	value := domain.ArchaeologySession{
		ID: "AR-legacy-max", State: "completed", DiscoveryState: "ready", Revision: 100, UpdatedAt: now,
		Config:  domain.ArchaeologyConfig{Depth: "deep", Sources: domain.ArchaeologySources{Git: true, Docs: true, CodexHistory: true}, MaxConcurrency: 2},
		Handoff: &domain.ArchaeologyHandoff{ID: strings.Repeat("h", 120), State: "claimed", ClaimedBy: strings.Repeat("c", 120), CreatedAt: now, UpdatedAt: now, ClaimedAt: now},
	}
	for index := 0; index < 100; index++ {
		candidateID := fmt.Sprintf("candidate-%03d-%s", index, strings.Repeat("i", 100))
		value.Config.SelectedProjectIDs = append(value.Config.SelectedProjectIDs, candidateID)
		value.Candidates = append(value.Candidates, domain.ArchaeologyCandidate{
			ID: candidateID, CanonicalProjectID: fmt.Sprintf("project-%03d", index), Name: strings.Repeat("N", 200), PathLabel: strings.Repeat("P", 300), RepositoryLabel: strings.Repeat("R", 300),
			LastActivityAt: now, HasGit: true, HasDocs: true, HasCodexHistory: true, FromCodexMetadata: true, FromConfiguredRoot: true,
			DurationMinSeconds: 1, DurationMaxSeconds: 600, RelativeCost: "high", PrivacyNote: strings.Repeat("V", 500), CodexThreadCount: 10000,
		})
		value.Runs = append(value.Runs, domain.ArchaeologyRun{ID: fmt.Sprintf("run-%03d", index), ProjectID: candidateID, State: "completed", PhaseLabel: strings.Repeat("F", 120), Error: strings.Repeat("E", 500), UpdatedAt: now})
		value.TaskLaunches = append(value.TaskLaunches, domain.ArchaeologyTaskLaunch{ID: fmt.Sprintf("launch-%03d-%s", index, strings.Repeat("l", 100)), ProjectID: candidateID, State: "completed", ThreadID: strings.Repeat("t", 120), TurnID: strings.Repeat("u", 120), CreatedAt: now, UpdatedAt: now})
	}
	for outcomeIndex := 0; outcomeIndex < domain.ArchaeologyLegacyOutcomePage; outcomeIndex++ {
		outcome := domain.ArchaeologyOutcome{ID: fmt.Sprintf("legacy-outcome-%03d", outcomeIndex), Title: strings.Repeat("T", 300), Summary: strings.Repeat("S", 4000), ProjectID: fmt.Sprintf("project-%03d", outcomeIndex), SourceCount: 1000, ProposalJSON: `{}`}
		for sourceIndex := 0; sourceIndex < domain.ArchaeologyLegacyProvenancePerOutcome; sourceIndex++ {
			outcome.Provenance = append(outcome.Provenance, domain.ArchaeologyProvenance{Kind: "docs", StableID: strings.Repeat("d", 300), Digest: "sha256:" + strings.Repeat("a", 64), OccurredAt: now})
		}
		for memberIndex := 0; memberIndex < domain.ArchaeologyLegacyContributorsPerOutcome; memberIndex++ {
			outcome.Contributors = append(outcome.Contributors, domain.ArchaeologyContributor{SessionID: fmt.Sprintf("session-%03d-%d-%s", outcomeIndex, memberIndex, strings.Repeat("s", 170)), Contribution: strings.Repeat("C", 1000), DemonstratedStrength: strings.Repeat("G", 300), Uncertainty: strings.Repeat("U", 500), Confidence: "verified"})
		}
		value.Outcomes = append(value.Outcomes, outcome)
	}
	view := (&Service{}).archaeologySessionView(value)
	encoded, err := json.Marshal(struct {
		Data ArchaeologySession `json:"data"`
	}{Data: view})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) >= 1<<20 {
		t.Fatalf("maximum bounded legacy archaeology response=%d bytes", len(encoded))
	}
	if len(view.Discovery.Candidates) != 100 || len(view.Runs) != 100 || view.Handoff == nil || len(view.Handoff.Tasks) != 100 || view.Review == nil || len(view.Review.ProposedOutcomes) != domain.ArchaeologyLegacyOutcomePage || len(view.Review.MemberSessions) != domain.ArchaeologyLegacyOutcomePage*domain.ArchaeologyLegacyContributorsPerOutcome {
		t.Fatalf("bounded legacy cardinality candidates=%d runs=%d handoff=%+v review=%+v", len(view.Discovery.Candidates), len(view.Runs), view.Handoff, view.Review)
	}
}

func TestProjectArchaeologySeparatesTaskLaunchCapabilityFromStartEligibility(t *testing.T) {
	for _, test := range []struct {
		name          string
		launcher      ArchaeologyHistorianLauncher
		selected      []string
		canonicalID   string
		wantAvailable bool
		wantCanStart  bool
	}{
		{
			name:          "healthy runtime with empty persisted selection",
			launcher:      &fakeArchaeologyLauncher{},
			wantAvailable: true,
		},
		{
			name:          "healthy runtime with mapped persisted selection",
			launcher:      &fakeArchaeologyLauncher{},
			selected:      []string{"project"},
			canonicalID:   "project",
			wantAvailable: true,
			wantCanStart:  true,
		},
		{
			name:         "unavailable runtime with mapped persisted selection",
			launcher:     &unavailableTaskLauncher{},
			selected:     []string{"project"},
			canonicalID:  "project",
			wantCanStart: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{archaeologyLauncher: test.launcher}
			view := service.archaeologySessionView(domain.ArchaeologySession{
				State: "draft",
				Config: domain.ArchaeologyConfig{
					SelectedProjectIDs: test.selected,
					Depth:              "standard",
					Sources:            domain.ArchaeologySources{Git: true},
					MaxConcurrency:     2,
				},
				Candidates: []domain.ArchaeologyCandidate{{ID: "project", CanonicalProjectID: test.canonicalID}},
			})
			if !view.Capabilities.TaskLaunch.Configured || view.Capabilities.TaskLaunch.Available != test.wantAvailable {
				t.Fatalf("task launch capability=%+v", view.Capabilities.TaskLaunch)
			}
			if view.Controls.CanStart != test.wantCanStart {
				t.Fatalf("start control=%+v", view.Controls)
			}
		})
	}
}

func TestProjectArchaeologyExportsTaskPackWithoutSupportedLauncher(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service := New(repository, nil, nil)
	service.ConfigureProjectArchaeology(fakeArchaeologyDiscoverer{}, nil)
	session, err := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "discover")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.ConfigureArchaeologySession(ctx, domain.HumanLocalPrincipal, "config", ArchaeologyConfigRequest{SelectedProjectIDs: []string{"p"}, Depth: "standard", Sources: ArchaeologySources{Git: true}, MaxConcurrency: 2, BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "start", ArchaeologyTransitionRequest{BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if started.Handoff == nil || started.Handoff.State != "ready_to_claim" || started.State != "draft" || len(started.Runs) != 0 {
		t.Fatalf("started=%+v", started)
	}
	persisted, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != "draft" || len(persisted.Runs) != 0 || persisted.Handoff == nil {
		t.Fatalf("unsupported launcher mutated state=%+v", persisted)
	}
}
func TestProjectArchaeologyDoesNotInvokeLegacyLauncher(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service := New(repository, nil, nil)
	launcher := &fakeArchaeologyLauncher{}
	service.ConfigureProjectArchaeology(fakeArchaeologyDiscoverer{}, launcher)
	session, err := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "discover")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.ConfigureArchaeologySession(ctx, domain.HumanLocalPrincipal, "config", ArchaeologyConfigRequest{SelectedProjectIDs: []string{"p"}, Depth: "quick", Sources: ArchaeologySources{Git: true}, MaxConcurrency: 1, BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "start", ArchaeologyTransitionRequest{BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if launcher.launches != 0 || started.State != "draft" || started.Handoff == nil || started.Handoff.State != "ready_to_claim" {
		t.Fatalf("launches=%d started=%+v", launcher.launches, started)
	}
}

type manyArchaeologyDiscoverer struct{ count int }

func (f manyArchaeologyDiscoverer) DiscoverMetadata(context.Context) (domain.ArchaeologyDiscovery, error) {
	out := domain.ArchaeologyDiscovery{Candidates: make([]domain.ArchaeologyCandidate, 0, f.count)}
	for index := 0; index < f.count; index++ {
		id := fmt.Sprintf("project-%02d", index)
		out.Candidates = append(out.Candidates, domain.ArchaeologyCandidate{ID: id, Name: id, PathLabel: id, HasGit: true, DurationMinSeconds: 60, DurationMaxSeconds: 120, RelativeCost: "low", PrivacyNote: "Metadata only."})
	}
	return out, nil
}

type boundedTaskLauncher struct {
	mu                 sync.Mutex
	active, max, total int
	perProject         map[string]int
}
type unavailableTaskLauncher struct{ calls int }

func (*unavailableTaskLauncher) Available(context.Context) error { return domain.ErrUnavailable }
func (*unavailableTaskLauncher) Launch(context.Context, domain.ArchaeologySession) error {
	return domain.ErrUnavailable
}
func (f *unavailableTaskLauncher) LaunchProject(context.Context, domain.ArchaeologySession, domain.ArchaeologyCandidate, string, string) (domain.ArchaeologyLaunchResult, error) {
	f.calls++
	return domain.ArchaeologyLaunchResult{}, domain.ErrUnavailable
}

func TestProjectArchaeologyUnavailableNativeLauncherDoesNotMutateOrLaunch(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	launcher := &unavailableTaskLauncher{}
	service := New(repository, nil, nil)
	service.ConfigureProjectArchaeology(fakeArchaeologyDiscoverer{}, launcher)
	session, err := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "discover-unavailable")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.ConfigureArchaeologySession(ctx, domain.HumanLocalPrincipal, "config-unavailable", ArchaeologyConfigRequest{SelectedProjectIDs: []string{"p"}, Depth: "standard", Sources: ArchaeologySources{Git: true}, MaxConcurrency: 2, BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	before, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "start-unavailable", ArchaeologyTransitionRequest{BaseRevision: session.Revision})
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("start err=%v", err)
	}
	after, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if launcher.calls != 0 || after.Revision != before.Revision || after.Handoff != nil || len(after.TaskLaunches) != 0 {
		t.Fatalf("calls=%d before_revision=%d after=%+v", launcher.calls, before.Revision, after)
	}
}

func (*boundedTaskLauncher) Available(context.Context) error { return nil }
func (*boundedTaskLauncher) Launch(context.Context, domain.ArchaeologySession) error {
	return domain.ErrUnavailable
}
func (f *boundedTaskLauncher) LaunchProject(_ context.Context, _ domain.ArchaeologySession, candidate domain.ArchaeologyCandidate, _, _ string) (domain.ArchaeologyLaunchResult, error) {
	f.mu.Lock()
	f.active++
	f.total++
	f.perProject[candidate.ID]++
	if f.active > f.max {
		f.max = f.active
	}
	f.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return domain.ArchaeologyLaunchResult{ThreadID: "thread-" + candidate.ID, CodexSessionID: "session-" + candidate.ID, TurnID: "turn-" + candidate.ID}, nil
}

func TestProjectArchaeologySubmitsThirtySelectedProjectsWithoutCommonsConcurrencyCapOrReplay(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service := New(repository, nil, nil)
	launcher := &boundedTaskLauncher{perProject: map[string]int{}}
	service.ConfigureProjectArchaeology(manyArchaeologyDiscoverer{count: 30}, launcher)
	session, err := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "discover-many")
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]string, 30)
	for index := range selected {
		selected[index] = fmt.Sprintf("project-%02d", index)
	}
	session, err = service.ConfigureArchaeologySession(ctx, domain.HumanLocalPrincipal, "config-many", ArchaeologyConfigRequest{SelectedProjectIDs: selected, Depth: "standard", Sources: ArchaeologySources{Git: true}, MaxConcurrency: 2, BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	baseRevision := session.Revision
	started, err := service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "start-many", ArchaeologyTransitionRequest{BaseRevision: baseRevision})
	if err != nil {
		t.Fatal(err)
	}
	if started.Handoff == nil || len(started.Handoff.Tasks) != 30 {
		t.Fatalf("handoff=%+v", started.Handoff)
	}
	launcher.mu.Lock()
	if launcher.total != 30 || launcher.max <= 2 {
		t.Fatalf("total=%d max=%d", launcher.total, launcher.max)
	}
	for _, id := range selected {
		if launcher.perProject[id] != 1 {
			t.Fatalf("project %s launches=%d", id, launcher.perProject[id])
		}
	}
	launcher.mu.Unlock()
	if _, err = service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "start-many", ArchaeologyTransitionRequest{BaseRevision: baseRevision}); err != nil {
		t.Fatal(err)
	}
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	if launcher.total != 30 {
		t.Fatalf("idempotent replay created tasks: total=%d", launcher.total)
	}
}

func TestProjectArchaeologyUpgradeRediscoveryStartsOneDirectTask(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service := New(repository, nil, nil)
	launcher := &boundedTaskLauncher{perProject: map[string]int{}}
	service.ConfigureProjectArchaeology(manyArchaeologyDiscoverer{count: 2}, launcher)
	session, err := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "legacy-discover-app")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.ConfigureArchaeologySession(ctx, domain.HumanLocalPrincipal, "legacy-config-app", ArchaeologyConfigRequest{SelectedProjectIDs: []string{"project-00"}, Depth: "quick", Sources: ArchaeologySources{Git: true}, MaxConcurrency: 1, BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := repository.StartArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "legacy-start-app", BaseRevision: session.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Handoff == nil || len(legacy.TaskLaunches) != 0 {
		t.Fatalf("legacy=%+v", legacy)
	}

	rediscovered, err := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "native-rediscover-app")
	if err != nil {
		t.Fatal(err)
	}
	if len(rediscovered.Discovery.Candidates) != 2 {
		t.Fatalf("candidates=%+v", rediscovered.Discovery.Candidates)
	}
	configured, err := service.ConfigureArchaeologySession(ctx, domain.HumanLocalPrincipal, "native-config-app", ArchaeologyConfigRequest{SelectedProjectIDs: []string{"project-01"}, Depth: "standard", Sources: ArchaeologySources{Git: true}, MaxConcurrency: 1, BaseRevision: rediscovered.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if configured.Config.MaxConcurrency != 2 || configured.Handoff != nil || !configured.Controls.CanStart {
		t.Fatalf("legacy handoff still controls native UI: handoff=%+v controls=%+v", configured.Handoff, configured.Controls)
	}
	baseRevision := configured.Revision
	started, err := service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "native-start-app", ArchaeologyTransitionRequest{BaseRevision: baseRevision})
	if err != nil {
		t.Fatal(err)
	}
	if started.Handoff == nil || started.Handoff.ID != "" || started.Handoff.State != "launching" ||
		len(started.Handoff.AllowedActions) != 0 || len(started.Handoff.Tasks) != 1 ||
		started.Handoff.Tasks[0].ProjectID != "project-01" || started.Handoff.Tasks[0].State != "task_created" {
		t.Fatalf("started=%+v", started)
	}
	launcher.mu.Lock()
	if launcher.total != 1 || launcher.perProject["project-01"] != 1 {
		t.Fatalf("launches=%d per=%v", launcher.total, launcher.perProject)
	}
	launcher.mu.Unlock()
	if _, err = service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "native-start-app", ArchaeologyTransitionRequest{BaseRevision: baseRevision}); err != nil {
		t.Fatal(err)
	}
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	if launcher.total != 1 {
		t.Fatalf("replay launched duplicate task: %d", launcher.total)
	}
	var handoffs int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_handoffs WHERE session_id=?`, legacy.ID).Scan(&handoffs); err != nil {
		t.Fatal(err)
	}
	if handoffs != 1 {
		t.Fatalf("legacy audit rows=%d", handoffs)
	}
	var imports int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM historical_import_batches`).Scan(&imports); err != nil {
		t.Fatal(err)
	}
	if imports != 0 {
		t.Fatalf("upgrade path auto-imported history: %d", imports)
	}
}

func TestNativeSchedulerHidesClaimedLegacyLaunchRowsFromCurrentWorkflow(t *testing.T) {
	service := &Service{archaeologyScheduler: &ArchaeologyScheduler{}}
	value := domain.ArchaeologySession{
		State: "draft",
		Config: domain.ArchaeologyConfig{
			SelectedProjectIDs: []string{"project"},
			Depth:              "standard",
			Sources:            domain.ArchaeologySources{Git: true},
			MaxConcurrency:     2,
		},
		Candidates: []domain.ArchaeologyCandidate{{ID: "project", CanonicalProjectID: "project"}},
		Handoff:    &domain.ArchaeologyHandoff{ID: "legacy-handoff", State: "ready_to_claim"},
	}
	for index := 0; index < 9; index++ {
		value.TaskLaunches = append(value.TaskLaunches, domain.ArchaeologyTaskLaunch{
			ID:        fmt.Sprintf("legacy-launch-%d", index),
			ProjectID: "project",
			State:     "claimed",
		})
	}

	view := service.archaeologySessionView(value)
	if view.Handoff != nil {
		t.Fatalf("legacy launches replaced current workflow: %+v", view.Handoff)
	}
	if !view.Controls.CanStart {
		t.Fatalf("legacy audit rows blocked a new native run: %+v", view.Controls)
	}
	if len(value.TaskLaunches) != 9 || value.Handoff == nil {
		t.Fatalf("projection mutated durable audit input: launches=%d handoff=%+v", len(value.TaskLaunches), value.Handoff)
	}
}

func TestArchaeologyViewCoheresAsyncNativeTimestampsWithoutMutatingAuditRows(t *testing.T) {
	sessionAt := time.Date(2026, 8, 14, 14, 52, 19, 724165176, time.UTC)
	batchAt := sessionAt.Add(70 * time.Millisecond)
	jobAt := batchAt.Add(4 * time.Millisecond)
	value := domain.ArchaeologySession{
		ID:             "AR-coherent",
		State:          "cancel_requested",
		DiscoveryState: "ready",
		Revision:       4,
		UpdatedAt:      sessionAt,
		Config: domain.ArchaeologyConfig{
			SelectedProjectIDs: []string{"candidate"},
			Depth:              "standard",
			Sources:            domain.ArchaeologySources{Git: true},
			MaxConcurrency:     2,
		},
		NativeBatches: []domain.ArchaeologyNativeBatch{{
			ID: "batch", State: "cancel_requested", MaxConcurrency: 2, PolicyAttested: true, UpdatedAt: batchAt,
			Policy: domain.ArchaeologyExecutionPolicy{Depth: "standard", Sources: domain.ArchaeologySources{Git: true}},
			Jobs:   []domain.ArchaeologyNativeJob{{ID: "job", BatchID: "batch", CandidateID: "candidate", ProjectID: "project", State: "cancel_requested", UpdatedAt: jobAt}},
		}},
	}
	view := archaeologyView(value)
	if view.UpdatedAt == nil || !view.UpdatedAt.Equal(jobAt) || view.Handoff == nil || view.Handoff.UpdatedAt == nil || !view.Handoff.UpdatedAt.Equal(jobAt) || view.Handoff.Progress.UpdatedAt == nil || !view.Handoff.Progress.UpdatedAt.Equal(jobAt) {
		t.Fatalf("session=%v handoff=%+v", view.UpdatedAt, view.Handoff)
	}
	if !value.UpdatedAt.Equal(sessionAt) || !value.NativeBatches[0].UpdatedAt.Equal(batchAt) || !value.NativeBatches[0].Jobs[0].UpdatedAt.Equal(jobAt) {
		t.Fatalf("projection mutated durable input: %+v", value)
	}
}

type archaeologyEligibilityHistoryRepository struct {
	detail domain.ArchaeologyBatchDetail
}

func (archaeologyEligibilityHistoryRepository) HomeSnapshot(context.Context, domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	return domain.HomeDurableSnapshot{}, nil
}

func (r archaeologyEligibilityHistoryRepository) ArchaeologyBatchHistory(context.Context, string, domain.ArchaeologyBatchHistoryQuery) (domain.ArchaeologyBatchHistoryPage, error) {
	return domain.ArchaeologyBatchHistoryPage{}, nil
}

func (r archaeologyEligibilityHistoryRepository) ArchaeologyBatch(context.Context, string, string) (domain.ArchaeologyBatchDetail, error) {
	return r.detail, nil
}

func (archaeologyEligibilityHistoryRepository) ArchaeologyBatchOutcomes(context.Context, string, string, domain.ArchaeologyOutcomePageQuery) (domain.ArchaeologyOutcomePage, error) {
	return domain.ArchaeologyOutcomePage{}, nil
}

func TestNativeApplyCapabilityUsesTheSameEligibilitySnapshotForSessionAndDetail(t *testing.T) {
	for _, test := range []struct {
		name         string
		eligibility  domain.ArchaeologyBatchEligibility
		wantCanApply bool
	}{
		{name: "eligible", eligibility: domain.ArchaeologyBatchEligibility{Eligible: true}, wantCanApply: true},
		{name: "ineligible", eligibility: domain.ArchaeologyBatchEligibility{Eligible: false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			batch := domain.ArchaeologyNativeBatch{ID: "batch", State: "completed", Eligibility: test.eligibility}
			detail := domain.ArchaeologyBatchDetail{Batch: batch, Outcomes: []domain.ArchaeologyOutcome{{ID: "outcome", Title: "Outcome", ProjectID: "project"}}}
			service := &Service{repository: archaeologyEligibilityHistoryRepository{detail: detail}, nativeApplyEnabled: true}

			sessionView := service.archaeologySessionView(domain.ArchaeologySession{
				NativeReviewBatchID: "batch",
				NativeBatches:       []domain.ArchaeologyNativeBatch{batch},
				Outcomes:            detail.Outcomes,
			})
			if sessionView.Review == nil || sessionView.Review.CanApply != test.wantCanApply || sessionView.Capabilities.CanonicalApply.Available != test.wantCanApply {
				t.Fatalf("session view=%+v", sessionView)
			}
			batchView, err := service.ProjectArchaeologyBatch(context.Background(), domain.HumanLocalPrincipal, "batch")
			if err != nil {
				t.Fatal(err)
			}
			if batchView.Review == nil || batchView.Review.CanApply != test.wantCanApply {
				t.Fatalf("batch view=%+v", batchView)
			}
		})
	}
}
