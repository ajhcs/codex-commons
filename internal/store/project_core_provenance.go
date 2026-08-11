package store

import (
	"context"
	"database/sql"
	"errors"

	"codex-commons/internal/domain"
)

const maxTaskContributors = 20

func loadHistoricalTaskProjection(ctx context.Context, tx *sql.Tx, task *domain.CanonicalTask) error {
	if task == nil || task.ID == "" {
		return domain.ErrInvalid
	}
	var value domain.HistoricalTaskImport
	var completed, recorded, sourceKind, sourceID, sourceDigest string
	err := tx.QueryRowContext(ctx, `SELECT batch.batch_id,imported.source_key,
(SELECT event.state FROM historical_import_batch_events event
 WHERE event.batch_record_id=batch.id ORDER BY event.sequence DESC LIMIT 1),
imported.source_kind,imported.source_stable_id,imported.source_digest,
imported.source_occurred_at,batch.recorded_at
FROM historical_import_tasks imported
JOIN historical_import_batches batch ON batch.id=imported.batch_record_id
WHERE imported.canonical_task_id=? AND imported.disposition='created'`, task.ID).Scan(
		&value.BatchID, &value.SourceKey, &value.State, &sourceKind, &sourceID, &sourceDigest, &completed, &recorded,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	value.SourceCompletedAt = parseStamp(completed)
	value.RecordedAt = parseStamp(recorded)
	value.Source = &domain.HistoricalSource{
		Kind: sourceKind, StableID: sourceID, Digest: sourceDigest, OccurredAt: value.SourceCompletedAt,
	}
	task.HistoricalImport = &value
	return nil
}

func loadTaskContributors(ctx context.Context, tx *sql.Tx, task *domain.CanonicalTask) error {
	if task == nil || task.ID == "" {
		return domain.ErrInvalid
	}
	rows, err := tx.QueryContext(ctx, `WITH contributors AS (
 SELECT 'attested' AS provenance_kind,events.actor_id,events.session_id,COALESCE(sessions.purpose,'') AS purpose,
        '' AS role,'' AS confidence,'' AS recorded_at,'' AS source_kind,'' AS source_stable_id,
        '' AS source_digest,'' AS source_occurred_at,'' AS recorded_by_actor,'' AS recorded_by_session
 FROM task_events events
 LEFT JOIN sessions ON sessions.id=events.session_id
 WHERE events.task_id=? AND events.session_id<>'' AND events.kind<>'imported'
 GROUP BY events.actor_id,events.session_id,sessions.purpose
 UNION ALL
 SELECT 'historical','',attribution.session_id,'',attribution.role,attribution.confidence,
        attribution.recorded_at,attribution.source_kind,attribution.source_stable_id,
        attribution.source_digest,attribution.source_occurred_at,
        attribution.recorded_by_actor,attribution.recorded_by_session
 FROM historical_task_attributions attribution
 JOIN historical_import_tasks imported ON imported.id=attribution.import_task_id
 WHERE attribution.task_id=?
   AND NOT EXISTS (
     SELECT 1 FROM historical_project_thread_aliases alias
     WHERE alias.batch_record_id=imported.batch_record_id AND alias.session_id=attribution.session_id
   )
)
SELECT provenance_kind,actor_id,session_id,purpose,role,confidence,recorded_at,
       source_kind,source_stable_id,source_digest,source_occurred_at,
       recorded_by_actor,recorded_by_session
FROM contributors
ORDER BY provenance_kind,session_id,role,source_kind,source_stable_id
LIMIT ?`, task.ID, task.ID, maxTaskContributors+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	task.Contributors = []domain.Provenance{}
	for rows.Next() {
		var item domain.Provenance
		var recorded, sourceOccurred, sourceKind, sourceID, sourceDigest, recorderActor, recorderSession string
		if err = rows.Scan(&item.Kind, &item.ActorID, &item.SessionID, &item.Purpose, &item.Role, &item.Confidence,
			&recorded, &sourceKind, &sourceID, &sourceDigest, &sourceOccurred, &recorderActor, &recorderSession); err != nil {
			return err
		}
		if len(task.Contributors) == maxTaskContributors {
			task.ContributorsTruncated = true
			continue
		}
		if recorded != "" {
			item.RecordedAt = parseStamp(recorded)
		}
		if sourceKind != "" {
			item.Source = &domain.HistoricalSource{
				Kind: sourceKind, StableID: sourceID, Digest: sourceDigest, OccurredAt: parseStamp(sourceOccurred),
			}
		}
		if recorderActor != "" || recorderSession != "" {
			item.RecordedBy = &domain.RecordedPrincipal{ActorID: recorderActor, SessionID: recorderSession}
		}
		task.Contributors = append(task.Contributors, item)
	}
	return rows.Err()
}
