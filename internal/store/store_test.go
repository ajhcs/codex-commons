package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

var testNow = time.Date(2026, 8, 9, 16, 0, 0, 123, time.UTC)

func sqliteAtLeast(got string, wantMajor, wantMinor, wantPatch int) bool {
	var major, minor, patch int
	if _, err := fmt.Sscanf(got, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return false
	}
	return major > wantMajor || major == wantMajor && (minor > wantMinor || minor == wantMinor && patch >= wantPatch)
}
func openTest(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "commons.sqlite")
	s, err := Open(context.Background(), path, WithClock(func() time.Time { return testNow }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func seed(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	must(t, s.CreateProject(ctx, domain.Project{ID: "commons-lab", Name: "Commons Lab", Status: "slice-1", Purpose: "Durable coordination", Milestone: "persistent core", Now: "verify"}))
	must(t, s.CreateTopic(ctx, domain.Topic{ID: "commons-lab", ProjectID: "commons-lab", Name: "Commons Lab"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-1", Host: "plumbob", ProjectID: "commons-lab", Purpose: "test persistence"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-2", Host: "studio", ProjectID: "commons-lab", Purpose: "compete"}))
	must(t, s.ObservePresence(ctx, "S-1", "connected", "live"))
	must(t, s.ObservePresence(ctx, "S-2", "connected", "idle"))
	must(t, s.CreateTask(ctx, domain.Task{ID: "T-1", ProjectID: "commons-lab", State: "ready", Title: "Ready task", Priority: 1}))
	must(t, s.CreateTask(ctx, domain.Task{ID: "T-2", ProjectID: "commons-lab", State: "ready", Title: "Blocked by unfinished task", Priority: 2}))
	must(t, s.AddDependency(ctx, "T-2", "T-1"))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBundledSQLiteCapabilitiesAndMigrations(t *testing.T) {
	s, path := openTest(t)
	ctx := context.Background()
	var version string
	must(t, s.DB().QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version))
	if !sqliteAtLeast(version, 3, 53, 3) {
		t.Fatalf("bundled SQLite version=%s, want >=3.53.3", version)
	}
	rows, err := s.DB().QueryContext(ctx, "PRAGMA compile_options")
	must(t, err)
	defer rows.Close()
	fts := false
	for rows.Next() {
		var option string
		must(t, rows.Scan(&option))
		if option == "ENABLE_FTS5" {
			fts = true
		}
	}
	if !fts {
		t.Fatal("bundled SQLite lacks ENABLE_FTS5")
	}
	var mode string
	must(t, s.DB().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("journal_mode=%s", mode)
	}
	var n int
	must(t, s.DB().QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n))
	if n != 1 {
		t.Fatalf("migrations=%d", n)
	}
	must(t, s.Close())
	reopened, err := Open(ctx, path)
	must(t, err)
	defer reopened.Close()
	must(t, reopened.DB().QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n))
	if n != 1 {
		t.Fatalf("reopen migrations=%d", n)
	}
	_, err = reopened.DB().ExecContext(ctx, "CREATE VIRTUAL TABLE temp.probe_fts USING fts5(body)")
	must(t, err)
	_, err = reopened.DB().ExecContext(ctx, "INSERT INTO probe_fts(body) VALUES('bundled full text works')")
	must(t, err)
	must(t, reopened.DB().QueryRowContext(ctx, "SELECT count(*) FROM probe_fts WHERE probe_fts MATCH 'text'").Scan(&n))
	if n != 1 {
		t.Fatalf("fts matches=%d", n)
	}
}

