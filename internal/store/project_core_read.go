package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"codex-commons/internal/domain"
)

func optionalCoreStamp(value sql.NullString) time.Time {
	if !value.Valid || value.String == "" || value.String == "1970-01-01T00:00:00Z" {
		return time.Time{}
	}
	return parseStamp(value.String)
}

func (s *Store) ProjectCoreSnapshot(ctx context.Context, query domain.ProjectCoreReadQuery) (domain.ProjectCoreSnapshot, error) {
	if !boundedCoreText(query.ProjectID, maxCoreIDBytes, true) || query.ActivityStart.IsZero() || query.ActivityEnd.IsZero() ||
		query.ActivityStart.Location() != time.UTC || query.ActivityEnd.Location() != time.UTC ||
		query.ActivityEnd.Sub(query.ActivityStart) != 14*24*time.Hour {
		return domain.ProjectCoreSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.ProjectCoreSnapshot{}, err
	}
	defer tx.Rollback()
	var out domain.ProjectCoreSnapshot
	var created, updated sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,name,status,purpose,now_text,revision,created_at,updated_at FROM projects WHERE id=?`, query.ProjectID).Scan(
		&out.Project.ID, &out.Project.Name, &out.Project.Status, &out.Project.Purpose, &out.Project.Now,
		&out.Project.Revision, &created, &updated,
	)
	if err != nil {
		return out, mapErr(err)
	}
	out.Project.CreatedAt, out.Project.UpdatedAt = optionalCoreStamp(created), optionalCoreStamp(updated)
	if err = tx.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM active_tasks WHERE project_id=?),
(SELECT count(*) FROM milestones WHERE project_id=?),
(SELECT count(*) FROM wiki_pages WHERE project_id=?)`, query.ProjectID, query.ProjectID, query.ProjectID).Scan(
		&out.Counts.Tasks, &out.Counts.Milestones, &out.Counts.WikiPages,
	); err != nil {
		return out, err
	}
	var milestone domain.Milestone
	var target, milestoneCreated, milestoneUpdated sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,project_id,title,status,position,target_date,project_revision,created_at,updated_at
FROM milestones WHERE project_id=? AND status='active' LIMIT 1`, query.ProjectID).Scan(
		&milestone.ID, &milestone.ProjectID, &milestone.Title, &milestone.Status, &milestone.Position,
		&target, &milestone.Revision, &milestoneCreated, &milestoneUpdated,
	)
	if err == nil {
		milestone.TargetDate = target.String
		milestone.CreatedAt, milestone.UpdatedAt = optionalCoreStamp(milestoneCreated), optionalCoreStamp(milestoneUpdated)
		out.ActiveMilestone = &milestone
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out.Activity, err = readProjectActivityDays(ctx, tx, query.ProjectID, query.ActivityStart, query.ActivityEnd)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return domain.ProjectCoreSnapshot{}, err
	}
	return out, nil
}

func readProjectActivityDays(ctx context.Context, tx *sql.Tx, projectID string, start, end time.Time) ([]domain.ProjectOverviewActivityDay, error) {
	rows, err := tx.QueryContext(ctx, `SELECT substr(occurred_at,1,10),count(*)
