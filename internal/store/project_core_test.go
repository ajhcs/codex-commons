package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/migrations"
)

func coreTestMeta(key string) domain.CoreWriteMeta {
	return domain.CoreWriteMeta{ActorID: "local-admin", SessionID: "human-local-admin", RequestID: key}
}

func mustCoreProject(t *testing.T, store *Store, id, key string) domain.WriteResult {
	t.Helper()
	result, err := store.CreateCanonicalProject(context.Background(), domain.CreateProjectCommand{
		ID: id, Name: "Project " + id, Purpose: "Durable continuity", Meta: coreTestMeta(key),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProjectCoreCanonicalWritesRelationshipsAndWikiHistory(t *testing.T) {
	store, _ := openTest(t)
	ctx := context.Background()
	created := mustCoreProject(t, store, "alpha", "project-alpha")
	if created.ID != "alpha" || created.Revision != 1 {
		t.Fatalf("project=%+v", created)
	}
	replay := mustCoreProject(t, store, "alpha", "project-alpha")
	if replay != created {
		t.Fatalf("project replay=%+v want=%+v", replay, created)
	}
	if _, err := store.CreateCanonicalProject(ctx, domain.CreateProjectCommand{
		ID: "alpha", Name: "Changed", Purpose: "Durable continuity", Meta: coreTestMeta("project-alpha"),
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed idempotent payload err=%v", err)
	}

	active, err := store.CreateMilestone(ctx, domain.CreateMilestoneCommand{
		ProjectID: "alpha", Title: "Pilot", Status: "active", Position: 0, TargetDate: "2026-09-01", Meta: coreTestMeta("milestone-active"),
	})
	must(t, err)
	if _, err = store.CreateMilestone(ctx, domain.CreateMilestoneCommand{
		ProjectID: "alpha", Title: "Second active", Status: "active", Position: 1, Meta: coreTestMeta("milestone-second-active"),
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second active milestone err=%v", err)
	}
	planned, err := store.CreateMilestone(ctx, domain.CreateMilestoneCommand{
		ProjectID: "alpha", Title: "Later", Status: "planned", Position: 1, Meta: coreTestMeta("milestone-planned"),
	})
	must(t, err)
	if _, err = store.UpdateMilestone(ctx, domain.UpdateMilestoneCommand{
		ID: planned.ID, Title: "Later", Status: "active", Position: 1, BaseRevision: planned.Revision, Meta: coreTestMeta("milestone-promote"),
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("active milestone promotion err=%v", err)
	}

	taskA, err := store.CreateCanonicalTask(ctx, domain.CreateTaskCommand{
		ProjectID: "alpha", Title: "A", Acceptance: "A is complete", Meta: coreTestMeta("task-a"),
	})
	must(t, err)
	taskB, err := store.CreateCanonicalTask(ctx, domain.CreateTaskCommand{
		ProjectID: "alpha", Title: "B", MilestoneID: active.ID, DependencyIDs: []string{taskA.ID}, Meta: coreTestMeta("task-b"),
	})
	must(t, err)
	taskC, err := store.CreateCanonicalTask(ctx, domain.CreateTaskCommand{
		ProjectID: "alpha", Title: "C", DependencyIDs: []string{taskB.ID}, Meta: coreTestMeta("task-c"),
	})
	must(t, err)

	openedB, err := store.TaskOpenSnapshot(ctx, domain.TaskOpenQuery{TaskID: taskB.ID, EventsLimit: 10})
	must(t, err)
	if openedB.Task.Milestone == nil || openedB.Task.Milestone.ID != active.ID || openedB.Task.Milestone.Title != "Pilot" ||
		len(openedB.Task.Dependencies) != 1 || openedB.Task.Dependencies[0].ID != taskA.ID {
		t.Fatalf("task B projection=%+v", openedB.Task)
	}

	if _, err = store.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{
		ID: taskA.ID, Title: "A", DependencyIDs: []string{taskB.ID}, BaseRevision: taskA.Revision, Meta: coreTestMeta("cycle-direct"),
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("direct cycle err=%v", err)
	}
	if _, err = store.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{
		ID: taskA.ID, Title: "A", DependencyIDs: []string{taskC.ID}, BaseRevision: taskA.Revision, Meta: coreTestMeta("cycle-multihop"),
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("multi-hop cycle err=%v", err)
	}
	if _, err = store.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{
		ID: taskA.ID, Title: "A", DependencyIDs: []string{taskA.ID}, BaseRevision: taskA.Revision, Meta: coreTestMeta("cycle-self"),
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("self dependency err=%v", err)
	}
	if _, err = store.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{
		ID: taskA.ID, Title: "A", DependencyIDs: []string{"missing"}, BaseRevision: taskA.Revision, Meta: coreTestMeta("dependency-missing"),
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing dependency err=%v", err)
	}
	mustCoreProject(t, store, "other", "project-other")
	otherTask, err := store.CreateCanonicalTask(ctx, domain.CreateTaskCommand{ProjectID: "other", Title: "Other", Meta: coreTestMeta("task-other")})
	must(t, err)
	if _, err = store.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{
		ID: taskA.ID, Title: "A", DependencyIDs: []string{otherTask.ID}, BaseRevision: taskA.Revision, Meta: coreTestMeta("dependency-cross-project"),
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("cross-project dependency err=%v", err)
	}

	updatedA, err := store.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{
		ID: taskA.ID, Title: "A revised", Description: "Compact detail", BaseRevision: taskA.Revision, Meta: coreTestMeta("task-a-update"),
	})
	must(t, err)
	if _, err = store.UpdateCanonicalTask(ctx, domain.UpdateTaskCommand{
		ID: taskA.ID, Title: "stale", BaseRevision: taskA.Revision, Meta: coreTestMeta("task-a-stale"),
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale task update err=%v", err)
	}
	stateA, err := store.ChangeCanonicalTaskState(ctx, domain.ChangeTaskStateCommand{
		ID: taskA.ID, State: "done", Basis: "Acceptance verified", BaseRevision: updatedA.Revision, Meta: coreTestMeta("task-a-done"),
	})
	must(t, err)
	if stateA.Revision <= updatedA.Revision {
		t.Fatalf("state result=%+v update=%+v", stateA, updatedA)
	}

	wiki1, err := store.AppendWikiRevision(ctx, domain.AppendWikiRevisionCommand{
		ProjectID: "alpha", Slug: "architecture", Title: "Architecture", Summary: "Initial", Body: "needle v1", BaseRevision: 0, Meta: coreTestMeta("wiki-1"),
	})
	must(t, err)
	wiki2, err := store.AppendWikiRevision(ctx, domain.AppendWikiRevisionCommand{
		ProjectID: "alpha", Slug: "architecture", Title: "Architecture", Summary: "Second", Body: "needle v2", BaseRevision: wiki1.Revision, Meta: coreTestMeta("wiki-2"),
	})
	must(t, err)
	if wiki2.Revision != 2 {
		t.Fatalf("wiki revision=%+v", wiki2)
	}
	if _, err = store.AppendWikiRevision(ctx, domain.AppendWikiRevisionCommand{
		ProjectID: "alpha", Slug: "architecture", Title: "Architecture", Summary: "Stale", Body: "lost update", BaseRevision: 1, Meta: coreTestMeta("wiki-stale"),
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale wiki append err=%v", err)
	}
	history, err := store.WikiHistorySnapshot(ctx, domain.WikiHistoryQuery{ProjectID: "alpha", Slug: "architecture", Limit: 10})
	must(t, err)
	if len(history.Items) != 2 || history.Items[0].Revision != 2 || history.Items[1].Revision != 1 {
		t.Fatalf("wiki history=%+v", history)
	}
	historical, err := store.OpenWikiRevision(ctx, "alpha", "architecture", 1)
	must(t, err)
	if historical.Body != "needle v1" {
		t.Fatalf("historical page=%+v", historical)
	}
	wikiSearch, err := store.WikiListSnapshot(ctx, domain.WikiListQuery{ProjectID: "alpha", Search: "needle", Limit: 10})
	must(t, err)
	if wikiSearch.Total != 1 || len(wikiSearch.Items) != 1 || wikiSearch.Items[0].Summary != "Second" {
		t.Fatalf("wiki search=%+v", wikiSearch)
	}

	tasks, err := store.TaskListSnapshot(ctx, domain.TaskListQuery{ProjectID: "alpha", Limit: 25})
	must(t, err)
	if tasks.Total != 3 || tasks.StateCounts.Total != 3 || tasks.StateCounts.Done != 1 {
		t.Fatalf("task list=%+v", tasks)
	}
	browse, err := store.ProjectBrowseSnapshot(ctx, domain.ProjectBrowseQuery{Limit: 10})
	must(t, err)
	var alpha *domain.ProjectBrowseItem
	for i := range browse.Items {
		if browse.Items[i].ID == "alpha" {
			alpha = &browse.Items[i]
		}
	}
	if alpha == nil || alpha.ActiveMilestone == nil || alpha.ActiveMilestone.ID != active.ID ||
		alpha.TaskCounts.Total != 3 || alpha.TaskCounts.Done != 1 || alpha.LastDurableActivity == nil {
		t.Fatalf("enriched project list=%+v", alpha)
	}

	detail, err := store.ProjectCoreSnapshot(ctx, domain.ProjectCoreReadQuery{
		ProjectID: "alpha", ActivityStart: testNow.AddDate(0, 0, -13).Truncate(24 * time.Hour), ActivityEnd: testNow.AddDate(0, 0, 1).Truncate(24 * time.Hour),
	})
	must(t, err)
	if detail.ActiveMilestone == nil || detail.Counts.Tasks != 3 || detail.Counts.Milestones != 2 || detail.Counts.WikiPages != 1 {
		t.Fatalf("project detail=%+v", detail)
	}
}

func TestProjectCoreConcurrentIdempotency(t *testing.T) {
	store, _ := openTest(t)
	command := domain.CreateProjectCommand{ID: "concurrent", Name: "Concurrent", Purpose: "One write", Meta: coreTestMeta("same-create")}
	start := make(chan struct{})
	results := make(chan domain.WriteResult, 2)
	errorsCh := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := store.CreateCanonicalProject(context.Background(), command)
			results <- result
			errorsCh <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent replay err=%v", err)
		}
	}
	for result := range results {
		if result.ID != "concurrent" || result.Revision != 1 {
			t.Fatalf("concurrent result=%+v", result)
		}
	}
	var projects, requests int
	must(t, store.DB().QueryRow(`SELECT count(*) FROM projects WHERE id='concurrent'`).Scan(&projects))
	must(t, store.DB().QueryRow(`SELECT count(*) FROM project_core_requests WHERE operation='project.create'`).Scan(&requests))
	if projects != 1 || requests != 1 {
		t.Fatalf("projects=%d requests=%d", projects, requests)
	}
}

func TestClaimReclaimAtExactExpiryIsAtomicAndHistorical(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	store, err := Open(ctx, filepath.Join(t.TempDir(), "claims.sqlite"), WithClock(func() time.Time { return now }))
	must(t, err)
	defer store.Close()
	mustCoreProject(t, store, "alpha", "project")
	task, err := store.CreateCanonicalTask(ctx, domain.CreateTaskCommand{ProjectID: "alpha", Title: "Lease", Meta: coreTestMeta("task")})
	must(t, err)
	expires := now.Add(time.Hour)
	first, err := store.Claim(ctx, domain.ClaimRequest{TaskID: task.ID, ActorID: "agent-1", SessionID: "S-1", RequestID: "claim-1", LeaseUntil: &expires})
	must(t, err)
	now = expires // equality is expired, not still live
	nextExpires := now.Add(time.Hour)
	start := make(chan struct{})
	claims := make(chan domain.Claim, 2)
	errorsCh := make(chan error, 2)
	var wait sync.WaitGroup
	for i, session := range []string{"S-2", "S-3"} {
		wait.Add(1)
		go func(index int, session string) {
			defer wait.Done()
			<-start
			claim, claimErr := store.Claim(ctx, domain.ClaimRequest{
				TaskID: task.ID, ActorID: "agent-" + session, SessionID: session,
				RequestID: "reclaim-" + string(rune('0'+index)), LeaseUntil: &nextExpires,
			})
			claims <- claim
			errorsCh <- claimErr
		}(i, session)
	}
	close(start)
	wait.Wait()
	close(claims)
	close(errorsCh)
	successes, conflicts := 0, 0
	for err := range errorsCh {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrConflict):
			conflicts++
		default:
			t.Fatalf("reclaim err=%v", err)
		}
	}
	var winner domain.Claim
	for claim := range claims {
		if claim.ID != "" {
			winner = claim
		}
	}
	if successes != 1 || conflicts != 1 || winner.ID == "" || winner.ID == first.ID {
		t.Fatalf("success=%d conflict=%d winner=%+v first=%+v", successes, conflicts, winner, first)
	}
	var history, current, reclaimed int
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM task_claims WHERE task_id=?`, task.ID).Scan(&history))
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM task_current_claims WHERE task_id=? AND claim_id=?`, task.ID, winner.ID).Scan(&current))
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM task_events WHERE task_id=? AND kind='reclaimed'`, task.ID).Scan(&reclaimed))
	if history != 2 || current != 1 || reclaimed != 1 {
		t.Fatalf("history=%d current=%d reclaimed=%d", history, current, reclaimed)
	}
	if _, err = store.DB().ExecContext(ctx, `DELETE FROM task_claims WHERE id=?`, first.ID); err == nil {
		t.Fatal("claim history was mutable")
	}
}

func createLegacyProjectCoreDatabase(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	must(t, err)
	defer db.Close()
	_, err = db.Exec(`PRAGMA foreign_keys=ON`)
	must(t, err)
	_, err = db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL) STRICT`)
	must(t, err)
	for version, name := range []string{"001_core.sql", "002_general_home.sql", "003_posts_feed.sql", "004_comment_intent.sql"} {
		body, readErr := migrations.FS.ReadFile(name)
		must(t, readErr)
		_, err = db.Exec(string(body))
		must(t, err)
		_, err = db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, version+1, name, "2026-08-01T00:00:00Z")
		must(t, err)
	}
	_, err = db.Exec(`INSERT INTO projects(id,name,status,purpose,milestone,now_text,revision) VALUES('legacy','Legacy','slice-8','Old project','Pilot milestone','Continue',7)`)
	must(t, err)
	_, err = db.Exec(`INSERT INTO tasks(id,project_id,state,title,priority,accept_text) VALUES('legacy-task','legacy','in_progress','Imported task',1,'Verify')`)
	must(t, err)
	oldLease := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	_, err = db.Exec(`INSERT INTO task_claims(id,task_id,session_id,request_id,claimed_at,lease_until,project_revision) VALUES(?,?,?,?,?,?,?)`,
		"legacy-claim", "legacy-task", "legacy-session", requestStorageKey("legacy-agent", "legacy-session", "legacy-request"),
		"2026-08-01T00:00:00Z", stamp(oldLease), 7)
	must(t, err)
}

