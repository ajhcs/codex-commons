package store

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	})
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
