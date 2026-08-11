package store

import (
	"context"
	"errors"
	"strings"

	"codex-commons/internal/domain"
)

const maxCommentMentions = 5

func contributorSearchPattern(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(value))
	return "%" + escaped + "%"
}

func (s *Store) Contributors(ctx context.Context, q domain.ContributorQuery) ([]domain.Contributor, error) {
	if q.Limit < 1 || q.Limit > 20 || len(q.Search) > 100 || strings.TrimSpace(q.Search) != q.Search || len(q.ProjectID) > 200 {
		return nil, domain.ErrInvalid
	}
	pattern := contributorSearchPattern(q.Search)
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,h.handle,s.host,COALESCE(s.project_id,''),COALESCE(p.name,''),s.purpose
FROM session_handles h JOIN sessions s ON s.id=h.session_id LEFT JOIN projects p ON p.id=s.project_id
WHERE (?='' OR lower(h.handle) LIKE ? ESCAPE '\' OR lower(s.purpose) LIKE ? ESCAPE '\')
AND (?='' OR s.project_id=?)
AND (?='' OR lower(h.handle)>lower(?) OR (lower(h.handle)=lower(?) AND s.id>?))
ORDER BY lower(h.handle),s.id LIMIT ?`, q.Search, pattern, pattern, q.ProjectID, q.ProjectID,
		q.AfterHandle, q.AfterHandle, q.AfterHandle, q.AfterSessionID, q.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Contributor{}
	for rows.Next() {
		var v domain.Contributor
		if err := rows.Scan(&v.SessionID, &v.Handle, &v.Host, &v.ProjectID, &v.ProjectName, &v.Purpose); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func normalizeMentionIDs(ids []string) ([]string, bool) {
	if len(ids) > maxCommentMentions {
		return nil, false
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || len(id) > 200 {
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, true
}

func readCommentMentionIDs(ctx context.Context, q queryer, commentID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT recipient_session_id FROM comment_mentions WHERE comment_id=? ORDER BY position`, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) perspectiveScopeByRequest(ctx context.Context, storageKey, requestID string) (domain.PerspectiveScopeRequest, domain.WriteResult, error) {
	var prior domain.PerspectiveScopeRequest
	var result domain.WriteResult
	err := s.db.QueryRowContext(ctx, `SELECT id,post_id,scope,base_revision,revision,session_id FROM post_perspective_scope_events WHERE request_id=?`, storageKey).
		Scan(&result.ID, &prior.PostID, &prior.Scope, &prior.BaseRevision, &result.Revision, &prior.SessionID)
	prior.RequestID = requestID
	return prior, result, mapErr(err)
}

func samePerspectiveScope(a, b domain.PerspectiveScopeRequest) bool {
	return a.PostID == b.PostID && a.Scope == b.Scope && a.BaseRevision == b.BaseRevision && a.SessionID == b.SessionID
}

func (s *Store) SetPerspectiveScope(ctx context.Context, req domain.PerspectiveScopeRequest) (domain.WriteResult, error) {
	if req.PostID == "" || req.SessionID == "" || (req.Scope != "closed" && req.Scope != "project" && req.Scope != "commons") || req.BaseRevision < 0 || req.RequestID == "" {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	storageKey := requestStorageKey(req.ActorID, req.SessionID, req.RequestID)
	if prior, result, err := s.perspectiveScopeByRequest(ctx, storageKey, req.RequestID); err == nil {
		if !samePerspectiveScope(prior, req) {
			return domain.WriteResult{}, domain.ErrConflict
		}
		return result, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.WriteResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	var current string
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT scope,revision FROM post_perspective_scopes WHERE post_id=?`, req.PostID).Scan(&current, &revision); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if revision != req.BaseRevision {
		return domain.WriteResult{}, domain.ErrConflict
	}
	id, created := newID("PSC-"), stamp(s.now())
	next := revision + 1
	if _, err = tx.ExecContext(ctx, `INSERT INTO post_perspective_scope_events(id,post_id,scope,base_revision,revision,actor_id,session_id,request_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, req.PostID, req.Scope, revision, next, req.ActorID, req.SessionID, storageKey, created); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE post_perspective_scopes SET scope=?,revision=?,event_id=?,updated_at=? WHERE post_id=? AND revision=?`, req.Scope, next, id, created, req.PostID, revision)
	if err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return domain.WriteResult{}, domain.ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	return domain.WriteResult{ID: id, Revision: next}, nil
}

func mentionAuthorsForComments(ctx context.Context, q queryer, commentIDs []string) (map[string][]domain.PostAuthor, error) {
	out := make(map[string][]domain.PostAuthor, len(commentIDs))
	if len(commentIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(commentIDs))
	marks := make([]string, len(commentIDs))
	for i, id := range commentIDs {
		marks[i] = "?"
		args = append(args, id)
	}
	rows, err := q.QueryContext(ctx, `SELECT m.comment_id,s.id,h.handle,s.purpose FROM comment_mentions m JOIN sessions s ON s.id=m.recipient_session_id JOIN session_handles h ON h.session_id=s.id WHERE m.comment_id IN (`+strings.Join(marks, ",")+`) ORDER BY m.comment_id,m.position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid string
		var a domain.PostAuthor
		if err := rows.Scan(&cid, &a.SessionID, &a.Handle, &a.Purpose); err != nil {
			return nil, err
		}
		out[cid] = append(out[cid], a)
	}
	return out, rows.Err()
}
