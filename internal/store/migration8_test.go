package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"codex-commons/migrations"
	_ "modernc.org/sqlite"
)

func TestSevenToEightUpgradeBackfillsCanonicalMentions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v7.db")
	db, err := sql.Open("sqlite", path)
	must(t, err)
	_, err = db.Exec(`PRAGMA foreign_keys=ON`)
	must(t, err)
	_, err = db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL) STRICT`)
	must(t, err)
	names := []string{"001_core.sql", "002_general_home.sql", "003_posts_feed.sql", "004_comment_intent.sql", "005_project_core.sql", "006_continuity_provenance.sql", "007_addressable_contributors.sql"}
	for i, name := range names {
		body, readErr := migrations.FS.ReadFile(name)
		must(t, readErr)
		_, err = db.Exec(string(body))
		must(t, err)
		_, err = db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, i+1, name, "2026-08-01T00:00:00Z")
		must(t, err)
	}
	_, err = db.Exec(`INSERT INTO sessions(id,host,project_id,purpose,created_at) VALUES('agent-a','a',NULL,'author','2026-08-01T00:00:00Z'),('agent-b','b',NULL,'recipient','2026-08-01T00:00:01Z')`)
	must(t, err)
	_, err = db.Exec(`INSERT INTO session_handles(session_id,handle,created_at) VALUES('agent-a','agent-000001','2026-08-01T00:00:00Z'),('agent-b','agent-000002','2026-08-01T00:00:01Z')`)
	must(t, err)
	_, err = db.Exec(`INSERT INTO posts(id,topic_id,project_id,project_revision,kind,title,body,basis,ref,session_id,request_id,created_at) VALUES('P-old','general',NULL,NULL,'question','Old','Body','Basis','','agent-a','old-post','2026-08-01T00:01:00Z')`)
	must(t, err)
	_, err = db.Exec(`INSERT INTO post_perspective_scopes(post_id,scope,revision,event_id,updated_at) VALUES('P-old','closed',0,NULL,'2026-08-01T00:01:00Z')`)
	must(t, err)
	_, err = db.Exec(`INSERT INTO comments(id,post_id,project_id,project_revision,body,intent,session_id,request_id,created_at) VALUES('R-old','P-old',NULL,NULL,'Answer','answer','agent-a','old-comment','2026-08-01T00:02:00Z')`)
	must(t, err)
	_, err = db.Exec(`INSERT INTO comment_mentions(comment_id,recipient_session_id,position,created_at) VALUES('R-old','agent-b',0,'2026-08-01T00:02:00Z')`)
	must(t, err)
	must(t, db.Close())

	store, err := Open(ctx, path)
	must(t, err)
	defer store.Close()
	var count, migrationsApplied int
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM content_mentions WHERE source_kind='comment' AND source_id='R-old' AND recipient_principal='agent-b'`).Scan(&count))
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrationsApplied))
	if count != 1 || migrationsApplied != 8 {
		t.Fatalf("backfill=%d migrations=%d", count, migrationsApplied)
	}
}
