package appbackend

import (
	"context"

	"codex-commons/internal/httpapi"
)

func (a *Adapter) BrowseTopics(ctx context.Context, query httpapi.TopicsQuery, meta httpapi.RequestMeta) (httpapi.TopicsResult, error) {
	if err := validateBrowseIdentity(meta); err != nil {
		return httpapi.TopicsResult{}, err
	}
	out, err := a.home.BrowseTopics(ctx, query)
	return out, mapBrowseError(err, "topics")
}
