package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"codex-commons/internal/domain"
)

const (
	maxCoreIDBytes          = 200
	maxProjectNameBytes     = 200
	maxProjectPurposeBytes  = 4000
	maxProjectNowBytes      = 4000
	maxMilestoneTitleBytes  = 300
	maxTaskTitleBytes       = 300
	maxTaskDescriptionBytes = 12000
	maxTaskAcceptanceBytes  = 4000
	maxTaskBasisBytes       = 2000
	maxTaskDependencies     = 20
	maxTaskEvents           = 50
	maxWikiTitleBytes       = 300
	maxWikiSummaryBytes     = 1000
	maxWikiBodyBytes        = 24000
	maxWikiSearchBytes      = 200
	maxCoreListLimit        = 100
)

var (
	projectIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)
	wikiSlugPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,119}$`)
)

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func boundedCoreText(value string, max int, required bool) bool {
	if !utf8.ValidString(value) || len(value) > max || strings.ContainsRune(value, 0) {
		return false
	}
	if required && strings.TrimSpace(value) == "" {
		return false
	}
	return true
}

func validCoreMeta(meta domain.CoreWriteMeta) bool {
	return boundedCoreText(meta.ActorID, maxCoreIDBytes, true) &&
		boundedCoreText(meta.SessionID, maxCoreIDBytes, true) &&
		boundedCoreText(meta.RequestID, 200, true)
}

func validTargetDate(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func normalizeDependencyIDs(values []string) ([]string, bool) {
	if len(values) > maxTaskDependencies {
		return nil, false
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	for i, value := range out {
		if !boundedCoreText(value, maxCoreIDBytes, true) || i > 0 && value == out[i-1] {
			return nil, false
		}
	}
	return out, true
}

func coreFingerprint(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type coreReplayResult struct {
	domain.WriteResult
	ProjectRevision int64
}

func coreRequestKey(meta domain.CoreWriteMeta) string {
	return requestStorageKey(meta.ActorID, meta.SessionID, meta.RequestID)
}

func readCoreReplay(ctx context.Context, q rowQuerier, key, operation, fingerprint string) (coreReplayResult, bool, error) {
	var result coreReplayResult
	var priorOperation, priorFingerprint string
	err := q.QueryRowContext(ctx, `SELECT operation,payload_hash,result_id,result_revision,project_revision
FROM project_core_requests WHERE request_key=?`, key).Scan(
		&priorOperation, &priorFingerprint, &result.ID, &result.Revision, &result.ProjectRevision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return coreReplayResult{}, false, nil
	}
	if err != nil {
		return coreReplayResult{}, false, err
	}
	if priorOperation != operation || priorFingerprint != fingerprint {
		return coreReplayResult{}, false, domain.ErrConflict
	}
	return result, true, nil
}

func recordCoreReplay(ctx context.Context, tx *sql.Tx, key, operation, fingerprint, resultID string, resultRevision, projectRevision int64, createdAt time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_core_requests(
request_key,operation,payload_hash,result_id,result_revision,project_revision,created_at
) VALUES(?,?,?,?,?,?,?)`, key, operation, fingerprint, resultID, resultRevision, projectRevision, stamp(createdAt))
	return err
}

func lockCoreWriter(ctx context.Context, tx *sql.Tx, requestKey string) error {
	_, err := tx.ExecContext(ctx, `UPDATE project_core_requests SET request_key=request_key WHERE request_key=?`, requestKey)
	return err
}

func bumpProjectRevision(ctx context.Context, tx *sql.Tx, projectID string, now time.Time) (int64, error) {
	var revision int64
	err := tx.QueryRowContext(ctx, `UPDATE projects SET revision=revision+1,updated_at=? WHERE id=? RETURNING revision`, stamp(now), projectID).Scan(&revision)
	return revision, mapErr(err)
}

func insertCoreChange(ctx context.Context, tx *sql.Tx, projectID string, revision int64, kind, ref, summary string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO changes(project_id,revision,kind,ref,summary,created_at) VALUES(?,?,?,?,?,?)`,
		projectID, revision, kind, ref, summary, stamp(now))
	return err
}

func insertCoreActivity(ctx context.Context, tx *sql.Tx, kind, projectID, actorID, ref, title, outcome string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO activity_events(
id,kind,project_id,actor_id,object_ref,object_title,outcome,untrusted,occurred_at,recorded_at
) VALUES(?,?,?,?,?,?,?,?,?,?)`, newID("A-"), kind, projectID, actorID, ref, title, outcome, 1, stamp(now), stamp(now))
	return err
}

func coreWriteFailure(ctx context.Context, s *Store, tx *sql.Tx, err error, key, operation, fingerprint string) (domain.WriteResult, error) {
	if err == nil {
		return domain.WriteResult{}, nil
	}
	if tx != nil {
		_ = tx.Rollback()
	}
	if replay, ok, replayErr := readCoreReplay(ctx, s.db, key, operation, fingerprint); replayErr == nil && ok {
		return replay.WriteResult, nil
	}
	return domain.WriteResult{}, mapErr(err)
}

func taskTransitionAllowed(from, to string) bool {
	if from == to || !domain.TaskStates[from] || !domain.TaskStates[to] || to == "in_progress" {
		return false
	}
	switch from {
	case "ready":
		return to == "blocked" || to == "done" || to == "cancelled"
	case "in_progress":
		return to == "blocked" || to == "done" || to == "cancelled"
	case "blocked":
		return to == "ready" || to == "cancelled"
	default:
		return false
	}
}

func validateTaskRelations(ctx context.Context, tx *sql.Tx, projectID, taskID, milestoneID string, dependencyIDs []string, checkCycle bool) error {
	if milestoneID != "" {
		var milestoneProject string
		if err := tx.QueryRowContext(ctx, `SELECT project_id FROM milestones WHERE id=?`, milestoneID).Scan(&milestoneProject); err != nil {
			return mapErr(err)
		}
		if milestoneProject != projectID {
			return domain.ErrInvalid
		}
	}
	for _, dependencyID := range dependencyIDs {
		if dependencyID == taskID {
			return domain.ErrInvalid
		}
		var dependencyProject string
		if err := tx.QueryRowContext(ctx, `SELECT project_id FROM tasks WHERE id=?`, dependencyID).Scan(&dependencyProject); err != nil {
			return mapErr(err)
		}
		if dependencyProject != projectID {
			return domain.ErrInvalid
		}
		if checkCycle {
			var cycle int
			err := tx.QueryRowContext(ctx, `WITH RECURSIVE reachable(id) AS (
  SELECT depends_on_task_id FROM task_dependencies WHERE task_id=?
  UNION
  SELECT d.depends_on_task_id FROM task_dependencies d JOIN reachable r ON d.task_id=r.id
) SELECT EXISTS(SELECT 1 FROM reachable WHERE id=?)`, dependencyID, taskID).Scan(&cycle)
			if err != nil {
				return err
			}
			if cycle != 0 {
				return domain.ErrConflict
			}
		}
	}
	return nil
}
