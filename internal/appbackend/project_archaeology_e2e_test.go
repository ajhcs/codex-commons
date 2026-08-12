package appbackend

import (
	"context"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	commonsstore "codex-commons/internal/store"
)

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
