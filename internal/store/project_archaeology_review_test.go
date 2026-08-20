package store

import (
	"context"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func TestSelectedReviewRequiresEveryBoundPageAndFixedExpiry(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	mustCoreProject(t, s, "review-project", "Review project")
	at := stamp(testNow)
	_, err := s.DB().ExecContext(ctx, `
INSERT INTO archaeology_sessions(id,principal,state,discovery_state,created_at,updated_at) VALUES('review-s','human:local-admin','completed','ready',?,?);
INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at,policy_attested) VALUES('review-b','review-s','r',zeroblob(32),'app_server_dynamic_tools','completed',1,?,?,1);
INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,canonical_project_id) VALUES('review-s','review-c','Review project','review-project',1,0,0,1,2,'low','','review-project');
INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,project_name,mode,state,report_digest,created_at,updated_at) VALUES('review-j','review-b','review-s','review-c','review-project','Review project','app_server_dynamic_tools','completed',zeroblob(32),?,?);
INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES
('a','review-j','review-project','A','',1,'{"batch_id":"review-b"}',?),
('b','review-j','review-project','B','',1,'{"batch_id":"review-b"}',?),
('c','review-j','review-project','C','',1,'{"batch_id":"review-b"}',?),
('d','review-j','review-project','D','',1,'{"batch_id":"review-b"}',?),
('e','review-j','review-project','E','',1,'{"batch_id":"review-b"}',?),
('f','review-j','review-project','F','',1,'{"batch_id":"review-b"}',?),
('new','review-j','review-project','New','',1,'{"batch_id":"review-b"}',?)`, at, at, at, at, at, at, at, at, at, at, at, at, at)
	must(t, err)
	selection, manifest := historicalDigest("a"), historicalDigest("b")
	ids := []string{"a", "b", "c", "d", "e", "f"}
	firstCommand := domain.ArchaeologySelectedReviewCommand{Principal: "human:local-admin", BatchID: "review-b", SelectionDigest: selection, ManifestDigest: manifest, RequestID: "page-0", OutcomeIDs: ids, Page: 0, PageCount: 2}
	first, err := s.AdvanceArchaeologySelectedReview(ctx, firstCommand)
	must(t, err)
	if first.SessionToken == "" || first.CompletionToken != "" || first.NextPage != 1 {
		t.Fatalf("first=%+v", first)
	}
	replayed, err := s.AdvanceArchaeologySelectedReview(ctx, firstCommand)
	must(t, err)
	if replayed.SessionToken != first.SessionToken || !replayed.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("page zero replay changed receipt: first=%+v replay=%+v", first, replayed)
	}
	changedKey := firstCommand
	changedKey.RequestID = "page-0-other"
	if _, err = s.AdvanceArchaeologySelectedReview(ctx, changedKey); err == nil {
		t.Fatal("same page with different idempotency key accepted")
	}
	if _, err = s.AdvanceArchaeologySelectedReview(ctx, domain.ArchaeologySelectedReviewCommand{Principal: "other", BatchID: "review-b", SelectionDigest: selection, ManifestDigest: manifest, SessionToken: first.SessionToken, RequestID: "page-1", OutcomeIDs: ids, Page: 1, PageCount: 2}); err == nil {
		t.Fatal("principal tamper accepted")
	}
	secondCommand := domain.ArchaeologySelectedReviewCommand{Principal: "human:local-admin", BatchID: "review-b", SelectionDigest: selection, ManifestDigest: manifest, SessionToken: first.SessionToken, RequestID: "page-1", OutcomeIDs: ids, Page: 1, PageCount: 2}
	second, err := s.AdvanceArchaeologySelectedReview(ctx, secondCommand)
	must(t, err)
	if second.CompletionToken == "" || !second.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("second=%+v first=%+v", second, first)
	}
	secondReplay, err := s.AdvanceArchaeologySelectedReview(ctx, secondCommand)
	must(t, err)
	if secondReplay.CompletionToken != second.CompletionToken || secondReplay.SessionToken != second.SessionToken {
		t.Fatalf("final page retry changed receipt: first=%+v replay=%+v", second, secondReplay)
	}
	delayedFirst, err := s.AdvanceArchaeologySelectedReview(ctx, firstCommand)
	must(t, err)
	if delayedFirst.NextPage != 1 || delayedFirst.CompletionToken != "" || delayedFirst.SessionToken != first.SessionToken || !delayedFirst.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("delayed first-page replay changed receipt: first=%+v replay=%+v", first, delayedFirst)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	must(t, err)
	command := domain.ArchaeologySelectedApplyCommand{Principal: "human:local-admin", BatchID: "review-b", SelectionDigest: selection, ManifestDigest: manifest, ReviewCompletionToken: second.CompletionToken}
	must(t, consumeSelectedReview(ctx, tx, command, ids, testNow))
	must(t, tx.Commit())
	tx, err = s.db.BeginTx(ctx, nil)
	must(t, err)
	if err = consumeSelectedReview(ctx, tx, command, ids, testNow); err == nil {
		t.Fatal("completion replay accepted")
	}
	_ = tx.Rollback()
	s.now = func() time.Time { return first.ExpiresAt.Add(time.Second) }
	if _, err = s.AdvanceArchaeologySelectedReview(ctx, secondCommand); err == nil {
		t.Fatal("expired session accepted")
	}
	newSelection := historicalDigest("c")
	newReview, err := s.AdvanceArchaeologySelectedReview(ctx, domain.ArchaeologySelectedReviewCommand{Principal: "human:local-admin", BatchID: "review-b", SelectionDigest: newSelection, ManifestDigest: manifest, RequestID: "new-page-0", OutcomeIDs: []string{"new"}, Page: 0, PageCount: 1})
	must(t, err)
	if newReview.CompletionToken == "" {
		t.Fatal("expired cleanup blocked subsequent review")
	}
	var violations int
	must(t, s.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations))
	if violations != 0 {
		t.Fatalf("foreign-key violations=%d", violations)
	}
}
