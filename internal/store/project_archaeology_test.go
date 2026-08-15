package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func TestProjectArchaeologyIdempotencyBoundsAndRestart(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	s, err := Open(ctx, ":memory:", WithClock(func() time.Time { return now }))
	must(t, err)
	defer s.Close()
	discovery := domain.ArchaeologyDiscovery{SourceRootsScanned: 2, Candidates: []domain.ArchaeologyCandidate{{ID: "commons", Name: "Codex Commons", PathLabel: "~/…/codex-commons", RepositoryLabel: "codex-commons", HasGit: true, HasDocs: true, HasCodexHistory: true, DurationMinSeconds: 240, DurationMaxSeconds: 480, RelativeCost: "medium", PrivacyNote: "Metadata only until started."}}}
	mutation := domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover-1"}
	first, err := s.ReplaceArchaeologyDiscovery(ctx, mutation, discovery)
	must(t, err)
	if !first.MetadataOnly || first.DiscoveryState != "ready" || len(first.Candidates) != 1 || first.Revision != 2 {
		t.Fatalf("first=%+v", first)
	}
	replay, err := s.ReplaceArchaeologyDiscovery(ctx, mutation, discovery)
	must(t, err)
	if replay.Revision != first.Revision {
		t.Fatalf("replay revision=%d want=%d", replay.Revision, first.Revision)
	}
	changed := discovery
	changed.SourceRootsScanned = 3
	changedReplay, err := s.ReplaceArchaeologyDiscovery(ctx, mutation, changed)
	must(t, err)
	if changedReplay.Revision != first.Revision || changedReplay.SourceRootsScanned != first.SourceRootsScanned {
		t.Fatalf("changed transport replay=%+v want original discovery", changedReplay)
	}
	configured, err := s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "config-1", BaseRevision: first.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"commons"}, Depth: "deep", Sources: domain.ArchaeologySources{Git: true, Docs: true, CodexHistory: true}, MaxConcurrency: 2}})
	must(t, err)
	if len(configured.Config.SelectedProjectIDs) != 1 || !configured.Config.Sources.CodexHistory {
		t.Fatalf("configured=%+v", configured.Config)
	}
	if _, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "config-1", BaseRevision: first.Revision + 1, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"commons"}, Depth: "deep", Sources: domain.ArchaeologySources{Git: true, Docs: true, CodexHistory: true}, MaxConcurrency: 2}}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed base revision replay err=%v", err)
	}
	if _, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "bad-config", BaseRevision: configured.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"commons"}, Depth: "deep", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 3}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("concurrency err=%v", err)
	}
	started, err := s.StartArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "start-1", BaseRevision: configured.Revision})
	must(t, err)
	if started.State != "draft" || len(started.Runs) != 0 || started.Handoff == nil || started.Handoff.State != "ready_to_claim" {
		t.Fatalf("started=%+v", started)
	}
	var packJSON string
	must(t, s.DB().QueryRowContext(ctx, `SELECT pack_json FROM archaeology_handoffs WHERE id=?`, started.Handoff.ID).Scan(&packJSON))
	if packJSON != "{}" {
		t.Fatalf("legacy prompt pack persisted: %s", packJSON)
	}
	digest := make([]byte, 32)
	digest[0] = 1
	_, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_task_launches(id,session_id,candidate_id,state,client_message_id,request_digest,grant_digest,grant_expires_at,created_at,updated_at) VALUES('launch-restart',?,'commons','starting_codex','message',?,?,?,?,?)`, started.ID, digest, digest, stamp(now.Add(time.Hour)), stamp(now), stamp(now))
	must(t, err)
	must(t, s.ReconcileArchaeology(ctx))
	recovered, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	must(t, err)
	if recovered.State != "draft" || recovered.Handoff == nil || recovered.Handoff.State != "ready_to_claim" {
		t.Fatalf("recovered=%+v", recovered)
	}
	if len(recovered.TaskLaunches) != 1 || recovered.TaskLaunches[0].State != "uncertain" {
		t.Fatalf("interrupted launch was not reconciled truthfully: %+v", recovered.TaskLaunches)
	}
}

func TestProjectArchaeologyUpgradeReusesUntouchedLegacyHandoffAfterRediscovery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 12, 14, 0, 0, 0, time.UTC)
	s, err := Open(ctx, ":memory:", WithClock(func() time.Time { return now }))
	must(t, err)
	defer s.Close()
	legacy := domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "legacy", Name: "Legacy", PathLabel: "Legacy", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}}
	session, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "legacy-discover"}, legacy)
	must(t, err)
	session, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "legacy-config", BaseRevision: session.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"legacy"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	must(t, err)
	session, err = s.StartArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "legacy-start", BaseRevision: session.Revision})
	must(t, err)
	legacyPack := `{"legacy":"preserved audit evidence"}`
	_, err = s.DB().ExecContext(ctx, `UPDATE archaeology_handoffs SET pack_json=? WHERE id=?`, legacyPack, session.Handoff.ID)
	must(t, err)
	var beforeCreated, beforeUpdated string
	must(t, s.DB().QueryRowContext(ctx, `SELECT created_at,updated_at FROM archaeology_handoffs WHERE id=?`, session.Handoff.ID).Scan(&beforeCreated, &beforeUpdated))

	now = now.Add(time.Hour)
	fresh := domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "fresh", Name: "Fresh", PathLabel: "Fresh", HasGit: true, HasCodexHistory: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}}
	session, err = s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "native-rediscover"}, fresh)
	must(t, err)
	session, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "native-config", BaseRevision: session.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"fresh"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	must(t, err)
	session, err = s.StartArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "native-start", BaseRevision: session.Revision})
	must(t, err)
	if session.Handoff == nil || session.Handoff.State != "ready_to_claim" {
		t.Fatalf("handoff=%+v", session.Handoff)
	}
	var count int
	var afterPack, afterCreated, afterUpdated string
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*),pack_json,created_at,updated_at FROM archaeology_handoffs WHERE session_id=?`, session.ID).Scan(&count, &afterPack, &afterCreated, &afterUpdated))
	if count != 1 || afterPack != legacyPack || afterCreated != beforeCreated || afterUpdated != beforeUpdated {
		t.Fatalf("legacy handoff changed count=%d pack=%q created=%q updated=%q", count, afterPack, afterCreated, afterUpdated)
	}
	var launches, outcomes int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_task_launches WHERE session_id=?`, session.ID).Scan(&launches))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_outcomes o JOIN archaeology_runs r ON r.id=o.run_id WHERE r.session_id=?`, session.ID).Scan(&outcomes))
	if launches != 0 || outcomes != 0 {
		t.Fatalf("unexpected launches=%d outcomes=%d", launches, outcomes)
	}
}

