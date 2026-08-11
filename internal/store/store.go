package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/migrations"
	_ "modernc.org/sqlite"
)

type Store struct {
	db          *sql.DB
	now         func() time.Time
	humanAuthMu sync.Mutex
}

type Option func(*Store)

func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

func Open(ctx context.Context, path string, opts ...Option) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: empty database path", domain.ErrInvalid)
	}
	dsn := path
	if path != ":memory:" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		dsn = "file:" + url.PathEscape(abs)
	} else {
		dsn = "file:commons-memory?mode=memory&cache=shared"
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB is exposed for capability diagnostics and migration verification, not
// application queries.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL) STRICT`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, version, entry.Name(), stamp(s.now()))
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseStamp(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}
func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "database is locked") || strings.Contains(msg, "database is busy") {
		return fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	if strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed") {
		return fmt.Errorf("%w: %v", domain.ErrConflict, err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func (s *Store) CreateProject(ctx context.Context, p domain.Project) error {
	if p.ID == "" || p.Name == "" || p.Purpose == "" {
		return domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if _, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,status,purpose,milestone,now_text,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Status, p.Purpose, p.Milestone, p.Now, p.Revision, stamp(now), stamp(now)); err != nil {
		return mapErr(err)
	}
	if p.Milestone != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO milestones(id,project_id,title,status,position,target_date,project_revision,created_at,updated_at) VALUES(?,?,?,'active',0,NULL,?,?,?)`,
			"MS-legacy-"+p.ID, p.ID, p.Milestone, p.Revision, stamp(now), stamp(now)); err != nil {
			return mapErr(err)
		}
	}
	return tx.Commit()
}

func (s *Store) CreateTopic(ctx context.Context, t domain.Topic) error {
	if t.ID == "" || t.ProjectID == "" || t.Name == "" || t.ID == domain.TopicGeneral {
		return domain.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO topics(id,project_id,name,created_at) VALUES(?,?,?,?)`, t.ID, t.ProjectID, t.Name, stamp(s.now()))
	return mapErr(err)
}

func (s *Store) CreateTask(ctx context.Context, task domain.Task) error {
	if task.ID == "" || task.ProjectID == "" || task.Title == "" {
		return domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if _, err = tx.ExecContext(ctx, `INSERT INTO tasks(id,project_id,state,title,priority,owner_session_id,accept_text,description,milestone_id,project_revision,created_at,updated_at) VALUES(?,?,?,?,?,NULLIF(?,''),?,'',NULL,0,?,?)`,
		task.ID, task.ProjectID, task.State, task.Title, task.Priority, task.OwnerSessionID, task.Accept, stamp(now), stamp(now)); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_events(id,task_id,project_id,project_revision,kind,summary,from_state,to_state,actor_id,session_id,created_at) VALUES(?,?,?,0,'imported','Imported through compatibility API',NULL,?,'','',?)`,
		newID("TE-"), task.ID, task.ProjectID, task.State, stamp(now)); err != nil {
		return mapErr(err)
	}
	return tx.Commit()
}

func (s *Store) AddDependency(ctx context.Context, taskID, dependsOn string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO task_dependencies(task_id,depends_on_task_id) VALUES(?,?)`, taskID, dependsOn)
	return mapErr(err)
}

func (s *Store) ProjectExists(ctx context.Context, projectID string) (bool, error) {
	if projectID == "" {
		return false, domain.ErrInvalid
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=?)`, projectID).Scan(&exists)
	return exists, mapErr(err)
}

func (s *Store) UpsertSession(ctx context.Context, v domain.Session) error {
	if v.ID == "" || v.Host == "" {
		return domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	created := stamp(s.now())
	if _, err = tx.ExecContext(ctx, `INSERT INTO sessions(id,host,project_id,purpose,created_at) VALUES(?,?,NULLIF(?,''),?,?) ON CONFLICT(id) DO UPDATE SET host=excluded.host,project_id=excluded.project_id,purpose=excluded.purpose`, v.ID, v.Host, v.ProjectID, v.Purpose, created); err != nil {
		return mapErr(err)
	}
	var exists, next int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM session_handles WHERE session_id=?`, v.ID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(CAST(substr(handle,7) AS INTEGER)),0)+1 FROM session_handles WHERE handle GLOB 'agent-[0-9]*'`).Scan(&next); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO session_handles(session_id,handle,created_at) VALUES(?,?,?)`, v.ID, fmt.Sprintf("agent-%06d", next), created); err != nil {
			return mapErr(err)
		}
	}
	return tx.Commit()
}

