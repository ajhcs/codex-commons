package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

type fakeArchaeologyDiscoverer struct{}

func (fakeArchaeologyDiscoverer) DiscoverMetadata(context.Context) (domain.ArchaeologyDiscovery, error) {
	return domain.ArchaeologyDiscovery{SourceRootsScanned: 1, Candidates: []domain.ArchaeologyCandidate{{ID: "p", Name: "Project", PathLabel: "~/…/project", HasGit: true, DurationMinSeconds: 60, DurationMaxSeconds: 120, RelativeCost: "low", PrivacyNote: "Metadata only."}}}, nil
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
