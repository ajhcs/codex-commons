package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"codex-commons/internal/domain"
)

func requestStorageKey(actorID, sessionID, requestID string) string {
	if requestID == "" {
		return ""
	}
	scope := actorID
	if scope == "" {
		scope = sessionID
	}
	return strconv.Itoa(len(scope)) + ":" + scope + requestID
}

func sameLease(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func (s *Store) claimByRequest(ctx context.Context, storageKey, requestID string) (domain.Claim, error) {
	var c domain.Claim
	var claimed string
	var lease sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,task_id,session_id,request_id,project_revision,claimed_at,lease_until FROM task_claims WHERE request_id=?`, storageKey).Scan(&c.ID, &c.TaskID, &c.SessionID, &c.RequestID, &c.Revision, &claimed, &lease)
	if err != nil {
		return c, mapErr(err)
	}
	c.RequestID = requestID
	c.ClaimedAt = parseStamp(claimed)
	if lease.Valid {
		t := parseStamp(lease.String)
		c.LeaseUntil = &t
	}
	return c, nil
}

func (s *Store) Claim(ctx context.Context, req domain.ClaimRequest) (domain.Claim, error) {
	if req.TaskID == "" || req.SessionID == "" || req.RequestID == "" {
		return domain.Claim{}, domain.ErrInvalid
	}
	storageKey := requestStorageKey(req.ActorID, req.SessionID, req.RequestID)
	if prior, err := s.claimByRequest(ctx, storageKey, req.RequestID); err == nil {
		if prior.TaskID != req.TaskID || prior.SessionID != req.SessionID || !sameLease(prior.LeaseUntil, req.LeaseUntil) {
			return domain.Claim{}, domain.ErrConflict
		}
		return prior, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.Claim{}, err
	}
	var projectID string
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM tasks WHERE id=?`, req.TaskID).Scan(&projectID); err != nil {
		return domain.Claim{}, mapErr(err)
	}
	id := newID("C-")
	claimedAt := s.now().UTC()
	var lease any
	if req.LeaseUntil != nil {
		lease = stamp(*req.LeaseUntil)
	}
	rev, err := s.mutate(ctx, projectID, "claim", req.TaskID, "task claimed", func(tx *sql.Tx, rev int64) error {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id=?`, req.TaskID).Scan(&state); err != nil {
			return err
		}
		if state != "ready" {
			return domain.ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_claims(id,task_id,session_id,request_id,claimed_at,lease_until,project_revision) VALUES(?,?,?,?,?,?,?)`, id, req.TaskID, req.SessionID, storageKey, stamp(claimedAt), lease, rev); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE tasks SET state='in_progress',owner_session_id=? WHERE id=? AND state='ready'`, req.SessionID, req.TaskID)
		return err
	})
	if err != nil {
		if prior, e := s.claimByRequest(ctx, storageKey, req.RequestID); e == nil && prior.TaskID == req.TaskID && prior.SessionID == req.SessionID && sameLease(prior.LeaseUntil, req.LeaseUntil) {
			return prior, nil
		}
		return domain.Claim{}, err
	}
	return domain.Claim{ID: id, TaskID: req.TaskID, SessionID: req.SessionID, RequestID: req.RequestID, Revision: rev, ClaimedAt: claimedAt, LeaseUntil: req.LeaseUntil}, nil
}

func (s *Store) postByRequest(ctx context.Context, storageKey, requestID string) (domain.Post, error) {
	var p domain.Post
	var at string
	var project sql.NullString
	var rev sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,topic_id,project_id,project_revision,kind,title,body,basis,ref,session_id,created_at FROM posts WHERE request_id=?`, storageKey).Scan(&p.ID, &p.TopicID, &project, &rev, &p.Kind, &p.Title, &p.Body, &p.Basis, &p.Ref, &p.SessionID, &at)
	if err != nil {
		return p, mapErr(err)
	}
	if project.Valid {
		p.ProjectID = project.String
	}
	if rev.Valid {
		p.Revision = rev.Int64
	}
	p.CreatedAt = parseStamp(at)
	p.RequestID = requestID
	return p, nil
}

