package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func completeSelectedReview(t *testing.T, s *Store, batch, selection, manifest string, ids []string) string {
	t.Helper()
	receipt, err := s.AdvanceArchaeologySelectedReview(context.Background(), domain.ArchaeologySelectedReviewCommand{Principal: "human:local-admin", BatchID: batch, SelectionDigest: selection, ManifestDigest: manifest, RequestID: "review-" + batch, OutcomeIDs: ids, Page: 0, PageCount: 1})
	must(t, err)
	if receipt.CompletionToken == "" {
		t.Fatal("missing completion token")
	}
	return receipt.CompletionToken
}

func TestSelectedApplyRealClockReceiptStable(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	mustCoreProject(t, s, "alpha", "Alpha")
	mustCoreProject(t, s, "beta", "Beta")
	at := stamp(testNow)
	_, err := s.DB().ExecContext(ctx, `
INSERT INTO archaeology_sessions(id,principal,state,discovery_state,created_at,updated_at) VALUES('sel-session','human:local-admin','completed','ready',?,?);
INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at,policy_attested) VALUES('sel-batch','sel-session','request',zeroblob(32),'app_server_dynamic_tools','completed',2,?,?,1);
INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,canonical_project_id) VALUES
('sel-session','ca','Alpha','alpha',1,0,0,1,2,'low','','alpha'),('sel-session','cb','Beta','beta',1,0,0,1,2,'low','','beta');
INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,mode,state,report_digest,created_at,updated_at) VALUES
('ja','sel-batch','sel-session','ca','alpha','app_server_dynamic_tools','completed',zeroblob(32),?,?),('jb','sel-batch','sel-session','cb','beta','app_server_dynamic_tools','completed',zeroblob(32),?,?);
INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES
('oa','ja','alpha','Alpha','',1,'{"batch_id":"ha"}',?),('ob','jb','beta','Beta','',1,'{"batch_id":"hb"}',?);`, at, at, at, at, at, at, at, at, at, at)
	must(t, err)
	ca := historicalCommand("alpha", "ha", "inner-a")
	cb := historicalCommand("beta", "hb", "inner-b")
	ids := []string{"oa", "ob"}
	proposals := map[string]string{"oa": `{"batch_id":"ha"}`, "ob": `{"batch_id":"hb"}`}
	selection := selectionDigest("sel-batch", ids, proposals)
	tx, err := s.db.BeginTx(ctx, nil)
	must(t, err)
	simulated, err := s.simulateSelectedImports(ctx, tx, "sel-batch", selection, "preview", []domain.HistoricalImportCommand{ca, cb})
	must(t, err)
	must(t, tx.Rollback())
	manifest := simulated.ManifestDigest
	completion := completeSelectedReview(t, s, "sel-batch", selection, manifest, ids)
	command := domain.ArchaeologySelectedApplyCommand{BatchID: "sel-batch", Principal: "human:local-admin", RequestID: "selected-request", SelectionDigest: selection, ManifestDigest: manifest, ReviewCompletionToken: completion, OutcomeIDs: ids, Imports: []domain.HistoricalImportCommand{ca, cb}}
	invalid := command
	invalid.ReviewCompletionToken = ""
	if _, err = s.ApplyArchaeologySelectedImports(ctx, invalid); err == nil {
		t.Fatal("missing completion token accepted")
	}
	invalid.ReviewCompletionToken = strings.Repeat("x", 43)
	if _, err = s.ApplyArchaeologySelectedImports(ctx, invalid); err == nil {
		t.Fatal("wrong completion token accepted")
	}
	invalid = command
	invalid.Principal = "human:other"
	if _, err = s.ApplyArchaeologySelectedImports(ctx, invalid); err == nil {
		t.Fatal("other principal completion token accepted")
	}
	var beforeTasks, beforeAudits int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&beforeTasks))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_imports`).Scan(&beforeAudits))
	if beforeTasks != 0 || beforeAudits != 0 {
		t.Fatalf("invalid bindings mutated tasks=%d audits=%d", beforeTasks, beforeAudits)
	}
	receipt, err := s.ApplyArchaeologySelectedImports(ctx, command)
	must(t, err)
	if len(receipt.Imports) != 2 || !receipt.Imports[0].Applied || !receipt.Imports[1].Applied {
		t.Fatalf("receipt=%+v", receipt)
	}
	replay, err := s.ApplyArchaeologySelectedImports(ctx, command)
	must(t, err)
	if replay.AuditID != receipt.AuditID {
		t.Fatalf("replay=%+v receipt=%+v", replay, receipt)
	}
	var audits, tasks int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
	exactReplay, found, err := s.ReplayArchaeologySelectedImports(ctx, domain.ArchaeologySelectedApplyReplayQuery{BatchID: command.BatchID, Principal: command.Principal, RequestID: command.RequestID, SelectionDigest: command.SelectionDigest, ManifestDigest: command.ManifestDigest, OutcomeIDs: command.OutcomeIDs})
	must(t, err)
	if !found || exactReplay.AuditID != receipt.AuditID {
		t.Fatalf("exact replay found=%t receipt=%+v", found, exactReplay)
	}
	mismatch := domain.ArchaeologySelectedApplyReplayQuery{BatchID: command.BatchID, Principal: command.Principal, RequestID: command.RequestID, SelectionDigest: command.SelectionDigest, ManifestDigest: historicalDigest("e"), OutcomeIDs: command.OutcomeIDs}
	if _, _, err = s.ReplayArchaeologySelectedImports(ctx, mismatch); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("mismatched replay err=%v", err)
	}
	missing := mismatch
	missing.RequestID = "not-applied"
	if _, found, err = s.ReplayArchaeologySelectedImports(ctx, missing); err != nil || found {
		t.Fatalf("missing replay found=%t err=%v", found, err)
	}
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
	if audits != 1 || tasks != 2 {
		t.Fatalf("audits=%d tasks=%d", audits, tasks)
	}
	command.OutcomeIDs = []string{"ob", "oa"}
	if _, err := s.ApplyArchaeologySelectedImports(ctx, command); err != domain.ErrInvalid {
		t.Fatalf("reverse order err=%v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE archaeology_native_outcomes SET title='mutated' WHERE id='oa'`); err == nil {
		t.Fatal("native outcome was mutable")
	}
}

