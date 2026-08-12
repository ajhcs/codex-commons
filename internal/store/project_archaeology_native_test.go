package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/migrations"
	_ "modernc.org/sqlite"
)

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
	value, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start-1", BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	return s, value
}

func TestNativeSchedulerGlobalActiveCapAndUncertainGate(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 3, 2)
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
	if _, err = s.ClaimArchaeologyNativeJob(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("third claim err=%v", err)
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
	outcome := domain.ArchaeologyOutcome{ID: "agent-outcome", Title: "First", Summary: "Grounded", ProjectID: "a", SourceCount: 1, ProposalJSON: `{"schema_version":1}`, Provenance: []domain.ArchaeologyProvenance{{Kind: "git", StableID: "commit:abc", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OccurredAt: time.Now().UTC().Add(-time.Hour)}}}
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
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure-2", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"a"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start-2", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	second, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, second.ID, "thread-b", "session-b", "turn-b"); err != nil {
		t.Fatal(err)
	}
	secondDigest := [32]byte{2}
	if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: second.ID, ThreadID: "thread-b", TurnID: "turn-b", Digest: secondDigest, Outcomes: []domain.ArchaeologyOutcome{outcome}}); err != nil {
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
	if len(value.NativeBatches) != 2 || len(value.Outcomes) != 2 {
		t.Fatalf("batches=%d outcomes=%d", len(value.NativeBatches), len(value.Outcomes))
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
	var cancelUncertain int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_jobs WHERE state='uncertain'`).Scan(&cancelUncertain); err != nil {
		t.Fatal(err)
	}
	if cancelUncertain != 2 {
		t.Fatalf("cancel restart uncertain=%d", cancelUncertain)
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
	var batches int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_batches`).Scan(&batches); err != nil {
		t.Fatal(err)
	}
	if batches != 1 {
		t.Fatalf("replay batches=%d", batches)
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

func TestNativeStartFailureTerminatesBatchWithAttention(t *testing.T) {
	ctx := context.Background()
	s, _ := nativeTestSession(t, 1, 1)
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FailArchaeologyNativeStart(ctx, job.ID, false); err != nil {
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
				outcome := domain.ArchaeologyOutcome{ID: "outcome", Title: "Report", Summary: "Grounded", ProjectID: "a", SourceCount: 1, ProposalJSON: `{"schema_version":1}`, Provenance: []domain.ArchaeologyProvenance{{Kind: "git", StableID: "commit:abc", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OccurredAt: time.Now().UTC().Add(-time.Hour)}}}
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
