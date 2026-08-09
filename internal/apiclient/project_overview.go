package apiclient

import (
	"context"
	"net/url"
	"strconv"

	"codex-commons/internal/httpapi"
)

func (c *Client) ProjectOverview(ctx context.Context, query httpapi.ProjectOverviewQuery) (httpapi.ProjectOverviewResult, error) {
	values := url.Values{}
	if query.AttentionLimit > 0 {
		values.Set("attention_limit", strconv.Itoa(query.AttentionLimit))
	}
	if query.WorkLimit > 0 {
		values.Set("work_limit", strconv.Itoa(query.WorkLimit))
	}
	var out httpapi.ProjectOverviewResult
	err := c.get(ctx, "/v1/projects/"+url.PathEscape(query.Project)+"/overview", values, "", &out)
	return out, err
}