func selectedApplyFailureFixture(t *testing.T) (*Store, domain.ArchaeologySelectedApplyCommand) {
	t.Helper()
	s, _ := openTest(t)
	mustCoreProject(t, s, "alpha", "Alpha")
	mustCoreProject(t, s, "beta", "Beta")
	at := stamp(testNow)
	_, err := s.DB().Exec(`
INSERT INTO archaeology_sessions(id,principal,state,discovery_state,created_at,updated_at) VALUES('fail-s','human:local-admin','completed','ready',?,?);
INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at,policy_attested) VALUES('fail-b','fail-s','r',zeroblob(32),'app_server_dynamic_tools','completed',2,?,?,1);
INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,canonical_project_id) VALUES('fail-s','ca','Alpha','alpha',1,0,0,1,2,'low','','alpha'),('fail-s','cb','Beta','beta',1,0,0,1,2,'low','','beta');
INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,mode,state,report_digest,created_at,updated_at) VALUES('fail-ja','fail-b','fail-s','ca','alpha','app_server_dynamic_tools','completed',zeroblob(32),?,?),('fail-jb','fail-b','fail-s','cb','beta','app_server_dynamic_tools','completed',zeroblob(32),?,?);
INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES('fail-oa','fail-ja','alpha','Alpha','',1,'{"batch_id":"fail-ha"}',?),('fail-ob','fail-jb','beta','Beta','',1,'{"batch_id":"fail-hb"}',?);`, at, at, at, at, at, at, at, at, at, at)
	must(t, err)
	imports := []domain.HistoricalImportCommand{historicalCommand("alpha", "fail-ha", "fail-a"), historicalCommand("beta", "fail-hb", "fail-b")}
	ids := []string{"fail-oa", "fail-ob"}
	preview, err := s.PreviewArchaeologySelectedImports(context.Background(), domain.ArchaeologySelectedPreviewCommand{BatchID: "fail-b", Principal: "human:local-admin", RequestID: "fail-preview", OutcomeIDs: ids, Imports: imports})
	must(t, err)
	completion := completeSelectedReview(t, s, "fail-b", preview.SelectionDigest, preview.ManifestDigest, ids)
	return s, domain.ArchaeologySelectedApplyCommand{BatchID: "fail-b", Principal: "human:local-admin", RequestID: "fail-apply", SelectionDigest: preview.SelectionDigest, ManifestDigest: preview.ManifestDigest, ReviewCompletionToken: completion, OutcomeIDs: ids, Imports: imports}
}

