package appbackend

import (
	"context"

	"codex-commons/internal/application"
	"codex-commons/internal/httpapi"
)

func (a *Adapter) Notifications(ctx context.Context, query httpapi.NotificationListQuery, meta httpapi.RequestMeta) (httpapi.NotificationListResult, error) {
	if meta.PrincipalKind != "human" || meta.Principal != "human:local-admin" {
		return httpapi.NotificationListResult{}, httpapi.NewError(httpapi.CodeForbidden, "only the authenticated local human may read notifications")
	}
	out, err := a.home.Notifications(ctx, query)
	return out, mapBrowseError(err, "notifications")
}

func (a *Adapter) MarkNotificationRead(ctx context.Context, request httpapi.NotificationReadRequest, meta httpapi.RequestMeta) (httpapi.WriteResult, error) {
	if meta.PrincipalKind != "human" || meta.Principal != "human:local-admin" {
		return httpapi.WriteResult{}, httpapi.NewError(httpapi.CodeForbidden, "only the authenticated local human may update notification receipts")
	}
	result, err := a.home.MarkNotificationRead(ctx, application.MarkNotificationReadRequest{
		ID: request.ID, Actor: meta.Actor, RequestID: meta.IdempotencyKey,
	})
	if err != nil {
		return httpapi.WriteResult{}, mapBrowseError(err, "notification receipt")
	}
	return httpapi.WriteResult{ID: result.ID, Revision: 0, Persisted: true}, nil
}
