package store

import (
	"context"
	"errors"
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
	must(t, s.ReconcileArchaeology(ctx))
	recovered, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	must(t, err)
	if recovered.State != "draft" || recovered.Handoff == nil || recovered.Handoff.State != "ready_to_claim" {
		t.Fatalf("recovered=%+v", recovered)
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
