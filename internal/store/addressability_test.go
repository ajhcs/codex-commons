package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"codex-commons/internal/domain"
	"codex-commons/migrations"
)

func TestAddressabilityHandlesScopesAndMentions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "commons.db")
	s, err := Open(ctx, path)
	must(t, err)
	must(t, s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Status: "active", Purpose: "test"}))
	must(t, s.CreateTopic(ctx, domain.Topic{ID: "alpha-posts", ProjectID: "alpha", Name: "Alpha posts"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-B", Host: "host-b", ProjectID: "alpha", Purpose: "review"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-a", Host: "host-a", ProjectID: "alpha", Purpose: "write"}))
	contributors, err := s.Contributors(ctx, domain.ContributorQuery{Limit: 20})
	must(t, err)
	if len(contributors) != 2 || contributors[0].Handle != "agent-000001" || contributors[0].SessionID != "S-B" || contributors[1].Handle != "agent-000002" || contributors[1].SessionID != "S-a" {
		t.Fatalf("contributors=%+v", contributors)
	}
	must(t, s.UpsertSession(ctx, domain.Session{ID: "S-a", Host: "host-a", ProjectID: "alpha", Purpose: "changed"}))
	contributors, err = s.Contributors(ctx, domain.ContributorQuery{Search: "changed", Limit: 20})
	must(t, err)
	if len(contributors) != 1 || contributors[0].Handle != "agent-000002" {
		t.Fatalf("stable handle=%+v", contributors)
	}
	contributors, err = s.Contributors(ctx, domain.ContributorQuery{Search: "%", Limit: 20})
	must(t, err)
	if len(contributors) != 0 {
		t.Fatalf("literal wildcard search returned contributors=%+v", contributors)
	}
	if _, err = s.DB().ExecContext(ctx, `INSERT INTO session_handles(session_id,handle,created_at) VALUES('S-B','AGENT-000001','x')`); err == nil {
		t.Fatal("case-insensitive collision accepted")
	}

	post, err := s.Post(ctx, domain.PostRequest{TopicID: "alpha-posts", Kind: "question", Title: "Need evidence", Body: "Body", Basis: "Basis", ActorID: "a", SessionID: "S-a", RequestID: "post"})
	must(t, err)
	thread, err := s.PostThread(ctx, domain.PostThreadQuery{PostID: post.ID, Limit: 10})
	must(t, err)
	if thread.PerspectiveScope.Scope != "closed" || thread.PerspectiveScope.Revision != 0 {
		t.Fatalf("scope=%+v", thread.PerspectiveScope)
	}
	changed, err := s.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: post.ID, Scope: "project", BaseRevision: 0, ActorID: "human", SessionID: "S-a", RequestID: "scope"})
	must(t, err)
	replay, err := s.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: post.ID, Scope: "project", BaseRevision: 0, ActorID: "human", SessionID: "S-a", RequestID: "scope"})
	must(t, err)
	if changed != replay || changed.Revision != 1 {
		t.Fatalf("scope replay=%+v %+v", changed, replay)
	}
	if _, err = s.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: post.ID, Scope: "commons", BaseRevision: 0, ActorID: "human", SessionID: "S-a", RequestID: "stale"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale=%v", err)
	}

	raw, err := s.Comment(ctx, domain.CommentRequest{PostID: post.ID, Body: "plain @agent-000002", Intent: "clarify", ActorID: "a", SessionID: "S-a", RequestID: "raw"})
	must(t, err)
	explicit := domain.CommentRequest{PostID: post.ID, Body: "structured", Intent: "answer", ActorID: "a", SessionID: "S-a", RequestID: "mention", MentionSessionIDs: []string{"S-B", "S-B"}}
	mention, err := s.Comment(ctx, explicit)
	must(t, err)
	again, err := s.Comment(ctx, explicit)
	must(t, err)
	if mention != again {
		t.Fatalf("comment replay=%+v %+v", mention, again)
	}
	changedMention := explicit
	changedMention.MentionSessionIDs = nil
	if _, err = s.Comment(ctx, changedMention); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed mention replay=%v", err)
	}
	inbox, err := s.Inbox(ctx, "alpha", "S-B", 20)
	must(t, err)
	if len(inbox) != 1 || inbox[0].Ref != post.ID || inbox[0].Kind != "mention" {
		t.Fatalf("inbox=%+v raw=%+v", inbox, raw)
	}
	thread, err = s.PostThread(ctx, domain.PostThreadQuery{PostID: post.ID, Limit: 10})
	must(t, err)
	if len(thread.Comments) != 2 || len(thread.Comments[0].Mentions) != 0 || len(thread.Comments[1].Mentions) != 1 || thread.Comments[1].Mentions[0].SessionID != "S-B" {
		t.Fatalf("comments=%+v", thread.Comments)
	}
	var before int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM comments`).Scan(&before))
	_, err = s.Comment(ctx, domain.CommentRequest{PostID: post.ID, Body: "bad", Intent: "clarify", ActorID: "a", SessionID: "S-a", RequestID: "bad", MentionSessionIDs: []string{"historical-only"}})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid mention=%v", err)
	}
	var after int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM comments`).Scan(&after))
	if before != after {
		t.Fatalf("non-atomic before=%d after=%d", before, after)
	}

	general, err := s.Post(ctx, domain.PostRequest{TopicID: domain.TopicGeneral, Kind: "notice", Title: "General", Body: "Body", Basis: "Basis", ActorID: "a", SessionID: "S-a", RequestID: "general"})
	must(t, err)
	_, err = s.Comment(ctx, domain.CommentRequest{PostID: general.ID, Body: "general mention", Intent: "clarify", ActorID: "a", SessionID: "S-a", RequestID: "general-comment", MentionSessionIDs: []string{"S-B"}})
	must(t, err)
	generalInbox, err := s.Inbox(ctx, domain.TopicGeneral, "S-B", 20)
	must(t, err)
	if len(generalInbox) != 1 || generalInbox[0].Ref != general.ID {
		t.Fatalf("general inbox=%+v", generalInbox)
	}
	must(t, s.Close())
	reopened, err := Open(ctx, path)
	must(t, err)
	defer reopened.Close()
	contributors, err = reopened.Contributors(ctx, domain.ContributorQuery{Limit: 20})
	must(t, err)
	if len(contributors) != 2 || contributors[0].Handle != "agent-000001" || contributors[1].Handle != "agent-000002" {
		t.Fatalf("reopen contributors=%+v", contributors)
	}
}

func TestAddressabilityDeterministicSixToSevenUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v6.db")
	db, err := sql.Open("sqlite", path)
	must(t, err)
	_, err = db.Exec(`PRAGMA foreign_keys=ON`)
	must(t, err)
	_, err = db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL) STRICT`)
	must(t, err)
	names := []string{"001_core.sql", "002_general_home.sql", "003_posts_feed.sql", "004_comment_intent.sql", "005_project_core.sql", "006_continuity_provenance.sql"}
	for i, name := range names {
		body, readErr := migrations.FS.ReadFile(name)
		must(t, readErr)
		_, err = db.Exec(string(body))
		must(t, err)
		_, err = db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, i+1, name, "2026-08-01T00:00:00Z")
		must(t, err)
	}
	_, err = db.Exec(`INSERT INTO sessions(id,host,project_id,purpose,created_at) VALUES('S-B','b',NULL,'second','2026-08-02T00:00:00Z'),('S-a','a',NULL,'first','2026-08-01T00:00:00Z')`)
	must(t, err)
	must(t, db.Close())
	s, err := Open(ctx, path)
	must(t, err)
	rows, err := s.Contributors(ctx, domain.ContributorQuery{Limit: 20})
	must(t, err)
	if len(rows) != 2 || rows[0].SessionID != "S-a" || rows[0].Handle != "agent-000001" || rows[1].SessionID != "S-B" || rows[1].Handle != "agent-000002" {
		t.Fatalf("backfill=%+v", rows)
	}
	must(t, s.Close())
	reopened, err := Open(ctx, path)
	must(t, err)
	defer reopened.Close()
	again, err := reopened.Contributors(ctx, domain.ContributorQuery{Limit: 20})
	must(t, err)
	if len(again) != 2 || again[0].Handle != rows[0].Handle || again[1].Handle != rows[1].Handle {
		t.Fatalf("reopen=%+v", again)
	}
}
