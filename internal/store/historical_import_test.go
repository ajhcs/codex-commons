package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func historicalDigest(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func historicalSource(kind, id, digest string, occurred time.Time) domain.HistoricalSource {
	return domain.HistoricalSource{Kind: kind, StableID: id, Digest: digest, OccurredAt: occurred}
}

func historicalCommand(project, batch, requestKey string) domain.HistoricalImportCommand {
	completed := testNow.Add(-48 * time.Hour)
	taskSource := historicalSource("codex_turn_audit", "turn-completion", historicalDigest("b"), completed)
	workerSource := historicalSource("codex_session_uuidv7", "thread-worker", historicalDigest("c"), completed)
	eventSource := historicalSource("repository_document", "review-receipt", historicalDigest("d"), completed.Add(-time.Hour))
	return domain.HistoricalImportCommand{
		ProjectID: project, SchemaVersion: 1, BatchID: batch, SourceDigest: historicalDigest("a"),
		CollisionPolicy: "current_wins",
		ProjectThreadAliases: []domain.HistoricalProjectThreadAliasInput{{
			Alias: "root", SessionID: "S-root",
			Source: historicalSource("codex_session_uuidv7", "thread-root", historicalDigest("e"), completed.Add(-time.Hour)),
		}},
		Tasks: []domain.HistoricalTaskInput{{
			Key: "outcome", Title: "Verified historical outcome", Description: "Recovered from durable task evidence",
			Acceptance: "Evidence and review preserved", State: "done", Source: taskSource,
			Attributions: []domain.HistoricalAttributionInput{{
				SessionID: "S-worker", Role: "implementer", Confidence: "verified", Source: workerSource,
			}},
			Events: []domain.HistoricalEventInput{{
				Key: "review", Kind: "reviewed", Summary: "Independent review completed", SessionID: "S-worker",
				Confidence: "verified", Source: eventSource,
			}},
		}},
		Meta: coreTestMeta(requestKey),
	}
}

func TestHistoricalImportPreviewApplyReplayAndSupersede(t *testing.T) {
	store, _ := openTest(t)
	ctx := context.Background()
	mustCoreProject(t, store, "commons", "project")
	command := historicalCommand("commons", "batch-1", "history-apply")

	preview, err := store.PreviewHistoricalImport(ctx, command)
	must(t, err)
	if preview.Applied || preview.State != "preview" || preview.Counts.ProjectThreadAliases != 1 ||
		preview.Counts.Tasks != 1 || preview.Counts.Attributions != 1 || preview.Counts.Events != 1 ||
		preview.Counts.Created != 1 || len(preview.Tasks) != 1 {
		t.Fatalf("preview=%+v", preview)
	}
	for _, table := range []string{"historical_import_batches", "historical_project_thread_aliases", "historical_import_tasks", "historical_task_attributions", "historical_task_events"} {
		var count int
		must(t, store.DB().QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count))
		if count != 0 {
			t.Fatalf("preview mutated %s count=%d", table, count)
		}
	}

	command.ConfirmSourceDigest = command.SourceDigest
	applied, err := store.ApplyHistoricalImport(ctx, command)
	must(t, err)
	if !applied.Applied || applied.State != "applied" || applied.Counts.Created != 1 ||
		applied.RecordedAt.IsZero() || applied.Tasks[0].TaskID != preview.Tasks[0].TaskID {
		t.Fatalf("applied=%+v", applied)
	}
	var fabricatedSessions, ownerCount, activityCount int
	must(t, store.DB().QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE id IN ('S-root','S-worker')").Scan(&fabricatedSessions))
	must(t, store.DB().QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE id=? AND owner_session_id IS NOT NULL", applied.Tasks[0].TaskID).Scan(&ownerCount))
	must(t, store.DB().QueryRowContext(ctx, "SELECT count(*) FROM activity_events WHERE project_id='commons' AND object_ref='batch-1'").Scan(&activityCount))
	if fabricatedSessions != 0 || ownerCount != 0 || activityCount != 1 {
		t.Fatalf("fabricatedSessions=%d ownerCount=%d activityCount=%d", fabricatedSessions, ownerCount, activityCount)
	}
	opened, err := store.TaskOpenSnapshot(ctx, domain.TaskOpenQuery{TaskID: applied.Tasks[0].TaskID, EventsLimit: 50})
	must(t, err)
	if opened.Task.HistoricalImport == nil || opened.Task.HistoricalImport.State != "applied" ||
		opened.Task.HistoricalImport.Source == nil || opened.Task.HistoricalImport.Source.Kind != "codex_turn_audit" ||
		len(opened.Task.Contributors) != 1 || opened.Task.Contributors[0].Kind != domain.ProvenanceHistorical ||
		opened.Task.Contributors[0].SessionID != "S-worker" || opened.Task.Contributors[0].RecordedBy == nil ||
		opened.Task.Contributors[0].RecordedBy.SessionID != "human-local-admin" {
		t.Fatalf("opened task provenance=%+v", opened.Task)
	}
	for _, contributor := range opened.Task.Contributors {
		if contributor.SessionID == "S-root" {
			t.Fatalf("project root alias leaked into contributors: %+v", contributor)
		}
	}
	foundHistoricalEvent := false
	for _, event := range opened.Events {
		if event.Kind == "reviewed" {
			foundHistoricalEvent = event.Provenance.Kind == domain.ProvenanceHistorical &&
				event.Provenance.SessionID == "S-worker" && event.Provenance.Source != nil &&
				event.Provenance.RecordedBy != nil && event.Provenance.RecordedBy.ActorID == "local-admin"
		}
	}
	if !foundHistoricalEvent {
		t.Fatalf("historical event provenance missing: %+v", opened.Events)
	}

	replayed, err := store.ApplyHistoricalImport(ctx, command)
	must(t, err)
	if replayed.Counts.Replayed != 1 || replayed.Tasks[0].Disposition != "replayed" {
		t.Fatalf("replayed=%+v", replayed)
	}
	changed := command
	changed.Tasks = append([]domain.HistoricalTaskInput(nil), command.Tasks...)
	changed.Tasks[0].Title = "Changed under reused key"
	if _, err = store.ApplyHistoricalImport(ctx, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed replay err=%v", err)
	}

	superseded, err := store.SupersedeHistoricalImport(ctx, domain.SupersedeHistoricalImportCommand{
		ProjectID: "commons", BatchID: "batch-1", Reason: "Replaced by reviewed reconstruction", Meta: coreTestMeta("history-supersede"),
	})
	must(t, err)
	if superseded.Applied || superseded.State != "superseded" {
		t.Fatalf("superseded=%+v", superseded)
	}
	list, err := store.TaskListSnapshot(ctx, domain.TaskListQuery{ProjectID: "commons", Limit: 25})
	must(t, err)
	if list.Total != 0 || list.StateCounts.Total != 0 {
		t.Fatalf("superseded projection remained in task list: %+v", list)
	}
	agentTasks, err := store.tasks(ctx, "commons", 25)
	must(t, err)
	var activeTasks int
	must(t, store.DB().QueryRowContext(ctx, "SELECT count(*) FROM active_tasks WHERE project_id='commons'").Scan(&activeTasks))
	if len(agentTasks) != 0 || activeTasks != 0 {
		t.Fatalf("superseded projection remained agent-visible tasks=%+v active=%d", agentTasks, activeTasks)
	}

	auditOpen, err := store.TaskOpenSnapshot(ctx, domain.TaskOpenQuery{TaskID: applied.Tasks[0].TaskID, EventsLimit: 50})
	must(t, err)
	if auditOpen.Task.HistoricalImport == nil || auditOpen.Task.HistoricalImport.State != "superseded" {
		t.Fatalf("superseded audit record unavailable: %+v", auditOpen.Task)
	}
	if _, err = store.DB().ExecContext(ctx, "UPDATE historical_task_attributions SET role='reviewer'"); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only attribution update err=%v", err)
	}
}

func TestHistoricalImportSeparatesFourRootAliasesFromThirtySevenTaskSessions(t *testing.T) {
	store, _ := openTest(t)
	mustCoreProject(t, store, "commons", "project")
	command := historicalCommand("commons", "accounting", "accounting")
	command.ProjectThreadAliases = nil
	at := testNow.Add(-72 * time.Hour)
	for i := 0; i < 4; i++ {
		command.ProjectThreadAliases = append(command.ProjectThreadAliases, domain.HistoricalProjectThreadAliasInput{
			Alias: "root-" + string(rune('a'+i)), SessionID: "S-root-" + string(rune('a'+i)),
			Source: historicalSource("codex_session_uuidv7", "root-"+string(rune('a'+i)), historicalDigest("d"), at),
		})
	}
	command.Tasks = nil
	links := 0
	for i := 0; i < 20; i++ {
		task := domain.HistoricalTaskInput{
			Key: "task-" + string(rune('a'+i)), Title: "Outcome " + string(rune('A'+i)), State: "done",
			Source: historicalSource("codex_turn_audit", "task-"+string(rune('a'+i)), historicalDigest("f"), at),
		}
		n := 1
		if i < 17 {
			n = 2
		}
		for j := 0; j < n; j++ {
			links++
			task.Attributions = append(task.Attributions, domain.HistoricalAttributionInput{
				SessionID: "S-worker-" + string(rune('A'+i)) + string(rune('a'+j)),
				Role:      []string{"implementer", "reviewer"}[j], Confidence: "supported",
				Source: historicalSource("codex_session_uuidv7", "spawn-"+string(rune('A'+i))+string(rune('a'+j)), historicalDigest("e"), at),
			})
		}
		command.Tasks = append(command.Tasks, task)
	}
	preview, err := store.PreviewHistoricalImport(context.Background(), command)
	must(t, err)
	if links != 37 || preview.Counts.ProjectThreadAliases != 4 || preview.Counts.Attributions != 37 ||
		preview.Counts.ProjectThreadAliases+preview.Counts.Attributions != 41 {
		t.Fatalf("accounting links=%d preview=%+v", links, preview.Counts)
	}

	invalid := command
	invalid.Tasks = append([]domain.HistoricalTaskInput(nil), command.Tasks...)
	invalid.Tasks[0] = command.Tasks[0]
	invalid.Tasks[0].Attributions = append([]domain.HistoricalAttributionInput(nil), command.Tasks[0].Attributions...)
	invalid.Tasks[0].Attributions[0].SessionID = command.ProjectThreadAliases[0].SessionID
	if _, err = store.PreviewHistoricalImport(context.Background(), invalid); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("root alias accepted as task contributor err=%v", err)
	}
}

