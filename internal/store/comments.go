package store

import (
	"context"

	"codex-commons/internal/domain"
)

func (s *Store) PostCommentByID(ctx context.Context, commentID, viewerKind, viewerSession string) (string, domain.PostComment, error) {
	if commentID == "" {
		return "", domain.PostComment{}, domain.ErrInvalid
	}
	discovery, args, ok := postDiscoveryPredicate(viewerKind, viewerSession)
	if !ok {
		return "", domain.PostComment{}, domain.ErrInvalid
	}
	queryArgs := append([]any{commentID}, args...)
	var postID, created string
	var item domain.PostComment
	err := s.db.QueryRowContext(ctx, `SELECT c.post_id,c.id,c.body,c.intent,c.session_id,COALESCE(h.handle,''),COALESCE(author.purpose,''),c.created_at
FROM comments c JOIN posts p ON p.id=c.post_id
LEFT JOIN sessions author ON author.id=c.session_id LEFT JOIN session_handles h ON h.session_id=c.session_id
WHERE c.id=? AND `+discovery, queryArgs...).Scan(&postID, &item.ID, &item.Body, &item.Intent, &item.Author.SessionID, &item.Author.Handle, &item.Author.Purpose, &created)
	if err != nil {
		return "", domain.PostComment{}, mapErr(err)
	}
	item.CreatedAt = parseStamp(created)
	setCanonicalAuthor(&item.Author)
	mentions, err := readContentMentions(ctx, s.db, "comment", []string{commentID})
	if err != nil {
		return "", domain.PostComment{}, err
	}
	item.Mentions = mentions[commentID]
	return postID, item, nil
}
