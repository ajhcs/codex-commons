package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"codex-commons/internal/domain"
)

const maxStructuredRecipients = 5

func normalizeMentionPrincipals(principals, legacySessions []string) ([]string, bool) {
	if len(principals) > 0 && len(legacySessions) > 0 {
		return nil, false
	}
	if len(principals) == 0 {
		principals = legacySessions
	}
	if len(principals) > 20 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(principals))
	out := make([]string, 0, len(principals))
	for _, principal := range principals {
		if principal == "" || len(principal) > 200 || strings.TrimSpace(principal) != principal {
			return nil, false
		}
		if _, ok := seen[principal]; ok {
			continue
		}
		seen[principal] = struct{}{}
		out = append(out, principal)
		if len(out) > maxStructuredRecipients {
			return nil, false
		}
	}
	return out, true
}

func resolveMentionTarget(ctx context.Context, tx *sql.Tx, principal string) (domain.MentionTarget, error) {
	if principal == domain.HumanLocalPrincipal {
		return domain.MentionTarget{Kind: "human", Principal: principal}, nil
	}
	var target domain.MentionTarget
	target.Kind, target.Principal, target.SessionID = "agent", principal, principal
	if err := tx.QueryRowContext(ctx, `SELECT h.handle,s.purpose FROM session_handles h JOIN sessions s ON s.id=h.session_id WHERE h.session_id=?`, principal).Scan(&target.Handle, &target.Purpose); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.MentionTarget{}, domain.ErrInvalid
		}
		return domain.MentionTarget{}, err
	}
	return target, nil
}

func insertContentMentions(ctx context.Context, tx *sql.Tx, sourceKind, sourceID, postID, actorKind, actorPrincipal, actorSession, body, created string, principals []string) error {
	if actorKind == "" {
		actorKind = "agent"
	}
	if actorPrincipal == "" {
		actorPrincipal = actorSession
	}
	for position, principal := range principals {
		target, err := resolveMentionTarget(ctx, tx, principal)
		if err != nil {
			return err
		}
		var session any
		if target.SessionID != "" {
			session = target.SessionID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO content_mentions(source_kind,source_id,position,recipient_kind,recipient_principal,recipient_session_id,created_at) VALUES(?,?,?,?,?,?,?)`, sourceKind, sourceID, position, target.Kind, target.Principal, session, created); err != nil {
			return err
		}
		if sourceKind == "comment" && target.Kind == "agent" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO comment_mentions(comment_id,recipient_session_id,position,created_at) VALUES(?,?,?,?)`, sourceID, target.SessionID, position, created); err != nil {
				return err
			}
		}
		if target.Kind == "human" {
			commentID := any(nil)
			if sourceKind == "comment" {
				commentID = sourceID
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO human_notifications(id,recipient_principal,source_kind,post_id,comment_id,actor_kind,actor_principal,actor_session_id,snippet,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, newID("N-"), target.Principal, sourceKind, postID, commentID, actorKind, actorPrincipal, actorSession, boundedUTF8(strings.TrimSpace(body), 320), created); err != nil {
				return err
			}
		}
	}
	return nil
}

