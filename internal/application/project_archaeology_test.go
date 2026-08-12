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
