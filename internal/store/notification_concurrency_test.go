package store

import (
	"context"
	"sync"
	"testing"

	"codex-commons/internal/domain"
)

func TestNotificationReadConcurrentReplayIsStable(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/concurrent.sqlite3"
	s, err := Open(ctx, path)
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
	s2, err := Open(ctx, path)
	must(t, err)
	defer s2.Close()
	results := make(chan domain.WriteResult, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		target := s
		if i%2 == 1 {
			target = s2
		}
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			result, err := store.MarkHumanNotificationRead(ctx, req)
			results <- result
			errs <- err
		}(target)
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

func TestGeneralPostConcurrentReplayIsStableAcrossStores(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/post-concurrent.sqlite3"
	first, err := Open(ctx, path)
	must(t, err)
	defer first.Close()
	must(t, first.UpsertSession(ctx, domain.Session{ID: "agent-a", Host: "fixture", Purpose: "author"}))
	second, err := Open(ctx, path)
	must(t, err)
	defer second.Close()

	req := domain.PostRequest{TopicID: domain.TopicGeneral, Kind: "question", Title: "One post", Body: "Body", Basis: "Evidence", ActorID: "agent-a", ActorKind: "agent", ActorPrincipal: "agent-a", SessionID: "agent-a", RequestID: "same-post", MentionPrincipals: []string{domain.HumanLocalPrincipal}}
	const workers = 8
	results := make(chan domain.Post, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		store := first
		if i%2 == 1 {
			store = second
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := store.Post(ctx, req)
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
	var id string
	for result := range results {
		if id == "" {
			id = result.ID
		}
		if result.ID == "" || result.ID != id {
			t.Fatalf("unstable post id=%q want=%q", result.ID, id)
		}
	}
	var posts, notifications int
	must(t, first.DB().QueryRowContext(ctx, `SELECT count(*) FROM posts WHERE title='One post'`).Scan(&posts))
	must(t, first.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_notifications WHERE post_id=?`, id).Scan(&notifications))
	if posts != 1 || notifications != 1 {
		t.Fatalf("posts=%d notifications=%d", posts, notifications)
	}
}
