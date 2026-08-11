package appbackend

import (
	"context"
	"errors"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
)

func validateBrowseIdentity(meta httpapi.RequestMeta) error {
	if meta.Actor == "" || meta.Session == "" || meta.Host == "" {
		return httpapi.NewError(httpapi.CodeInvalid, "attested identity required")
	}
	return nil
}

func mapBrowseError(err error, resource string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrNotFound):
		return httpapi.NewError(httpapi.CodeNotFound, resource+" source not found")
	case errors.Is(err, domain.ErrInvalid):
		return httpapi.NewError(httpapi.CodeInvalid, "invalid "+resource+" query")
	case errors.Is(err, domain.ErrUnavailable):
		return httpapi.NewError(httpapi.CodeUnavailable, resource+" unavailable")
	default:
		return err
	}
}

func (a *Adapter) BrowseAttention(ctx context.Context, query httpapi.AttentionBrowseQuery, meta httpapi.RequestMeta) (httpapi.AttentionBrowseResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.AttentionBrowseResult{}, err
	}
	out, err := a.home.BrowseAttention(ctx, query)
	return out, mapBrowseError(err, "attention")
}

func (a *Adapter) BrowseProjects(ctx context.Context, query httpapi.ProjectsBrowseQuery, meta httpapi.RequestMeta) (httpapi.ProjectsBrowseResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.ProjectsBrowseResult{}, err
	}
	out, err := a.home.BrowseProjects(ctx, query)
	return out, mapBrowseError(err, "projects")
}

func (a *Adapter) BrowsePeople(ctx context.Context, query httpapi.PeopleBrowseQuery, meta httpapi.RequestMeta) (httpapi.PeopleBrowseResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.PeopleBrowseResult{}, err
	}
	out, err := a.home.BrowsePeople(ctx, query)
	return out, mapBrowseError(err, "people")
}

func (a *Adapter) BrowsePosts(ctx context.Context, query httpapi.PostFeedQuery, meta httpapi.RequestMeta) (httpapi.PostFeedResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.PostFeedResult{}, err
	}
	out, err := a.home.BrowsePosts(ctx, query)
	return out, mapBrowseError(err, "posts")
}

func (a *Adapter) OpenPost(ctx context.Context, query httpapi.PostOpenQuery, meta httpapi.RequestMeta) (httpapi.PostOpenResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.PostOpenResult{}, err
	}
	out, err := a.home.OpenPost(ctx, query)
	return out, mapBrowseError(err, "post")
}

func (a *Adapter) SetPostState(ctx context.Context, request httpapi.PostStateWriteRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.WriteResult{}, err
	}
	result, err := a.home.SetPostState(ctx, application.PostStateRequest{
		Ref: request.Ref, State: request.State, SupersededBy: request.SupersededBy,
		Actor: meta.Actor, Session: meta.Session, RequestID: meta.IdempotencyKey,
	})
	if err != nil {
		return httpapi.WriteResult{}, mapBrowseError(err, "post state")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}

func (a *Adapter) LookupContributors(ctx context.Context, query httpapi.ContributorLookupQuery, meta httpapi.RequestMeta) (httpapi.ContributorLookupResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.ContributorLookupResult{}, err
	}
	out, err := a.home.LookupContributors(ctx, query)
	return out, mapBrowseError(err, "contributors")
}
func (a *Adapter) SetPerspectiveScope(ctx context.Context, request httpapi.PerspectiveScopeWriteRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if meta.PrincipalKind != "human" {
		return httpapi.WriteResult{}, httpapi.NewError(httpapi.CodeForbidden, "only the local human may change perspective scope")
	}
	result, err := a.home.SetPerspectiveScope(ctx, application.PerspectiveScopeRequest{Ref: request.Ref, Scope: request.Scope, BaseRevision: request.BaseRevision, Actor: meta.Actor, Session: meta.Session, RequestID: meta.IdempotencyKey})
	if err != nil {
		return httpapi.WriteResult{}, mapBrowseError(err, "perspective scope")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}
func (a *Adapter) Comment(ctx context.Context, request httpapi.CommentRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	ids := make([]string, 0, len(request.Mentions))
	for _, m := range request.Mentions {
		ids = append(ids, m.Session)
	}
	result, err := a.home.Comment(ctx, application.CommentRequest{Ref: request.Ref, Body: request.Body, Intent: request.Intent, Actor: meta.Actor, Session: meta.Session, RequestID: meta.IdempotencyKey, MentionSessionIDs: ids})
	if err != nil {
		return httpapi.WriteResult{}, mapBrowseError(err, "comment")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: result.Revision, Persisted: true}, nil
}