func TestSelectedApplyRejectsInvalidReviewBindingsWithoutMutation(t *testing.T) {
	s, command := selectedApplyFailureFixture(t)
	defer s.Close()
	for name, mutate := range map[string]func(*domain.ArchaeologySelectedApplyCommand){
		"missing":   func(c *domain.ArchaeologySelectedApplyCommand) { c.ReviewCompletionToken = "" },
		"wrong":     func(c *domain.ArchaeologySelectedApplyCommand) { c.ReviewCompletionToken = strings.Repeat("x", 43) },
		"principal": func(c *domain.ArchaeologySelectedApplyCommand) { c.Principal = "human:other" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := command
			mutate(&invalid)
			if _, err := s.ApplyArchaeologySelectedImports(context.Background(), invalid); err == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
	if _, err := s.DB().Exec(`UPDATE archaeology_selected_reviews SET expires_at='2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyArchaeologySelectedImports(context.Background(), command); err == nil {
		t.Fatal("expired completion token accepted")
	}
	var tasks, audits, consumed int
	must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
	if tasks != 0 || audits != 0 || consumed != 0 {
		t.Fatalf("tasks=%d audits=%d consumed=%d", tasks, audits, consumed)
	}
}

func TestSelectedApplyCanonicalDriftAndMidBatchFailureRollback(t *testing.T) {
	t.Run("canonical drift", func(t *testing.T) {
		s, command := selectedApplyFailureFixture(t)
		defer s.Close()
		must(t, s.CreateTask(context.Background(), domain.Task{ID: "T-drift", ProjectID: "alpha", State: "done", Title: "VERIFIED historical outcome"}))
		if _, err := s.ApplyArchaeologySelectedImports(context.Background(), command); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("drift err=%v", err)
		}
		var tasks, audits, consumed int
		must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta') AND id!='T-drift'`).Scan(&tasks))
		must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
		must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
		if tasks != 0 || audits != 0 || consumed != 0 {
			t.Fatalf("tasks=%d audits=%d consumed=%d", tasks, audits, consumed)
		}
	})
	t.Run("mid batch", func(t *testing.T) {
		s, command := selectedApplyFailureFixture(t)
		defer s.Close()
		_, err := s.DB().Exec(`CREATE TRIGGER fail_beta_selected BEFORE INSERT ON tasks WHEN NEW.project_id='beta' BEGIN SELECT RAISE(ABORT,'forced second import failure'); END`)
		must(t, err)
		if _, err = s.ApplyArchaeologySelectedImports(context.Background(), command); err == nil {
			t.Fatal("forced mid-batch failure committed")
		}
		var tasks, audits, consumed, batches int
		must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
		must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
		must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
		must(t, s.DB().QueryRow(`SELECT count(*) FROM historical_import_batches`).Scan(&batches))
		if tasks != 0 || audits != 0 || consumed != 0 || batches != 0 {
			t.Fatalf("tasks=%d audits=%d consumed=%d batches=%d", tasks, audits, consumed, batches)
		}
		_, err = s.DB().ExecContext(context.Background(), `DROP TRIGGER fail_beta_selected`)
		must(t, err)
		receipt, err := s.ApplyArchaeologySelectedImports(context.Background(), command)
		must(t, err)
		if receipt.AuditID == "" || len(receipt.Imports) != 2 {
			t.Fatalf("retry receipt=%+v", receipt)
		}
		must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
		must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
		must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
		if tasks != 2 || audits != 1 || consumed != 1 {
			t.Fatalf("retry state tasks=%d audits=%d consumed=%d", tasks, audits, consumed)
		}
	})
}