FROM activity_events
WHERE project_id=? AND substr(occurred_at,1,10)>=? AND substr(occurred_at,1,10)<?
GROUP BY substr(occurred_at,1,10)
ORDER BY substr(occurred_at,1,10)`, projectID, start.Format(time.DateOnly), end.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ProjectOverviewActivityDay
	for rows.Next() {
		var day string
		var item domain.ProjectOverviewActivityDay
		if err := rows.Scan(&day, &item.Count); err != nil {
			return nil, err
		}
		item.Day, err = time.ParseInLocation(time.DateOnly, day, time.UTC)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) MilestoneListSnapshot(ctx context.Context, query domain.MilestoneListQuery) (domain.MilestoneListSnapshot, error) {
	if !boundedCoreText(query.ProjectID, maxCoreIDBytes, true) || query.Limit < 1 || query.Limit > maxCoreListLimit {
		return domain.MilestoneListSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.MilestoneListSnapshot{}, err
	}
	defer tx.Rollback()
	var out domain.MilestoneListSnapshot
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM milestones WHERE project_id=?`, query.ProjectID).Scan(&out.Total); err != nil {
		return out, err
	}
	if out.Total == 0 {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=?)`, query.ProjectID).Scan(&exists); err != nil {
			return out, err
		}
		if exists == 0 {
			return out, domain.ErrNotFound
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,project_id,title,status,position,target_date,project_revision,created_at,updated_at
FROM milestones WHERE project_id=? ORDER BY position,id LIMIT ?`, query.ProjectID, query.Limit+1)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.Milestone
		var target, created, updated sql.NullString
		if err = rows.Scan(&item.ID, &item.ProjectID, &item.Title, &item.Status, &item.Position, &target, &item.Revision, &created, &updated); err != nil {
			rows.Close()
			return out, err
		}
		item.TargetDate = target.String
		item.CreatedAt, item.UpdatedAt = optionalCoreStamp(created), optionalCoreStamp(updated)
		out.Items = append(out.Items, item)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return domain.MilestoneListSnapshot{}, err
	}
	return out, nil
}

func (s *Store) TaskListSnapshot(ctx context.Context, query domain.TaskListQuery) (domain.TaskListSnapshot, error) {
	if !boundedCoreText(query.ProjectID, maxCoreIDBytes, true) || query.Limit < 1 || query.Limit > maxCoreListLimit ||
		(query.State != "" && !domain.TaskStates[query.State]) || !boundedCoreText(query.MilestoneID, maxCoreIDBytes, false) ||
		(query.After != nil && (query.After.Time.IsZero() || !boundedCoreText(query.After.ID, maxCoreIDBytes, true))) {
		return domain.TaskListSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.TaskListSnapshot{}, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=?)`, query.ProjectID).Scan(&exists); err != nil {
		return domain.TaskListSnapshot{}, err
	}
	if exists == 0 {
		return domain.TaskListSnapshot{}, domain.ErrNotFound
	}
	baseWhere, baseArgs := []string{"project_id=?"}, []any{query.ProjectID}
	if query.MilestoneID != "" {
		baseWhere, baseArgs = append(baseWhere, "milestone_id=?"), append(baseArgs, query.MilestoneID)
	}
	var out domain.TaskListSnapshot
	countSQL := `SELECT
COALESCE(sum(state='ready'),0),COALESCE(sum(state='in_progress'),0),COALESCE(sum(state='blocked'),0),
COALESCE(sum(state='done'),0),COALESCE(sum(state='cancelled'),0),count(*)
FROM active_tasks WHERE ` + strings.Join(baseWhere, " AND ")
	if err = tx.QueryRowContext(ctx, countSQL, baseArgs...).Scan(&out.StateCounts.Ready, &out.StateCounts.InProgress,
		&out.StateCounts.Blocked, &out.StateCounts.Done, &out.StateCounts.Cancelled, &out.StateCounts.Total); err != nil {
		return out, err
	}
	where, args := append([]string(nil), baseWhere...), append([]any(nil), baseArgs...)
	if query.State != "" {
		where, args = append(where, "state=?"), append(args, query.State)
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM active_tasks WHERE `+strings.Join(where, " AND "), args...).Scan(&out.Total); err != nil {
		return out, err
	}
	if query.After != nil {
		where, args = append(where, `(julianday(updated_at)<julianday(?) OR (updated_at=? AND id<?))`),
			append(args, stamp(query.After.Time), stamp(query.After.Time), query.After.ID)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT id,project_id,title,description,accept_text,state,priority,
COALESCE(milestone_id,''),COALESCE(owner_session_id,''),COALESCE((SELECT purpose FROM sessions s WHERE s.id=owner_session_id),''),project_revision,created_at,updated_at
FROM active_tasks WHERE `+strings.Join(where, " AND ")+`
ORDER BY julianday(updated_at) DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.CanonicalTask
		var created, updated sql.NullString
		if err = rows.Scan(&item.ID, &item.ProjectID, &item.Title, &item.Description, &item.Acceptance, &item.State,
			&item.Priority, &item.MilestoneID, &item.OwnerSessionID, &item.OwnerPurpose, &item.Revision, &created, &updated); err != nil {
			rows.Close()
			return out, err
		}
		item.CreatedAt, item.UpdatedAt = optionalCoreStamp(created), optionalCoreStamp(updated)
		out.Items = append(out.Items, item)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	if err = loadTaskDependencies(ctx, tx, out.Items); err != nil {
		return out, err
	}
	if err = loadTaskMilestones(ctx, tx, out.Items); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return domain.TaskListSnapshot{}, err
	}
	return out, nil
}

