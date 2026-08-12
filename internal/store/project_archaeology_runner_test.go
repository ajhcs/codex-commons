package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func TestArchaeologyHandoffClaimsExactSessionAndPreservesOfflineMembers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 12, 14, 0, 0, 0, time.UTC)
	s, err := Open(ctx, ":memory:", WithClock(func() time.Time { return now }))
	must(t, err)
	defer s.Close()
	principal := domain.HumanLocalPrincipal
	discovered, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: principal, RequestID: "d"}, domain.ArchaeologyDiscovery{SourceRootsScanned: 1, Candidates: []domain.ArchaeologyCandidate{{ID: "a", Name: "A", PathLabel: "A", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}, {ID: "b", Name: "B", PathLabel: "B", HasDocs: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	must(t, err)
	if _, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: principal, RequestID: "incompatible", BaseRevision: discovered.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"b"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("incompatible source selection err=%v", err)
	}
	configured, err := s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: principal, RequestID: "c", BaseRevision: discovered.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"a", "b"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true, Docs: true}, MaxConcurrency: 1}})
	must(t, err)
	started, err := s.StartArchaeology(ctx, domain.ArchaeologyMutation{Principal: principal, RequestID: "s", BaseRevision: configured.Revision})
	must(t, err)
	claimed, err := s.ClaimArchaeologyHandoff(ctx, domain.ArchaeologyHandoffClaim{HandoffID: started.Handoff.ID, RequestID: "claim", SessionID: "runner-1"})
	must(t, err)
	if _, err = s.ClaimArchaeologyHandoff(ctx, domain.ArchaeologyHandoffClaim{HandoffID: started.Handoff.ID, RequestID: "claim", SessionID: "runner-1"}); err != nil {
		t.Fatalf("claim replay: %v", err)
	}
	if _, err = s.ClaimArchaeologyHandoff(ctx, domain.ArchaeologyHandoffClaim{HandoffID: started.Handoff.ID, RequestID: "other-claim", SessionID: "runner-2"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("other claimant err=%v", err)
	}
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if claimed.State != "running" || claimed.Handoff.ClaimedBy != "runner-1" {
		t.Fatalf("claimed=%+v", claimed)
	}
	if _, err = s.PauseArchaeology(ctx, domain.ArchaeologyMutation{Principal: principal, RequestID: "unsupported-pause", BaseRevision: claimed.Revision}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("exported handoff pause err=%v", err)
	}
	wrongReport := domain.ArchaeologyHandoffReport{HandoffID: started.Handoff.ID, RequestID: "wrong-report", SessionID: "runner-2", Outcomes: []domain.ArchaeologyOutcome{{ProjectID: "a", Title: "Wrong", Summary: "Wrong session", SourceCount: 1, ProposalJSON: `{}`, Provenance: []domain.ArchaeologyProvenance{{Kind: "git", StableID: "commit:abc", Digest: digest, OccurredAt: now}}}}}
	if _, err = s.ReportArchaeologyHandoff(ctx, wrongReport); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("wrong reporter err=%v", err)
	}
	completed, err := s.ReportArchaeologyHandoff(ctx, domain.ArchaeologyHandoffReport{HandoffID: started.Handoff.ID, RequestID: "report", SessionID: "runner-1", Outcomes: []domain.ArchaeologyOutcome{{ProjectID: "a", Title: "Historical outcome", Summary: "Bounded result", SourceCount: 1, ProposalJSON: `{"schema_version":1}`, Provenance: []domain.ArchaeologyProvenance{{Kind: "git", StableID: "commit:abc", Digest: digest, OccurredAt: now}}, Contributors: []domain.ArchaeologyContributor{{SessionID: "019-session-exact", Contribution: "Implemented the outcome", DemonstratedStrength: "migration design", Uncertainty: "historical task is offline", Confidence: "verified"}}}}})
	must(t, err)
	if len(completed.Outcomes) != 1 || len(completed.Outcomes[0].Contributors) != 1 || completed.Outcomes[0].Contributors[0].SessionID != "019-session-exact" {
		t.Fatalf("completed=%+v", completed.Outcomes)
	}
	if completed.State != "completed" || completed.Handoff.State != "completed" {
		t.Fatalf("completed state=%+v", completed)
	}
	if len(completed.Runs) != 2 {
		t.Fatalf("completed runs=%+v", completed.Runs)
	}
	runs := map[string]domain.ArchaeologyRun{}
	for _, run := range completed.Runs {
		runs[run.ProjectID] = run
	}
	if runs["a"].OutcomesFound != 1 || runs["a"].SourcesExamined != 1 || runs["b"].OutcomesFound != 0 || runs["b"].SourcesExamined != 0 {
		t.Fatalf("truthful run accounting=%+v", runs)
	}
}

func TestArchaeologyRunnerRejectsMalformedManifests(t *testing.T) {
	now := time.Now().UTC()
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	base := domain.ArchaeologyOutcome{ProjectID: "p", Title: "t", Summary: "s", SourceCount: 1, ProposalJSON: `{}`, Provenance: []domain.ArchaeologyProvenance{{Kind: "docs", StableID: "doc", Digest: digest, OccurredAt: now}}}
	cases := map[string]domain.ArchaeologyOutcome{}
	private := base
	private.ProposalJSON = `{"prompt":"raw private prompt"}`
	cases["private narrative"] = private
	duplicateSource := base
	duplicateSource.SourceCount = 2
	duplicateSource.Provenance = append(duplicateSource.Provenance, duplicateSource.Provenance[0])
	cases["duplicate provenance"] = duplicateSource
	duplicateMember := base
	duplicateMember.Contributors = []domain.ArchaeologyContributor{{SessionID: "S-1", Contribution: "one", Confidence: "verified"}, {SessionID: "S-1", Contribution: "two", Confidence: "supported"}}
	cases["duplicate member"] = duplicateMember
	oversizedID := base
	oversizedID.ID = strings.Repeat("x", 121)
	cases["oversized outcome id"] = oversizedID
	understatedSources := base
	understatedSources.Provenance = append(understatedSources.Provenance, domain.ArchaeologyProvenance{Kind: "git", StableID: "commit", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", OccurredAt: now})
	cases["understated source count"] = understatedSources
	for name, outcome := range cases {
		t.Run(name, func(t *testing.T) {
			if validArchaeologyOutcome(outcome, stamp(now.Add(time.Minute))) {
				t.Fatal("malformed manifest accepted")
			}
		})
	}
}