func selectedApplyDatabasePath(t *testing.T, s *Store) string {
	t.Helper()
	var sequence int
	var name, path string
	must(t, s.DB().QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path))
	if name != "main" || path == "" {
		t.Fatalf("unexpected database path name=%q path=%q", name, path)
	}
	return path
}

func assertSelectedApplyIntegrity(t *testing.T, s *Store) {
	t.Helper()
	var integrity string
	must(t, s.DB().QueryRowContext(context.Background(), `PRAGMA integrity_check`).Scan(&integrity))
	if integrity != "ok" {
		t.Fatalf("integrity_check=%q", integrity)
	}
	var violations int
	must(t, s.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations))
	if violations != 0 {
		t.Fatalf("foreign_key_check violations=%d", violations)
	}
}

func awaitSelectedApplySignal(t *testing.T, ctx context.Context, signals <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signals:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s: %v", label, ctx.Err())
	}
}

type selectedApplyConcurrentResult struct {
	receipt domain.ArchaeologySelectedApplyReceipt
	err     error
}

func runConcurrentSelectedApply(t *testing.T, s *Store, firstCommand, secondCommand domain.ArchaeologySelectedApplyCommand) (selectedApplyConcurrentResult, selectedApplyConcurrentResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := selectedApplyDatabasePath(t, s)
	sibling, err := Open(ctx, path, WithClock(func() time.Time { return testNow }))
	must(t, err)
	defer sibling.Close()
	entered := make(chan struct{}, 2)
	allow := make(chan struct{})
	restoreBoundary := setArchaeologySelectedApplyTestBoundary(func(stage string) {
		if stage == "before_reserve" {
			entered <- struct{}{}
			<-allow
		}
	})
	defer restoreBoundary()
	released := false
	defer func() {
		if !released {
			close(allow)
		}
	}()
	results := make(chan selectedApplyConcurrentResult, 2)
	go func() {
		receipt, applyErr := s.ApplyArchaeologySelectedImports(ctx, firstCommand)
		results <- selectedApplyConcurrentResult{receipt: receipt, err: applyErr}
	}()
	go func() {
		receipt, applyErr := sibling.ApplyArchaeologySelectedImports(ctx, secondCommand)
		results <- selectedApplyConcurrentResult{receipt: receipt, err: applyErr}
	}()
	awaitSelectedApplySignal(t, ctx, entered, "first Apply reservation boundary")
	awaitSelectedApplySignal(t, ctx, entered, "second Apply reservation boundary")
	close(allow)
	released = true
	var first, second selectedApplyConcurrentResult
	select {
	case first = <-results:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for first concurrent Apply: %v", ctx.Err())
	}
	select {
	case second = <-results:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for second concurrent Apply: %v", ctx.Err())
	}
	return first, second
}

