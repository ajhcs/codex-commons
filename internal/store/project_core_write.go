package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"codex-commons/internal/domain"
)

func (s *Store) CreateCanonicalProject(ctx context.Context, command domain.CreateProjectCommand) (domain.WriteResult, error) {
	if command.Status == "" {
		command.Status = "active"
	}
	if !projectIDPattern.MatchString(command.ID) || command.ID == domain.TopicGeneral ||
		!boundedCoreText(command.Name, maxProjectNameBytes, true) || !domain.ProjectWriteStatuses[command.Status] ||
		!boundedCoreText(command.Purpose, maxProjectPurposeBytes, true) || !boundedCoreText(command.Now, maxProjectNowBytes, false) ||
		!validCoreMeta(command.Meta) {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	payload := struct{ ID, Name, Status, Purpose, Now string }{command.ID, command.Name, command.Status, command.Purpose, command.Now}
	fingerprint, _ := coreFingerprint(payload)
	key, operation := coreRequestKey(command.Meta), "project.create"
	if replay, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if replay, ok, err := readCoreReplay(ctx, tx, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	now := s.now().UTC()
	const revision int64 = 1
	if _, err = tx.ExecContext(ctx, `INSERT INTO projects(
id,name,status,purpose,milestone,now_text,revision,created_at,updated_at
) VALUES(?,?,?,?,?,?,1,?,?)`, command.ID, command.Name, command.Status, command.Purpose, "", command.Now, stamp(now), stamp(now)); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO topics(id,project_id,name,created_at) VALUES(?,?,?,?)`, command.ID, command.ID, command.Name, stamp(now)); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if err = insertCoreChange(ctx, tx, command.ID, revision, "project_created", command.ID, command.Name, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreActivity(ctx, tx, "project_updated", command.ID, command.Meta.ActorID, command.ID, command.Name, "created", now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, command.ID, revision, revision, now); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = tx.Commit(); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	return domain.WriteResult{ID: command.ID, Revision: revision}, nil
}

func (s *Store) UpdateCanonicalProject(ctx context.Context, command domain.UpdateProjectCommand) (domain.WriteResult, error) {
	if !boundedCoreText(command.ID, maxCoreIDBytes, true) || !boundedCoreText(command.Name, maxProjectNameBytes, true) ||
		!domain.ProjectWriteStatuses[command.Status] || !boundedCoreText(command.Purpose, maxProjectPurposeBytes, true) ||
		!boundedCoreText(command.Now, maxProjectNowBytes, false) || command.BaseRevision < 0 || !validCoreMeta(command.Meta) {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	payload := struct {
		ID, Name, Status, Purpose, Now string
		BaseRevision                   int64
	}{command.ID, command.Name, command.Status, command.Purpose, command.Now, command.BaseRevision}
	fingerprint, _ := coreFingerprint(payload)
	key, operation := coreRequestKey(command.Meta), "project.update"
	if replay, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if replay, ok, err := readCoreReplay(ctx, tx, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id=?`, command.ID).Scan(&current); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if current != command.BaseRevision {
		return domain.WriteResult{}, domain.ErrConflict
	}
	now := s.now().UTC()
	revision, err := bumpProjectRevision(ctx, tx, command.ID, now)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE projects SET name=?,status=?,purpose=?,now_text=? WHERE id=?`, command.Name, command.Status, command.Purpose, command.Now, command.ID); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE topics SET name=? WHERE id=? AND project_id=?`, command.Name, command.ID, command.ID); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreChange(ctx, tx, command.ID, revision, "project_updated", command.ID, command.Name, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreActivity(ctx, tx, "project_updated", command.ID, command.Meta.ActorID, command.ID, command.Name, "updated", now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, command.ID, revision, revision, now); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = tx.Commit(); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	return domain.WriteResult{ID: command.ID, Revision: revision}, nil
}

func (s *Store) CreateMilestone(ctx context.Context, command domain.CreateMilestoneCommand) (domain.WriteResult, error) {
	if !boundedCoreText(command.ProjectID, maxCoreIDBytes, true) || !boundedCoreText(command.Title, maxMilestoneTitleBytes, true) ||
		!domain.MilestoneStatuses[command.Status] || command.Position < 0 || command.Position > 100000 ||
		!validTargetDate(command.TargetDate) || !validCoreMeta(command.Meta) {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	payload := struct {
		ProjectID, Title, Status, TargetDate string
		Position                             int
	}{command.ProjectID, command.Title, command.Status, command.TargetDate, command.Position}
	fingerprint, _ := coreFingerprint(payload)
	key, operation := coreRequestKey(command.Meta), "milestone.create"
	if replay, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if replay, ok, err := readCoreReplay(ctx, tx, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	now, id := s.now().UTC(), newID("MS-")
	revision, err := bumpProjectRevision(ctx, tx, command.ProjectID, now)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO milestones(
id,project_id,title,status,position,target_date,project_revision,created_at,updated_at
) VALUES(?,?,?,?,?,NULLIF(?,''),?,?,?)`, id, command.ProjectID, command.Title, command.Status, command.Position, command.TargetDate, revision, stamp(now), stamp(now)); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = insertCoreChange(ctx, tx, command.ProjectID, revision, "milestone_created", id, command.Title, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreActivity(ctx, tx, "project_updated", command.ProjectID, command.Meta.ActorID, id, command.Title, "milestone created", now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, id, revision, revision, now); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = tx.Commit(); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	return domain.WriteResult{ID: id, Revision: revision}, nil
}

func (s *Store) UpdateMilestone(ctx context.Context, command domain.UpdateMilestoneCommand) (domain.WriteResult, error) {
	if !boundedCoreText(command.ID, maxCoreIDBytes, true) || !boundedCoreText(command.Title, maxMilestoneTitleBytes, true) ||
		!domain.MilestoneStatuses[command.Status] || command.Position < 0 || command.Position > 100000 ||
		!validTargetDate(command.TargetDate) || command.BaseRevision < 0 || !validCoreMeta(command.Meta) {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	payload := struct {
		ID, Title, Status, TargetDate string
		Position                      int
		BaseRevision                  int64
	}{command.ID, command.Title, command.Status, command.TargetDate, command.Position, command.BaseRevision}
	fingerprint, _ := coreFingerprint(payload)
	key, operation := coreRequestKey(command.Meta), "milestone.update"
	if replay, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if replay, ok, err := readCoreReplay(ctx, tx, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	var projectID string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT project_id,project_revision FROM milestones WHERE id=?`, command.ID).Scan(&projectID, &current); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if current != command.BaseRevision {
		return domain.WriteResult{}, domain.ErrConflict
	}
	now := s.now().UTC()
	revision, err := bumpProjectRevision(ctx, tx, projectID, now)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE milestones SET title=?,status=?,position=?,target_date=NULLIF(?,''),project_revision=?,updated_at=? WHERE id=?`,
		command.Title, command.Status, command.Position, command.TargetDate, revision, stamp(now), command.ID); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = insertCoreChange(ctx, tx, projectID, revision, "milestone_updated", command.ID, command.Title, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreActivity(ctx, tx, "project_updated", projectID, command.Meta.ActorID, command.ID, command.Title, "milestone updated", now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, command.ID, revision, revision, now); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = tx.Commit(); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	return domain.WriteResult{ID: command.ID, Revision: revision}, nil
}

func (s *Store) CreateCanonicalTask(ctx context.Context, command domain.CreateTaskCommand) (domain.WriteResult, error) {
	if command.State == "" {
		command.State = "ready"
	}
	dependencies, ok := normalizeDependencyIDs(command.DependencyIDs)
	if !ok || !boundedCoreText(command.ProjectID, maxCoreIDBytes, true) ||
		!boundedCoreText(command.Title, maxTaskTitleBytes, true) || !boundedCoreText(command.Description, maxTaskDescriptionBytes, false) ||
		!boundedCoreText(command.Acceptance, maxTaskAcceptanceBytes, false) || (command.State != "ready" && command.State != "blocked") ||
		command.Priority < -1000 || command.Priority > 1000 || !boundedCoreText(command.MilestoneID, maxCoreIDBytes, false) || !validCoreMeta(command.Meta) {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	command.DependencyIDs = dependencies
	payload := struct {
		ProjectID, Title, Description, Acceptance, State, MilestoneID string
		Priority                                                      int
		Dependencies                                                  []string
	}{command.ProjectID, command.Title, command.Description, command.Acceptance, command.State, command.MilestoneID, command.Priority, dependencies}
	fingerprint, _ := coreFingerprint(payload)
	key, operation := coreRequestKey(command.Meta), "task.create"
	if replay, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if replay, ok, err := readCoreReplay(ctx, tx, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	id := newID("T-")
	if err = validateTaskRelations(ctx, tx, command.ProjectID, id, command.MilestoneID, dependencies, false); err != nil {
		return domain.WriteResult{}, err
	}
	now := s.now().UTC()
	revision, err := bumpProjectRevision(ctx, tx, command.ProjectID, now)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tasks(
id,project_id,state,title,priority,owner_session_id,accept_text,description,milestone_id,project_revision,created_at,updated_at
) VALUES(?,?,?,?,?,NULL,?,?,NULLIF(?,''),?,?,?)`, id, command.ProjectID, command.State, command.Title, command.Priority,
		command.Acceptance, command.Description, command.MilestoneID, revision, stamp(now), stamp(now)); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	for _, dependencyID := range dependencies {
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_dependencies(task_id,depends_on_task_id) VALUES(?,?)`, id, dependencyID); err != nil {
			return domain.WriteResult{}, mapErr(err)
		}
	}
	if err = insertTaskEvent(ctx, tx, id, command.ProjectID, revision, "created", "Task created", "", command.State, command.Meta, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreChange(ctx, tx, command.ProjectID, revision, "task_created", id, command.Title, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreActivity(ctx, tx, "task_status_changed", command.ProjectID, command.Meta.ActorID, id, command.Title, "created", now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, id, revision, revision, now); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = tx.Commit(); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	return domain.WriteResult{ID: id, Revision: revision}, nil
}

func (s *Store) UpdateCanonicalTask(ctx context.Context, command domain.UpdateTaskCommand) (domain.WriteResult, error) {
	dependencies, ok := normalizeDependencyIDs(command.DependencyIDs)
	if !ok || !boundedCoreText(command.ID, maxCoreIDBytes, true) || !boundedCoreText(command.Title, maxTaskTitleBytes, true) ||
		!boundedCoreText(command.Description, maxTaskDescriptionBytes, false) || !boundedCoreText(command.Acceptance, maxTaskAcceptanceBytes, false) ||
		command.Priority < -1000 || command.Priority > 1000 || !boundedCoreText(command.MilestoneID, maxCoreIDBytes, false) ||
		command.BaseRevision < 0 || !validCoreMeta(command.Meta) {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	command.DependencyIDs = dependencies
	payload := struct {
		ID, Title, Description, Acceptance, MilestoneID string
		Priority                                        int
		Dependencies                                    []string
		BaseRevision                                    int64
	}{command.ID, command.Title, command.Description, command.Acceptance, command.MilestoneID, command.Priority, dependencies, command.BaseRevision}
	fingerprint, _ := coreFingerprint(payload)
	key, operation := coreRequestKey(command.Meta), "task.update"
	if replay, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if replay, ok, err := readCoreReplay(ctx, tx, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	var projectID, state string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT project_id,state,project_revision FROM tasks WHERE id=?`, command.ID).Scan(&projectID, &state, &current); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if current != command.BaseRevision {
		return domain.WriteResult{}, domain.ErrConflict
	}
	if err = validateTaskRelations(ctx, tx, projectID, command.ID, command.MilestoneID, dependencies, true); err != nil {
		return domain.WriteResult{}, err
	}
	now := s.now().UTC()
	revision, err := bumpProjectRevision(ctx, tx, projectID, now)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tasks SET title=?,description=?,accept_text=?,priority=?,milestone_id=NULLIF(?,''),project_revision=?,updated_at=? WHERE id=?`,
		command.Title, command.Description, command.Acceptance, command.Priority, command.MilestoneID, revision, stamp(now), command.ID); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM task_dependencies WHERE task_id=?`, command.ID); err != nil {
		return domain.WriteResult{}, err
	}
	for _, dependencyID := range dependencies {
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_dependencies(task_id,depends_on_task_id) VALUES(?,?)`, command.ID, dependencyID); err != nil {
			return domain.WriteResult{}, mapErr(err)
		}
	}
	if err = insertTaskEvent(ctx, tx, command.ID, projectID, revision, "updated", "Task details updated", state, state, command.Meta, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreChange(ctx, tx, projectID, revision, "task_updated", command.ID, command.Title, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreActivity(ctx, tx, "task_status_changed", projectID, command.Meta.ActorID, command.ID, command.Title, "updated", now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, command.ID, revision, revision, now); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = tx.Commit(); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	return domain.WriteResult{ID: command.ID, Revision: revision}, nil
}

func (s *Store) ChangeCanonicalTaskState(ctx context.Context, command domain.ChangeTaskStateCommand) (domain.WriteResult, error) {
	if !boundedCoreText(command.ID, maxCoreIDBytes, true) || !domain.TaskStates[command.State] ||
		!boundedCoreText(command.Basis, maxTaskBasisBytes, true) || command.BaseRevision < 0 || !validCoreMeta(command.Meta) {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	payload := struct {
		ID, State, Basis string
		BaseRevision     int64
	}{command.ID, command.State, command.Basis, command.BaseRevision}
	fingerprint, _ := coreFingerprint(payload)
	key, operation := coreRequestKey(command.Meta), "task.state"
	if replay, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if replay, ok, err := readCoreReplay(ctx, tx, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	var projectID, currentState, title string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT project_id,state,title,project_revision FROM tasks WHERE id=?`, command.ID).Scan(&projectID, &currentState, &title, &current); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if current != command.BaseRevision || !taskTransitionAllowed(currentState, command.State) {
		return domain.WriteResult{}, domain.ErrConflict
	}
	now := s.now().UTC()
	revision, err := bumpProjectRevision(ctx, tx, projectID, now)
	if err != nil {
		return domain.WriteResult{}, err
	}
	ownerExpression := "owner_session_id"
	if command.State == "ready" {
		ownerExpression = "NULL"
	}
	query := fmt.Sprintf(`UPDATE tasks SET state=?,project_revision=?,updated_at=?,owner_session_id=%s WHERE id=?`, ownerExpression)
	if _, err = tx.ExecContext(ctx, query, command.State, revision, stamp(now), command.ID); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if command.State == "ready" || command.State == "done" || command.State == "cancelled" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM task_current_claims WHERE task_id=?`, command.ID); err != nil {
			return domain.WriteResult{}, err
		}
	}
	if err = insertTaskEvent(ctx, tx, command.ID, projectID, revision, "state_changed", command.Basis, currentState, command.State, command.Meta, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreChange(ctx, tx, projectID, revision, "task_state_changed", command.ID, command.State, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreActivity(ctx, tx, "task_status_changed", projectID, command.Meta.ActorID, command.ID, title, command.State, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, command.ID, revision, revision, now); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = tx.Commit(); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	return domain.WriteResult{ID: command.ID, Revision: revision}, nil
}

func (s *Store) AppendWikiRevision(ctx context.Context, command domain.AppendWikiRevisionCommand) (domain.WriteResult, error) {
	if !boundedCoreText(command.ProjectID, maxCoreIDBytes, true) || !wikiSlugPattern.MatchString(command.Slug) ||
		!boundedCoreText(command.Title, maxWikiTitleBytes, true) || !boundedCoreText(command.Summary, maxWikiSummaryBytes, true) ||
		!boundedCoreText(command.Body, maxWikiBodyBytes, true) || command.BaseRevision < 0 || !validCoreMeta(command.Meta) {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	payload := struct {
		ProjectID, Slug, Title, Summary, Body string
		BaseRevision                          int64
	}{command.ProjectID, command.Slug, command.Title, command.Summary, command.Body, command.BaseRevision}
	fingerprint, _ := coreFingerprint(payload)
	key, operation := coreRequestKey(command.Meta), "wiki.append"
	if replay, ok, err := readCoreReplay(ctx, s.db, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WriteResult{}, err
	}
	defer tx.Rollback()
	if err = lockCoreWriter(ctx, tx, key); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if replay, ok, err := readCoreReplay(ctx, tx, key, operation, fingerprint); err != nil {
		return domain.WriteResult{}, err
	} else if ok {
		return replay.WriteResult, nil
	}
	var pageID string
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT id,current_revision FROM wiki_pages WHERE project_id=? AND slug=?`, command.ProjectID, command.Slug).Scan(&pageID, &current)
	if errors.Is(err, sql.ErrNoRows) {
		if command.BaseRevision != 0 {
			return domain.WriteResult{}, domain.ErrConflict
		}
		pageID, current, err = newID("W-"), 0, nil
	} else if err != nil {
		return domain.WriteResult{}, err
	} else if current != command.BaseRevision {
		return domain.WriteResult{}, domain.ErrConflict
	}
	now := s.now().UTC()
	projectRevision, err := bumpProjectRevision(ctx, tx, command.ProjectID, now)
	if err != nil {
		return domain.WriteResult{}, err
	}
	pageRevision := current + 1
	if current == 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO wiki_pages(id,project_id,slug,title,current_revision) VALUES(?,?,?,?,?)`, pageID, command.ProjectID, command.Slug, command.Title, pageRevision); err != nil {
			return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
		}
	} else if _, err = tx.ExecContext(ctx, `UPDATE wiki_pages SET title=?,current_revision=? WHERE id=?`, command.Title, pageRevision, pageID); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wiki_revisions(page_id,revision,summary,body,author_session_id,created_at) VALUES(?,?,?,?,?,?)`,
		pageID, pageRevision, command.Summary, command.Body, command.Meta.SessionID, stamp(now)); err != nil {
		return domain.WriteResult{}, mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO search_documents(project_id,ref,kind,revision,title,body) VALUES(?,?,?,?,?,?)
ON CONFLICT(ref) DO UPDATE SET revision=excluded.revision,title=excluded.title,body=excluded.body`,
		command.ProjectID, pageID, "wiki", projectRevision, command.Title, command.Summary+" "+command.Body); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreChange(ctx, tx, command.ProjectID, projectRevision, "wiki_revised", pageID, command.Summary, now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = insertCoreActivity(ctx, tx, "wiki_revised", command.ProjectID, command.Meta.ActorID, pageID, command.Title, "revision appended", now); err != nil {
		return domain.WriteResult{}, err
	}
	if err = recordCoreReplay(ctx, tx, key, operation, fingerprint, pageID, pageRevision, projectRevision, now); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	if err = tx.Commit(); err != nil {
		return coreWriteFailure(ctx, s, tx, err, key, operation, fingerprint)
	}
	return domain.WriteResult{ID: pageID, Revision: pageRevision}, nil
}

func insertTaskEvent(ctx context.Context, tx *sql.Tx, taskID, projectID string, revision int64, kind, summary, fromState, toState string, meta domain.CoreWriteMeta, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO task_events(id,task_id,project_id,project_revision,kind,summary,from_state,to_state,actor_id,session_id,created_at) VALUES(?,?,?,?,?,?,NULLIF(?,''),NULLIF(?,''),?,?,?)`, newID("TE-"), taskID, projectID, revision, kind, summary, fromState, toState, meta.ActorID, meta.SessionID, stamp(now))
	return err
}
