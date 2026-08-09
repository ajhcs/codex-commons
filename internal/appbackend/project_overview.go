package appbackend

import (
	"context"

	"codex-commons/internal/httpapi"
)

func (a *Adapter) ProjectOverview(ctx context.Context, query httpapi.ProjectOverviewQuery, meta httpapi.RequestMeta) (httpapi.ProjectOverviewResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.ProjectOverviewResult{}, err
	}
	out, err := a.home.ProjectOverview(ctx, query)
	return out, mapBrowseError(err, "project overview")
}