func TestSelectedApplyConcurrentExactRequestsShareReceipt(t *testing.T) {
	s, command := selectedApplyFailureFixture(t)
	first, second := runConcurrentSelectedApply(t, s, command, command)
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent results first=%+v second=%+v", first, second)
	}
	if first.receipt.AuditID == "" || first.receipt.AuditID != second.receipt.AuditID || first.receipt.SelectionDigest != second.receipt.SelectionDigest || first.receipt.ManifestDigest != second.receipt.ManifestDigest || !sameSelectedIDs(first.receipt.OutcomeIDs, second.receipt.OutcomeIDs) {
		t.Fatalf("receipts diverged first=%+v second=%+v", first.receipt, second.receipt)
	}
	ctx := context.Background()
	var audits, tasks, violations int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations))
	if audits != 1 || tasks != 2 || violations != 0 {
		t.Fatalf("concurrent state audits=%d tasks=%d fk=%d", audits, tasks, violations)
	}
	assertSelectedApplyIntegrity(t, s)
}

func TestSelectedApplyConcurrentDifferentRequestsShareOneSelection(t *testing.T) {
	s, command := selectedApplyFailureFixture(t)
	different := command
	different.RequestID = "different-selected-request"
	first, second := runConcurrentSelectedApply(t, s, command, different)
	if (first.err == nil) == (second.err == nil) {
		t.Fatalf("expected one winner and one conflict: first=%+v second=%+v", first, second)
	}
	if first.err != nil && !errors.Is(first.err, domain.ErrConflict) {
		t.Fatalf("first err=%v, want conflict", first.err)
	}
	if second.err != nil && !errors.Is(second.err, domain.ErrConflict) {
		t.Fatalf("second err=%v, want conflict", second.err)
	}
	winner := first
	if winner.err != nil {
		winner = second
	}
	if winner.receipt.AuditID == "" {
		t.Fatal("winner missing receipt")
	}
	var audits, tasks, consumed, violations int
	ctx := context.Background()
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations))
	if audits != 1 || tasks != 2 || consumed != 1 || violations != 0 {
		t.Fatalf("different-request race state audits=%d tasks=%d consumed=%d fk=%d", audits, tasks, consumed, violations)
	}
	assertSelectedApplyIntegrity(t, s)
}

