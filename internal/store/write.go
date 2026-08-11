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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Claim{}, err
	}
	defer tx.Rollback()
	// The first statement is a harmless write. It acquires SQLite's single
	// writer before any transactional reads, so concurrent claimers observe the
	// just-committed current-claim row rather than upgrading stale snapshots.
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET state=state WHERE id=?`, req.TaskID)
	if err != nil {
		return domain.Claim{}, mapErr(err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.Claim{}, domain.ErrNotFound
	}
	if prior, err := claimByRequestQuery(ctx, tx, storageKey, req.RequestID); err == nil {
		if prior.TaskID != req.TaskID || prior.SessionID != req.SessionID || !sameLease(prior.LeaseUntil, req.LeaseUntil) {
			return domain.Claim{}, domain.ErrConflict
		}
		return prior, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.Claim{}, err
	}

	var projectID, state, title string
	if err = tx.QueryRowContext(ctx, `SELECT project_id,state,title FROM tasks WHERE id=?`, req.TaskID).Scan(&projectID, &state, &title); err != nil {
		return domain.Claim{}, mapErr(err)
	}
	claimedAt := s.now().UTC()
	var currentClaimID string
	var currentLease sql.NullString
	currentErr := tx.QueryRowContext(ctx, `SELECT claim_id,lease_until FROM task_current_claims WHERE task_id=?`, req.TaskID).Scan(&currentClaimID, &currentLease)
	reclaim := false
	switch {
	case errors.Is(currentErr, sql.ErrNoRows):
		if state != "ready" {
			return domain.Claim{}, domain.ErrConflict
		}
	case currentErr != nil:
		return domain.Claim{}, currentErr
	case !currentLease.Valid || parseStamp(currentLease.String).After(claimedAt):
		return domain.Claim{}, domain.ErrConflict
	case state != "ready" && state != "in_progress":
		return domain.Claim{}, domain.ErrConflict
	default:
		reclaim = true
	}

	revision, err := bumpProjectRevision(ctx, tx, projectID, claimedAt)
	if err != nil {
		return domain.Claim{}, err
	}
	id := newID("C-")
	var lease any
	if req.LeaseUntil != nil {
		lease = stamp(*req.LeaseUntil)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_claims(id,task_id,session_id,request_id,claimed_at,lease_until,project_revision) VALUES(?,?,?,?,?,?,?)`,
		id, req.TaskID, req.SessionID, storageKey, stamp(claimedAt), lease, revision); err != nil {
		_ = tx.Rollback()
		if prior, replayErr := s.claimByRequest(ctx, storageKey, req.RequestID); replayErr == nil && prior.TaskID == req.TaskID && prior.SessionID == req.SessionID && sameLease(prior.LeaseUntil, req.LeaseUntil) {
			return prior, nil
		}
		return domain.Claim{}, mapErr(err)
	}
	if reclaim {
		result, err = tx.ExecContext(ctx, `UPDATE task_current_claims SET claim_id=?,lease_until=?,updated_at=? WHERE task_id=? AND claim_id=? AND lease_until=?`,
			id, lease, stamp(claimedAt), req.TaskID, currentClaimID, currentLease.String)
		if err != nil {
			return domain.Claim{}, mapErr(err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return domain.Claim{}, domain.ErrConflict
		}
	} else if _, err = tx.ExecContext(ctx, `INSERT INTO task_current_claims(task_id,claim_id,lease_until,updated_at) VALUES(?,?,?,?)`,
		req.TaskID, id, lease, stamp(claimedAt)); err != nil {
		return domain.Claim{}, mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tasks SET state='in_progress',owner_session_id=?,project_revision=?,updated_at=? WHERE id=?`,
		req.SessionID, revision, stamp(claimedAt), req.TaskID); err != nil {
		return domain.Claim{}, mapErr(err)
	}
	kind, summary := "claimed", "Task claimed"
	if reclaim {
		kind, summary = "reclaimed", "Expired task claim handed off"
	}
	meta := domain.CoreWriteMeta{ActorID: req.ActorID, SessionID: req.SessionID, RequestID: req.RequestID}
	if err = insertTaskEvent(ctx, tx, req.TaskID, projectID, revision, kind, summary, state, "in_progress", meta, claimedAt); err != nil {
		return domain.Claim{}, err
	}
	if err = insertCoreChange(ctx, tx, projectID, revision, "claim", req.TaskID, summary, claimedAt); err != nil {
		return domain.Claim{}, err
	}
	if err = insertCoreActivity(ctx, tx, "task_claimed", projectID, req.ActorID, req.TaskID, title, kind, claimedAt); err != nil {
		return domain.Claim{}, err
	}
	if err = tx.Commit(); err != nil {
		_ = tx.Rollback()
		if prior, replayErr := s.claimByRequest(ctx, storageKey, req.RequestID); replayErr == nil && prior.TaskID == req.TaskID && prior.SessionID == req.SessionID && sameLease(prior.LeaseUntil, req.LeaseUntil) {
			return prior, nil
		}
		return domain.Claim{}, mapErr(err)
	}
	return domain.Claim{ID: id, TaskID: req.TaskID, SessionID: req.SessionID, RequestID: req.RequestID, Revision: revision, ClaimedAt: claimedAt, LeaseUntil: req.LeaseUntil}, nil
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
	p.Attachments, err = readPostAttachments(ctx, s.db, p.ID)
	if err != nil {
		return p, err
	}
	mentions, err := readContentMentions(ctx, s.db, "post", []string{p.ID})
	if err != nil {
		return p, err
	}
	for _, target := range mentions[p.ID] {
		p.MentionPrincipals = append(p.MentionPrincipals, target.Principal)
	}
	return p, nil
}

func samePost(a domain.Post, b domain.PostRequest) bool {
	if a.TopicID != b.TopicID || a.Kind != b.Kind || a.Title != b.Title || a.Body != b.Body || a.Basis != b.Basis || a.Ref != b.Ref || a.SessionID != b.SessionID || len(a.Attachments) != len(b.Attachments) || len(a.MentionPrincipals) != len(b.MentionPrincipals) {
		return false
	}
	for i := range a.Attachments {
		if a.Attachments[i] != b.Attachments[i] {
			return false
		}
	}
	for i := range a.MentionPrincipals {
		if a.MentionPrincipals[i] != b.MentionPrincipals[i] {
			return false
		}
	}
	return true
}

func (s *Store) Post(ctx context.Context, req domain.PostRequest) (domain.Post, error) {
	mentions, valid := normalizeMentionPrincipals(req.MentionPrincipals, nil)
	if !valid {
		return domain.Post{}, domain.ErrInvalid
	}
	req.MentionPrincipals = mentions
	if req.TopicID == "" || !domain.PostKinds[req.Kind] || req.Title == "" || req.Body == "" || req.Basis == "" || req.SessionID == "" || !validPostAttachments(req.Attachments) {
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO post_perspective_scopes(post_id,scope,revision,event_id,updated_at) VALUES(?,'closed',0,NULL,?)`, id, stamp(created)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO search_documents(project_id,ref,kind,revision,title,body) VALUES(?,?,?,?,?,?)`, project, id, req.Kind, valueRev(rev), req.Title, req.Body+" "+req.Basis); err != nil {
			return err
		}
		for position, attachment := range req.Attachments {
			if _, err := tx.ExecContext(ctx, `INSERT INTO post_attachments(post_id,position,kind,url,title) VALUES(?,?,?,?,?)`, id, position, attachment.Kind, attachment.URL, attachment.Title); err != nil {
				return err
			}
		}
		return insertContentMentions(ctx, tx, "post", id, id, req.ActorKind, req.ActorPrincipal, req.SessionID, req.Body, stamp(created), req.MentionPrincipals)
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
	return domain.Post{ID: id, TopicID: req.TopicID, ProjectID: project.String, Kind: req.Kind, Title: req.Title, Body: req.Body, Basis: req.Basis, Ref: req.Ref, SessionID: req.SessionID, RequestID: req.RequestID, Revision: rev, CreatedAt: created, Attachments: append([]domain.PostAttachment(nil), req.Attachments...), MentionPrincipals: append([]string(nil), req.MentionPrincipals...)}, nil
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
	err := s.db.QueryRowContext(ctx, `SELECT id,project_revision,post_id,body,intent,session_id FROM comments WHERE request_id=?`, storageKey).Scan(&result.ID, &rev, &prior.PostID, &prior.Body, &prior.Intent, &prior.SessionID)
	if err != nil {
		return prior, result, mapErr(err)
	}
	if rev.Valid {
		result.Revision = rev.Int64
	}
	targets, err := readContentMentions(ctx, s.db, "comment", []string{result.ID})
	if err != nil {
		return prior, result, err
	}
	for _, target := range targets[result.ID] {
		prior.MentionPrincipals = append(prior.MentionPrincipals, target.Principal)
	}
	return prior, result, nil
}

func sameComment(a, b domain.CommentRequest) bool {
	if a.PostID != b.PostID || a.Body != b.Body || a.Intent != b.Intent || a.SessionID != b.SessionID || len(a.MentionPrincipals) != len(b.MentionPrincipals) {
		return false
	}
	for i := range a.MentionPrincipals {
		if a.MentionPrincipals[i] != b.MentionPrincipals[i] {
			return false
		}
	}
	return true
}

func (s *Store) Comment(ctx context.Context, req domain.CommentRequest) (domain.WriteResult, error) {
	mentions, valid := normalizeMentionPrincipals(req.MentionPrincipals, req.MentionSessionIDs)
	if !valid {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	req.MentionPrincipals = mentions
	req.MentionSessionIDs = nil
	if req.PostID == "" || req.Body == "" || !domain.CommentIntents[req.Intent] || req.SessionID == "" {
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO comments(id,post_id,project_id,project_revision,body,intent,session_id,request_id,created_at) VALUES(?,?,?,?,?,?,?,NULLIF(?,''),?)`, id, req.PostID, project, rev, req.Body, req.Intent, req.SessionID, storageKey, created); err != nil {
			return err
		}
		return insertContentMentions(ctx, tx, "comment", id, req.PostID, req.ActorKind, req.ActorPrincipal, req.SessionID, req.Body, created, req.MentionPrincipals)
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