func TestProjectArchaeologyMigrationProtectsProvenance(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	must(t, err)
	defer s.Close()
	var count int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'archaeology_provenance_no_%'`).Scan(&count))
	if count != 2 {
		t.Fatalf("append-only triggers=%d", count)
	}
	if _, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_sessions(id,principal,state,discovery_state,created_at,updated_at) VALUES('s','human:test','draft','idle','x','x'); INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note) VALUES('s','p','p','private label',1,0,0,1,2,'low',''); INSERT INTO archaeology_runs(id,session_id,candidate_id,state,created_at,updated_at) VALUES('r','s','p','completed','x','x'); INSERT INTO archaeology_outcomes(id,run_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES('o','r','p','t','s',1,'{}','x'); INSERT INTO archaeology_provenance(outcome_id,position,kind,stable_id,digest,occurred_at) VALUES('o',0,'git','commit','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().ExecContext(ctx, `DELETE FROM archaeology_provenance WHERE outcome_id='o'`); err == nil {
		t.Fatal("append-only provenance was deletable")
	}
}

func TestLegacyArchaeologyProjectionKeepsLatestBoundedReviewWithoutDeletingAuditRows(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	s, err := Open(ctx, ":memory:", WithClock(func() time.Time { return now }))
	must(t, err)
	defer s.Close()
	session, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "legacy-page-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "legacy-page", Name: "Legacy page", PathLabel: "Legacy page", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	must(t, err)
	_, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_runs(id,session_id,candidate_id,state,created_at,updated_at) VALUES('legacy-page-run',?,'legacy-page','completed',?,?)`, session.ID, stamp(now), stamp(now))
	must(t, err)
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for outcomeIndex := 0; outcomeIndex < domain.ArchaeologyLegacyOutcomePage+2; outcomeIndex++ {
		outcomeID := fmt.Sprintf("legacy-outcome-%02d", outcomeIndex)
		at := stamp(now.Add(time.Duration(outcomeIndex) * time.Second))
		_, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_outcomes(id,run_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES(?,'legacy-page-run','legacy-page',?,?,9,'{}',?)`, outcomeID, outcomeID, "bounded audit", at)
		must(t, err)
		for sourceIndex := 0; sourceIndex < domain.ArchaeologyLegacyProvenancePerOutcome+1; sourceIndex++ {
			_, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_provenance(outcome_id,position,kind,stable_id,digest,occurred_at) VALUES(?,?,'git',?,?,?)`, outcomeID, sourceIndex, fmt.Sprintf("commit:%040x", outcomeIndex*100+sourceIndex+1), digest, at)
			must(t, err)
		}
		for memberIndex := 0; memberIndex < domain.ArchaeologyLegacyContributorsPerOutcome+1; memberIndex++ {
			_, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_outcome_contributors(outcome_id,session_id,contribution,confidence) VALUES(?,?,?,'verified')`, outcomeID, fmt.Sprintf("session-%02d", memberIndex), "bounded contributor")
			must(t, err)
		}
	}
	loaded, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	must(t, err)
	if len(loaded.Outcomes) != domain.ArchaeologyLegacyOutcomePage || loaded.Outcomes[0].ID != "legacy-outcome-02" || loaded.Outcomes[len(loaded.Outcomes)-1].ID != "legacy-outcome-31" {
		t.Fatalf("bounded outcomes=%d first=%q last=%q", len(loaded.Outcomes), loaded.Outcomes[0].ID, loaded.Outcomes[len(loaded.Outcomes)-1].ID)
	}
	for _, outcome := range loaded.Outcomes {
		if len(outcome.Provenance) != domain.ArchaeologyLegacyProvenancePerOutcome || len(outcome.Contributors) != domain.ArchaeologyLegacyContributorsPerOutcome {
			t.Fatalf("outcome %s provenance=%d contributors=%d", outcome.ID, len(outcome.Provenance), len(outcome.Contributors))
		}
	}
	var durableOutcomes, durableProvenance, durableContributors int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_outcomes`).Scan(&durableOutcomes))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_provenance`).Scan(&durableProvenance))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_outcome_contributors`).Scan(&durableContributors))
	if durableOutcomes != domain.ArchaeologyLegacyOutcomePage+2 || durableProvenance != durableOutcomes*(domain.ArchaeologyLegacyProvenancePerOutcome+1) || durableContributors != durableOutcomes*(domain.ArchaeologyLegacyContributorsPerOutcome+1) {
		t.Fatalf("durable audit rows outcomes=%d provenance=%d contributors=%d", durableOutcomes, durableProvenance, durableContributors)
	}
}

