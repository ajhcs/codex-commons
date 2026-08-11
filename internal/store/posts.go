package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"codex-commons/internal/domain"
)

const (
	maxPostFeedLimit   = 50
	maxPostAttachments = 8
	maxPostURL         = 2048
	maxPostTitle       = 200
)

func validPostAttachments(items []domain.PostAttachment) bool {
	if len(items) > maxPostAttachments {
		return false
	}
	for _, item := range items {
		if item.Kind != "link" && item.Kind != "github" && item.Kind != "image" && item.Kind != "video" ||
			item.URL == "" || len(item.URL) > maxPostURL || len(item.Title) > maxPostTitle ||
			strings.TrimSpace(item.URL) != item.URL || strings.TrimSpace(item.Title) != item.Title {
			return false
		}
		parsed, err := url.Parse(item.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return false
		}
		if item.Kind == "github" && !strings.EqualFold(parsed.Hostname(), "github.com") {
			return false
		}
	}
	return true
}

func validatePostBrowseQuery(query domain.PostBrowseQuery) bool {
	if _, _, ok := postDiscoveryPredicate(query.ViewerKind, query.ViewerSession); !ok {
		return false
	}
	if query.Limit < 1 || query.Limit > maxPostFeedLimit {
		return false
	}
	f := query.Filters
	if len(f.Search) > maxBrowseSearch || strings.TrimSpace(f.Search) != f.Search ||
		len(f.TopicID) > maxHomeIdentifier || len(f.ProjectID) > maxHomeIdentifier ||
		f.Kind != "" && !domain.PostKinds[f.Kind] ||
		f.CreatedFrom != nil && f.CreatedTo != nil && f.CreatedFrom.After(*f.CreatedTo) {
		return false
	}
	return query.After == nil || (!query.After.Time.IsZero() && boundedHomeValue(query.After.ID, maxHomeIdentifier))
}

func postWhere(filters domain.PostFilters, after *domain.BrowseCursor) (string, []any, error) {
	where, args := []string{"1=1"}, []any{}
	if filters.Search != "" {
		query, err := ftsQuery(filters.Search)
		if err != nil {
			return "", nil, err
		}
		where, args = append(where, "search_fts MATCH ?"), append(args, query)
	}
	if filters.TopicID != "" {
		where, args = append(where, "p.topic_id=?"), append(args, filters.TopicID)
	}
	if filters.ProjectID != "" {
		where, args = append(where, "p.project_id=?"), append(args, filters.ProjectID)
	}
	if filters.Kind != "" {
		where, args = append(where, "p.kind=?"), append(args, filters.Kind)
	}
	if filters.CreatedFrom != nil {
		where, args = append(where, "julianday(p.created_at)>=julianday(?)"), append(args, stamp(filters.CreatedFrom.UTC()))
	}
	if filters.CreatedTo != nil {
		where, args = append(where, "julianday(p.created_at)<=julianday(?)"), append(args, stamp(filters.CreatedTo.UTC()))
	}
	if after != nil {
		where, args = append(where, "(julianday(p.created_at)<julianday(?) OR (julianday(p.created_at)=julianday(?) AND p.id>?))"),
			append(args, stamp(after.Time.UTC()), stamp(after.Time.UTC()), after.ID)
	}
	return strings.Join(where, " AND "), args, nil
}

func postFrom(filters domain.PostFilters) string {
	if filters.Search == "" {
		return "posts p"
	}
	return "search_fts JOIN search_documents d ON d.id=search_fts.rowid JOIN posts p ON p.id=d.ref"
}