func TestSelectedApplyReviewExpiryCapturedAfterWriterReservation(t *testing.T) {
	s, command := selectedApplyFailureFixture(t)
	defer s.Close()
	now := testNow
	s.now = func() time.Time { return now }
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entered := make(chan struct{}, 1)
	allow := make(chan struct{})
	restoreBoundary := setArchaeologySelectedApplyTestBoundary(func(stage string) {
		if stage == "before_reserve" {
			entered <- struct{}{}
			<-allow
		}
	})
	defer restoreBoundary()
	released := false
	defer func() {
		if !released {
			close(allow)
		}
	}()
	result := make(chan selectedApplyConcurrentResult, 1)
	go func() {
		receipt, err := s.ApplyArchaeologySelectedImports(ctx, command)
		result <- selectedApplyConcurrentResult{receipt: receipt, err: err}
	}()
	awaitSelectedApplySignal(t, ctx, entered, "Apply reservation boundary")
	// The review remains valid while the call is waiting to acquire the
	// writer. It must be checked with the clock observed after reservation,
	// immediately before consumeSelectedReview.
	now = testNow.Add(31 * time.Minute)
	close(allow)
	released = true
	var outcome selectedApplyConcurrentResult
	select {
	case outcome = <-result:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for expired Apply: %v", ctx.Err())
	}
	if !errors.Is(outcome.err, domain.ErrConflict) {
		t.Fatalf("expired review err=%v receipt=%+v", outcome.err, outcome.receipt)
	}
	var tasks, audits, batches, consumed, pages, nextPage int
	must(t, s.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
	must(t, s.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
	must(t, s.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM historical_import_batches`).Scan(&batches))
	must(t, s.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
	must(t, s.DB().QueryRowContext(context.Background(), `SELECT count(*),coalesce(max(next_page),-1) FROM archaeology_selected_reviews`).Scan(&pages, &nextPage))
	if tasks != 0 || audits != 0 || batches != 0 || consumed != 0 || pages != 1 || nextPage != 1 {
		t.Fatalf("expired review mutated tasks=%d audits=%d batches=%d consumed=%d pages=%d next_page=%d", tasks, audits, batches, consumed, pages, nextPage)
	}
	assertSelectedApplyIntegrity(t, s)
}

func TestSelectedApplyRequestKeyAndSelectionConflictsDoNotMutate(t *testing.T) {
	s, command := selectedApplyFailureFixture(t)
	defer s.Close()
	ctx := context.Background()
	receipt, err := s.ApplyArchaeologySelectedImports(ctx, command)
	must(t, err)
	if receipt.AuditID == "" {
		t.Fatal("missing initial receipt")
	}
	var beforeTasks, beforeAudits, beforeConsumed int
	must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&beforeTasks))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&beforeAudits))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&beforeConsumed))

	for name, mutate := range map[string]func(*domain.ArchaeologySelectedApplyCommand){
		"batch":     func(c *domain.ArchaeologySelectedApplyCommand) { c.BatchID = "changed-batch" },
		"selection": func(c *domain.ArchaeologySelectedApplyCommand) { c.SelectionDigest = historicalDigest("e") },
		"manifest":  func(c *domain.ArchaeologySelectedApplyCommand) { c.ManifestDigest = historicalDigest("e") },
		"outcomes":  func(c *domain.ArchaeologySelectedApplyCommand) { c.OutcomeIDs = []string{"fail-oa", "fail-x"} },
	} {
		t.Run(name, func(t *testing.T) {
			changedTuple := command
			mutate(&changedTuple)
			if _, err = s.ApplyArchaeologySelectedImports(ctx, changedTuple); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("same request changed tuple err=%v", err)
			}
			replayed, found, replayErr := s.ReplayArchaeologySelectedImports(ctx, domain.ArchaeologySelectedApplyReplayQuery{BatchID: command.BatchID, Principal: command.Principal, RequestID: command.RequestID, SelectionDigest: command.SelectionDigest, ManifestDigest: command.ManifestDigest, OutcomeIDs: command.OutcomeIDs})
			if replayErr != nil || !found {
				t.Fatalf("original receipt replay found=%t err=%v", found, replayErr)
			}
			wantJSON, _ := json.Marshal(receipt)
			gotJSON, _ := json.Marshal(replayed)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("original receipt changed want=%s got=%s", wantJSON, gotJSON)
			}
		})
	}
	differentRequest := command
	differentRequest.RequestID = "different-selected-request"
	if _, err = s.ApplyArchaeologySelectedImports(ctx, differentRequest); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("different request same selection err=%v", err)
	}
	var afterTasks, afterAudits, afterConsumed int
	must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&afterTasks))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&afterAudits))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&afterConsumed))
	if beforeTasks != afterTasks || beforeAudits != afterAudits || beforeConsumed != afterConsumed {
		t.Fatalf("conflicts mutated tasks/audits/consumed: before=%d/%d/%d after=%d/%d/%d", beforeTasks, beforeAudits, beforeConsumed, afterTasks, afterAudits, afterConsumed)
	}
	assertSelectedApplyIntegrity(t, s)
}

func TestSelectedApplyExactReplayRevalidatesNativeEligibility(t *testing.T) {
	s, command := selectedApplyFailureFixture(t)
	defer s.Close()
	ctx := context.Background()
	initial, err := s.ApplyArchaeologySelectedImports(ctx, command)
	must(t, err)
	if initial.AuditID == "" {
		t.Fatal("missing initial receipt")
	}
	if _, err = s.DB().ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='active' WHERE id='fail-ja'`); err != nil {
		t.Fatal(err)
	}
	query := domain.ArchaeologySelectedApplyReplayQuery{BatchID: command.BatchID, Principal: command.Principal, RequestID: command.RequestID, SelectionDigest: command.SelectionDigest, ManifestDigest: command.ManifestDigest, OutcomeIDs: command.OutcomeIDs}
	if _, found, replayErr := s.ReplayArchaeologySelectedImports(ctx, query); !errors.Is(replayErr, domain.ErrConflict) || found {
		t.Fatalf("stale public replay found=%t err=%v", found, replayErr)
	}
	if _, err = s.ApplyArchaeologySelectedImports(ctx, command); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale exact replay err=%v, want conflict", err)
	}
	var tasks, audits, consumed int
	must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
	if tasks != 2 || audits != 1 || consumed != 1 {
		t.Fatalf("stale replay mutated tasks=%d audits=%d consumed=%d", tasks, audits, consumed)
	}
	assertSelectedApplyIntegrity(t, s)
}

func TestSelectedApplyUniqueAuditConflictRollsBackAndLeavesReviewAvailable(t *testing.T) {
	s, command := selectedApplyFailureFixture(t)
	defer s.Close()
	ctx := context.Background()
	idsJSON, err := json.Marshal(command.OutcomeIDs)
	must(t, err)
	resultJSON, err := json.Marshal(domain.ArchaeologySelectedApplyReceipt{
		AuditID:         "other-audit",
		BatchID:         command.BatchID,
		SelectionDigest: command.SelectionDigest,
		ManifestDigest:  command.ManifestDigest,
		OutcomeIDs:      command.OutcomeIDs,
		Imports:         []domain.HistoricalImportReceipt{{}, {}},
	})
	must(t, err)
	_, err = s.DB().ExecContext(ctx, `INSERT INTO archaeology_selected_imports(id,batch_id,principal,request_key,selection_digest,manifest_digest,outcome_ids_json,result_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, "other-audit", command.BatchID, "human:other", "other-request", command.SelectionDigest, command.ManifestDigest, string(idsJSON), string(resultJSON), stamp(testNow))
	must(t, err)
	if _, err = s.ApplyArchaeologySelectedImports(ctx, command); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unique audit err=%v, want conflict", err)
	}
	var tasks, audits, batches, consumed, violations int
	must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM historical_import_batches`).Scan(&batches))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
	must(t, s.DB().QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations))
	if tasks != 0 || audits != 1 || batches != 0 || consumed != 0 || violations != 0 {
		t.Fatalf("unique conflict state tasks=%d audits=%d batches=%d consumed=%d fk=%d", tasks, audits, batches, consumed, violations)
	}
	assertSelectedApplyIntegrity(t, s)
}

