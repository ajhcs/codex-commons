package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"codex-commons/internal/httpapi"
)

func coreProjectPath(project string) string { return "/v1/projects/" + url.PathEscape(project) }
func coreTaskPath(task string) string       { return "/v1/tasks/" + url.PathEscape(task) }

func (c *Client) ProjectCoreDetail(ctx context.Context, project string) (httpapi.ProjectCoreDetailResult, error) {
	var out httpapi.ProjectCoreDetailResult
	err := c.get(ctx, coreProjectPath(project), nil, "", &out)
	return out, err
}

func (c *Client) ListProjectMilestones(ctx context.Context, query httpapi.ProjectMilestoneListQuery) (httpapi.ProjectMilestoneListResult, error) {
	values := url.Values{}
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	var out httpapi.ProjectMilestoneListResult
	err := c.get(ctx, coreProjectPath(query.Project)+"/milestones", values, "", &out)
	return out, err
}

func (c *Client) ListProjectTasks(ctx context.Context, query httpapi.ProjectTaskListQuery) (httpapi.ProjectTaskListResult, error) {
	values := url.Values{}
	setBrowsePage(values, query.Cursor, query.Limit)
	setIfPresent(values, "state", query.State)
	setIfPresent(values, "milestone", query.Milestone)
	var out httpapi.ProjectTaskListResult
	err := c.get(ctx, coreProjectPath(query.Project)+"/tasks", values, "", &out)
	return out, err
}

func (c *Client) OpenProjectTask(ctx context.Context, query httpapi.ProjectTaskOpenQuery) (httpapi.ProjectTaskOpenResult, error) {
	values := url.Values{}
	if query.EventsLimit > 0 {
		values.Set("events_limit", strconv.Itoa(query.EventsLimit))
	}
	var out httpapi.ProjectTaskOpenResult
	err := c.get(ctx, coreTaskPath(query.Task), values, "", &out)
	return out, err
}

func (c *Client) ListProjectTaskEvents(ctx context.Context, query httpapi.ProjectTaskEventListQuery) (httpapi.ProjectTaskEventListResult, error) {
	values := url.Values{}
	setBrowsePage(values, query.Cursor, query.Limit)
	var out httpapi.ProjectTaskEventListResult
	err := c.get(ctx, coreTaskPath(query.Task)+"/events", values, "", &out)
	return out, err
}

func (c *Client) ListProjectWiki(ctx context.Context, query httpapi.ProjectWikiListQuery) (httpapi.ProjectWikiListResult, error) {
	values := url.Values{}
	setBrowsePage(values, query.Cursor, query.Limit)
	setIfPresent(values, "q", query.Search)
	var out httpapi.ProjectWikiListResult
	err := c.get(ctx, coreProjectPath(query.Project)+"/wiki", values, "", &out)
	return out, err
}

func (c *Client) OpenProjectWiki(ctx context.Context, query httpapi.ProjectWikiOpenQuery) (httpapi.ProjectWikiOpenResult, error) {
	path := coreProjectPath(query.Project) + "/wiki/" + url.PathEscape(query.Slug)
	if query.Revision > 0 {
		path += "/revisions/" + strconv.FormatInt(query.Revision, 10)
	}
	var out httpapi.ProjectWikiOpenResult
	err := c.get(ctx, path, nil, "", &out)
	return out, err
}

func (c *Client) ListProjectWikiRevisions(ctx context.Context, query httpapi.ProjectWikiHistoryQuery) (httpapi.ProjectWikiHistoryResult, error) {
	values := url.Values{}
	setBrowsePage(values, query.Cursor, query.Limit)
	var out httpapi.ProjectWikiHistoryResult
	err := c.get(ctx, coreProjectPath(query.Project)+"/wiki/"+url.PathEscape(query.Slug)+"/revisions", values, "", &out)
	return out, err
}

func (c *Client) CreateCoreTask(ctx context.Context, project string, request httpapi.CreateCoreTaskRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, coreProjectPath(project)+"/tasks", request, key, &out)
	return out, err
}

func (c *Client) UpdateCoreTask(ctx context.Context, id string, request httpapi.UpdateCoreTaskRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.put(ctx, coreTaskPath(id), request, key, &out)
	return out, err
}

func (c *Client) ChangeCoreTaskState(ctx context.Context, id string, request httpapi.ChangeCoreTaskStateRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, coreTaskPath(id)+"/state", request, key, &out)
	return out, err
}

func (c *Client) put(ctx context.Context, path string, body any, key string, dst any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPut, path, nil, bytes.NewReader(encoded), "", key, dst)
}
