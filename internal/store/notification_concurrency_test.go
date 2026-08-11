package store

import (
	"context"
	"sync"
	"testing"

	"codex-commons/internal/domain"
)

func TestNotificationReadConcurrentReplayIsStable(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir()+"/concurrent.sqlite3")
	must(t, err)
	defer s.Close()
	must(t, s.UpsertSession(ctx, domain.Session{ID: "agent-a", Host: "fixture", Purpose: "author"}))
	post, err := s.Post(ctx, domain.PostRequest{TopicID: domain.TopicGeneral, Kind: "question", Title: "Q", Body: "B", Basis: "E", ActorID: "agent-a", ActorKind: "agent", ActorPrincipal: "agent-a", SessionID: "agent-a", RequestID: "post", MentionPrincipals: []string{domain.HumanLocalPrincipal}})
	must(t, err)
	page, err := s.HumanNotifications(ctx, domain.NotificationQuery{RecipientPrincipal: domain.HumanLocalPrincipal, Limit: 1})
	must(t, err)
	if len(page.Items) != 1 || page.Items[0].PostID != post.ID {
		t.Fatalf("page=%+v", page)
	}
	req := domain.MarkNotificationReadRequest{NotificationID: page.Items[0].ID, RecipientPrincipal: domain.HumanLocalPrincipal, ActorID: "local-admin", RequestID: "same-read"}
	const workers = 8
	results := make(chan domain.WriteResult, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.MarkHumanNotificationRead(ctx, req)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for result := range results {
		if first == "" {
			first = result.ID
		}
		if result.ID == "" || result.ID != first {
			t.Fatalf("unstable replay id=%q want=%q", result.ID, first)
		}
	}
	var events int
	must(t, s.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_notification_receipt_events WHERE notification_id=?`, page.Items[0].ID).Scan(&events))
	if events != 1 {
		t.Fatalf("receipt events=%d", events)
	}
}