func TestHistoricalImportCurrentWinsWithoutAttributingCurrentTask(t *testing.T) {
	store, _ := openTest(t)
	ctx := context.Background()
	mustCoreProject(t, store, "commons", "project")
	command := historicalCommand("commons", "collision", "collision")
	currentID := "T-current"
	must(t, store.CreateTask(ctx, domain.Task{
		ID: currentID, ProjectID: "commons", State: "done", Title: "  VERIFIED   historical OUTCOME  ", Accept: "Keep current",
	}))
	command.ConfirmSourceDigest = command.SourceDigest
	receipt, err := store.ApplyHistoricalImport(ctx, command)
	must(t, err)
	if receipt.Counts.SkippedCurrent != 1 || receipt.Counts.Created != 0 ||
		receipt.Tasks[0].Disposition != "skipped_current" || receipt.Tasks[0].TaskID != currentID {
		t.Fatalf("collision receipt=%+v", receipt)
	}
	var title string
	must(t, store.DB().QueryRowContext(ctx, "SELECT title FROM tasks WHERE id=?", currentID).Scan(&title))
	var attributions int
	must(t, store.DB().QueryRowContext(ctx, "SELECT count(*) FROM historical_task_attributions").Scan(&attributions))
	if title != "  VERIFIED   historical OUTCOME  " || attributions != 0 {
		t.Fatalf("title=%q attributions=%d", title, attributions)
	}
}