func samePost(a domain.Post, b domain.PostRequest) bool {
	return a.TopicID == b.TopicID && a.Kind == b.Kind && a.Title == b.Title && a.Body == b.Body && a.Basis == b.Basis && a.Ref == b.Ref && a.SessionID == b.SessionID
}

func (s *Store) Post(ctx context.Context, req domain.PostRequest) (domain.Post, error) {
	if req.TopicID == "" || !domain.PostKinds[req.Kind] || req.Title == "" || req.Body == "" || req.Basis == "" || req.SessionID == "" {
		return domain.Post{}, domain.ErrInvalid
	}
	if req.Kind == "topic_request" && req.TopicID != domain.TopicGeneral {
		return domain.Post{}, domain.ErrInvalid
	}
	storageKey := requestStorageKey(req.ActorID, req.SessionID, req.RequestID)
	if storageKey != "" {
		if prior, err := s.postByRequest(ctx, storageKey, req.RequestID); err == nil {
			if !samePost(prior, req) {
				return domain.Post{}, domain.ErrConflict
			}
			return prior, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return domain.Post{}, err
		}
	}
	var project sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM topics WHERE id=?`, req.TopicID).Scan(&project); err != nil {
		return domain.Post{}, mapErr(err)
	}
	id := newID("P-")
	created := s.now().UTC()
	insert := func(tx *sql.Tx, rev any) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO posts(id,topic_id,project_id,project_revision,kind,title,body,basis,ref,session_id,request_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,NULLIF(?,''),?)`, id, req.TopicID, project, rev, req.Kind, req.Title, req.Body, req.Basis, req.Ref, req.SessionID, storageKey, stamp(created)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO search_documents(project_id,ref,kind,revision,title,body) VALUES(?,?,?,?,?,?)`, project, id, req.Kind, valueRev(rev), req.Title, req.Body+" "+req.Basis)
		return err
	}
	var rev int64
	if project.Valid {
		var err error
		rev, err = s.mutate(ctx, project.String, "post", id, req.Title, func(tx *sql.Tx, r int64) error { return insert(tx, r) })
		if err != nil {
			if storageKey != "" {
				if prior, replayErr := s.postByRequest(ctx, storageKey, req.RequestID); replayErr == nil && samePost(prior, req) {
					return prior, nil
				}
			}
			return domain.Post{}, err
		}
	} else {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return domain.Post{}, err
		}
		defer tx.Rollback()
		if err = insert(tx, nil); err != nil {
			return domain.Post{}, mapErr(err)
		}
		if err = tx.Commit(); err != nil {
			return domain.Post{}, mapErr(err)
		}
	}
	return domain.Post{ID: id, TopicID: req.TopicID, ProjectID: project.String, Kind: req.Kind, Title: req.Title, Body: req.Body, Basis: req.Basis, Ref: req.Ref, SessionID: req.SessionID, RequestID: req.RequestID, Revision: rev, CreatedAt: created}, nil
}
func valueRev(v any) int64 {
	if n, ok := v.(int64); ok {
		return n
	}
	return 0
}

func (s *Store) commentByRequest(ctx context.Context, storageKey string) (domain.CommentRequest, domain.WriteResult, error) {
	var prior domain.CommentRequest
	var result domain.WriteResult
	var rev sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,project_revision,post_id,body,session_id FROM comments WHERE request_id=?`, storageKey).Scan(&result.ID, &rev, &prior.PostID, &prior.Body, &prior.SessionID)
	if err != nil {
		return prior, result, mapErr(err)
	}
	if rev.Valid {
		result.Revision = rev.Int64
	}
	return prior, result, nil
}

func sameComment(a, b domain.CommentRequest) bool {
	return a.PostID == b.PostID && a.Body == b.Body && a.SessionID == b.SessionID
}