func TestProjectCoreUpgradePreservesLegacyMeaningWithoutFabricatedDates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	createLegacyProjectCoreDatabase(t, path)
	store, err := Open(context.Background(), path)
	must(t, err)
	ctx := context.Background()
	detail, err := store.ProjectCoreSnapshot(ctx, domain.ProjectCoreReadQuery{
		ProjectID: "legacy", ActivityStart: time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC), ActivityEnd: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	})
	must(t, err)
	if !detail.Project.CreatedAt.IsZero() || !detail.Project.UpdatedAt.IsZero() || detail.ActiveMilestone == nil ||
		detail.ActiveMilestone.Title != "Pilot milestone" || !detail.ActiveMilestone.CreatedAt.IsZero() {
		t.Fatalf("legacy detail=%+v", detail)
	}
	task, err := store.TaskOpenSnapshot(ctx, domain.TaskOpenQuery{TaskID: "legacy-task", EventsLimit: 10})
	must(t, err)
	if !task.Task.CreatedAt.IsZero() || !task.Task.UpdatedAt.IsZero() || len(task.Events) != 1 ||
		task.Events[0].Kind != "imported" || !task.Events[0].CreatedAt.IsZero() {
		t.Fatalf("legacy task=%+v", task)
	}
	oldLease := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	replayedClaim, err := store.Claim(ctx, domain.ClaimRequest{TaskID: "legacy-task", ActorID: "legacy-agent", SessionID: "legacy-session", RequestID: "legacy-request", LeaseUntil: &oldLease})
	must(t, err)
	if replayedClaim.ID != "legacy-claim" {
		t.Fatalf("legacy claim replay=%+v", replayedClaim)
	}
	newLease := store.now().UTC().Add(time.Hour)
	if _, err = store.Claim(ctx, domain.ClaimRequest{TaskID: "legacy-task", ActorID: "new-agent", SessionID: "new-session", RequestID: "new-request", LeaseUntil: &newLease}); err != nil {
		t.Fatalf("legacy reclaim err=%v", err)
	}
	var claimHistory int
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM task_claims WHERE task_id='legacy-task'`).Scan(&claimHistory))
	if claimHistory != 2 {
		t.Fatalf("legacy claim history=%d", claimHistory)
	}

	var migrationsApplied, milestones, activeTasks int
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrationsApplied))
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM milestones WHERE project_id='legacy' AND status='active'`).Scan(&milestones))
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM active_tasks WHERE project_id='legacy'`).Scan(&activeTasks))
	if migrationsApplied != 12 || milestones != 1 || activeTasks != 1 {
		t.Fatalf("migrations=%d milestones=%d active_tasks=%d", migrationsApplied, milestones, activeTasks)
	}
	must(t, store.Close())
	reopened, err := Open(ctx, path)
	must(t, err)
	defer reopened.Close()
	must(t, reopened.DB().QueryRowContext(ctx, `SELECT count(*) FROM milestones WHERE project_id='legacy'`).Scan(&milestones))
	must(t, reopened.DB().QueryRowContext(ctx, `SELECT count(*) FROM active_tasks WHERE project_id='legacy'`).Scan(&activeTasks))
	if milestones != 1 || activeTasks != 1 {
		t.Fatalf("reopen milestones=%d active_tasks=%d", milestones, activeTasks)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkProjectCoreSnapshot(b *testing.B) {
	b.StopTimer()
	path := filepath.Join(b.TempDir(), "benchmark.sqlite")
	benchStore, err := Open(context.Background(), path, WithClock(func() time.Time { return testNow }))
	if err != nil {
		b.Fatal(err)
	}
	defer benchStore.Close()
	if _, err = benchStore.CreateCanonicalProject(context.Background(), domain.CreateProjectCommand{ID: "bench", Name: "Bench", Purpose: "Bounded read", Meta: coreTestMeta("bench-project")}); err != nil {
		b.Fatal(err)
	}
	query := domain.ProjectCoreReadQuery{ProjectID: "bench", ActivityStart: time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC), ActivityEnd: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)}
	b.StartTimer()
	for range b.N {
		if _, err = benchStore.ProjectCoreSnapshot(context.Background(), query); err != nil {
			b.Fatal(err)
		}
	}
}
