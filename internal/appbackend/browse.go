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
