package demodata

import (
	"context"
	"path/filepath"
	"testing"

	"codex-commons/internal/application"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

func TestDurableDemoDataSurvivesRestartButPresenceDoesNot(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "restart.sqlite")
	clock := fixedClock{value: Anchor}
	first, err := commonsstore.Open(ctx, database, commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	if err := Seed(ctx, first, presence.New(clock), clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := commonsstore.Open(ctx, database, commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	emptyLive := presence.New(clock)
	service := application.New(reopened, emptyLive, clock)
	home, err := service.GeneralHome(ctx, application.HomeQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if home.Navigation.Projects != 4 || home.NeedsAttention.Total != 5 || home.RecentActivity.Total != len(activity) {
		t.Fatalf("durable demo facts did not survive restart: %+v", home)
	}
	if home.Navigation.People != 0 || home.Presence.Total != 0 {
		t.Fatalf("live presence was incorrectly inferred from durable sessions: %+v", home.Presence)
	}

	if err := Seed(ctx, reopened, emptyLive, clock.Now()); err != nil {
		t.Fatal(err)
	}
	home, err = service.GeneralHome(ctx, application.HomeQuery{PresenceLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if home.Navigation.People != 6 || home.Presence.Total != 6 {
		t.Fatalf("explicit reseed did not restore process-local demo presence: %+v", home.Presence)
	}
}
