package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"codex-commons/internal/domain"
)

const (
	maxHomeIdentifier = 200
	maxHomeTitle      = 200
	maxHomeAction     = 240
	maxHomeOutcome    = 120
	maxHomePage       = 20
	maxHomeOffset     = 10000
	maxHomeSessions   = 20
)

func boundedHomeValue(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func attentionUntrusted(kind string, requested bool) bool {
	return requested || kind == "forum_question" || strings.HasPrefix(kind, "github_")
}

func activityUntrusted(kind string, requested bool) bool {
	return requested || kind == "post_published" || kind == "comment_added" || strings.HasPrefix(kind, "github_")
}

// RecordAttention appends an explicit state transition. It never derives
// severity, ownership, or a next action from another record.
func (s *Store) RecordAttention(ctx context.Context, event domain.AttentionEvent) error {
	if !boundedHomeValue(event.EventID, maxHomeIdentifier) ||
		!boundedHomeValue(event.AttentionID, maxHomeIdentifier) ||
		(event.State != domain.AttentionOpen && event.State != domain.AttentionResolved) ||
		!domain.AttentionSeverities[event.Severity] ||
		!boundedHomeValue(event.Title, maxHomeTitle) ||
		!boundedHomeValue(event.SourceRef, maxHomeIdentifier) ||
		!boundedHomeValue(event.NextAction, maxHomeAction) ||
		!domain.AttentionSourceKinds[event.SourceKind] ||
		len(event.ProjectID) > maxHomeIdentifier ||
		len(event.AccountableSessionID) > maxHomeIdentifier {
		return domain.ErrInvalid
	}
	untrusted := attentionUntrusted(event.SourceKind, event.Untrusted)
	var prior domain.AttentionEvent
	var priorUntrusted int
	err := s.db.QueryRowContext(ctx, `SELECT event_id,attention_id,state,severity,title,COALESCE(project_id,''),source_ref,
COALESCE(accountable_session_id,''),next_action,source_kind,untrusted
FROM attention_events WHERE event_id=?`, event.EventID).Scan(
		&prior.EventID, &prior.AttentionID, &prior.State, &prior.Severity, &prior.Title,
		&prior.ProjectID, &prior.SourceRef, &prior.AccountableSessionID, &prior.NextAction,
		&prior.SourceKind, &priorUntrusted,
	)
	if err == nil {
		prior.Untrusted = priorUntrusted == 1
		if prior.EventID == event.EventID && prior.AttentionID == event.AttentionID &&
			prior.State == event.State && prior.Severity == event.Severity &&
			prior.Title == event.Title && prior.ProjectID == event.ProjectID &&
			prior.SourceRef == event.SourceRef &&
			prior.AccountableSessionID == event.AccountableSessionID &&
			prior.NextAction == event.NextAction && prior.SourceKind == event.SourceKind &&
			prior.Untrusted == untrusted {
			return nil
		}
		return domain.ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO attention_events(
event_id,attention_id,state,severity,title,project_id,source_ref,
accountable_session_id,next_action,source_kind,untrusted,recorded_at
) VALUES(?,?,?,?,?,NULLIF(?,''),?,NULLIF(?,''),?,?,?,?)`,
		event.EventID, event.AttentionID, event.State, event.Severity, event.Title,
		event.ProjectID, event.SourceRef, event.AccountableSessionID, event.NextAction,
		event.SourceKind, boolInt(untrusted), stamp(s.now()))
	return mapErr(err)
}

// RecordActivity accepts only the closed set of action-changing event kinds.
// Heartbeats and free-form chatter cannot be represented.
func (s *Store) RecordActivity(ctx context.Context, event domain.ActivityEvent) error {
	if !boundedHomeValue(event.ID, maxHomeIdentifier) ||
		!domain.ActivityKinds[event.Kind] ||
		!boundedHomeValue(event.ActorID, maxHomeIdentifier) ||
		!boundedHomeValue(event.ObjectRef, maxHomeIdentifier) ||
		!boundedHomeValue(event.ObjectTitle, maxHomeTitle) ||
		len(event.ProjectID) > maxHomeIdentifier ||
		len(event.Outcome) > maxHomeOutcome ||
		event.OccurredAt.IsZero() {
		return domain.ErrInvalid
	}
	occurred := event.OccurredAt.UTC()
	untrusted := activityUntrusted(event.Kind, event.Untrusted)
	var prior domain.ActivityEvent
	var priorUntrusted int
	var priorOccurred string
	err := s.db.QueryRowContext(ctx, `SELECT id,kind,COALESCE(project_id,''),actor_id,object_ref,
object_title,outcome,untrusted,occurred_at FROM activity_events WHERE id=?`, event.ID).Scan(
		&prior.ID, &prior.Kind, &prior.ProjectID, &prior.ActorID, &prior.ObjectRef,
		&prior.ObjectTitle, &prior.Outcome, &priorUntrusted, &priorOccurred,
	)
	if err == nil {
		prior.Untrusted = priorUntrusted == 1
		prior.OccurredAt = parseStamp(priorOccurred)
		if prior.ID == event.ID && prior.Kind == event.Kind &&
			prior.ProjectID == event.ProjectID && prior.ActorID == event.ActorID &&
			prior.ObjectRef == event.ObjectRef && prior.ObjectTitle == event.ObjectTitle &&
			prior.Outcome == event.Outcome && prior.Untrusted == untrusted &&
			prior.OccurredAt.Equal(occurred) {
			return nil
		}
		return domain.ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO activity_events(
id,kind,project_id,actor_id,object_ref,object_title,outcome,untrusted,occurred_at,recorded_at
) VALUES(?,?,NULLIF(?,''),?,?,?,?,?,?,?)`,
		event.ID, event.Kind, event.ProjectID, event.ActorID, event.ObjectRef,
		event.ObjectTitle, event.Outcome, boolInt(untrusted), stamp(occurred), stamp(s.now()))
	return mapErr(err)
}

func validateHomePage(page domain.HomePageRequest) bool {
	return page.Limit >= 1 && page.Limit <= maxHomePage && page.Offset >= 0 && page.Offset <= maxHomeOffset
}

// HomeSnapshot reads counts, pages, project names, and persisted session facts
// from one SQLite read transaction. No search index or denormalized forum body
// participates in this read model.
func (s *Store) HomeSnapshot(ctx context.Context, query domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	if !validateHomePage(query.Attention) || !validateHomePage(query.Activity) ||
		len(query.SessionIDs) > maxHomeSessions {
		return domain.HomeDurableSnapshot{}, domain.ErrInvalid
	}
	seen := make(map[string]bool, len(query.SessionIDs))
	for _, id := range query.SessionIDs {
		if !boundedHomeValue(id, maxHomeIdentifier) || seen[id] {
			return domain.HomeDurableSnapshot{}, domain.ErrInvalid
		}
		seen[id] = true
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.HomeDurableSnapshot{}, err
	}
	defer tx.Rollback()
	out := domain.HomeDurableSnapshot{Sessions: make(map[string]domain.SessionFact)}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects`).Scan(&out.ProjectsTotal); err != nil {
		return out, err
	}
	const latest = `WITH latest AS (
 SELECT attention_id,max(sequence) AS sequence FROM attention_events GROUP BY attention_id
)`
	if err := tx.QueryRowContext(ctx, latest+`
SELECT count(*) FROM attention_events e JOIN latest l ON l.sequence=e.sequence WHERE e.state='open'`).Scan(&out.AttentionTotal); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, latest+`
SELECT e.attention_id,e.severity,e.title,COALESCE(e.project_id,''),COALESCE(p.name,''),
e.source_ref,COALESCE(e.accountable_session_id,''),e.next_action,e.source_kind,e.untrusted,e.recorded_at
FROM attention_events e
JOIN latest l ON l.sequence=e.sequence
LEFT JOIN projects p ON p.id=e.project_id
WHERE e.state='open'
ORDER BY julianday(e.recorded_at) DESC,e.attention_id
LIMIT ? OFFSET ?`, query.Attention.Limit, query.Attention.Offset)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.HomeAttention
		var untrusted int
		var updated string
		if err := rows.Scan(&item.ID, &item.Severity, &item.Title, &item.ProjectID,
			&item.ProjectName, &item.SourceRef, &item.AccountableSessionID,
			&item.NextAction, &item.SourceKind, &untrusted, &updated); err != nil {
			rows.Close()
			return out, err
		}
		item.Untrusted = untrusted == 1
		item.UpdatedAt = parseStamp(updated)
		out.Attention = append(out.Attention, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}

	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM activity_events`).Scan(&out.ActivityTotal); err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT a.id,a.kind,COALESCE(a.project_id,''),COALESCE(p.name,''),
a.actor_id,a.object_ref,a.object_title,a.outcome,a.untrusted,a.occurred_at
FROM activity_events a
LEFT JOIN projects p ON p.id=a.project_id
ORDER BY a.occurred_at DESC,a.id
LIMIT ? OFFSET ?`, query.Activity.Limit, query.Activity.Offset)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.HomeActivity
		var untrusted int
		var occurred string
		if err := rows.Scan(&item.ID, &item.Kind, &item.ProjectID, &item.ProjectName,
			&item.ActorID, &item.ObjectRef, &item.ObjectTitle, &item.Outcome,
			&untrusted, &occurred); err != nil {
			rows.Close()
			return out, err
		}
		item.Untrusted = untrusted == 1
		item.OccurredAt = parseStamp(occurred)
		out.Activity = append(out.Activity, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}

	for _, id := range query.SessionIDs {
		var fact domain.SessionFact
		err := tx.QueryRowContext(ctx, `SELECT s.id,s.host,COALESCE(s.project_id,''),
COALESCE(p.name,''),s.purpose FROM sessions s LEFT JOIN projects p ON p.id=s.project_id WHERE s.id=?`, id).Scan(
			&fact.ID, &fact.Host, &fact.ProjectID, &fact.ProjectName, &fact.Purpose)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return out, err
		}
		out.Sessions[id] = fact
	}
	if err := tx.Commit(); err != nil {
		return domain.HomeDurableSnapshot{}, err
	}
	return out, nil
}