func loadTaskDependencies(ctx context.Context, tx *sql.Tx, tasks []domain.CanonicalTask) error {
	if len(tasks) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(tasks)), ",")
	args := make([]any, len(tasks))
	byID := make(map[string]int, len(tasks))
	for i := range tasks {
		args[i], byID[tasks[i].ID] = tasks[i].ID, i
		tasks[i].Dependencies = []domain.TaskDependency{}
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`WITH ranked AS (
 SELECT d.task_id,b.id,b.title,b.state,
 row_number() OVER (PARTITION BY d.task_id ORDER BY b.id) AS n
 FROM task_dependencies d JOIN tasks b ON b.id=d.depends_on_task_id
 WHERE d.task_id IN (%s)
) SELECT task_id,id,title,state,n FROM ranked WHERE n<=? ORDER BY task_id,n`, marks), append(args, maxTaskDependencies+1)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		var dependency domain.TaskDependency
		var rank int
		if err = rows.Scan(&taskID, &dependency.ID, &dependency.Title, &dependency.State, &rank); err != nil {
			return err
		}
		index := byID[taskID]
		if rank > maxTaskDependencies {
			tasks[index].DependenciesTruncated = true
			continue
		}
		tasks[index].Dependencies = append(tasks[index].Dependencies, dependency)
	}
	return rows.Err()
}

func loadTaskMilestones(ctx context.Context, tx *sql.Tx, tasks []domain.CanonicalTask) error {
	byMilestone := make(map[string][]int)
	for i := range tasks {
		if tasks[i].MilestoneID != "" {
			byMilestone[tasks[i].MilestoneID] = append(byMilestone[tasks[i].MilestoneID], i)
		}
	}
	if len(byMilestone) == 0 {
		return nil
	}
	ids := make([]string, 0, len(byMilestone))
	for id := range byMilestone {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = ids[i]
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT id,title,status FROM milestones WHERE id IN (%s)`, marks), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value domain.TaskMilestoneSummary
		if err = rows.Scan(&value.ID, &value.Title, &value.Status); err != nil {
			return err
		}
		for _, index := range byMilestone[value.ID] {
			copy := value
			tasks[index].Milestone = &copy
		}
	}
	return rows.Err()
}

func (s *Store) TaskOpenSnapshot(ctx context.Context, query domain.TaskOpenQuery) (domain.TaskOpenSnapshot, error) {
	if !boundedCoreText(query.TaskID, maxCoreIDBytes, true) || query.EventsLimit < 1 || query.EventsLimit > maxTaskEvents {
		return domain.TaskOpenSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.TaskOpenSnapshot{}, err
	}
	defer tx.Rollback()
	var out domain.TaskOpenSnapshot
	var created, updated sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,project_id,title,description,accept_text,state,priority,
COALESCE(milestone_id,''),COALESCE(owner_session_id,''),COALESCE((SELECT purpose FROM sessions s WHERE s.id=owner_session_id),''),project_revision,created_at,updated_at
FROM tasks WHERE id=?`, query.TaskID).Scan(&out.Task.ID, &out.Task.ProjectID, &out.Task.Title, &out.Task.Description,
		&out.Task.Acceptance, &out.Task.State, &out.Task.Priority, &out.Task.MilestoneID, &out.Task.OwnerSessionID, &out.Task.OwnerPurpose,
		&out.Task.Revision, &created, &updated)
	if err != nil {
		return out, mapErr(err)
	}
	out.Task.CreatedAt, out.Task.UpdatedAt = optionalCoreStamp(created), optionalCoreStamp(updated)
	tasks := []domain.CanonicalTask{out.Task}
	if err = loadTaskDependencies(ctx, tx, tasks); err != nil {
		return out, err
	}
	if err = loadTaskMilestones(ctx, tx, tasks); err != nil {
		return out, err
	}
	out.Task = tasks[0]
	if err = loadHistoricalTaskProjection(ctx, tx, &out.Task); err != nil {
		return out, err
	}
	if err = loadTaskContributors(ctx, tx, &out.Task); err != nil {
		return out, err
	}
	out.Events, err = readTaskEventsWithProvenance(ctx, tx, domain.TaskEventListQuery{TaskID: query.TaskID, Limit: query.EventsLimit})
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return domain.TaskOpenSnapshot{}, err
	}
	return out, nil
}

