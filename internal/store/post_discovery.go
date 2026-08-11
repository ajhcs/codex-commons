package store

import (
	"context"
	"database/sql"

	"codex-commons/internal/domain"
)

func postDiscoveryPredicate(viewerKind, viewerSession string) (string, []any, bool) {
	// Empty is retained only for direct repository callers and migration-era
	// tests. HTTP always supplies an authenticated, server-attested viewer.
	if viewerKind == "" || viewerKind == "human" {
		return "1=1", nil, true
	}
	if viewerKind != "agent" || viewerSession == "" {
		return "", nil, false
	}
	return `(p.session_id=?
 OR EXISTS (SELECT 1 FROM content_mentions vm WHERE vm.source_kind='post' AND vm.source_id=p.id AND vm.recipient_kind='agent' AND vm.recipient_principal=?)
 OR EXISTS (SELECT 1 FROM content_mentions vm JOIN comments vc ON vm.source_kind='comment' AND vc.id=vm.source_id WHERE vc.post_id=p.id AND vm.recipient_kind='agent' AND vm.recipient_principal=?)
 OR COALESCE((SELECT scope FROM post_perspective_scopes vps WHERE vps.post_id=p.id),'closed')='commons'
 OR (COALESCE((SELECT scope FROM post_perspective_scopes vps WHERE vps.post_id=p.id),'closed')='project'
     AND p.project_id IS NOT NULL
     AND p.project_id=(SELECT project_id FROM sessions WHERE id=?)))`, []any{viewerSession, viewerSession, viewerSession, viewerSession}, true
}

func (s *Store) CanDiscoverPost(ctx context.Context, viewerKind, viewerSession, postID string) (bool, error) {
	if postID == "" {
		return false, domain.ErrInvalid
	}
	predicate, args, ok := postDiscoveryPredicate(viewerKind, viewerSession)
	if !ok {
		return false, domain.ErrInvalid
	}
	args = append([]any{postID}, args...)
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM posts p WHERE p.id=? AND `+predicate, args...).Scan(&count)
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func (s *Store) PostForComment(ctx context.Context, commentID string) (string, error) {
	if commentID == "" {
		return "", domain.ErrInvalid
	}
	var postID string
	if err := s.db.QueryRowContext(ctx, `SELECT post_id FROM comments WHERE id=?`, commentID).Scan(&postID); err != nil {
		return "", mapErr(err)
	}
	return postID, nil
}

func postHasProject(ctx context.Context, q queryer, postID string) (bool, error) {
	var project sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT project_id FROM posts WHERE id=?`, postID).Scan(&project); err != nil {
		return false, mapErr(err)
	}
	return project.Valid, nil
}