// PostBrowseSnapshot returns compact chronological metadata only. Canonical
// bodies, bases, and comments are intentionally reserved for PostThread.
func (s *Store) PostBrowseSnapshot(ctx context.Context, query domain.PostBrowseQuery) (domain.PostBrowseSnapshot, error) {
	if !validatePostBrowseQuery(query) {
		return domain.PostBrowseSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.PostBrowseSnapshot{}, err
	}
	defer tx.Rollback()
	out := domain.PostBrowseSnapshot{Items: []domain.PostBrowseItem{}}
	where, args, err := postWhere(query.Filters, nil)
	if err != nil {
		return out, err
	}
	discovery, discoveryArgs, _ := postDiscoveryPredicate(query.ViewerKind, query.ViewerSession)
	where += " AND " + discovery
	args = append(args, discoveryArgs...)
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+postFrom(query.Filters)+" WHERE "+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	pageWhere, pageArgs, err := postWhere(query.Filters, query.After)
	if err != nil {
		return out, err
	}
	pageWhere += " AND " + discovery
	pageArgs = append(pageArgs, discoveryArgs...)
	pageArgs = append(pageArgs, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.kind,p.title,p.body,p.created_at,t.id,t.name,
COALESCE(pr.id,''),COALESCE(pr.name,''),p.session_id,COALESCE(h.handle,''),COALESCE(s.purpose,''),
(SELECT count(*) FROM comments c WHERE c.post_id=p.id),
COALESCE((SELECT e.state FROM post_state_events e WHERE e.post_id=p.id ORDER BY e.sequence DESC LIMIT 1),'open'),
COALESCE((SELECT e.superseded_by FROM post_state_events e WHERE e.post_id=p.id ORDER BY e.sequence DESC LIMIT 1),''),
COALESCE(psc.scope,'closed'),COALESCE(psc.revision,0)
FROM `+postFrom(query.Filters)+`
JOIN topics t ON t.id=p.topic_id
LEFT JOIN projects pr ON pr.id=p.project_id
LEFT JOIN sessions s ON s.id=p.session_id
LEFT JOIN session_handles h ON h.session_id=p.session_id
LEFT JOIN post_perspective_scopes psc ON psc.post_id=p.id
WHERE `+pageWhere+`
ORDER BY julianday(p.created_at) DESC,p.id
LIMIT ?`, pageArgs...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.PostBrowseItem
		var body, created, projectID, projectName string
		if err := rows.Scan(&item.ID, &item.Kind, &item.Title, &body, &created,
			&item.Topic.ID, &item.Topic.Name, &projectID, &projectName,
			&item.Author.SessionID, &item.Author.Handle, &item.Author.Purpose, &item.CommentCount,
			&item.State, &item.SupersededBy, &item.PerspectiveScope.Scope, &item.PerspectiveScope.Revision); err != nil {
			rows.Close()
			return out, err
		}
		item.Title = boundedUTF8(item.Title, 200)
		item.Preview = boundedUTF8(strings.TrimSpace(body), 320)
		item.CreatedAt = parseStamp(created)
		setCanonicalAuthor(&item.Author)
		if projectID != "" {
			item.Project = &domain.PostProject{ID: projectID, Name: projectName}
		}
		item.Attachments, err = readPostAttachments(ctx, tx, item.ID)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	postIDs := make([]string, 0, len(out.Items))
	for _, item := range out.Items {
		postIDs = append(postIDs, item.ID)
	}
	mentions, err := readContentMentions(ctx, tx, "post", postIDs)
	if err != nil {
		return domain.PostBrowseSnapshot{}, err
	}
	for i := range out.Items {
		out.Items[i].Mentions = mentions[out.Items[i].ID]
	}
	if err := tx.Commit(); err != nil {
		return domain.PostBrowseSnapshot{}, err
	}
	return out, nil
}

func readPostAttachments(ctx context.Context, q queryer, postID string) ([]domain.PostAttachment, error) {
	rows, err := q.QueryContext(ctx, `SELECT kind,url,title FROM post_attachments WHERE post_id=? ORDER BY position`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PostAttachment{}
	for rows.Next() {
		var item domain.PostAttachment
		if err := rows.Scan(&item.Kind, &item.URL, &item.Title); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) PostThread(ctx context.Context, query domain.PostThreadQuery) (domain.PostThread, error) {
	if query.PostID == "" || query.Limit < 1 || query.Limit > maxPostFeedLimit ||
		query.After != nil && (query.After.Time.IsZero() || !boundedHomeValue(query.After.ID, maxHomeIdentifier)) {
		return domain.PostThread{}, domain.ErrInvalid
	}
	discovery, discoveryArgs, ok := postDiscoveryPredicate(query.ViewerKind, query.ViewerSession)
	if !ok {
		return domain.PostThread{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.PostThread{}, err
	}
	defer tx.Rollback()
	var out domain.PostThread
	var created, projectID, projectName string
	err = tx.QueryRowContext(ctx, `SELECT p.id,COALESCE(p.project_id,''),p.topic_id,p.kind,
COALESCE(p.project_revision,0),p.title,p.body,p.basis,p.ref,p.session_id,p.created_at,
t.id,t.name,COALESCE(pr.id,''),COALESCE(pr.name,''),COALESCE(h.handle,''),COALESCE(s.purpose,''),
COALESCE((SELECT e.state FROM post_state_events e WHERE e.post_id=p.id ORDER BY e.sequence DESC LIMIT 1),'open'),
COALESCE((SELECT e.superseded_by FROM post_state_events e WHERE e.post_id=p.id ORDER BY e.sequence DESC LIMIT 1),''),
(SELECT count(*) FROM comments c WHERE c.post_id=p.id),
COALESCE(psc.scope,'closed'),COALESCE(psc.revision,0)
FROM posts p JOIN topics t ON t.id=p.topic_id
LEFT JOIN projects pr ON pr.id=p.project_id LEFT JOIN sessions s ON s.id=p.session_id
LEFT JOIN session_handles h ON h.session_id=p.session_id LEFT JOIN post_perspective_scopes psc ON psc.post_id=p.id
WHERE p.id=? AND `+discovery, append([]any{query.PostID}, discoveryArgs...)...).Scan(&out.Post.Ref, &out.Post.ProjectID, &out.Post.TopicID,
		&out.Post.Kind, &out.Post.Revision, &out.Post.Title, &out.Post.Body, &out.Post.Basis,
		&out.Post.RelatedRef, &out.Post.SessionID, &created, &out.Topic.ID, &out.Topic.Name,
		&projectID, &projectName, &out.Author.Handle, &out.Author.Purpose, &out.State, &out.SupersededBy, &out.CommentCount, &out.PerspectiveScope.Scope, &out.PerspectiveScope.Revision)
	if err != nil {
		return out, mapErr(err)
	}
	out.Post.CreatedAt = parseStamp(created)
	out.Author.SessionID = out.Post.SessionID
	setCanonicalAuthor(&out.Author)
	if projectID != "" {
		out.Project = &domain.PostProject{ID: projectID, Name: projectName}
	}
	out.Attachments, err = readPostAttachments(ctx, tx, query.PostID)
	if err != nil {
		return out, err
	}
	where, args := "c.post_id=?", []any{query.PostID}
	if query.After != nil {
		where += " AND (julianday(c.created_at)>julianday(?) OR (julianday(c.created_at)=julianday(?) AND c.id>?))"
		args = append(args, stamp(query.After.Time.UTC()), stamp(query.After.Time.UTC()), query.After.ID)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.body,c.intent,c.session_id,COALESCE(h.handle,''),COALESCE(s.purpose,''),c.created_at
FROM comments c LEFT JOIN sessions s ON s.id=c.session_id LEFT JOIN session_handles h ON h.session_id=c.session_id WHERE `+where+`
ORDER BY julianday(c.created_at),c.id LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.PostComment
		var stampValue string
		if err := rows.Scan(&item.ID, &item.Body, &item.Intent, &item.Author.SessionID, &item.Author.Handle, &item.Author.Purpose, &stampValue); err != nil {
			rows.Close()
			return out, err
		}
		item.CreatedAt = parseStamp(stampValue)
		setCanonicalAuthor(&item.Author)
		out.Comments = append(out.Comments, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	postMentions, err := readContentMentions(ctx, tx, "post", []string{query.PostID})
	if err != nil {
		return out, err
	}
	out.Mentions = postMentions[query.PostID]
	commentIDs := make([]string, 0, len(out.Comments))
	for _, item := range out.Comments {
		commentIDs = append(commentIDs, item.ID)
	}
	mentions, err := readContentMentions(ctx, tx, "comment", commentIDs)
	if err != nil {
		return out, err
	}
	for i := range out.Comments {
		out.Comments[i].Mentions = mentions[out.Comments[i].ID]
	}
	if err := tx.Commit(); err != nil {
		return domain.PostThread{}, err
	}
	return out, nil
}

func (s *Store) postStateByRequest(ctx context.Context, storageKey, requestID string) (domain.PostStateRequest, domain.WriteResult, error) {
	var prior domain.PostStateRequest
	var result domain.WriteResult
	var projectRevision sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,project_revision,post_id,state,COALESCE(superseded_by,''),session_id
FROM post_state_events WHERE request_id=?`, storageKey).Scan(&result.ID, &projectRevision,
		&prior.PostID, &prior.State, &prior.SupersededBy, &prior.SessionID)
	if err != nil {
		return prior, result, mapErr(err)
	}
	if projectRevision.Valid {
		result.Revision = projectRevision.Int64
	}
	prior.RequestID = requestID
	return prior, result, nil
}