func TestProjectArchaeologyRefreshRetainsReferencedStaleCandidateTruthfully(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 12, 20, 0, 0, 0, time.UTC)
	s, err := Open(ctx, ":memory:", WithClock(func() time.Time { return now }))
	must(t, err)
	defer s.Close()
	legacy := domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "legacy", Name: "Legacy", PathLabel: "Legacy", HasGit: true, FromCodexMetadata: true, CodexThreadCount: 9, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}}
	session, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "legacy-discover"}, legacy)
	must(t, err)
	digest := make([]byte, 32)
	digest[0] = 1
	_, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_task_launches(id,session_id,candidate_id,state,client_message_id,request_digest,grant_digest,grant_expires_at,created_at,updated_at) VALUES('legacy-launch',?,'legacy','failed','message',?,?,?,?,?)`, session.ID, digest, digest, stamp(now.Add(time.Hour)), stamp(now), stamp(now))
	must(t, err)
	fresh := domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "fresh", Name: "Fresh", PathLabel: "Fresh", HasGit: true, FromCodexMetadata: true, CodexThreadCount: 30, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}}
	refreshed, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "fresh-discover"}, fresh)
	must(t, err)
	if len(refreshed.Candidates) != 2 || len(refreshed.Config.SelectedProjectIDs) != 0 {
		t.Fatalf("candidates=%+v selected=%v", refreshed.Candidates, refreshed.Config.SelectedProjectIDs)
	}
	var retained *domain.ArchaeologyCandidate
	for index := range refreshed.Candidates {
		if refreshed.Candidates[index].ID == "legacy" {
			retained = &refreshed.Candidates[index]
		}
	}
	if retained == nil || retained.FromCodexMetadata || retained.FromConfiguredRoot || retained.Selected || retained.CodexThreadCount != 0 || retained.PrivacyNote != "Retained for historical audit; refresh metadata before selecting." {
		t.Fatalf("retained=%+v", retained)
	}
	var launches int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_task_launches WHERE id='legacy-launch' AND state='failed'`).Scan(&launches))
	if launches != 1 {
		t.Fatalf("legacy launch changed=%d", launches)
	}
}