func (s *Store) TaskEventListSnapshot(ctx context.Context, query domain.TaskEventListQuery) (domain.TaskEventListSnapshot, error) {
	if !boundedCoreText(query.TaskID, maxCoreIDBytes, true) || query.Limit < 1 || query.Limit > maxTaskEvents ||
		(query.After != nil && (query.After.Time.IsZero() || !boundedCoreText(query.After.ID, maxCoreIDBytes, true))) {
		return domain.TaskEventListSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.TaskEventListSnapshot{}, err
	}
	defer tx.Rollback()
	var out domain.TaskEventListSnapshot
	if err = tx.QueryRowContext(ctx, `SELECT project_id FROM tasks WHERE id=?`, query.TaskID).Scan(&out.ProjectID); err != nil {
		return out, mapErr(err)
	}
	out.Items, err = readTaskEventsWithProvenance(ctx, tx, query)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return domain.TaskEventListSnapshot{}, err
	}
	return out, nil
}

func readTaskEvents(ctx context.Context, tx *sql.Tx, query domain.TaskEventListQuery) ([]domain.TaskEvent, error) {
	where, args := []string{"task_id=?"}, []any{query.TaskID}
	if query.After != nil {
		where, args = append(where, `(julianday(created_at)<julianday(?) OR (created_at=? AND id<?))`),
			append(args, stamp(query.After.Time), stamp(query.After.Time), query.After.ID)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT id,task_id,project_id,kind,summary,COALESCE(from_state,''),
COALESCE(to_state,''),actor_id,session_id,project_revision,created_at
FROM task_events WHERE `+strings.Join(where, " AND ")+` ORDER BY julianday(created_at) DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TaskEvent
	for rows.Next() {
		var item domain.TaskEvent
		var created sql.NullString
		if err = rows.Scan(&item.ID, &item.TaskID, &item.ProjectID, &item.Kind, &item.Summary, &item.FromState,
			&item.ToState, &item.ActorID, &item.SessionID, &item.Revision, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = optionalCoreStamp(created)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) WikiListSnapshot(ctx context.Context, query domain.WikiListQuery) (domain.WikiListSnapshot, error) {
	if !boundedCoreText(query.ProjectID, maxCoreIDBytes, true) || !boundedCoreText(query.Search, maxWikiSearchBytes, false) ||
		strings.TrimSpace(query.Search) != query.Search || query.Limit < 1 || query.Limit > maxCoreListLimit ||
		(query.After != nil && (query.After.Text == "" || !boundedCoreText(query.After.ID, maxCoreIDBytes, true))) {
		return domain.WikiListSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.WikiListSnapshot{}, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=?)`, query.ProjectID).Scan(&exists); err != nil {
		return domain.WikiListSnapshot{}, err
	}
	if exists == 0 {
		return domain.WikiListSnapshot{}, domain.ErrNotFound
	}
	joins, where, args := "", []string{"w.project_id=?"}, []any{query.ProjectID}
	if query.Search != "" {
		fts, err := ftsQuery(query.Search)
		if err != nil {
			return domain.WikiListSnapshot{}, err
		}
		joins = ` JOIN search_documents d ON d.ref=w.id JOIN search_fts ON search_fts.rowid=d.id`
		where, args = append(where, `search_fts MATCH ?`), append(args, fts)
	}
	countSQL := `SELECT count(*) FROM wiki_pages w` + joins + ` WHERE ` + strings.Join(where, " AND ")
	var out domain.WikiListSnapshot
	if err = tx.QueryRowContext(ctx, countSQL, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	if query.After != nil {
		where, args = append(where, `(lower(w.title)>lower(?) OR (lower(w.title)=lower(?) AND w.id>?))`),
			append(args, query.After.Text, query.After.Text, query.After.ID)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT w.id,w.project_id,w.slug,w.title,w.current_revision,r.summary,r.created_at
FROM wiki_pages w`+joins+` JOIN wiki_revisions r ON r.page_id=w.id AND r.revision=w.current_revision
WHERE `+strings.Join(where, " AND ")+` ORDER BY lower(w.title),w.id LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.WikiPageSummary
		var updated sql.NullString
		if err = rows.Scan(&item.ID, &item.ProjectID, &item.Slug, &item.Title, &item.CurrentRevision, &item.Summary, &updated); err != nil {
			rows.Close()
			return out, err
		}
		item.UpdatedAt = optionalCoreStamp(updated)
		out.Items = append(out.Items, item)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return domain.WikiListSnapshot{}, err
	}
	return out, nil
}

func (s *Store) OpenWikiRevision(ctx context.Context, projectID, slug string, revision int64) (domain.WikiRevision, error) {
	if !boundedCoreText(projectID, maxCoreIDBytes, true) || !wikiSlugPattern.MatchString(slug) || revision < 0 {
		return domain.WikiRevision{}, domain.ErrInvalid
	}
	query := `SELECT w.id,w.project_id,w.slug,w.title,r.revision,r.summary,r.body,r.author_session_id,
COALESCE(s.purpose,''),r.created_at
FROM wiki_pages w JOIN wiki_revisions r ON r.page_id=w.id LEFT JOIN sessions s ON s.id=r.author_session_id WHERE w.project_id=? AND w.slug=?`
	args := []any{projectID, slug}
	if revision == 0 {
		query += ` AND r.revision=w.current_revision`
	} else {
		query += ` AND r.revision=?`
		args = append(args, revision)
	}
	var out domain.WikiRevision
	var created sql.NullString
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&out.PageID, &out.ProjectID, &out.Slug, &out.Title, &out.Revision,
		&out.Summary, &out.Body, &out.AuthorSessionID, &out.AuthorPurpose, &created)
	out.CreatedAt = optionalCoreStamp(created)
	return out, mapErr(err)
}

func (s *Store) WikiHistorySnapshot(ctx context.Context, query domain.WikiHistoryQuery) (domain.WikiHistorySnapshot, error) {
	if !boundedCoreText(query.ProjectID, maxCoreIDBytes, true) || !wikiSlugPattern.MatchString(query.Slug) ||
		query.BeforeRevision < 0 || query.Limit < 1 || query.Limit > maxCoreListLimit {
		return domain.WikiHistorySnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.WikiHistorySnapshot{}, err
	}
	defer tx.Rollback()
	var out domain.WikiHistorySnapshot
	var pageID string
	if err = tx.QueryRowContext(ctx, `SELECT id,current_revision FROM wiki_pages WHERE project_id=? AND slug=?`, query.ProjectID, query.Slug).Scan(&pageID, &out.CurrentRevision); err != nil {
		return out, mapErr(err)
	}
	where, args := []string{"r.page_id=?"}, []any{pageID}
	if query.BeforeRevision > 0 {
		where, args = append(where, "r.revision<?"), append(args, query.BeforeRevision)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT r.revision,r.summary,r.author_session_id,COALESCE(s.purpose,''),r.created_at
FROM wiki_revisions r LEFT JOIN sessions s ON s.id=r.author_session_id
WHERE `+strings.Join(where, " AND ")+` ORDER BY r.revision DESC LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.WikiRevisionSummary
		var created sql.NullString
		if err = rows.Scan(&item.Revision, &item.Summary, &item.AuthorSessionID, &item.AuthorPurpose, &created); err != nil {
			rows.Close()
			return out, err
		}
		item.CreatedAt = optionalCoreStamp(created)
		out.Items = append(out.Items, item)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return domain.WikiHistorySnapshot{}, err
	}
	return out, nil
}
