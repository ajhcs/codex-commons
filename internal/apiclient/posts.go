package apiclient

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"codex-commons/internal/httpapi"
)

func (c *Client) BrowsePosts(ctx context.Context, q httpapi.PostFeedQuery) (httpapi.PostFeedResult, error) {
	values := url.Values{}
	setBrowsePage(values, q.Cursor, q.Limit)
	setIfPresent(values, "q", q.Search)
	setIfPresent(values, "topic", q.Topic)
	setIfPresent(values, "project", q.Project)
	setIfPresent(values, "kind", q.Kind)
	if q.CreatedFrom != nil {
		values.Set("created_from", q.CreatedFrom.UTC().Format(time.RFC3339Nano))
	}
	if q.CreatedTo != nil {
		values.Set("created_to", q.CreatedTo.UTC().Format(time.RFC3339Nano))
	}
	var out httpapi.PostFeedResult
	err := c.get(ctx, "/v1/posts", values, "", &out)
	return out, err
}

func (c *Client) OpenComment(ctx context.Context, id string) (httpapi.CommentOpenResult, error) {
	var out httpapi.CommentOpenResult
	err := c.get(ctx, "/v1/comments/"+url.PathEscape(id), nil, "", &out)
	return out, err
}

func (c *Client) OpenPost(ctx context.Context, q httpapi.PostOpenQuery) (httpapi.PostOpenResult, error) {
	values := url.Values{}
	setIfPresent(values, "comments_cursor", q.CommentsCursor)
	if q.CommentsLimit > 0 {
		values.Set("comments_limit", strconv.Itoa(q.CommentsLimit))
	}
	var out httpapi.PostOpenResult
	err := c.get(ctx, "/v1/posts/"+url.PathEscape(q.Ref), values, "", &out)
	return out, err
}

func (c *Client) SetPostState(ctx context.Context, in httpapi.PostStateWriteRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, "/v1/post-states", in, key, &out)
	return out, err
}

func (c *Client) LookupContributors(ctx context.Context, q httpapi.ContributorLookupQuery) (httpapi.ContributorLookupResult, error) {
	values := url.Values{}
	setBrowsePage(values, q.Cursor, q.Limit)
	setIfPresent(values, "q", q.Search)
	setIfPresent(values, "project", q.Project)
	var out httpapi.ContributorLookupResult
	err := c.get(ctx, "/v1/contributors", values, "", &out)
	return out, err
}
func (c *Client) SetPerspectiveScope(ctx context.Context, in httpapi.PerspectiveScopeWriteRequest, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, "/v1/post-perspective-scopes", in, key, &out)
	return out, err
}