func TestSelectedApplyStaleStateConflictsBeforeMutation(t *testing.T) {
	tests := []struct {
		name   string
		update string
	}{
		{name: "batch state", update: `UPDATE archaeology_native_batches SET state='running' WHERE id='fail-b'`},
		{name: "policy attestation", update: `UPDATE archaeology_native_batches SET policy_attested=0 WHERE id='fail-b'`},
		{name: "active job", update: `UPDATE archaeology_native_jobs SET state='active' WHERE id='fail-ja'`},
		{name: "missing report", update: `UPDATE archaeology_native_jobs SET report_digest=NULL WHERE id='fail-ja'`},
		{name: "expired review", update: `UPDATE archaeology_selected_reviews SET expires_at='2000-01-01T00:00:00Z'`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, command := selectedApplyFailureFixture(t)
			defer s.Close()
			ctx := context.Background()
			_, err := s.DB().ExecContext(ctx, test.update)
			must(t, err)
			if _, err := s.ApplyArchaeologySelectedImports(ctx, command); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("stale apply err=%v", err)
			}
			var tasks, audits, batches, consumed int
			must(t, s.DB().QueryRow(`SELECT count(*) FROM tasks WHERE project_id IN ('alpha','beta')`).Scan(&tasks))
			must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
			must(t, s.DB().QueryRow(`SELECT count(*) FROM historical_import_batches`).Scan(&batches))
			must(t, s.DB().QueryRow(`SELECT count(*) FROM archaeology_selected_reviews WHERE consumed_at IS NOT NULL`).Scan(&consumed))
			if tasks != 0 || audits != 0 || batches != 0 || consumed != 0 {
				t.Fatalf("stale apply mutated tasks=%d audits=%d batches=%d consumed=%d", tasks, audits, batches, consumed)
			}
			assertSelectedApplyIntegrity(t, s)
		})
	}
}

