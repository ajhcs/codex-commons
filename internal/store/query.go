package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"codex-commons/internal/domain"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Store) ChangesSince(ctx context.Context, projectID string, since int64) (domain.ChangesResult, error) {
	return changesSince(ctx, s.db, projectID, since)
}

func changesSince(ctx context.Context, q queryer, projectID string, since int64) (domain.ChangesResult, error) {
	var current int64
	if err := q.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id=?`, projectID).Scan(&current); err != nil {
		return domain.ChangesResult{}, mapErr(err)
	}
	if since < 0 {
		return domain.ChangesResult{}, domain.ErrInvalid
	}
	if since > current {
		return domain.ChangesResult{}, domain.ErrFutureRevision
	}
	out := domain.ChangesResult{ProjectID: projectID, From: since, Current: current, Unchanged: since == current}
	if out.Unchanged {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT revision,kind,ref,summary,created_at FROM changes WHERE project_id=? AND revision>? ORDER BY revision`, projectID, since)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.Change
		var at string
		if err := rows.Scan(&c.Revision, &c.Kind, &c.Ref, &c.Summary, &at); err != nil {
			return out, err
		}
		c.CreatedAt = parseStamp(at)
		out.Changes = append(out.Changes, c)
	}
	return out, rows.Err()
}

func (s *Store) Context(ctx context.Context, projectID, recipientSessionID string, since int64) (domain.ContextPacket, error) {
	if recipientSessionID == "" {
		return domain.ContextPacket{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.ContextPacket{}, err
	}
	defer tx.Rollback()

	delta, err := changesSince(ctx, tx, projectID, since)
	if err != nil {
		return domain.ContextPacket{}, err
	}
	var p domain.Project
	err = tx.QueryRowContext(ctx, `SELECT id,name,status,purpose,milestone,now_text,revision FROM projects WHERE id=?`, projectID).Scan(&p.ID, &p.Name, &p.Status, &p.Purpose, &p.Milestone, &p.Now, &p.Revision)
	if err != nil {
		return domain.ContextPacket{}, mapErr(err)
	}
	out := domain.ContextPacket{Project: p, Changes: delta.Changes, Unchanged: delta.Unchanged, FromCursor: since}
	if out.Unchanged {
		if err := tx.Commit(); err != nil {
			return domain.ContextPacket{}, err
		}
		return out, nil
	}
	out.Tasks, err = tasks(ctx, tx, projectID, 20)
	if err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,project_revision,title,rationale FROM decisions WHERE project_id=? ORDER BY project_revision DESC LIMIT 10`, projectID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var d domain.Decision
		d.ProjectID = projectID
		if err := rows.Scan(&d.ID, &d.Revision, &d.Title, &d.Rationale); err != nil {
			rows.Close()
			return out, err
		}
		out.Decisions = append(out.Decisions, d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = tx.QueryContext(ctx, `SELECT w.id,w.slug,w.title,w.current_revision,r.summary,r.body FROM wiki_pages w JOIN wiki_revisions r ON r.page_id=w.id AND r.revision=w.current_revision WHERE w.project_id=? ORDER BY w.slug LIMIT 10`, projectID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var w domain.WikiPage
		w.ProjectID = projectID
		if err := rows.Scan(&w.ID, &w.Slug, &w.Title, &w.Revision, &w.Summary, &w.Body); err != nil {
			rows.Close()
			return out, err
		}
		out.Wiki = append(out.Wiki, w)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	out.Sessions, err = who(ctx, tx, projectID, 10)
	if err != nil {
		return out, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM inbox_items WHERE project_id=? AND recipient_session_id=? AND unread=1`, projectID, recipientSessionID).Scan(&out.Unread); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ContextPacket{}, err
	}
	return out, nil
}

func (s *Store) tasks(ctx context.Context, projectID string, limit int) ([]domain.Task, error) {
	return tasks(ctx, s.db, projectID, limit)
}

