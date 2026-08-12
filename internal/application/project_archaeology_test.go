package application

import (
	"context"
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

func TestProjectArchaeologyLaunchesTenSelectedProjectsWithBoundedConcurrencyAndNoReplay(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service := New(repository, nil, nil)
	launcher := &boundedTaskLauncher{perProject: map[string]int{}}
	service.ConfigureProjectArchaeology(manyArchaeologyDiscoverer{count: 12}, launcher)
	session, err := service.DiscoverProjectArchaeology(ctx, domain.HumanLocalPrincipal, "discover-many")
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]string, 10)
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
	if started.Handoff == nil || len(started.Handoff.Tasks) != 10 {
		t.Fatalf("handoff=%+v", started.Handoff)
	}
	launcher.mu.Lock()
	if launcher.total != 10 || launcher.max < 2 || launcher.max > 2 {
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
	if launcher.total != 10 {
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
	if configured.Handoff != nil || !configured.Controls.CanStart {
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
