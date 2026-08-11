package appbackend

import (
	"context"

	"codex-commons/internal/application"
	"codex-commons/internal/httpapi"
)

func (a *Adapter) OpenComment(ctx context.Context, query httpapi.CommentOpenQuery, meta httpapi.RequestMeta) (httpapi.CommentOpenResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.CommentOpenResult{}, err
	}
	query.ViewerKind, query.ViewerPrincipal, query.ViewerSession = meta.PrincipalKind, meta.Principal, meta.Session
	out, err := a.home.OpenComment(ctx, application.CommentOpenRequest(query))
	return out, mapBrowseError(err, "comment")
}
