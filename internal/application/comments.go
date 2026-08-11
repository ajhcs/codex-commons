package application

import (
	"context"

	"codex-commons/internal/domain"
)

type CommentOpenRequest struct {
	ID, ViewerKind, ViewerPrincipal, ViewerSession string
}

type CommentOpenResult struct {
	PostRef string      `json:"post_ref"`
	Comment PostComment `json:"comment"`
}

func (s *Service) OpenComment(ctx context.Context, request CommentOpenRequest) (CommentOpenResult, error) {
	if s == nil || request.ID == "" {
		return CommentOpenResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(PostRepository)
	if !ok {
		return CommentOpenResult{}, domain.ErrInvalid
	}
	postID, item, err := repository.PostCommentByID(ctx, request.ID, request.ViewerKind, request.ViewerSession)
	if err != nil {
		return CommentOpenResult{}, err
	}
	return CommentOpenResult{PostRef: postID, Comment: PostComment{
		ID: item.ID, Body: item.Body, Intent: item.Intent, Author: appAuthor(item.Author),
		CreatedAt: item.CreatedAt, Mentions: s.appMentions(item.Mentions),
	}}, nil
}