func (s *Store) Comment(ctx context.Context, req domain.CommentRequest) (domain.WriteResult, error) {
	if req.PostID == "" || req.Body == "" || req.SessionID == "" {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	storageKey := requestStorageKey(req.ActorID, req.SessionID, req.RequestID)
	if storageKey != "" {
		prior, result, err := s.commentByRequest(ctx, storageKey)
		if err == nil {
			if !sameComment(prior, req) {
				return domain.WriteResult{}, domain.ErrConflict
			}
			return result, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return domain.WriteResult{}, err
		}
	}
	var project sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM posts WHERE id=?`, req.PostID).Scan(&project); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	id := newID("R-")
	created := stamp(s.now())
	insert := func(tx *sql.Tx, rev any) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO comments(id,post_id,project_id,project_revision,body,session_id,request_id,created_at) VALUES(?,?,?,?,?,?,NULLIF(?,''),?)`, id, req.PostID, project, rev, req.Body, req.SessionID, storageKey, created)
		return err
	}
	var result domain.WriteResult
	var err error
	if project.Valid {
		result.Revision, err = s.mutate(ctx, project.String, "comment", id, "comment on "+req.PostID, func(tx *sql.Tx, r int64) error { return insert(tx, r) })
	} else {
		var tx *sql.Tx
		tx, err = s.db.BeginTx(ctx, nil)
		if err == nil {
			defer tx.Rollback()
			err = insert(tx, nil)
		}
		if err == nil {
			err = tx.Commit()
		}
	}
	if err != nil {
		if storageKey != "" {
			prior, replay, replayErr := s.commentByRequest(ctx, storageKey)
			if replayErr == nil && sameComment(prior, req) {
				return replay, nil
			}
		}
		return domain.WriteResult{}, mapErr(err)
	}
	result.ID = id
	return result, nil
}

func (s *Store) statusByRequest(ctx context.Context, storageKey string) (domain.StatusRequest, domain.WriteResult, error) {
	var prior domain.StatusRequest
	var result domain.WriteResult
	err := s.db.QueryRowContext(ctx, `SELECT id,project_revision,project_id,ref,state,detail,session_id FROM status_events WHERE request_id=?`, storageKey).Scan(&result.ID, &result.Revision, &prior.ProjectID, &prior.Ref, &prior.State, &prior.Detail, &prior.SessionID)
	if err != nil {
		return prior, result, mapErr(err)
	}
	return prior, result, nil
}

func sameStatus(a, b domain.StatusRequest) bool {
	return a.ProjectID == b.ProjectID && a.Ref == b.Ref && a.State == b.State && a.Detail == b.Detail && a.SessionID == b.SessionID
}

func (s *Store) Status(ctx context.Context, req domain.StatusRequest) (domain.WriteResult, error) {
	if req.ProjectID == "" || req.Ref == "" || req.State == "" || req.SessionID == "" {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	storageKey := requestStorageKey(req.ActorID, req.SessionID, req.RequestID)
	if storageKey != "" {
		prior, result, err := s.statusByRequest(ctx, storageKey)
		if err == nil {
			if !sameStatus(prior, req) {
				return domain.WriteResult{}, domain.ErrConflict
			}
			return result, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return domain.WriteResult{}, err
		}
	}
	id := newID("E-")
	rev, err := s.mutate(ctx, req.ProjectID, "status", req.Ref, req.State, func(tx *sql.Tx, r int64) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO status_events(id,project_id,project_revision,ref,state,detail,session_id,request_id,created_at) VALUES(?,?,?,?,?,?,?,NULLIF(?,''),?)`, id, req.ProjectID, r, req.Ref, req.State, req.Detail, req.SessionID, storageKey, stamp(s.now()))
		return err
	})
	if err != nil {
		if storageKey != "" {
			prior, replay, replayErr := s.statusByRequest(ctx, storageKey)
			if replayErr == nil && sameStatus(prior, req) {
				return replay, nil
			}
		}
		return domain.WriteResult{}, err
	}
	return domain.WriteResult{ID: id, Revision: rev}, nil
}

func (s *Store) Redact(ctx context.Context, projectID, ref, reason, replacement, humanActor string) (domain.WriteResult, error) {
	if projectID == "" || ref == "" || reason == "" || humanActor == "" {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	id := newID("X-")
	rev, err := s.mutate(ctx, projectID, "redaction", ref, "human audited redaction", func(tx *sql.Tx, r int64) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO redactions(id,project_id,object_ref,reason,replacement,human_actor,created_at) VALUES(?,?,?,?,?,?,?)`, id, projectID, ref, reason, replacement, humanActor, stamp(s.now()))
		return err
	})
	return domain.WriteResult{ID: id, Revision: rev}, err
}