func (s *Store) ObservePresence(ctx context.Context, sessionID, hostState, turn string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO presence_facts(session_id,host_state,turn_state,observed_at) VALUES(?,?,?,?)`, sessionID, hostState, turn, stamp(s.now()))
	return mapErr(err)
}

func (s *Store) AddInbox(ctx context.Context, projectID, recipient, kind, from, ref, snippet string) (string, error) {
	id := newID("M-")
	_, err := s.db.ExecContext(ctx, `INSERT INTO inbox_items(id,project_id,recipient_session_id,kind,from_session_id,ref,snippet,unread,created_at) VALUES(?,?,?,?,?,?,?,1,?)`, id, projectID, recipient, kind, from, ref, snippet, stamp(s.now()))
	return id, mapErr(err)
}

func (s *Store) mutate(ctx context.Context, projectID, kind, ref, summary string, fn func(*sql.Tx, int64) error) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE projects SET revision=revision+1,updated_at=? WHERE id=?`, stamp(s.now()), projectID)
	if err != nil {
		return 0, mapErr(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return 0, domain.ErrNotFound
	}
	var rev int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id=?`, projectID).Scan(&rev); err != nil {
		return 0, err
	}
	if err = fn(tx, rev); err != nil {
		return 0, mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO changes(project_id,revision,kind,ref,summary,created_at) VALUES(?,?,?,?,?,?)`, projectID, rev, kind, ref, summary, stamp(s.now())); err != nil {
		return 0, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return 0, mapErr(err)
	}
	return rev, nil
}

func (s *Store) PutWiki(ctx context.Context, pageID, projectID, slug, title, summary, body, sessionID string) (int64, error) {
	return s.mutate(ctx, projectID, "wiki", pageID, summary, func(tx *sql.Tx, rev int64) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO wiki_pages(id,project_id,slug,title,current_revision) VALUES(?,?,?,?,1) ON CONFLICT(id) DO UPDATE SET title=excluded.title,current_revision=wiki_pages.current_revision+1`, pageID, projectID, slug, title); err != nil {
			return err
		}
		var pageRev int64
		if err := tx.QueryRowContext(ctx, `SELECT current_revision FROM wiki_pages WHERE id=?`, pageID).Scan(&pageRev); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO wiki_revisions(page_id,revision,summary,body,author_session_id,created_at) VALUES(?,?,?,?,?,?)`, pageID, pageRev, summary, body, sessionID, stamp(s.now())); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO search_documents(project_id,ref,kind,revision,title,body) VALUES(?,?,?,?,?,?) ON CONFLICT(ref) DO UPDATE SET revision=excluded.revision,title=excluded.title,body=excluded.body`, projectID, pageID, "wiki", rev, title, summary+" "+body)
		return err
	})
}

func (s *Store) AddDecision(ctx context.Context, d domain.Decision) (int64, error) {
	return s.mutate(ctx, d.ProjectID, "decision", d.ID, d.Title, func(tx *sql.Tx, rev int64) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO decisions(id,project_id,project_revision,title,rationale,created_at) VALUES(?,?,?,?,?,?)`, d.ID, d.ProjectID, rev, d.Title, d.Rationale, stamp(s.now())); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO search_documents(project_id,ref,kind,revision,title,body) VALUES(?,?,?,?,?,?)`, d.ProjectID, d.ID, "decision", rev, d.Title, d.Rationale)
		return err
	})
}