func TestPersistenceFTSRevisionAndNoChange(t *testing.T) {
	s, path := openTest(t)
	seed(t, s)
	ctx := context.Background()
	rev, err := s.PutWiki(ctx, "W-home", "commons-lab", "home", "Project home", "context budget", "Use revision deltas and explicit open.", "S-1")
	must(t, err)
	if rev != 1 {
		t.Fatalf("wiki revision=%d", rev)
	}
	rev, err = s.AddDecision(ctx, domain.Decision{ID: "D-7", ProjectID: "commons-lab", Title: "Use bundled SQLite", Rationale: "PostgreSQL remains the measured escape hatch for context budget workloads."})
	must(t, err)
	if rev != 2 {
		t.Fatalf("decision revision=%d", rev)
	}
	hits, err := s.Search(ctx, "commons-lab", "context budget", 5)
	must(t, err)
	if len(hits) != 2 {
		t.Fatalf("hits=%+v", hits)
	}
	delta, err := s.ChangesSince(ctx, "commons-lab", 0)
	must(t, err)
	if delta.Unchanged || delta.Current != 2 || len(delta.Changes) != 2 {
		t.Fatalf("delta=%+v", delta)
	}
	none, err := s.ChangesSince(ctx, "commons-lab", 2)
	must(t, err)
	if !none.Unchanged || len(none.Changes) != 0 {
		t.Fatalf("unchanged=%+v", none)
	}
	if _, err := s.ChangesSince(ctx, "commons-lab", 3); !errors.Is(err, domain.ErrFutureRevision) {
		t.Fatalf("future err=%v", err)
	}
	must(t, s.Close())
	r, err := Open(ctx, path)
	must(t, err)
	defer r.Close()
	o, err := r.Open(ctx, "D-7")
	must(t, err)
	if !strings.Contains(o.Body, "PostgreSQL") {
		t.Fatalf("open=%+v", o)
	}
	packet, err := r.Context(ctx, "commons-lab", "S-1", 2)
	must(t, err)
	if !packet.Unchanged || packet.Project.Revision != 2 {
		t.Fatalf("packet=%+v", packet)
	}
}

func TestAppendOnlyHistoryAndWrites(t *testing.T) {
	s, _ := openTest(t)
	seed(t, s)
	ctx := context.Background()
	post, err := s.Post(ctx, domain.PostRequest{TopicID: "commons-lab", Kind: "finding", Title: "Reusable result", Body: "The persistent path works.", Basis: "Deterministic test.", SessionID: "S-1", RequestID: "post-1"})
	must(t, err)
	if post.Revision != 1 {
		t.Fatalf("post revision=%d", post.Revision)
	}
	again, err := s.Post(ctx, domain.PostRequest{TopicID: "commons-lab", Kind: "finding", Title: "Reusable result", Body: "The persistent path works.", Basis: "Deterministic test.", SessionID: "S-1", RequestID: "post-1"})
	must(t, err)
	if again.ID != post.ID || again.Revision != post.Revision {
		t.Fatalf("idempotent post=%+v", again)
	}
	comment, err := s.Comment(ctx, domain.CommentRequest{PostID: post.ID, Body: "Evidence retained.", SessionID: "S-2", RequestID: "comment-1"})
	must(t, err)
	if comment.Revision != 2 {
		t.Fatalf("comment=%+v", comment)
	}
	status, err := s.Status(ctx, domain.StatusRequest{ProjectID: "commons-lab", Ref: "T-1", State: "verified", Detail: "test passed", SessionID: "S-1", RequestID: "status-1"})
	must(t, err)
	if status.Revision != 3 {
		t.Fatalf("status=%+v", status)
	}
	for _, q := range []string{"UPDATE posts SET body='rewrite' WHERE id='" + post.ID + "'", "DELETE FROM comments WHERE id='" + comment.ID + "'", "UPDATE status_events SET detail='rewrite' WHERE id='" + status.ID + "'", "DELETE FROM presence_facts"} {
		if _, err := s.DB().ExecContext(ctx, q); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("query %q err=%v", q, err)
		}
	}
	var posts, comments, statuses int
	must(t, s.DB().QueryRowContext(ctx, "SELECT (SELECT count(*) FROM posts),(SELECT count(*) FROM comments),(SELECT count(*) FROM status_events)").Scan(&posts, &comments, &statuses))
	if posts != 1 || comments != 1 || statuses != 1 {
		t.Fatalf("history=%d/%d/%d", posts, comments, statuses)
	}
}

func TestAtomicIdempotentClaim(t *testing.T) {
	s, _ := openTest(t)
	seed(t, s)
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan error, 2)
	claims := make(chan domain.Claim, 2)
	var wg sync.WaitGroup
	for _, session := range []string{"S-1", "S-2"} {
		wg.Add(1)
		go func(session string) {
			defer wg.Done()
			<-start
			c, err := s.Claim(ctx, domain.ClaimRequest{TaskID: "T-1", SessionID: session, RequestID: "claim-" + session})
			if err == nil {
				claims <- c
			}
			results <- err
		}(session)
	}
	close(start)
	wg.Wait()
	close(results)
	close(claims)
	success, conflict := 0, 0
	var winner domain.Claim
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, domain.ErrConflict) {
			conflict++
		} else {
			t.Fatalf("claim err=%v", err)
		}
	}
	for c := range claims {
		winner = c
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	again, err := s.Claim(ctx, domain.ClaimRequest{TaskID: "T-1", SessionID: winner.SessionID, RequestID: winner.RequestID})
	must(t, err)
	if again.ID != winner.ID || again.Revision != winner.Revision {
		t.Fatalf("repeat=%+v winner=%+v", again, winner)
	}
	var count int
	must(t, s.DB().QueryRowContext(ctx, "SELECT count(*) FROM task_claims WHERE task_id='T-1'").Scan(&count))
	if count != 1 {
		t.Fatalf("claims=%d", count)
	}
	next, err := s.Next(ctx, "commons-lab", 5)
	must(t, err)
	if len(next) != 0 {
		t.Fatalf("blocked dependency offered: %+v", next)
	}
}

