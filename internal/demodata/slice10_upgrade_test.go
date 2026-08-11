package demodata

import (
	"context"
	"path/filepath"
	"testing"

	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

func TestPreSlice10DemoCommentsMigrateAndReseedIdempotently(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "pre-slice-10.sqlite")
	clock := fixedClock{value: Anchor}

	legacy, err := commonsstore.Open(ctx, database, commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	if err := Seed(ctx, legacy, presence.New(clock), clock.Now()); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	// Reconstruct the exact pre-004 shape while retaining the durable Slice 9
	// rows and their actor-scoped idempotency keys. Reopen must apply 004 and
	// give both historical comments the neutral `clarify` backfill.
	if _, err := legacy.DB().ExecContext(ctx, `ALTER TABLE comments DROP COLUMN intent`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.DB().ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=4`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := commonsstore.Open(ctx, database, commonsstore.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var neutral int
	if err := upgraded.DB().QueryRowContext(ctx, `SELECT count(*) FROM comments WHERE intent='clarify'`).Scan(&neutral); err != nil {
		t.Fatal(err)
	}
	if neutral != len(demoComments) {
		t.Fatalf("neutral backfill=%d comments=%d", neutral, len(demoComments))
	}
	if err := Seed(ctx, upgraded, presence.New(clock), clock.Now()); err != nil {
		t.Fatalf("post-migration reseed conflicted: %v", err)
	}
	var comments int
	if err := upgraded.DB().QueryRowContext(ctx, `SELECT count(*) FROM comments WHERE request_id LIKE '%demo-comment-finding-%'`).Scan(&comments); err != nil {
		t.Fatal(err)
	}
	if comments != len(demoComments) {
		t.Fatalf("reseed duplicated comments=%d want=%d", comments, len(demoComments))
	}
}