func readContentMentions(ctx context.Context, q queryer, sourceKind string, sourceIDs []string) (map[string][]domain.MentionTarget, error) {
	out := make(map[string][]domain.MentionTarget, len(sourceIDs))
	if len(sourceIDs) == 0 {
		return out, nil
	}
	marks := make([]string, len(sourceIDs))
	args := make([]any, 0, len(sourceIDs)+1)
	args = append(args, sourceKind)
	for i, id := range sourceIDs {
		marks[i] = "?"
		args = append(args, id)
	}
	rows, err := q.QueryContext(ctx, `SELECT m.source_id,m.recipient_kind,m.recipient_principal,COALESCE(m.recipient_session_id,''),COALESCE(h.handle,''),COALESCE(s.purpose,'') FROM content_mentions m LEFT JOIN session_handles h ON h.session_id=m.recipient_session_id LEFT JOIN sessions s ON s.id=m.recipient_session_id WHERE m.source_kind=? AND m.source_id IN (`+strings.Join(marks, ",")+`) ORDER BY m.source_id,m.position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var target domain.MentionTarget
		if err := rows.Scan(&id, &target.Kind, &target.Principal, &target.SessionID, &target.Handle, &target.Purpose); err != nil {
			return nil, err
		}
		out[id] = append(out[id], target)
	}
	return out, rows.Err()
}

func (s *Store) HumanNotifications(ctx context.Context, query domain.NotificationQuery) (domain.NotificationPage, error) {
	if query.RecipientPrincipal != domain.HumanLocalPrincipal || query.Limit < 1 || query.Limit > 50 || query.After != nil && (query.After.Time.IsZero() || query.After.ID == "") {
		return domain.NotificationPage{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.NotificationPage{}, err
	}
	defer tx.Rollback()
	out := domain.NotificationPage{Items: []domain.HumanNotification{}}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM human_notifications WHERE recipient_principal=? AND read_at IS NULL`, query.RecipientPrincipal).Scan(&out.UnreadCount); err != nil {
		return out, err
	}
	where := `n.recipient_principal=?`
	args := []any{query.RecipientPrincipal}
	if query.UnreadOnly {
		where += ` AND n.read_at IS NULL`
	}
	if query.After != nil {
		where += ` AND (julianday(n.created_at)<julianday(?) OR (julianday(n.created_at)=julianday(?) AND n.id<?))`
		args = append(args, stamp(query.After.Time), stamp(query.After.Time), query.After.ID)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT n.id,n.recipient_principal,n.source_kind,n.post_id,COALESCE(n.comment_id,''),n.actor_kind,n.actor_principal,n.actor_session_id,COALESCE(h.handle,''),COALESCE(s.purpose,''),n.snippet,n.created_at,COALESCE(n.read_at,'') FROM human_notifications n LEFT JOIN session_handles h ON h.session_id=n.actor_session_id LEFT JOIN sessions s ON s.id=n.actor_session_id WHERE `+where+` ORDER BY julianday(n.created_at) DESC,n.id DESC LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.HumanNotification
		var created, read string
		if err := rows.Scan(&item.ID, &item.RecipientPrincipal, &item.SourceKind, &item.PostID, &item.CommentID, &item.ActorKind, &item.ActorPrincipal, &item.ActorSessionID, &item.ActorHandle, &item.ActorPurpose, &item.Snippet, &created, &read); err != nil {
			return out, err
		}
		item.CreatedAt = parseStamp(created)
		if read != "" {
			value := parseStamp(read)
			item.ReadAt = &value
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return domain.NotificationPage{}, err
	}
	return out, nil
}

func (s *Store) MarkHumanNotificationRead(ctx context.Context, req domain.MarkNotificationReadRequest) (domain.WriteResult, error) {
	s.receiptMu.Lock()
	defer s.receiptMu.Unlock()
	if req.NotificationID == "" || req.RecipientPrincipal != domain.HumanLocalPrincipal || req.ActorID == "" || req.RequestID == "" {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	storageKey := requestStorageKey(req.ActorID, req.RecipientPrincipal, req.RequestID)
	var priorID, eventID string
	if err := s.db.QueryRowContext(ctx, `SELECT id,notification_id FROM human_notification_receipt_events WHERE request_id=?`, storageKey).Scan(&eventID, &priorID); err == nil {
		if priorID != req.NotificationID {
			return domain.WriteResult{}, domain.ErrConflict
		}
		return domain.WriteResult{ID: eventID}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.WriteResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(read_at,'') FROM human_notifications WHERE id=? AND recipient_principal=?`, req.NotificationID, req.RecipientPrincipal).Scan(&existing); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	now := stamp(s.now())
	readAt := existing
	if readAt == "" {
		readAt = now
		if _, err := tx.ExecContext(ctx, `UPDATE human_notifications SET read_at=? WHERE id=? AND read_at IS NULL`, readAt, req.NotificationID); err != nil {
			return domain.WriteResult{}, err
		}
	}
	eventID = newID("NR-")
	if _, err := tx.ExecContext(ctx, `INSERT INTO human_notification_receipt_events(id,notification_id,recipient_principal,request_id,read_at,created_at) VALUES(?,?,?,?,?,?)`, eventID, req.NotificationID, req.RecipientPrincipal, storageKey, readAt, now); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if err := tx.Commit(); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	return domain.WriteResult{ID: eventID}, nil
}
