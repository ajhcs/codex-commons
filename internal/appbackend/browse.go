package appbackend

import (
	"context"
	"errors"

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