func TestSelectedApplySameProjectUsesOrderedReviewedDispositions(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	mustCoreProject(t, s, "alpha", "Alpha")
	at := stamp(testNow)
	_, err := s.DB().ExecContext(ctx, `
INSERT INTO archaeology_sessions(id,principal,state,discovery_state,created_at,updated_at) VALUES('same-s','human:local-admin','completed','ready',?,?);
INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at,policy_attested) VALUES('same-b','same-s','r',zeroblob(32),'app_server_dynamic_tools','completed',1,?,?,1);
INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,canonical_project_id) VALUES('same-s','ca','Alpha','alpha',1,0,0,1,2,'low','','alpha');
INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,mode,state,report_digest,created_at,updated_at) VALUES('same-j','same-b','same-s','ca','alpha','app_server_dynamic_tools','completed',zeroblob(32),?,?);
INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES('same-o1','same-j','alpha','One','',1,'{"batch_id":"same-h1"}',?),('same-o2','same-j','alpha','Two','',1,'{"batch_id":"same-h2"}',?);`, at, at, at, at, at, at, at, at)
	must(t, err)
	one, two := historicalCommand("alpha", "same-h1", "one"), historicalCommand("alpha", "same-h2", "two")
	ids := []string{"same-o1", "same-o2"}
	if _, err = s.PreviewArchaeologySelectedImports(ctx, domain.ArchaeologySelectedPreviewCommand{BatchID: "same-b", Principal: "human:local-admin", RequestID: "same-source", OutcomeIDs: ids, Imports: []domain.HistoricalImportCommand{one, two}}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("same project/source preview err=%v", err)
	}
	var beforeTasks, beforeBatches int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE project_id='alpha'`).Scan(&beforeTasks))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM historical_import_batches WHERE project_id='alpha'`).Scan(&beforeBatches))
	if beforeTasks != 0 || beforeBatches != 0 {
		t.Fatalf("same-source conflict tasks=%d batches=%d", beforeTasks, beforeBatches)
	}
	two.SourceDigest = historicalDigest("f")
	preview, err := s.PreviewArchaeologySelectedImports(ctx, domain.ArchaeologySelectedPreviewCommand{BatchID: "same-b", Principal: "human:local-admin", RequestID: "same-preview", OutcomeIDs: ids, Imports: []domain.HistoricalImportCommand{one, two}})
	must(t, err)
	if preview.Imports[0].Tasks[0].Disposition != "created" || preview.Imports[1].Tasks[0].Disposition != "skipped_current" {
		t.Fatalf("ordered preview=%+v", preview.Imports)
	}
	completion := completeSelectedReview(t, s, "same-b", preview.SelectionDigest, preview.ManifestDigest, ids)
	receipt, err := s.ApplyArchaeologySelectedImports(ctx, domain.ArchaeologySelectedApplyCommand{BatchID: "same-b", Principal: "human:local-admin", RequestID: "same-apply", SelectionDigest: preview.SelectionDigest, ManifestDigest: preview.ManifestDigest, ReviewCompletionToken: completion, OutcomeIDs: ids, Imports: []domain.HistoricalImportCommand{one, two}})
	must(t, err)
	if receipt.Imports[0].Tasks[0].Disposition != "created" || receipt.Imports[1].Tasks[0].Disposition != "skipped_current" {
		t.Fatalf("ordered apply=%+v", receipt.Imports)
	}
	var tasks, audits int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE project_id='alpha'`).Scan(&tasks))
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_imports`).Scan(&audits))
	if tasks != 1 || audits != 1 {
		t.Fatalf("tasks=%d audits=%d", tasks, audits)
	}
}