func TestActorScopedSemanticIdempotency(t *testing.T) {
	s, _ := openTest(t)
	seed(t, s)
	ctx := context.Background()

	lease := testNow.Add(2 * time.Hour)
	claimReq := domain.ClaimRequest{TaskID: "T-1", ActorID: "agent-a", SessionID: "S-1", RequestID: "shared", LeaseUntil: &lease}
	firstClaim, err := s.Claim(ctx, claimReq)
	must(t, err)
	replayedClaim, err := s.Claim(ctx, claimReq)
	must(t, err)
	if replayedClaim.ID != firstClaim.ID || replayedClaim.Revision != firstClaim.Revision {
		t.Fatalf("claim replay=%+v first=%+v", replayedClaim, firstClaim)
	}
	changedLease := lease.Add(time.Hour)
	claimReq.LeaseUntil = &changedLease
	if _, err := s.Claim(ctx, claimReq); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed claim lease err=%v", err)
	}
	otherClaim, err := s.Claim(ctx, domain.ClaimRequest{TaskID: "T-2", ActorID: "agent-b", SessionID: "S-2", RequestID: "shared", LeaseUntil: &lease})
	must(t, err)
	if otherClaim.ID == firstClaim.ID {
		t.Fatalf("actor-scoped claims collided: %+v", otherClaim)
	}

	post, err := s.Post(ctx, domain.PostRequest{TopicID: "commons-lab", Kind: "finding", Title: "Replay", Body: "body", Basis: "basis", SessionID: "S-1", RequestID: "post-replay"})
	must(t, err)
	commentReq := domain.CommentRequest{PostID: post.ID, Body: "same", ActorID: "agent-a", SessionID: "S-1", RequestID: "comment-shared"}
	firstComment, err := s.Comment(ctx, commentReq)
	must(t, err)
	replayedComment, err := s.Comment(ctx, commentReq)
	must(t, err)
	if replayedComment != firstComment {
		t.Fatalf("comment replay=%+v first=%+v", replayedComment, firstComment)
	}
	commentReq.Body = "changed"
	if _, err := s.Comment(ctx, commentReq); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed comment err=%v", err)
	}
	otherComment, err := s.Comment(ctx, domain.CommentRequest{PostID: post.ID, Body: "changed", ActorID: "agent-b", SessionID: "S-2", RequestID: "comment-shared"})
	must(t, err)
	if otherComment.ID == firstComment.ID {
		t.Fatalf("actor-scoped comments collided: %+v", otherComment)
	}

	statusReq := domain.StatusRequest{ProjectID: "commons-lab", Ref: "T-1", State: "verified", Detail: "same", ActorID: "agent-a", SessionID: "S-1", RequestID: "status-shared"}
	firstStatus, err := s.Status(ctx, statusReq)
	must(t, err)
	replayedStatus, err := s.Status(ctx, statusReq)
	must(t, err)
	if replayedStatus != firstStatus {
		t.Fatalf("status replay=%+v first=%+v", replayedStatus, firstStatus)
	}
	statusReq.Detail = "changed"
	if _, err := s.Status(ctx, statusReq); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed status err=%v", err)
	}
	otherStatus, err := s.Status(ctx, domain.StatusRequest{ProjectID: "commons-lab", Ref: "T-1", State: "verified", Detail: "changed", ActorID: "agent-b", SessionID: "S-2", RequestID: "status-shared"})
	must(t, err)
	if otherStatus.ID == firstStatus.ID {
		t.Fatalf("actor-scoped statuses collided: %+v", otherStatus)
	}
}

