package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"codex-commons/migrations"
)

func TestMigrationOneAndThirteenUpgradeToSeventeenCleanly(t *testing.T) {
	for _, from := range []int{1, 13} {
		t.Run(fmt.Sprintf("from_%d", from), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.sqlite3")
			db, err := sql.Open("sqlite", path)
			must(t, err)
			_, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL) STRICT`)
			must(t, err)
			entries, err := fs.ReadDir(migrations.FS, ".")
			must(t, err)
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
					continue
				}
				var version int
				_, err = fmt.Sscanf(entry.Name(), "%03d_", &version)
				must(t, err)
				if version > from {
					break
				}
				body, readErr := migrations.FS.ReadFile(entry.Name())
				must(t, readErr)
				_, err = db.Exec(string(body))
				must(t, err)
				_, err = db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, version, entry.Name(), "2026-08-01T00:00:00Z")
				must(t, err)
			}
			must(t, db.Close())
			s, err := Open(context.Background(), path)
			must(t, err)
			defer s.Close()
			var version int
			must(t, s.db.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version))
			if version != 17 {
				t.Fatalf("version=%d", version)
			}
			var integrity string
			must(t, s.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity))
			if integrity != "ok" {
				t.Fatalf("integrity=%s", integrity)
			}
			var violations int
			must(t, s.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations))
			if violations != 0 {
				t.Fatalf("foreign keys=%d", violations)
			}
		})
	}
}
