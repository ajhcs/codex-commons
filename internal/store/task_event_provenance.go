package store

import (
	"context"
	"database/sql"

	"codex-commons/internal/domain"
)

func readTaskEventsWithProvenance(ctx context.Context, tx *sql.Tx, query domain.TaskEventListQuery) ([]domain.TaskEvent, error) {
	cursor := ""
	args := []any{query.TaskID, query.TaskID}
	if query.After != nil {
		cursor = ` AND (julianday(event_at)<julianday(?) OR (event_at=? AND id<?))`
		args = append(args, stamp(query.After.Time), stamp(query.After.Time), query.After.ID)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `WITH combined AS (
 SELECT events.id,events.task_id,events.project_id,events.kind,events.summary,
        COALESCE(events.from_state,'') AS from_state,COALESCE(events.to_state,'') AS to_state,events.actor_id,events.session_id,
        COALESCE(sessions.purpose,'') AS purpose,events.project_revision,events.created_at AS event_at,
        'attested' AS provenance_kind,'' AS confidence,'' AS source_kind,'' AS source_stable_id,
        '' AS source_digest,'' AS source_occurred_at,'' AS recorded_by_actor,
        '' AS recorded_by_session,'' AS recorded_at
 FROM task_events events
 LEFT JOIN sessions ON sessions.id=events.session_id
 WHERE events.task_id=?
 UNION ALL
 SELECT events.id,events.task_id,tasks.project_id,events.kind,events.summary,
        '','', '',events.source_session_id,'',tasks.project_revision,events.occurred_at,
        'historical',events.confidence,events.source_kind,events.source_stable_id,
        events.source_digest,events.occurred_at,events.recorded_by_actor,
        events.recorded_by_session,events.recorded_at
 FROM historical_task_events events
 JOIN tasks ON tasks.id=events.task_id
 JOIN historical_import_tasks imported ON imported.id=events.import_task_id
 WHERE events.task_id=?
   AND NOT EXISTS (
     SELECT 1 FROM historical_project_thread_aliases alias
     WHERE alias.batch_record_id=imported.batch_record_id AND alias.session_id=events.source_session_id
   )
)
SELECT id,task_id,project_id,kind,summary,from_state,to_state,actor_id,session_id,purpose,
       project_revision,event_at,provenance_kind,confidence,source_kind,source_stable_id,
       source_digest,source_occurred_at,recorded_by_actor,recorded_by_session,recorded_at
FROM combined WHERE 1=1`+cursor+`
ORDER BY julianday(event_at) DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TaskEvent{}
	for rows.Next() {
		var item domain.TaskEvent
		var occurred, provenanceKind, confidence, sourceKind, sourceID, sourceDigest, sourceOccurred string
		var recordedActor, recordedSession, recordedAt string
		if err = rows.Scan(&item.ID, &item.TaskID, &item.ProjectID, &item.Kind, &item.Summary, &item.FromState,
			&item.ToState, &item.ActorID, &item.SessionID, &item.Purpose, &item.Revision, &occurred,
			&provenanceKind, &confidence, &sourceKind, &sourceID, &sourceDigest, &sourceOccurred,
			&recordedActor, &recordedSession, &recordedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = optionalCoreStamp(sql.NullString{String: occurred, Valid: occurred != ""})
		item.Provenance = domain.Provenance{
			Kind: provenanceKind, ActorID: item.ActorID, SessionID: item.SessionID,
			Purpose: item.Purpose, Confidence: confidence,
		}
		if sourceKind != "" {
			item.Provenance.Source = &domain.HistoricalSource{
				Kind: sourceKind, StableID: sourceID, Digest: sourceDigest, OccurredAt: parseStamp(sourceOccurred),
			}
		}
		if recordedAt != "" {
			item.Provenance.RecordedAt = parseStamp(recordedAt)
		}
		if recordedActor != "" || recordedSession != "" {
			item.Provenance.RecordedBy = &domain.RecordedPrincipal{ActorID: recordedActor, SessionID: recordedSession}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