func TestContextSnapshotUnreadScopeAndErrors(t *testing.T) {
	s, _ := openTest(t)
	seed(t, s)
	ctx := context.Background()
	_, err := s.AddDecision(ctx, domain.Decision{ID: "D-context", ProjectID: "commons-lab", Title: "Snapshot", Rationale: "one transaction"})
	must(t, err)
	_, err = s.AddInbox(ctx, "commons-lab", "S-1", "mention", "S-2", "D-context", "one")
	must(t, err)
	_, err = s.AddInbox(ctx, "commons-lab", "S-1", "reply", "S-2", "D-context", "two")
	must(t, err)
	_, err = s.AddInbox(ctx, "commons-lab", "S-2", "mention", "S-1", "D-context", "other")
	must(t, err)

	for session, want := range map[string]int{"S-1": 2, "S-2": 1} {
		packet, err := s.Context(ctx, "commons-lab", session, 0)
		must(t, err)
		if packet.Unread != want {
			t.Fatalf("session=%s unread=%d want=%d", session, packet.Unread, want)
		}
		if len(packet.Changes) == 0 || packet.Project.Revision != packet.Changes[len(packet.Changes)-1].Revision {
			t.Fatalf("mixed context snapshot: %+v", packet)
		}
	}

	writerDone := make(chan error, 1)
	go func() {
		for i := 0; i < 25; i++ {
			_, err := s.Status(ctx, domain.StatusRequest{ProjectID: "commons-lab", Ref: "T-1", State: "observed", Detail: fmt.Sprintf("write-%d", i), ActorID: "writer", SessionID: "S-1", RequestID: fmt.Sprintf("context-%d", i)})
			if err != nil {
				writerDone <- err
				return
			}
		}
		writerDone <- nil
	}()
	for i := 0; i < 25; i++ {
		packet, err := s.Context(ctx, "commons-lab", "S-1", 0)
		must(t, err)
		if len(packet.Changes) == 0 || packet.Project.Revision != packet.Changes[len(packet.Changes)-1].Revision {
			t.Fatalf("mixed concurrent snapshot: project=%d changes=%+v", packet.Project.Revision, packet.Changes)
		}
	}
	must(t, <-writerDone)

	_, err = s.DB().ExecContext(ctx, "DROP TABLE inbox_items")
	must(t, err)
	if _, err := s.Context(ctx, "commons-lab", "S-1", 0); err == nil {
		t.Fatal("Context ignored unread query failure")
	}
}

func TestOpenUsesCanonicalSourceBoundaries(t *testing.T) {
	s, _ := openTest(t)
	seed(t, s)
	ctx := context.Background()
	post, err := s.Post(ctx, domain.PostRequest{TopicID: "commons-lab", Kind: "finding", Title: "Canonical title", Body: "Canonical body", Basis: "Canonical basis", Ref: "T-1", SessionID: "S-1", RequestID: "open-post"})
	must(t, err)
	_, err = s.DB().ExecContext(ctx, "UPDATE search_documents SET title='index title',body='index body and basis' WHERE ref=?", post.ID)
	must(t, err)
	object, err := s.Open(ctx, post.ID)
	must(t, err)
	if object.Title != post.Title || object.Body != post.Body || object.Basis != post.Basis || object.RelatedRef != post.Ref {
		t.Fatalf("canonical post boundaries lost: %+v", object)
	}
	if strings.Contains(object.Body, object.Basis) {
		t.Fatalf("basis was concatenated into body: %+v", object)
	}

	_, err = s.AddDecision(ctx, domain.Decision{ID: "D-open", ProjectID: "commons-lab", Title: "Decision title", Rationale: "Decision rationale"})
	must(t, err)
	decision, err := s.Open(ctx, "D-open")
	must(t, err)
	if decision.Kind != "decision" || decision.Title != "Decision title" || decision.Body != "Decision rationale" {
		t.Fatalf("canonical decision=%+v", decision)
	}
	task, err := s.Open(ctx, "T-1")
	must(t, err)
	if task.Kind != "task" || task.State != "ready" || task.Title != "Ready task" {
		t.Fatalf("canonical task=%+v", task)
	}
}