func TestHistoricalImportEventSessionMustBeAttributed(t *testing.T) {
	store, _ := openTest(t)
	mustCoreProject(t, store, "commons", "project")
	command := historicalCommand("commons", "event-membership", "event-membership")
	command.Tasks = append([]domain.HistoricalTaskInput(nil), command.Tasks...)
	command.Tasks[0].Events = append([]domain.HistoricalEventInput(nil), command.Tasks[0].Events...)
	command.Tasks[0].Events[0].SessionID = "S-not-attributed"
	if _, err := store.PreviewHistoricalImport(context.Background(), command); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unattributed event session accepted err=%v", err)
	}
}

func TestHistoricalImportStableKeyPrecedesAmbiguousTitleFallback(t *testing.T) {
	store, _ := openTest(t)
	ctx := context.Background()
	mustCoreProject(t, store, "commons", "project")
	first := historicalCommand("commons", "seed", "seed")
	first.ConfirmSourceDigest = first.SourceDigest
	applied, err := store.ApplyHistoricalImport(ctx, first)
	must(t, err)
	if len(applied.Tasks) != 1 || applied.Tasks[0].Disposition != "created" {
		t.Fatalf("seed receipt=%+v", applied)
	}
	for _, current := range []domain.Task{
		{ID: "T-current-a", ProjectID: "commons", State: "done", Title: "Ambiguous fallback"},
		{ID: "T-current-b", ProjectID: "commons", State: "done", Title: "  ambiguous   FALLBACK  "},
	} {
		must(t, store.CreateTask(ctx, current))
	}
	exact := historicalCommand("commons", "exact-key", "exact-key")
	exact.SourceDigest = historicalDigest("7")
	exact.Tasks = append([]domain.HistoricalTaskInput(nil), exact.Tasks...)
	exact.Tasks[0] = first.Tasks[0]
	exact.Tasks[0].Title = "ambiguous fallback"
	preview, err := store.PreviewHistoricalImport(ctx, exact)
	must(t, err)
	if preview.Tasks[0].Disposition != "skipped_current" || preview.Tasks[0].TaskID != applied.Tasks[0].TaskID {
		t.Fatalf("exact key did not disambiguate: %+v", preview)
	}
	ambiguous := exact
	ambiguous.BatchID = "ambiguous-title"
	ambiguous.SourceDigest = historicalDigest("8")
	ambiguous.Tasks = append([]domain.HistoricalTaskInput(nil), exact.Tasks...)
	ambiguous.Tasks[0] = exact.Tasks[0]
	ambiguous.Tasks[0].Key = "new-outcome"
	if _, err = store.PreviewHistoricalImport(ctx, ambiguous); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ambiguous title did not block err=%v", err)
	}
}

