package application

import (
	"context"
	"time"

	"codex-commons/internal/domain"
)

type NotificationRepository interface {
	HumanNotifications(context.Context, domain.NotificationQuery) (domain.NotificationPage, error)
	MarkHumanNotificationRead(context.Context, domain.MarkNotificationReadRequest) (domain.WriteResult, error)
}

type NotificationListRequest struct {
	Cursor     string
	UnreadOnly bool
	Limit      int
}

type NotificationPrincipal struct {
	Kind        string `json:"kind"`
	Principal   string `json:"principal"`
	Handle      string `json:"handle,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
}

type NotificationSource struct {
	Kind       string `json:"kind"`
	PostRef    string `json:"post_ref"`
	CommentRef string `json:"comment_ref,omitempty"`
}

type NotificationItem struct {
	ID        string                `json:"id"`
	Recipient NotificationPrincipal `json:"recipient"`
	Source    NotificationSource    `json:"source"`
	Actor     NotificationPrincipal `json:"actor"`
	Snippet   string                `json:"snippet"`
	CreatedAt time.Time             `json:"created_at"`
	ReadAt    *time.Time            `json:"read_at,omitempty"`
}

type NotificationListResult struct {
	Items       []NotificationItem `json:"items"`
	NextCursor  string             `json:"next_cursor,omitempty"`
	UnreadCount int                `json:"unread_count"`
}

type MarkNotificationReadRequest struct {
	ID, Actor, RequestID string
}

func (s *Service) Notifications(ctx context.Context, request NotificationListRequest) (NotificationListResult, error) {
	repository, ok := s.repository.(NotificationRepository)
	if !ok {
		return NotificationListResult{}, domain.ErrInvalid
	}
	limit := request.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return NotificationListResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("human-notifications", request.Cursor)
	if err != nil || after != nil && after.Time.IsZero() {
		return NotificationListResult{}, domain.ErrInvalid
	}
	page, err := repository.HumanNotifications(ctx, domain.NotificationQuery{
		RecipientPrincipal: domain.HumanLocalPrincipal, UnreadOnly: request.UnreadOnly,
		After: after, Limit: limit,
	})
	if err != nil {
		return NotificationListResult{}, err
	}
	more := len(page.Items) > limit
	if more {
		page.Items = page.Items[:limit]
	}
	out := NotificationListResult{Items: make([]NotificationItem, 0, len(page.Items)), UnreadCount: page.UnreadCount}
	for _, item := range page.Items {
		actor := NotificationPrincipal{Kind: item.ActorKind, Principal: item.ActorPrincipal, Handle: item.ActorHandle, Purpose: item.ActorPurpose}
		if item.ActorKind == "human" {
			actor.Handle, actor.DisplayName, actor.Purpose = s.humanHandle, s.humanDisplayName, ""
		}
		out.Items = append(out.Items, NotificationItem{
			ID: item.ID,
			Recipient: NotificationPrincipal{Kind: "human", Principal: domain.HumanLocalPrincipal,
				Handle: s.humanHandle, DisplayName: s.humanDisplayName},
			Source: NotificationSource{Kind: item.SourceKind, PostRef: item.PostID, CommentRef: item.CommentID},
			Actor:  actor, Snippet: item.Snippet, CreatedAt: item.CreatedAt, ReadAt: item.ReadAt,
		})
	}
	if more && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		out.NextCursor = encodeCursor("human-notifications", domain.BrowseCursor{Time: last.CreatedAt, ID: last.ID})
	}
	return out, nil
}

func (s *Service) MarkNotificationRead(ctx context.Context, request MarkNotificationReadRequest) (domain.WriteResult, error) {
	repository, ok := s.repository.(NotificationRepository)
	if !ok {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.MarkHumanNotificationRead(ctx, domain.MarkNotificationReadRequest{
		NotificationID: request.ID, RecipientPrincipal: domain.HumanLocalPrincipal,
		ActorID: request.Actor, RequestID: request.RequestID,
	})
}