func TestForumKindsGovernanceAttestationAndCompactSearch(t *testing.T) {
	s, _ := openTest(t)
	seed(t, s)
	ctx := context.Background()

	for i, kind := range []string{"finding", "question", "notice", "decision"} {
		post, err := s.Post(ctx, domain.PostRequest{
			TopicID: "commons-lab", Kind: kind, Title: "Action record " + kind,
			Body:  "A next action changes when retrieval finds this " + kind + ".",
			Basis: "fixed evidence", ActorID: "agent-a", SessionID: "S-1", RequestID: "kind-" + kind,
		})
		must(t, err)
		if post.Kind != kind || post.Revision != int64(i+1) || post.SessionID != "S-1" || post.CreatedAt.IsZero() {
			t.Fatalf("kind=%s post=%+v", kind, post)
		}
		opened, err := s.Open(ctx, post.ID)
		must(t, err)
		if opened.SessionID != "S-1" || opened.TopicID != "commons-lab" || !opened.CreatedAt.Equal(testNow) {
			t.Fatalf("attested open=%+v", opened)
		}
	}

	generalFinding, err := s.Post(ctx, domain.PostRequest{TopicID: domain.TopicGeneral, Kind: "finding", Title: "Shared operational fact", Body: "Reusable across projects", Basis: "verified evidence", ActorID: "agent-a", SessionID: "S-1", RequestID: "general-finding"})
	must(t, err)
	if generalFinding.Revision != 0 || generalFinding.ProjectID != "" {
		t.Fatalf("global finding=%+v", generalFinding)
	}
	generalHits, err := s.Search(ctx, domain.TopicGeneral, "Shared operational fact", 5)
	must(t, err)
	if len(generalHits) != 1 || generalHits[0].Ref != generalFinding.ID || generalHits[0].ProjectID != "" {
		t.Fatalf("General discovery=%+v", generalHits)
	}
	topic, err := s.Post(ctx, domain.PostRequest{TopicID: domain.TopicGeneral, Kind: "topic_request", Title: "Atlas", Body: "Create it", Basis: "recurring need", ActorID: "agent-a", SessionID: "S-1", RequestID: "topic"})
	must(t, err)
	if topic.Revision != 0 || topic.ProjectID != "" {
		t.Fatalf("global topic request=%+v", topic)
	}
	topicHits, err := s.Search(ctx, domain.TopicGeneral, "Atlas recurring need", 5)
	must(t, err)
	if len(topicHits) != 1 || topicHits[0].Ref != topic.ID {
		t.Fatalf("General topic request discovery=%+v", topicHits)
	}
	delta, err := s.ChangesSince(ctx, "commons-lab", 4)
	must(t, err)
	if !delta.Unchanged || delta.Current != 4 {
		t.Fatalf("General posts changed project cursor: %+v", delta)
	}
	for _, req := range []domain.PostRequest{
		{TopicID: "commons-lab", Kind: "topic_request", Title: "bad", Body: "bad", Basis: "bad", SessionID: "S-1"},
		{TopicID: "commons-lab", Kind: "poll", Title: "bad", Body: "bad", Basis: "bad", SessionID: "S-1"},
	} {
		if _, err := s.Post(ctx, req); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid post %+v err=%v", req, err)
		}
	}

	first, err := s.Post(ctx, domain.PostRequest{TopicID: "commons-lab", Kind: "finding", Title: "Scoped A", Body: "identityscope alpha", Basis: "evidence", ActorID: "agent-a", SessionID: "S-1", RequestID: "shared-post"})
	must(t, err)
	replay, err := s.Post(ctx, domain.PostRequest{TopicID: "commons-lab", Kind: "finding", Title: "Scoped A", Body: "identityscope alpha", Basis: "evidence", ActorID: "agent-a", SessionID: "S-1", RequestID: "shared-post"})
	must(t, err)
	if replay.ID != first.ID {
		t.Fatalf("post replay=%+v first=%+v", replay, first)
	}
	other, err := s.Post(ctx, domain.PostRequest{TopicID: "commons-lab", Kind: "finding", Title: "Scoped B", Body: "identityscope beta", Basis: "evidence", ActorID: "agent-b", SessionID: "S-2", RequestID: "shared-post"})
	must(t, err)
	if other.ID == first.ID {
		t.Fatalf("actor-scoped post keys collided: %+v", other)
	}

	longPost, err := s.Post(ctx, domain.PostRequest{
		TopicID: "commons-lab", Kind: "notice", Title: "Needle " + strings.Repeat("t", 400),
		Body: "giantneedle " + strings.Repeat("x", 2000), Basis: "bounded metadata",
		ActorID: "agent-a", SessionID: "S-1", RequestID: "long-search",
	})
	must(t, err)
	hits, err := s.Search(ctx, "commons-lab", "giantneedle", 5)
	must(t, err)
	if len(hits) != 1 || hits[0].Ref != longPost.ID || hits[0].CreatedAt.IsZero() {
		t.Fatalf("search hits=%+v", hits)
	}
	if len(hits[0].Title) > 200 || len(hits[0].Snippet) > 240 || !strings.Contains(hits[0].Snippet, "giantneedle") {
		t.Fatalf("unbounded discovery metadata title=%d snippet=%d hit=%+v", len(hits[0].Title), len(hits[0].Snippet), hits[0])
	}
}
