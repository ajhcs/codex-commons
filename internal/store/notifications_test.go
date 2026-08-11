package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func TestSinglePlaneStructuredMentionsAndHumanReceipts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	s, err := Open(ctx, t.TempDir()+"/commons.sqlite3", WithClock(func() time.Time { return now }))
	must(t, err)
	defer s.Close()
	must(t, s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Status: "active", Purpose: "test"}))
	must(t, s.CreateTopic(ctx, domain.Topic{ID: "alpha-posts", ProjectID: "alpha", Name: "Alpha posts"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "agent-a", Host: "host-a", ProjectID: "alpha", Purpose: "Author"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "agent-b", Host: "host-b", ProjectID: "alpha", Purpose: "Reviewer"}))

	request := domain.PostRequest{TopicID: "alpha-posts", Kind: "question", Title: "Canonical question", Body: "Raw @text is inert.", Basis: "Test evidence", ActorID: "actor-a", ActorKind: "agent", ActorPrincipal: "agent-a", SessionID: "agent-a", RequestID: "post-human-agent", MentionPrincipals: []string{domain.HumanLocalPrincipal, "agent-b", "agent-b"}}
	post, err := s.Post(ctx, request)
	must(t, err)
	replay, err := s.Post(ctx, request)
	must(t, err)
	if replay.ID != post.ID {
		t.Fatalf("replay id=%s want=%s", replay.ID, post.ID)
	}
	changed := request
	changed.MentionPrincipals = []string{domain.HumanLocalPrincipal}
	if _, err := s.Post(ctx, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed replay err=%v", err)
	}

	page, err := s.HumanNotifications(ctx, domain.NotificationQuery{RecipientPrincipal: domain.HumanLocalPrincipal, UnreadOnly: true, Limit: 10})
	must(t, err)
	if page.UnreadCount != 1 || len(page.Items) != 1 || page.Items[0].PostID != post.ID || page.Items[0].CommentID != "" || page.Items[0].ActorPrincipal != "agent-a" {
		t.Fatalf("human notifications=%+v", page)
	}
	inbox, err := s.Inbox(ctx, "alpha", "agent-b", 10)
	must(t, err)
	if len(inbox) != 1 || inbox[0].Ref != post.ID || inbox[0].FromSessionID != "agent-a" {
		t.Fatalf("agent inbox=%+v", inbox)
	}

	receipt := domain.MarkNotificationReadRequest{NotificationID: page.Items[0].ID, RecipientPrincipal: domain.HumanLocalPrincipal, ActorID: "local-admin", RequestID: "read-one"}
	firstRead, err := s.MarkHumanNotificationRead(ctx, receipt)
	must(t, err)
	secondRead, err := s.MarkHumanNotificationRead(ctx, receipt)
	must(t, err)
	if firstRead.ID != secondRead.ID {
		t.Fatalf("read replay=%+v/%+v", firstRead, secondRead)
	}
	changedRead := receipt
	changedRead.NotificationID = "missing"
	if _, err := s.MarkHumanNotificationRead(ctx, changedRead); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed read replay err=%v", err)
	}
	page, err = s.HumanNotifications(ctx, domain.NotificationQuery{RecipientPrincipal: domain.HumanLocalPrincipal, Limit: 10})
	must(t, err)
	if page.UnreadCount != 0 || page.Items[0].ReadAt == nil {
		t.Fatalf("read projection=%+v", page)
	}

	comment, err := s.Comment(ctx, domain.CommentRequest{PostID: post.ID, Body: "Canonical answer", Intent: "answer", ActorID: "actor-b", ActorKind: "agent", ActorPrincipal: "agent-b", SessionID: "agent-b", RequestID: "comment-human", MentionPrincipals: []string{domain.HumanLocalPrincipal}})
	must(t, err)
	page, err = s.HumanNotifications(ctx, domain.NotificationQuery{RecipientPrincipal: domain.HumanLocalPrincipal, UnreadOnly: true, Limit: 10})
	must(t, err)
	if page.UnreadCount != 1 || len(page.Items) != 1 || page.Items[0].CommentID != comment.ID || page.Items[0].PostID != post.ID {
		t.Fatalf("comment notification=%+v", page)
	}

	var before int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM comments`).Scan(&before))
	_, err = s.Comment(ctx, domain.CommentRequest{PostID: post.ID, Body: "Must roll back", Intent: "clarify", ActorID: "actor-a", ActorKind: "agent", ActorPrincipal: "agent-a", SessionID: "agent-a", RequestID: "bad-recipient", MentionPrincipals: []string{"historical-only"}})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("historical recipient err=%v", err)
	}
	var after int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM comments`).Scan(&after))
	if after != before {
		t.Fatalf("non-atomic comment count before=%d after=%d", before, after)
	}
}

func TestMigrationEightBackfillsMentionsOnReopen(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/legacy.sqlite3"
	s, err := Open(ctx, path)
	must(t, err)
	must(t, s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Status: "active", Purpose: "test"}))
	must(t, s.CreateTopic(ctx, domain.Topic{ID: "alpha-posts", ProjectID: "alpha", Name: "Alpha posts"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "agent-a", Host: "a", ProjectID: "alpha", Purpose: "A"}))
	must(t, s.UpsertSession(ctx, domain.Session{ID: "agent-b", Host: "b", ProjectID: "alpha", Purpose: "B"}))
	post, err := s.Post(ctx, domain.PostRequest{TopicID: "alpha-posts", Kind: "question", Title: "Q", Body: "Body", Basis: "Basis", SessionID: "agent-a", RequestID: "p"})
	must(t, err)
	comment, err := s.Comment(ctx, domain.CommentRequest{PostID: post.ID, Body: "Answer", Intent: "answer", SessionID: "agent-a", RequestID: "c", MentionSessionIDs: []string{"agent-b"}})
	must(t, err)
	must(t, s.Close())

	reopened, err := Open(ctx, path)
	must(t, err)
	defer reopened.Close()
	var count int
	must(t, reopened.DB().QueryRowContext(ctx, `SELECT count(*) FROM content_mentions WHERE source_kind='comment' AND source_id=? AND recipient_principal='agent-b'`, comment.ID).Scan(&count))
	if count != 1 {
		t.Fatalf("backfilled mention count=%d", count)
	}
}