func tasks(ctx context.Context, q queryer, projectID string, limit int) ([]domain.Task, error) {
	if limit < 1 || limit > 100 {
		return nil, domain.ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT id,state,title,priority,COALESCE(owner_session_id,''),accept_text FROM tasks WHERE project_id=? ORDER BY priority,id LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	var out []domain.Task
	for rows.Next() {
		var t domain.Task
		t.ProjectID = projectID
		if err := rows.Scan(&t.ID, &t.State, &t.Title, &t.Priority, &t.OwnerSessionID, &t.Accept); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		drows, err := q.QueryContext(ctx, `SELECT depends_on_task_id FROM task_dependencies WHERE task_id=? ORDER BY depends_on_task_id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for drows.Next() {
			var id string
			if err := drows.Scan(&id); err != nil {
				drows.Close()
				return nil, err
			}
			out[i].Dependencies = append(out[i].Dependencies, id)
		}
		if err := drows.Err(); err != nil {
			drows.Close()
			return nil, err
		}
		if err := drows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) Who(ctx context.Context, projectID string, limit int) ([]domain.Session, error) {
	return who(ctx, s.db, projectID, limit)
}

func who(ctx context.Context, q queryer, projectID string, limit int) ([]domain.Session, error) {
	if limit < 1 || limit > 100 {
		return nil, domain.ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT s.id,s.host,COALESCE(s.project_id,''),s.purpose,p.host_state,p.turn_state,p.observed_at
FROM sessions s JOIN presence_facts p ON p.id=(SELECT p2.id FROM presence_facts p2 WHERE p2.session_id=s.id ORDER BY p2.observed_at DESC,p2.id DESC LIMIT 1)
WHERE (?='' OR s.project_id=?) ORDER BY p.observed_at DESC,s.id LIMIT ?`, projectID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Session
	for rows.Next() {
		var v domain.Session
		var at string
		if err := rows.Scan(&v.ID, &v.Host, &v.ProjectID, &v.Purpose, &v.HostState, &v.Turn, &at); err != nil {
			return nil, err
		}
		v.LastActivity = parseStamp(at)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Inbox(ctx context.Context, projectID, recipient string, limit int) ([]domain.InboxItem, error) {
	if recipient == "" || limit < 1 || limit > 100 {
		return nil, domain.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,from_session_id,ref,snippet,unread,created_at FROM inbox_items WHERE recipient_session_id=? AND (?='' OR project_id=?) ORDER BY created_at DESC,id LIMIT ?`, recipient, projectID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InboxItem
	for rows.Next() {
		var v domain.InboxItem
		var unread int
		var at string
		if err := rows.Scan(&v.ID, &v.Kind, &v.FromSessionID, &v.Ref, &v.Snippet, &unread, &at); err != nil {
			return nil, err
		}
		v.Unread = unread == 1
		v.CreatedAt = parseStamp(at)
		out = append(out, v)
	}
	return out, rows.Err()
}

func ftsQuery(q string) (string, error) {
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return "", domain.ErrInvalid
	}
	for i, v := range fields {
		fields[i] = `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return strings.Join(fields, " AND "), nil
}

func (s *Store) Search(ctx context.Context, projectID, query string, limit int) ([]domain.SearchHit, error) {
	if projectID == "" || limit < 1 || limit > 10 {
		return nil, domain.ErrInvalid
	}
	q, err := ftsQuery(query)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.ref,COALESCE(d.project_id,''),d.kind,d.revision,d.title,
snippet(search_fts,1,'','', ' … ',12),COALESCE(p.created_at,x.created_at,wr.created_at,'')
FROM search_fts
JOIN search_documents d ON d.id=search_fts.rowid
LEFT JOIN posts p ON p.id=d.ref
LEFT JOIN decisions x ON x.id=d.ref
LEFT JOIN wiki_pages w ON w.id=d.ref
LEFT JOIN wiki_revisions wr ON wr.page_id=w.id AND wr.revision=w.current_revision
WHERE search_fts MATCH ?
AND ((?='general' AND d.project_id IS NULL) OR (?<>'general' AND d.project_id=?))
ORDER BY bm25(search_fts),d.ref LIMIT ?`, q, projectID, projectID, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	defer rows.Close()
	var out []domain.SearchHit
	for rows.Next() {
		var h domain.SearchHit
		var at string
		if err := rows.Scan(&h.Ref, &h.ProjectID, &h.Kind, &h.Revision, &h.Title, &h.Snippet, &at); err != nil {
			return nil, err
		}
		h.Title = boundedUTF8(h.Title, 200)
		h.Snippet = boundedUTF8(h.Snippet, 240)
		h.CreatedAt = parseStamp(at)
		out = append(out, h)
	}
	return out, rows.Err()
}

func boundedUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	const suffix = " …"
	end := maxBytes - len(suffix)
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return strings.TrimSpace(value[:end]) + suffix
}

func (s *Store) Open(ctx context.Context, ref string) (domain.Object, error) {
	var o domain.Object
	var at string
	err := s.db.QueryRowContext(ctx, `SELECT id,COALESCE(project_id,''),topic_id,kind,COALESCE(project_revision,0),title,body,basis,ref,session_id,created_at FROM posts WHERE id=?`, ref).Scan(&o.Ref, &o.ProjectID, &o.TopicID, &o.Kind, &o.Revision, &o.Title, &o.Body, &o.Basis, &o.RelatedRef, &o.SessionID, &at)
	if err == nil {
		o.CreatedAt = parseStamp(at)
		return o, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return o, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT id,project_id,'decision',project_revision,title,rationale FROM decisions WHERE id=?`, ref).Scan(&o.Ref, &o.ProjectID, &o.Kind, &o.Revision, &o.Title, &o.Body)
	if err == nil {
		return o, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return o, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT w.id,w.project_id,'wiki',w.current_revision,w.title,r.summary,r.body FROM wiki_pages w JOIN wiki_revisions r ON r.page_id=w.id AND r.revision=w.current_revision WHERE w.id=?`, ref).Scan(&o.Ref, &o.ProjectID, &o.Kind, &o.Revision, &o.Title, &o.Summary, &o.Body)
	if err == nil {
		return o, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return o, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT id,project_id,'task',0,title,state,accept_text FROM tasks WHERE id=?`, ref).Scan(&o.Ref, &o.ProjectID, &o.Kind, &o.Revision, &o.Title, &o.State, &o.Accept)
	return o, mapErr(err)
}

func (s *Store) Next(ctx context.Context, projectID string, limit int) ([]domain.Task, error) {
	if limit < 1 || limit > 20 {
		return nil, domain.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.state,t.title,t.priority,COALESCE(t.owner_session_id,''),t.accept_text
FROM tasks t WHERE t.project_id=? AND t.state='ready' AND NOT EXISTS(
 SELECT 1 FROM task_dependencies d JOIN tasks b ON b.id=d.depends_on_task_id WHERE d.task_id=t.id AND b.state<>'done')
ORDER BY t.priority,t.id LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		var t domain.Task
		t.ProjectID = projectID
		if err := rows.Scan(&t.ID, &t.State, &t.Title, &t.Priority, &t.OwnerSessionID, &t.Accept); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
