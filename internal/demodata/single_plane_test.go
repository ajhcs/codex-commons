package demodata

import (
	"context"
	"testing"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

func TestSinglePlaneFixtureIsExplicitCanonicalAndReplaySafe(t *testing.T) {
	ctx := context.Background()
	store, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := SeedSinglePlaneFixture(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SeedSinglePlaneFixture(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.ProjectPost == "" || first.CommonsPost == "" {
		t.Fatalf("refs first=%+v second=%+v", first, second)
	}
	projectThread, err := store.PostThread(ctx, domain.PostThreadQuery{PostID: first.ProjectPost, ViewerKind: "human", ViewerPrincipal: domain.HumanLocalPrincipal, Limit: 1})
	if err != nil || projectThread.PerspectiveScope.Scope != "project" || len(projectThread.Mentions) != 1 || projectThread.Mentions[0].Principal != domain.HumanLocalPrincipal {
		t.Fatalf("project thread=%+v err=%v", projectThread, err)
	}
	commonsThread, err := store.PostThread(ctx, domain.PostThreadQuery{PostID: first.CommonsPost, ViewerKind: "agent", ViewerSession: "FIXTURE-AGENT-B", Limit: 1})
	if err != nil || commonsThread.PerspectiveScope.Scope != "commons" || len(commonsThread.Mentions) != 1 || commonsThread.Mentions[0].Principal != "FIXTURE-AGENT-B" {
		t.Fatalf("commons thread=%+v err=%v", commonsThread, err)
	}
	inbox, err := store.Inbox(ctx, "", "FIXTURE-AGENT-B", 10)
	if err != nil || len(inbox) != 1 || inbox[0].Ref != first.CommonsPost {
		t.Fatalf("agent inbox=%+v err=%v", inbox, err)
	}
	notifications, err := store.HumanNotifications(ctx, domain.NotificationQuery{RecipientPrincipal: domain.HumanLocalPrincipal, Limit: 10})
	if err != nil || len(notifications.Items) != 1 || notifications.Items[0].PostID != first.ProjectPost {
		t.Fatalf("human notifications=%+v err=%v", notifications, err)
	}
}