func TestHistoricalImportConcurrentApplyIsSingleBatch(t *testing.T) {
	store, _ := openTest(t)
	mustCoreProject(t, store, "commons", "project")
	command := historicalCommand("commons", "concurrent", "concurrent")
	command.ConfirmSourceDigest = command.SourceDigest
	start := make(chan struct{})
	results := make(chan domain.HistoricalImportReceipt, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := store.ApplyHistoricalImport(context.Background(), command)
			results <- result
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent apply err=%v", err)
		}
	}
	var created, replayed int
	for result := range results {
		created += result.Counts.Created
		replayed += result.Counts.Replayed
	}
	var batches, tasks int
	must(t, store.DB().QueryRow("SELECT count(*) FROM historical_import_batches").Scan(&batches))
	must(t, store.DB().QueryRow("SELECT count(*) FROM historical_import_tasks").Scan(&tasks))
	if batches != 1 || tasks != 1 || created != 1 || replayed != 1 {
		t.Fatalf("batches=%d tasks=%d created=%d replayed=%d", batches, tasks, created, replayed)
	}
}

func TestHistoricalImportBoundsAndConfirmation(t *testing.T) {
	store, _ := openTest(t)
	mustCoreProject(t, store, "commons", "project")
	command := historicalCommand("commons", "bounds", "bounds")
	command.Tasks = make([]domain.HistoricalTaskInput, 26)
	if _, err := store.PreviewHistoricalImport(context.Background(), command); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversized task batch err=%v", err)
	}
	command = historicalCommand("commons", "confirm", "confirm")
	if _, err := store.ApplyHistoricalImport(context.Background(), command); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("missing confirmation err=%v", err)
	}
	command.ConfirmSourceDigest = historicalDigest("f")
	if _, err := store.ApplyHistoricalImport(context.Background(), command); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("mismatched confirmation err=%v", err)
	}
}