func samePostState(a, b domain.PostStateRequest) bool {
	return a.PostID == b.PostID && a.State == b.State && a.SupersededBy == b.SupersededBy && a.SessionID == b.SessionID
}

func (s *Store) SetPostState(ctx context.Context, req domain.PostStateRequest) (domain.WriteResult, error) {
	if req.PostID == "" || req.SessionID == "" ||
		(req.State != "open" && req.State != "resolved" && req.State != "superseded") ||
		(req.State == "superseded") != (req.SupersededBy != "") || req.PostID == req.SupersededBy {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	storageKey := requestStorageKey(req.ActorID, req.SessionID, req.RequestID)
	if storageKey != "" {
		prior, result, err := s.postStateByRequest(ctx, storageKey, req.RequestID)
		if err == nil {
			if !samePostState(prior, req) {
				return domain.WriteResult{}, domain.ErrConflict
			}
			return result, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return domain.WriteResult{}, err
		}
	}
	var project sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT project_id FROM posts WHERE id=?", req.PostID).Scan(&project); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if req.SupersededBy != "" {
		var exists int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM posts WHERE id=?", req.SupersededBy).Scan(&exists); err != nil {
			return domain.WriteResult{}, err
		}
		if exists != 1 {
			return domain.WriteResult{}, domain.ErrNotFound
		}
	}
	id, created := newID("PS-"), stamp(s.now())
	insert := func(tx *sql.Tx, revision any) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO post_state_events(
id,post_id,project_id,project_revision,state,superseded_by,session_id,request_id,created_at
) VALUES(?,?,?,?,?,NULLIF(?,''),?,NULLIF(?,''),?)`,
			id, req.PostID, project, revision, req.State, req.SupersededBy, req.SessionID, storageKey, created)
		return err
	}
	var result domain.WriteResult
	var err error
	if project.Valid {
		result.Revision, err = s.mutate(ctx, project.String, "post_state", req.PostID, req.State,
			func(tx *sql.Tx, revision int64) error { return insert(tx, revision) })
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
			prior, replay, replayErr := s.postStateByRequest(ctx, storageKey, req.RequestID)
			if replayErr == nil && samePostState(prior, req) {
				return replay, nil
			}
		}
		return domain.WriteResult{}, mapErr(err)
	}
	result.ID = id
	return result, nil
}
