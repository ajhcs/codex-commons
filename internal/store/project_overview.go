package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"codex-commons/internal/domain"
)

const maxProjectOverviewLimit = 20

func validProjectOverviewQuery(query domain.ProjectOverviewReadQuery) bool {
	return boundedHomeValue(query.ProjectID, maxHomeIdentifier) &&
		query.AttentionLimit >= 1 && query.AttentionLimit <= maxProjectOverviewLimit &&
		query.WorkLimit >= 1 && query.WorkLimit <= maxProjectOverviewLimit &&
		!query.ActivityStart.IsZero() && !query.ActivityEnd.IsZero() &&
		query.ActivityStart.Location() == time.UTC && query.ActivityEnd.Location() == time.UTC &&
		query.ActivityStart.Before(query.ActivityEnd) &&
		query.ActivityEnd.Sub(query.ActivityStart) == 14*24*time.Hour &&
		validPeopleSessionIDs(query.SessionIDs)
}

// ProjectOverviewSnapshot reads all durable overview fields in one SQLite read
// transaction. It reuses the canonical attention/activity ledgers and performs
// no GitHub network call.
func (s *Store) ProjectOverviewSnapshot(ctx context.Context, query domain.ProjectOverviewReadQuery) (domain.ProjectOverviewDurableSnapshot, error) {
	if !validProjectOverviewQuery(query) {
		return domain.ProjectOverviewDurableSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.ProjectOverviewDurableSnapshot{}, err
	}
	defer tx.Rollback()
	out := domain.ProjectOverviewDurableSnapshot{Sessions: make(map[string]domain.PeopleSessionFact, len(query.SessionIDs))}
	if err := tx.QueryRowContext(ctx, `SELECT id,name,status,purpose,milestone,now_text,revision
FROM projects WHERE id=?`, query.ProjectID).Scan(
		&out.Project.ID, &out.Project.Name, &out.Project.Status, &out.Project.Purpose,
		&out.Project.Milestone, &out.Project.Now, &out.Project.Revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, domain.ErrNotFound
		}
		return out, err
	}

	const latestAttention = `WITH latest AS (
 SELECT attention_id,max(sequence) AS sequence FROM attention_events GROUP BY attention_id
)`
	if err := tx.QueryRowContext(ctx, latestAttention+`
SELECT count(*),COALESCE(sum(CASE WHEN e.severity='high' THEN 1 ELSE 0 END),0)
FROM attention_events e JOIN latest l ON l.sequence=e.sequence
WHERE e.state='open' AND e.project_id=?`, query.ProjectID).Scan(&out.AttentionTotal, &out.AttentionHigh); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, latestAttention+`
SELECT e.attention_id,e.severity,e.title,e.project_id,p.name,e.source_ref,
COALESCE(e.accountable_session_id,''),e.next_action,e.source_kind,e.untrusted,e.recorded_at,
CASE
 WHEN e.source_kind='task' AND EXISTS(SELECT 1 FROM tasks t WHERE t.id=e.source_ref) THEN 'task'
 WHEN e.source_kind='forum_question' AND EXISTS(SELECT 1 FROM posts x WHERE x.id=e.source_ref) THEN 'post'
 ELSE '' END
FROM attention_events e
JOIN latest l ON l.sequence=e.sequence
JOIN projects p ON p.id=e.project_id
WHERE e.state='open' AND e.project_id=?
ORDER BY julianday(e.recorded_at) DESC,e.attention_id
LIMIT ?`, query.ProjectID, query.AttentionLimit)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.HomeAttention
		var untrusted int
		var updated, destinationKind string
		if err := rows.Scan(&item.ID, &item.Severity, &item.Title, &item.ProjectID,
			&item.ProjectName, &item.SourceRef, &item.AccountableSessionID,
			&item.NextAction, &item.SourceKind, &untrusted, &updated, &destinationKind); err != nil {
			rows.Close()
			return out, err
		}
		item.Untrusted = untrusted == 1
		item.UpdatedAt = parseStamp(updated)
		if destinationKind != "" {
			item.Destination = &domain.BrowseDestination{Kind: destinationKind, Ref: item.SourceRef}
		}
		out.Attention = append(out.Attention, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}

	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM active_tasks
WHERE project_id=? AND state IN ('ready','in_progress','blocked')`, query.ProjectID).Scan(&out.OpenWorkTotal); err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT t.id,t.title,t.state,t.priority,COALESCE(t.owner_session_id,''),
(SELECT a.occurred_at FROM activity_events a
 WHERE a.project_id=t.project_id AND a.object_ref=t.id
 AND a.kind IN ('task_claimed','task_status_changed')
 ORDER BY julianday(a.occurred_at) DESC,a.id DESC LIMIT 1) AS updated_at
FROM active_tasks t
WHERE t.project_id=? AND t.state IN ('ready','in_progress','blocked')
ORDER BY CASE t.state WHEN 'in_progress' THEN 0 WHEN 'blocked' THEN 1 ELSE 2 END,
t.priority,t.id
LIMIT ?`, query.ProjectID, query.WorkLimit)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.ProjectOverviewWork
		var updated sql.NullString
		if err := rows.Scan(&item.ID, &item.Title, &item.State, &item.Priority,
			&item.OwnerSessionID, &updated); err != nil {
			rows.Close()
			return out, err
		}
		if updated.Valid {
			value := parseStamp(updated.String)
			item.UpdatedAt = &value
		}
		out.CurrentWork = append(out.CurrentWork, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}

	out.Activity, err = readProjectActivityDays(ctx, tx, query.ProjectID, query.ActivityStart, query.ActivityEnd)
	if err != nil {
		return out, err
	}

	var last sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT occurred_at FROM activity_events
WHERE project_id=? ORDER BY julianday(occurred_at) DESC,id DESC LIMIT 1`, query.ProjectID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil && last.Valid && strings.TrimSpace(last.String) != "" {
		value := parseStamp(last.String)
		out.LastActionChangingActivity = &value
	}
	// No canonical persisted GitHub snapshot exists yet. Nil is an explicit
	// unavailable metric, not a guessed zero.
	out.MergedPullRequests = nil
	out.Sessions, err = readPeopleSessionFacts(ctx, tx, query.SessionIDs)
	if err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ProjectOverviewDurableSnapshot{}, err
	}
	return out, nil
}
