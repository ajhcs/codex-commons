package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"codex-commons/internal/domain"
)

const (
	maxHistoricalAliases      = 20
	maxHistoricalTasks        = 25
	maxHistoricalAttributions = 200
	maxHistoricalEvents       = 500
	maxHistoricalTaskEvents   = 25
	maxHistoricalSummaryBytes = 1000
	maxHistoricalBatchIDBytes = 64
	maxHistoricalKeyBytes     = 64
	maxHistoricalSourceKind   = 40
	maxHistoricalStableID     = 300
	maxHistoricalReasonBytes  = 1000
)

var (
	historicalKeyPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	historicalDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type historicalQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validHistoricalSource(source domain.HistoricalSource, now time.Time) bool {
	return boundedCoreText(source.Kind, maxHistoricalSourceKind, true) &&
		boundedCoreText(source.StableID, maxHistoricalStableID, true) &&
		historicalDigestPattern.MatchString(source.Digest) &&
		!source.OccurredAt.IsZero() && !source.OccurredAt.After(now.Add(5*time.Minute))
}

func normalizeHistoricalImport(command domain.HistoricalImportCommand, now time.Time) (domain.HistoricalImportCommand, string, domain.HistoricalImportCounts, error) {
	if command.SchemaVersion != domain.HistoricalImportSchemaVersion ||
		!boundedCoreText(command.ProjectID, maxCoreIDBytes, true) ||
		!historicalKeyPattern.MatchString(command.BatchID) || len(command.BatchID) > maxHistoricalBatchIDBytes ||
		!historicalDigestPattern.MatchString(command.SourceDigest) ||
		command.CollisionPolicy != domain.HistoricalCollisionCurrentWins ||
		len(command.Tasks) < 1 || len(command.Tasks) > maxHistoricalTasks {
		return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, domain.ErrInvalid
	}
	out := command
	out.ConfirmSourceDigest = ""
	out.Meta = domain.CoreWriteMeta{}
	out.ProjectThreadAliases = append([]domain.HistoricalProjectThreadAliasInput(nil), command.ProjectThreadAliases...)
	if len(out.ProjectThreadAliases) > maxHistoricalAliases {
		return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, domain.ErrInvalid
	}
	aliasSessions := make(map[string]bool, len(out.ProjectThreadAliases))
	seenAliases := make(map[string]bool, len(out.ProjectThreadAliases))
	for i := range out.ProjectThreadAliases {
		item := &out.ProjectThreadAliases[i]
		if !historicalKeyPattern.MatchString(item.Alias) || seenAliases[item.Alias] ||
			!boundedCoreText(item.SessionID, maxCoreIDBytes, true) || aliasSessions[item.SessionID] ||
			!validHistoricalSource(item.Source, now) {
			return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, domain.ErrInvalid
		}
		seenAliases[item.Alias] = true
		aliasSessions[item.SessionID] = true
		item.Source.OccurredAt = item.Source.OccurredAt.UTC()
	}
	sort.Slice(out.ProjectThreadAliases, func(i, j int) bool {
		return out.ProjectThreadAliases[i].Alias < out.ProjectThreadAliases[j].Alias
	})
	out.Tasks = append([]domain.HistoricalTaskInput(nil), command.Tasks...)
	counts := domain.HistoricalImportCounts{ProjectThreadAliases: len(out.ProjectThreadAliases), Tasks: len(out.Tasks)}
	seenTasks := make(map[string]bool, len(out.Tasks))
	for i := range out.Tasks {
		task := &out.Tasks[i]
		if !historicalKeyPattern.MatchString(task.Key) || len(task.Key) > maxHistoricalKeyBytes || seenTasks[task.Key] ||
			!boundedCoreText(task.Title, maxTaskTitleBytes, true) ||
			!boundedCoreText(task.Description, maxTaskDescriptionBytes, false) ||
			!boundedCoreText(task.Acceptance, maxTaskAcceptanceBytes, false) ||
			task.State != "done" || task.Priority < -1000 || task.Priority > 1000 ||
			!validHistoricalSource(task.Source, now) ||
			len(task.Attributions) < 1 || len(task.Attributions) > 20 ||
			len(task.Events) > maxHistoricalTaskEvents {
			return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, domain.ErrInvalid
		}
		seenTasks[task.Key] = true
		task.Source.OccurredAt = task.Source.OccurredAt.UTC()
		task.Attributions = append([]domain.HistoricalAttributionInput(nil), task.Attributions...)
		task.Events = append([]domain.HistoricalEventInput(nil), task.Events...)
		seenAttributions := make(map[string]bool, len(task.Attributions))
		attributedSessions := make(map[string]bool, len(task.Attributions))
		for j := range task.Attributions {
			item := &task.Attributions[j]
			key := strings.Join([]string{item.SessionID, item.Role}, "\x00")
			if !boundedCoreText(item.SessionID, maxCoreIDBytes, true) || aliasSessions[item.SessionID] || !domain.HistoricalRoles[item.Role] ||
				!domain.HistoricalConfidences[item.Confidence] || !validHistoricalSource(item.Source, now) ||
				seenAttributions[key] {
				return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, domain.ErrInvalid
			}
			seenAttributions[key] = true
			attributedSessions[item.SessionID] = true
			item.Source.OccurredAt = item.Source.OccurredAt.UTC()
			counts.Attributions++
		}
		if counts.Attributions > maxHistoricalAttributions {
			return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, domain.ErrInvalid
		}
		sort.Slice(task.Attributions, func(a, b int) bool {
			left, right := task.Attributions[a], task.Attributions[b]
			return strings.Join([]string{left.SessionID, left.Role, left.Source.Kind, left.Source.StableID, left.Source.Digest}, "\x00") <
				strings.Join([]string{right.SessionID, right.Role, right.Source.Kind, right.Source.StableID, right.Source.Digest}, "\x00")
		})
		seenEvents := make(map[string]bool, len(task.Events))
		for j := range task.Events {
			item := &task.Events[j]
			if !historicalKeyPattern.MatchString(item.Key) || len(item.Key) > maxHistoricalKeyBytes || seenEvents[item.Key] ||
				!domain.HistoricalEventKinds[item.Kind] ||
				!boundedCoreText(item.Summary, maxHistoricalSummaryBytes, true) ||
				!boundedCoreText(item.SessionID, maxCoreIDBytes, false) ||
				(item.SessionID != "" && aliasSessions[item.SessionID]) ||
				(item.SessionID != "" && !attributedSessions[item.SessionID]) ||
				!domain.HistoricalConfidences[item.Confidence] ||
				!validHistoricalSource(item.Source, now) {
				return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, domain.ErrInvalid
			}
			seenEvents[item.Key] = true
			item.Source.OccurredAt = item.Source.OccurredAt.UTC()
			counts.Events++
		}
		if counts.Events > maxHistoricalEvents {
			return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, domain.ErrInvalid
		}
		sort.Slice(task.Events, func(a, b int) bool { return task.Events[a].Key < task.Events[b].Key })
	}
	sort.Slice(out.Tasks, func(i, j int) bool { return out.Tasks[i].Key < out.Tasks[j].Key })
	payload, err := json.Marshal(out)
	if err != nil {
		return domain.HistoricalImportCommand{}, "", domain.HistoricalImportCounts{}, err
	}
	digest := sha256.Sum256(payload)
	return out, "sha256:" + hex.EncodeToString(digest[:]), counts, nil
}

func deterministicHistoricalID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(sum[:12])
}

