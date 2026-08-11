package apiclient

import (
	"context"
	"net/url"
	"strconv"

	"codex-commons/internal/httpapi"
)

func (c *Client) Notifications(ctx context.Context, q httpapi.NotificationListQuery) (httpapi.NotificationListResult, error) {
	values := url.Values{}
	setIfPresent(values, "cursor", q.Cursor)
	if q.Limit > 0 {
		values.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.UnreadOnly {
		values.Set("unread", "true")
	}
	var out httpapi.NotificationListResult
	err := c.get(ctx, "/v1/notifications", values, "", &out)
	return out, err
}

func (c *Client) MarkNotificationRead(ctx context.Context, id, key string) (httpapi.WriteResult, error) {
	var out httpapi.WriteResult
	err := c.post(ctx, "/v1/notification-reads", httpapi.NotificationReadRequest{ID: id}, key, &out)
	return out, err
}