func normalizeHistoricalTitle(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func historicalBatchRecordID(projectID, batchID string) string {
	return deterministicHistoricalID("HIB-", projectID, batchID)
}

func historicalTaskID(projectID, batchID, key string) string {
	return deterministicHistoricalID("T-H-", projectID, batchID, key)
}

func latestHistoricalBatchState(ctx context.Context, q rowQuerier, recordID string) (string, error) {
	var state string
	err := q.QueryRowContext(ctx, `SELECT state FROM historical_import_batch_events
WHERE batch_record_id=? ORDER BY sequence DESC LIMIT 1`, recordID).Scan(&state)
	return state, mapErr(err)
}

func readHistoricalImportReceipt(ctx context.Context, q historicalQuerier, projectID, batchID string, replay bool) (domain.HistoricalImportReceipt, error) {
	var out domain.HistoricalImportReceipt
	var recordID, recorded string
	err := q.QueryRowContext(ctx, `SELECT id,project_id,batch_id,source_digest,manifest_digest,collision_policy,recorded_at
FROM historical_import_batches WHERE project_id=? AND batch_id=?`, projectID, batchID).Scan(
		&recordID, &out.ProjectID, &out.BatchID, &out.SourceDigest, &out.ManifestDigest, &out.CollisionPolicy, &recorded,
	)
	if err != nil {
		return out, mapErr(err)
	}
	out.RecordedAt = parseStamp(recorded)
	out.State, err = latestHistoricalBatchState(ctx, q, recordID)
	if err != nil {
		return out, err
	}
	out.Applied = out.State == "applied"
	rows, err := q.QueryContext(ctx, `SELECT source_key,canonical_task_id,disposition
FROM historical_import_tasks WHERE batch_record_id=? ORDER BY source_key`, recordID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.HistoricalImportTaskReceipt
		if err = rows.Scan(&item.Key, &item.TaskID, &item.Disposition); err != nil {
			return out, err
		}
		out.Counts.Tasks++
		if replay {
			item.Disposition = "replayed"
			out.Counts.Replayed++
		} else if item.Disposition == "created" {
			out.Counts.Created++
		} else {
			out.Counts.SkippedCurrent++
		}
		out.Tasks = append(out.Tasks, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = q.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM historical_project_thread_aliases WHERE batch_record_id=?),
(SELECT count(*) FROM historical_task_attributions a JOIN historical_import_tasks t ON t.id=a.import_task_id WHERE t.batch_record_id=?),
(SELECT count(*) FROM historical_task_events e JOIN historical_import_tasks t ON t.id=e.import_task_id WHERE t.batch_record_id=?)
`, recordID, recordID, recordID).Scan(&out.Counts.ProjectThreadAliases, &out.Counts.Attributions, &out.Counts.Events); err != nil {
		return out, err
	}
	return out, nil
}

func previewHistoricalImport(ctx context.Context, q historicalQuerier, command domain.HistoricalImportCommand, manifestDigest string, counts domain.HistoricalImportCounts) (domain.HistoricalImportReceipt, error) {
	var priorManifest, priorSource string
	err := q.QueryRowContext(ctx, `SELECT manifest_digest,source_digest FROM historical_import_batches
WHERE project_id=? AND batch_id=?`, command.ProjectID, command.BatchID).Scan(&priorManifest, &priorSource)
	if err == nil {
		if priorManifest != manifestDigest || priorSource != command.SourceDigest {
			return domain.HistoricalImportReceipt{}, domain.ErrConflict
		}
		return readHistoricalImportReceipt(ctx, q, command.ProjectID, command.BatchID, true)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.HistoricalImportReceipt{}, err
	}
	var exists int
	if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=?)`, command.ProjectID).Scan(&exists); err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	if exists == 0 {
		return domain.HistoricalImportReceipt{}, domain.ErrNotFound
	}
	taskIDs := make([]string, len(command.Tasks))
	for i, task := range command.Tasks {
		taskIDs[i] = historicalTaskID(command.ProjectID, command.BatchID, task.Key)
	}
	existingIDs := make(map[string]bool)
	existingKeys := make(map[string]string)
	ambiguousKeys := make(map[string]bool)
	existingTitles := make(map[string]string)
	ambiguousTitles := make(map[string]bool)
	collisions := make([]bool, len(command.Tasks))
	if len(taskIDs) > 0 {
		rows, queryErr := q.QueryContext(ctx, `SELECT id,title FROM active_tasks
WHERE project_id=? ORDER BY id`, command.ProjectID)
		if queryErr != nil {
			return domain.HistoricalImportReceipt{}, queryErr
		}
		for rows.Next() {
			var id, title string
			if queryErr = rows.Scan(&id, &title); queryErr != nil {
				rows.Close()
				return domain.HistoricalImportReceipt{}, queryErr
			}
			existingIDs[id] = true
			key := normalizeHistoricalTitle(title)
			if previous, present := existingTitles[key]; !present {
				existingTitles[key] = id
			} else if previous != id {
				ambiguousTitles[key] = true
			}
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			return domain.HistoricalImportReceipt{}, queryErr
		}
		if queryErr = rows.Close(); queryErr != nil {
			return domain.HistoricalImportReceipt{}, queryErr
		}
		rows, queryErr = q.QueryContext(ctx, `SELECT imported.source_key,imported.canonical_task_id
FROM historical_import_tasks imported
JOIN historical_import_batches batch ON batch.id=imported.batch_record_id
WHERE batch.project_id=? AND imported.disposition='created'
AND (SELECT state FROM historical_import_batch_events event
     WHERE event.batch_record_id=batch.id ORDER BY sequence DESC LIMIT 1)='applied'
ORDER BY imported.source_key,imported.canonical_task_id`, command.ProjectID)
		if queryErr != nil {
			return domain.HistoricalImportReceipt{}, queryErr
		}
		for rows.Next() {
			var key, id string
			if queryErr = rows.Scan(&key, &id); queryErr != nil {
				rows.Close()
				return domain.HistoricalImportReceipt{}, queryErr
			}
			if previous, present := existingKeys[key]; !present {
				existingKeys[key] = id
			} else if previous != id {
				ambiguousKeys[key] = true
			}
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			return domain.HistoricalImportReceipt{}, queryErr
		}
		if queryErr = rows.Close(); queryErr != nil {
			return domain.HistoricalImportReceipt{}, queryErr
		}
		for i, task := range command.Tasks {
			collisionID := ""
			if exact := existingKeys[task.Key]; exact != "" {
				if ambiguousKeys[task.Key] {
					return domain.HistoricalImportReceipt{}, domain.ErrConflict
				}
				collisionID = exact
			} else if existingIDs[taskIDs[i]] {
				collisionID = taskIDs[i]
			} else {
				titleKey := normalizeHistoricalTitle(task.Title)
				if ambiguousTitles[titleKey] {
					return domain.HistoricalImportReceipt{}, domain.ErrConflict
				}
				collisionID = existingTitles[titleKey]
			}
			if collisionID != "" {
				taskIDs[i] = collisionID
				collisions[i] = true
			}
		}
	}
	out := domain.HistoricalImportReceipt{
		ProjectID: command.ProjectID, BatchID: command.BatchID, SourceDigest: command.SourceDigest,
		ManifestDigest: manifestDigest, CollisionPolicy: command.CollisionPolicy, State: "preview",
		Counts: counts, Tasks: make([]domain.HistoricalImportTaskReceipt, 0, len(command.Tasks)),
	}
	for i, task := range command.Tasks {
		disposition := "created"
		if collisions[i] {
			disposition = "skipped_current"
			out.Counts.SkippedCurrent++
		} else {
			out.Counts.Created++
		}
		out.Tasks = append(out.Tasks, domain.HistoricalImportTaskReceipt{Key: task.Key, TaskID: taskIDs[i], Disposition: disposition})
	}
	return out, nil
}

func (s *Store) PreviewHistoricalImport(ctx context.Context, command domain.HistoricalImportCommand) (domain.HistoricalImportReceipt, error) {
	normalized, manifestDigest, counts, err := normalizeHistoricalImport(command, s.now().UTC())
	if err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	return previewHistoricalImport(ctx, s.db, normalized, manifestDigest, counts)
}

func (s *Store) ApplyHistoricalImport(ctx context.Context, command domain.HistoricalImportCommand) (domain.HistoricalImportReceipt, error) {
	if !validCoreMeta(command.Meta) || command.ConfirmSourceDigest == "" || command.ConfirmSourceDigest != command.SourceDigest {
		return domain.HistoricalImportReceipt{}, domain.ErrInvalid
	}
	now := s.now().UTC()
	normalized, manifestDigest, counts, err := normalizeHistoricalImport(command, now)
	if err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	fingerprint, _ := coreFingerprint(struct {
		ManifestDigest, ConfirmSourceDigest string
	}{manifestDigest, command.ConfirmSourceDigest})
	key, operation := coreRequestKey(command.Meta), "historical_import.apply"
	if _, ok, replayErr := readCoreReplay(ctx, s.db, key, operation, fingerprint); replayErr != nil {
		return domain.HistoricalImportReceipt{}, replayErr
	} else if ok {
		return readHistoricalImportReceipt(ctx, s.db, command.ProjectID, command.BatchID, true)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	if _, ok, replayErr := readCoreReplay(ctx, tx, key, operation, fingerprint); replayErr != nil {
		return domain.HistoricalImportReceipt{}, replayErr
	} else if ok {
		return readHistoricalImportReceipt(ctx, tx, command.ProjectID, command.BatchID, true)
	}
	preview, err := previewHistoricalImport(ctx, tx, normalized, manifestDigest, counts)
	if err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	if preview.State != "preview" {
		return preview, nil
	}
	revision, err := bumpProjectRevision(ctx, tx, command.ProjectID, now)
	if err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	recordID := historicalBatchRecordID(command.ProjectID, command.BatchID)
	if _, err = tx.ExecContext(ctx, `INSERT INTO historical_import_batches(
id,project_id,batch_id,schema_version,source_digest,manifest_digest,collision_policy,request_key,
recorded_by_actor,recorded_by_session,recorded_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, recordID, command.ProjectID, command.BatchID, command.SchemaVersion,
		command.SourceDigest, manifestDigest, command.CollisionPolicy, key, command.Meta.ActorID, command.Meta.SessionID, stamp(now)); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	for _, alias := range normalized.ProjectThreadAliases {
		source := alias.Source
		id := deterministicHistoricalID("HPA-", recordID, alias.Alias, alias.SessionID)
		if _, err = tx.ExecContext(ctx, `INSERT INTO historical_project_thread_aliases(
id,batch_record_id,alias,session_id,source_kind,source_stable_id,source_digest,source_occurred_at,
recorded_by_actor,recorded_by_session,recorded_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, recordID, alias.Alias, alias.SessionID, source.Kind,
			source.StableID, source.Digest, stamp(source.OccurredAt), command.Meta.ActorID,
			command.Meta.SessionID, stamp(now)); err != nil {
			return domain.HistoricalImportReceipt{}, mapErr(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO historical_import_batch_events(
id,batch_record_id,state,reason,request_key,recorded_by_actor,recorded_by_session,recorded_at
) VALUES(?,?,'applied','',NULL,?,?,?)`, deterministicHistoricalID("HBE-", recordID, "applied"),
		recordID, command.Meta.ActorID, command.Meta.SessionID, stamp(now)); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	byKey := make(map[string]domain.HistoricalTaskInput, len(normalized.Tasks))
	for _, task := range normalized.Tasks {
		byKey[task.Key] = task
	}
	for _, receipt := range preview.Tasks {
		task := byKey[receipt.Key]
		source := task.Source
		importTaskID := deterministicHistoricalID("HIT-", recordID, task.Key)
		if _, err = tx.ExecContext(ctx, `INSERT INTO historical_import_tasks(
id,batch_record_id,source_key,canonical_task_id,disposition,source_kind,source_stable_id,
source_digest,source_occurred_at,recorded_at
) VALUES(?,?,?,?,?,?,?,?,?,?)`, importTaskID, recordID, task.Key, receipt.TaskID, receipt.Disposition,
			source.Kind, source.StableID, source.Digest, stamp(source.OccurredAt), stamp(now)); err != nil {
			return domain.HistoricalImportReceipt{}, mapErr(err)
		}
		if receipt.Disposition != "created" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tasks(
id,project_id,state,title,priority,owner_session_id,accept_text,description,milestone_id,
project_revision,created_at,updated_at
) VALUES(?,?, 'done',?,?,NULL,?,?,NULL,?,?,?)`, receipt.TaskID, command.ProjectID, task.Title,
			task.Priority, task.Acceptance, task.Description, revision, stamp(source.OccurredAt), stamp(source.OccurredAt)); err != nil {
			return domain.HistoricalImportReceipt{}, mapErr(err)
		}
		if err = insertTaskEvent(ctx, tx, receipt.TaskID, command.ProjectID, revision, "imported",
			"Historical outcome recorded", "", "done", command.Meta, now); err != nil {
			return domain.HistoricalImportReceipt{}, err
		}
		for _, attribution := range task.Attributions {
			source := attribution.Source
			id := deterministicHistoricalID("HTA-", importTaskID, attribution.SessionID, attribution.Role,
				source.Kind, source.StableID, source.Digest)
			if _, err = tx.ExecContext(ctx, `INSERT INTO historical_task_attributions(
id,import_task_id,task_id,session_id,role,confidence,source_kind,source_stable_id,source_digest,
source_occurred_at,recorded_by_actor,recorded_by_session,recorded_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, importTaskID, receipt.TaskID, attribution.SessionID,
				attribution.Role, attribution.Confidence, source.Kind, source.StableID, source.Digest,
				stamp(source.OccurredAt), command.Meta.ActorID, command.Meta.SessionID, stamp(now)); err != nil {
				return domain.HistoricalImportReceipt{}, mapErr(err)
			}
		}
		for _, event := range task.Events {
			source := event.Source
			id := deterministicHistoricalID("HTE-", importTaskID, event.Key)
			if _, err = tx.ExecContext(ctx, `INSERT INTO historical_task_events(
id,import_task_id,task_id,event_key,kind,summary,source_session_id,confidence,occurred_at,
source_kind,source_stable_id,source_digest,recorded_by_actor,recorded_by_session,recorded_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, importTaskID, receipt.TaskID, event.Key, event.Kind,
				event.Summary, event.SessionID, event.Confidence, stamp(source.OccurredAt), source.Kind,
				source.StableID, source.Digest, command.Meta.ActorID,
				command.Meta.SessionID, stamp(now)); err != nil {
				return domain.HistoricalImportReceipt{}, mapErr(err)
			}
		}
	}
	if err = insertCoreChange(ctx, tx, command.ProjectID, revision, "historical_import", command.BatchID,
		fmt.Sprintf("Recorded %d historical outcomes", preview.Counts.Created), now); err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	if err = insertCoreActivity(ctx, tx, "project_updated", command.ProjectID, command.Meta.ActorID,
		command.BatchID, "Historical task import", fmt.Sprintf("%d outcomes recorded", preview.Counts.Created), now); err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, command.BatchID, revision, revision, now); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		if replay, replayErr := readHistoricalImportReceipt(ctx, s.db, command.ProjectID, command.BatchID, true); replayErr == nil {
			return replay, nil
		}
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	return readHistoricalImportReceipt(ctx, s.db, command.ProjectID, command.BatchID, false)
}

func (s *Store) SupersedeHistoricalImport(ctx context.Context, command domain.SupersedeHistoricalImportCommand) (domain.HistoricalImportReceipt, error) {
	if !boundedCoreText(command.ProjectID, maxCoreIDBytes, true) ||
		!historicalKeyPattern.MatchString(command.BatchID) ||
		!boundedCoreText(command.Reason, maxHistoricalReasonBytes, true) || !validCoreMeta(command.Meta) {
		return domain.HistoricalImportReceipt{}, domain.ErrInvalid
	}
	fingerprint, _ := coreFingerprint(struct{ ProjectID, BatchID, Reason string }{command.ProjectID, command.BatchID, command.Reason})
	key, operation := coreRequestKey(command.Meta), "historical_import.supersede"
	if _, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.HistoricalImportReceipt{}, err
	} else if ok {
		return readHistoricalImportReceipt(ctx, s.db, command.ProjectID, command.BatchID, true)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	var recordID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM historical_import_batches WHERE project_id=? AND batch_id=?`,
		command.ProjectID, command.BatchID).Scan(&recordID); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	state, err := latestHistoricalBatchState(ctx, tx, recordID)
	if err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	if state != "applied" {
		return domain.HistoricalImportReceipt{}, domain.ErrConflict
	}
	now := s.now().UTC()
	revision, err := bumpProjectRevision(ctx, tx, command.ProjectID, now)
	if err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO historical_import_batch_events(
id,batch_record_id,state,reason,request_key,recorded_by_actor,recorded_by_session,recorded_at
) VALUES(?,?,'superseded',?,?,?,?,?)`, deterministicHistoricalID("HBE-", recordID, "superseded"),
		recordID, command.Reason, key, command.Meta.ActorID, command.Meta.SessionID, stamp(now)); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	if err = insertCoreChange(ctx, tx, command.ProjectID, revision, "historical_import_superseded", command.BatchID, command.Reason, now); err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	if err = insertCoreActivity(ctx, tx, "project_updated", command.ProjectID, command.Meta.ActorID,
		command.BatchID, "Historical task import", "superseded", now); err != nil {
		return domain.HistoricalImportReceipt{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, command.BatchID, revision, revision, now); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.HistoricalImportReceipt{}, mapErr(err)
	}
	return readHistoricalImportReceipt(ctx, s.db, command.ProjectID, command.BatchID, false)
}
