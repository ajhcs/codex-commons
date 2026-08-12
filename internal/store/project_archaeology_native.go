package store

import (
	"context"
	"database/sql"
	"errors"

	"codex-commons/internal/domain"
)

func (s *Store) QueueArchaeologyNativeBatch(ctx context.Context, command domain.ArchaeologyNativeBatchRequest) (domain.ArchaeologySession, error) {
	if !boundedCoreText(command.Principal, 200, true) || !boundedCoreText(command.RequestID, 200, true) {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	s.archaeologyLaunchMu.Lock()
	defer s.archaeologyLaunchMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	var sessionID, state string
	var revision int64
	var maximum int
	if err = tx.QueryRowContext(ctx, `SELECT id,state,revision,max_concurrency FROM archaeology_sessions WHERE principal=?`, command.Principal).Scan(&sessionID, &state, &revision, &maximum); err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	digest := archaeologyDigest("native_start", struct{ BaseRevision int64 }{command.BaseRevision})
	var batchID string
	var prior []byte
	err = tx.QueryRowContext(ctx, `SELECT id,request_digest FROM archaeology_native_batches WHERE session_id=? AND request_key=?`, sessionID, command.RequestID).Scan(&batchID, &prior)
	if err == nil {
		if string(prior) != string(digest[:]) {
			return domain.ArchaeologySession{}, domain.ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return domain.ArchaeologySession{}, err
		}
		return s.ArchaeologySession(ctx, command.Principal)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.ArchaeologySession{}, err
	}
	if state != "draft" || revision != command.BaseRevision {
		return domain.ArchaeologySession{}, domain.ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,canonical_project_id,name,from_configured_root FROM archaeology_candidates WHERE session_id=? AND selected=1 ORDER BY id LIMIT 101`, sessionID)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	type selectedCandidate struct {
		candidateID, projectID, name string
		fromConfiguredRoot           int
	}
	var selected []selectedCandidate
	for rows.Next() {
		var item selectedCandidate
		if err = rows.Scan(&item.candidateID, &item.projectID, &item.name, &item.fromConfiguredRoot); err != nil {
			rows.Close()
			return domain.ArchaeologySession{}, err
		}
		if !boundedCoreText(item.candidateID, 120, true) || !projectIDPattern.MatchString(item.projectID) || item.projectID == domain.TopicGeneral || !boundedCoreText(item.name, maxProjectNameBytes, true) {
			rows.Close()
			return domain.ArchaeologySession{}, domain.ErrInvalid
		}
		selected = append(selected, item)
	}
	if err = rows.Close(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	if len(selected) < 1 || len(selected) > 100 {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	batchID = deterministicHistoricalID("ARB-", sessionID, command.RequestID)
	at := stamp(now)
	if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at) VALUES(?,?,?,?,'app_server_dynamic_tools','queued',?,?,?)`, batchID, sessionID, command.RequestID, digest[:], maximum, at, at); err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	const purpose = "Added from the Codex project catalog; history pending review."
	for _, item := range selected {
		var mapped string
		err = tx.QueryRowContext(ctx, `SELECT project_id FROM archaeology_candidate_projects WHERE session_id=? AND candidate_id=?`, sessionID, item.candidateID).Scan(&mapped)
		if err == nil && mapped != item.projectID {
			return domain.ArchaeologySession{}, domain.ErrConflict
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return domain.ArchaeologySession{}, err
		}
		var existingName string
		projectErr := tx.QueryRowContext(ctx, `SELECT name FROM projects WHERE id=?`, item.projectID).Scan(&existingName)
		created := 0
		if errors.Is(projectErr, sql.ErrNoRows) {
			if _, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,status,purpose,milestone,now_text,revision,created_at,updated_at) VALUES(?,?,'active',?,"","",1,?,?)`, item.projectID, item.name, purpose, at, at); err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO topics(id,project_id,name,created_at) VALUES(?,?,?,?)`, item.projectID, item.projectID, item.name, at); err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
			if err = insertCoreChange(ctx, tx, item.projectID, 1, "project_created", item.projectID, item.name, now); err != nil {
				return domain.ArchaeologySession{}, err
			}
			if err = insertCoreActivity(ctx, tx, "project_updated", item.projectID, command.Principal, item.projectID, item.name, "created", now); err != nil {
				return domain.ArchaeologySession{}, err
			}
			created = 1
		} else if projectErr != nil {
			return domain.ArchaeologySession{}, mapErr(projectErr)
		} else {
			if mapped == "" && item.fromConfiguredRoot == 0 {
				return domain.ArchaeologySession{}, domain.ErrConflict
			}
			var topicProject string
			if err = tx.QueryRowContext(ctx, `SELECT project_id FROM topics WHERE id=?`, item.projectID).Scan(&topicProject); err != nil || topicProject != item.projectID {
				return domain.ArchaeologySession{}, domain.ErrConflict
			}
		}
		if mapped == "" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_candidate_projects(session_id,candidate_id,project_id,mapped_by_principal,purpose,created_project,mapped_at) VALUES(?,?,?,?,?,?,?)`, sessionID, item.candidateID, item.projectID, command.Principal, purpose, created, at); err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
		}
		jobID := deterministicHistoricalID("ARJ-", batchID, item.candidateID)
		if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,mode,state,created_at,updated_at) VALUES(?,?,?,?,?,"app_server_dynamic_tools","queued",?,?)`, jobID, batchID, sessionID, item.candidateID, item.projectID, at, at); err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='running',revision=revision+1,updated_at=? WHERE id=?`, at, sessionID); err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	return s.ArchaeologySession(ctx, command.Principal)
}

func (s *Store) ClaimArchaeologyNativeJob(ctx context.Context) (domain.ArchaeologyNativeJob, error) {
	s.archaeologyLaunchMu.Lock()
	defer s.archaeologyLaunchMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologyNativeJob{}, err
	}
	defer tx.Rollback()
	var out domain.ArchaeologyNativeJob
	var ignoredSession string
	var maximum, active, blocking int
	err = tx.QueryRowContext(ctx, `SELECT j.id,j.batch_id,j.session_id,j.candidate_id,j.project_id,j.mode,b.max_concurrency,
(SELECT count(*) FROM archaeology_native_jobs a WHERE a.session_id=j.session_id AND a.state IN ('starting','active','report_ready','cancel_requested')),
(SELECT count(*) FROM archaeology_native_jobs u WHERE u.session_id=j.session_id AND u.state='uncertain')
FROM archaeology_native_jobs j JOIN archaeology_native_batches b ON b.id=j.batch_id
WHERE j.state='queued' AND b.state IN ('queued','running') ORDER BY b.created_at,j.created_at,j.id LIMIT 1`).Scan(&out.ID, &out.BatchID, &ignoredSession, &out.CandidateID, &out.ProjectID, &out.Mode, &maximum, &active, &blocking)
	if errors.Is(err, sql.ErrNoRows) {
		return out, domain.ErrConflict
	}
	if err != nil {
		return out, mapErr(err)
	}
	if blocking > 0 || active >= maximum {
		return out, domain.ErrConflict
	}
	now := stamp(s.now().UTC())
	result, err := tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='starting',started_at=?,updated_at=? WHERE id=? AND state='queued'`, now, now, out.ID)
	if err != nil {
		return out, mapErr(err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return out, domain.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_batches SET state='running',updated_at=? WHERE id=?`, now, out.BatchID); err != nil {
		return out, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out.State, out.StartedAt, out.UpdatedAt = "starting", parseStamp(now), parseStamp(now)
	return out, nil
}

func (s *Store) BindArchaeologyNativeJob(ctx context.Context, jobID, threadID, sessionID, turnID string) error {
	if !boundedCoreText(jobID, 120, true) || !boundedCoreText(threadID, 120, true) || !boundedCoreText(sessionID, 120, true) || !boundedCoreText(turnID, 120, true) {
		return domain.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='active',thread_id=?,codex_session_id=?,turn_id=?,phase_label='Historian task accepted by Codex',updated_at=? WHERE id=? AND state='starting'`, threadID, sessionID, turnID, stamp(s.now().UTC()), jobID)
	if err != nil {
		return mapErr(err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) FailArchaeologyNativeStart(ctx context.Context, jobID string, uncertain bool) error {
	state, code := "failed", "codex_start_failed"
	if uncertain {
		state, code = "uncertain", "codex_acceptance_uncertain"
	}
	now := stamp(s.now().UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var batchID string
	if err = tx.QueryRowContext(ctx, `SELECT batch_id FROM archaeology_native_jobs WHERE id=? AND state='starting'`, jobID).Scan(&batchID); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state=?,error_code=?,terminal_at=?,updated_at=? WHERE id=?`, state, code, now, now, jobID); err != nil {
		return mapErr(err)
	}
	var remaining, attention, uncertainCount int
	if err = tx.QueryRowContext(ctx, `SELECT
sum(state IN ('queued','starting','active','report_ready','cancel_requested')),
sum(state IN ('failed','interrupted','attention')),
sum(state='uncertain')
FROM archaeology_native_jobs WHERE batch_id=?`, batchID).Scan(&remaining, &attention, &uncertainCount); err != nil {
		return mapErr(err)
	}
	batchState := "running"
	if uncertainCount > 0 || remaining == 0 && attention > 0 {
		batchState = "attention"
	} else if remaining == 0 {
		batchState = "completed"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_batches SET state=?,updated_at=? WHERE id=?`, batchState, now, batchID); err != nil {
		return mapErr(err)
	}
	if (batchState == "completed" || batchState == "attention") && uncertainCount == 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='draft',revision=revision+1,updated_at=? WHERE id=(SELECT session_id FROM archaeology_native_jobs WHERE id=?)`, now, jobID); err != nil {
			return mapErr(err)
		}
	}
	return tx.Commit()
}

func (s *Store) UpdateArchaeologyNativeProgress(ctx context.Context, p domain.ArchaeologyNativeProgress) error {
	if !boundedCoreText(p.JobID, 120, true) || !boundedCoreText(p.ThreadID, 120, true) || !boundedCoreText(p.TurnID, 120, true) || !boundedCoreText(p.PhaseLabel, 120, true) || p.SourcesExamined < 0 || p.SourcesExamined > 10000 {
		return domain.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `UPDATE archaeology_native_jobs SET phase_label=?,sources_examined=?,updated_at=? WHERE id=? AND thread_id=? AND turn_id=? AND state='active'`, p.PhaseLabel, p.SourcesExamined, stamp(s.now().UTC()), p.JobID, p.ThreadID, p.TurnID)
	if err != nil {
		return mapErr(err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return domain.ErrConflict
	}
	return nil
}
func (s *Store) ReportArchaeologyNativeJob(ctx context.Context, command domain.ArchaeologyNativeReport) error {
	if !boundedCoreText(command.JobID, 120, true) || !boundedCoreText(command.ThreadID, 120, true) || !boundedCoreText(command.TurnID, 120, true) || command.Digest == [32]byte{} || len(command.Outcomes) < 1 || len(command.Outcomes) > 20 {
		return domain.ErrInvalid
	}
	nowTime := s.now().UTC()
	now := stamp(nowTime)
	for _, outcome := range command.Outcomes {
		if !validArchaeologyOutcome(outcome, now) {
			return domain.ErrInvalid
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state, projectID, batchID string
	var prior []byte
	if err = tx.QueryRowContext(ctx, `SELECT state,project_id,batch_id,report_digest FROM archaeology_native_jobs WHERE id=? AND thread_id=? AND turn_id=?`, command.JobID, command.ThreadID, command.TurnID).Scan(&state, &projectID, &batchID, &prior); err != nil {
		return mapErr(err)
	}
	if len(prior) > 0 {
		if string(prior) != string(command.Digest[:]) {
			return domain.ErrConflict
		}
		return tx.Commit()
	}
	if state != "active" {
		return domain.ErrConflict
	}
	for _, outcome := range command.Outcomes {
		if outcome.ProjectID != projectID {
			return domain.ErrInvalid
		}
	}
	for _, outcome := range command.Outcomes {
		seed := outcome.ID
		if seed == "" {
			seed = outcome.Title
		}
		id := deterministicHistoricalID("ARON-", command.JobID, seed)
		if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, command.JobID, outcome.ProjectID, outcome.Title, outcome.Summary, outcome.SourceCount, outcome.ProposalJSON, now); err != nil {
			return mapErr(err)
		}
		for position, source := range outcome.Provenance {
			if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_native_provenance(outcome_id,position,kind,stable_id,digest,occurred_at) VALUES(?,?,?,?,?,?)`, id, position, source.Kind, source.StableID, source.Digest, stamp(source.OccurredAt)); err != nil {
				return mapErr(err)
			}
		}
		for _, member := range outcome.Contributors {
			if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_native_outcome_contributors(outcome_id,session_id,contribution,demonstrated_strength,uncertainty,confidence) VALUES(?,?,?,?,?,?)`, id, member.SessionID, member.Contribution, member.DemonstratedStrength, member.Uncertainty, member.Confidence); err != nil {
				return mapErr(err)
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='report_ready',report_digest=?,reported_at=?,phase_label='Report ready for human review',updated_at=? WHERE id=?`, command.Digest[:], now, now, command.JobID); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_batches SET updated_at=? WHERE id=?`, now, batchID); err != nil {
		return mapErr(err)
	}
	return tx.Commit()
}

func (s *Store) CompleteArchaeologyNativeTurn(ctx context.Context, terminal domain.ArchaeologyNativeTerminal) error {
	if !boundedCoreText(terminal.JobID, 120, true) || !boundedCoreText(terminal.ThreadID, 120, true) || !boundedCoreText(terminal.TurnID, 120, true) || (terminal.Status != "completed" && terminal.Status != "interrupted" && terminal.Status != "failed") || (terminal.DurationMS != nil && (*terminal.DurationMS < 0 || *terminal.DurationMS > 604800000)) {
		return domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := stamp(s.now().UTC())
	var state, batchID, batchCurrent string
	var report []byte
	if err = tx.QueryRowContext(ctx, `SELECT j.state,j.batch_id,j.report_digest,b.state FROM archaeology_native_jobs j JOIN archaeology_native_batches b ON b.id=j.batch_id WHERE j.id=? AND j.thread_id=? AND j.turn_id=?`, terminal.JobID, terminal.ThreadID, terminal.TurnID).Scan(&state, &batchID, &report, &batchCurrent); err != nil {
		return mapErr(err)
	}
	if state == "completed" || state == "failed" || state == "interrupted" || state == "attention" {
		return tx.Commit()
	}
	next, code := "failed", "codex_turn_failed"
	if terminal.Status == "interrupted" {
		next, code = "interrupted", "codex_turn_interrupted"
	} else if terminal.Status == "completed" && len(report) > 0 {
		next, code = "completed", ""
	} else if terminal.Status == "completed" {
		next, code = "attention", "completed_without_report"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state=?,error_code=?,duration_ms=?,terminal_at=?,updated_at=? WHERE id=?`, next, code, terminal.DurationMS, now, now, terminal.JobID); err != nil {
		return mapErr(err)
	}
	var remaining, attention, uncertain int
	if err = tx.QueryRowContext(ctx, `SELECT
sum(state IN ('queued','starting','active','report_ready','cancel_requested')),
sum(state IN ('failed','interrupted','attention')),
sum(state='uncertain')
FROM archaeology_native_jobs WHERE batch_id=?`, batchID).Scan(&remaining, &attention, &uncertain); err != nil {
		return err
	}
	batchState := "running"
	if uncertain > 0 {
		batchState = "attention"
	} else if remaining == 0 {
		if batchCurrent == "cancel_requested" {
			batchState = "canceled"
		} else if attention > 0 {
			batchState = "attention"
		} else {
			batchState = "completed"
		}
	} else if batchCurrent == "cancel_requested" {
		batchState = "cancel_requested"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_batches SET state=?,updated_at=? WHERE id=?`, batchState, now, batchID); err != nil {
		return mapErr(err)
	}
	if (batchState == "completed" || batchState == "canceled" || batchState == "attention") && uncertain == 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='draft',revision=revision+1,updated_at=? WHERE id=(SELECT session_id FROM archaeology_native_jobs WHERE id=?)`, now, terminal.JobID); err != nil {
			return mapErr(err)
		}
	}
	return tx.Commit()
}
func (s *Store) ReconcileArchaeologyNative(ctx context.Context) error {
	now := stamp(s.now().UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='uncertain',error_code='server_restarted_during_active_task',terminal_at=?,updated_at=? WHERE state IN ('starting','active','report_ready','cancel_requested')`, now, now); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_batches SET state='attention',updated_at=? WHERE id IN (SELECT DISTINCT batch_id FROM archaeology_native_jobs WHERE state='uncertain')`, now); err != nil {
		return mapErr(err)
	}
	return tx.Commit()
}

func (s *Store) loadArchaeologyNative(ctx context.Context, sessionID string) ([]domain.ArchaeologyNativeBatch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,state,mode,max_concurrency,created_at,updated_at FROM archaeology_native_batches WHERE session_id=? ORDER BY created_at DESC,id DESC LIMIT 20`, sessionID)
	if err != nil {
		return nil, err
	}
	var out []domain.ArchaeologyNativeBatch
	for rows.Next() {
		var item domain.ArchaeologyNativeBatch
		var created, updated string
		if err = rows.Scan(&item.ID, &item.State, &item.Mode, &item.MaxConcurrency, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		item.CreatedAt, item.UpdatedAt = parseStamp(created), parseStamp(updated)
		out = append(out, item)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for index := range out {
		jobRows, queryErr := s.db.QueryContext(ctx, `SELECT id,batch_id,candidate_id,project_id,mode,state,thread_id,codex_session_id,turn_id,phase_label,sources_examined,duration_ms,error_code,created_at,started_at,reported_at,terminal_at,updated_at FROM archaeology_native_jobs WHERE batch_id=? ORDER BY created_at,id LIMIT 100`, out[index].ID)
		if queryErr != nil {
			return nil, queryErr
		}
		for jobRows.Next() {
			var job domain.ArchaeologyNativeJob
			var duration sql.NullInt64
			var created, updated string
			var started, reported, terminal sql.NullString
			if queryErr = jobRows.Scan(&job.ID, &job.BatchID, &job.CandidateID, &job.ProjectID, &job.Mode, &job.State, &job.ThreadID, &job.CodexSessionID, &job.TurnID, &job.PhaseLabel, &job.SourcesExamined, &duration, &job.ErrorCode, &created, &started, &reported, &terminal, &updated); queryErr != nil {
				jobRows.Close()
				return nil, queryErr
			}
			job.CreatedAt, job.UpdatedAt = parseStamp(created), parseStamp(updated)
			if duration.Valid {
				v := duration.Int64
				job.DurationMS = &v
			}
			if started.Valid {
				job.StartedAt = parseStamp(started.String)
			}
			if reported.Valid {
				job.ReportedAt = parseStamp(reported.String)
			}
			if terminal.Valid {
				job.TerminalAt = parseStamp(terminal.String)
			}
			out[index].Jobs = append(out[index].Jobs, job)
		}
		if queryErr = jobRows.Close(); queryErr != nil {
			return nil, queryErr
		}
	}
	return out, nil
}

func (s *Store) loadArchaeologyNativeOutcomes(ctx context.Context, sessionID string) ([]domain.ArchaeologyOutcome, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.id,o.project_id,o.title,o.summary,o.source_count,o.proposal_json FROM archaeology_native_outcomes o JOIN archaeology_native_jobs j ON j.id=o.job_id WHERE j.session_id=? ORDER BY o.created_at,o.id LIMIT 400`, sessionID)
	if err != nil {
		return nil, err
	}
	var out []domain.ArchaeologyOutcome
	for rows.Next() {
		var item domain.ArchaeologyOutcome
		if err = rows.Scan(&item.ID, &item.ProjectID, &item.Title, &item.Summary, &item.SourceCount, &item.ProposalJSON); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for index := range out {
		p, queryErr := s.db.QueryContext(ctx, `SELECT kind,stable_id,digest,occurred_at FROM archaeology_native_provenance WHERE outcome_id=? ORDER BY position LIMIT 100`, out[index].ID)
		if queryErr != nil {
			return nil, queryErr
		}
		for p.Next() {
			var source domain.ArchaeologyProvenance
			var occurred string
			if queryErr = p.Scan(&source.Kind, &source.StableID, &source.Digest, &occurred); queryErr != nil {
				p.Close()
				return nil, queryErr
			}
			source.OccurredAt = parseStamp(occurred)
			out[index].Provenance = append(out[index].Provenance, source)
		}
		if queryErr = p.Close(); queryErr != nil {
			return nil, queryErr
		}
		c, queryErr := s.db.QueryContext(ctx, `SELECT session_id,contribution,demonstrated_strength,uncertainty,confidence FROM archaeology_native_outcome_contributors WHERE outcome_id=? ORDER BY session_id LIMIT 100`, out[index].ID)
		if queryErr != nil {
			return nil, queryErr
		}
		for c.Next() {
			var member domain.ArchaeologyContributor
			if queryErr = c.Scan(&member.SessionID, &member.Contribution, &member.DemonstratedStrength, &member.Uncertainty, &member.Confidence); queryErr != nil {
				c.Close()
				return nil, queryErr
			}
			out[index].Contributors = append(out[index].Contributors, member)
		}
		if queryErr = c.Close(); queryErr != nil {
			return nil, queryErr
		}
	}
	return out, nil
}

func (s *Store) LoseArchaeologyNativeTurn(ctx context.Context, jobID, threadID, turnID string) error {
	if !boundedCoreText(jobID, 120, true) || !boundedCoreText(threadID, 120, true) || !boundedCoreText(turnID, 120, true) {
		return domain.ErrInvalid
	}
	now := stamp(s.now().UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var batchID string
	result, err := tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='uncertain',error_code='codex_process_unavailable',terminal_at=?,updated_at=? WHERE id=? AND thread_id=? AND turn_id=? AND state IN ('active','report_ready','cancel_requested')`, now, now, jobID, threadID, turnID)
	if err != nil {
		return mapErr(err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return domain.ErrConflict
	}
	if err = tx.QueryRowContext(ctx, `SELECT batch_id FROM archaeology_native_jobs WHERE id=?`, jobID).Scan(&batchID); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_batches SET state='attention',updated_at=? WHERE id=?`, now, batchID); err != nil {
		return mapErr(err)
	}
	return tx.Commit()
}

func (s *Store) CancelArchaeologyNativeBatch(ctx context.Context, principal, requestID string, baseRevision int64) ([]domain.ArchaeologyNativeJob, domain.ArchaeologySession, error) {
	if !boundedCoreText(principal, 200, true) || !boundedCoreText(requestID, 200, true) {
		return nil, domain.ArchaeologySession{}, domain.ErrInvalid
	}
	s.archaeologyLaunchMu.Lock()
	defer s.archaeologyLaunchMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	var sessionID, sessionState string
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT id,revision,state FROM archaeology_sessions WHERE principal=?`, principal).Scan(&sessionID, &revision, &sessionState); err != nil {
		return nil, domain.ArchaeologySession{}, mapErr(err)
	}
	digest := archaeologyDigest("native_cancel", baseRevision)
	replay, err := claimArchaeologyRequest(ctx, tx, principal, requestID, "native_cancel", sessionID, digest, s.now().UTC())
	if err != nil {
		return nil, domain.ArchaeologySession{}, err
	}
	if !replay && (revision != baseRevision || sessionState != "running") {
		return nil, domain.ArchaeologySession{}, domain.ErrConflict
	}
	if replay {
		if err = tx.Commit(); err != nil {
			return nil, domain.ArchaeologySession{}, err
		}
		value, readErr := s.ArchaeologySession(ctx, principal)
		return nil, value, readErr
	}
	now := stamp(s.now().UTC())
	if !replay {
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='interrupted',error_code='canceled_before_start',terminal_at=?,updated_at=? WHERE session_id=? AND state='queued'`, now, now, sessionID); err != nil {
			return nil, domain.ArchaeologySession{}, mapErr(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='cancel_requested',error_code='human_cancel_requested',updated_at=? WHERE session_id=? AND state IN ('active','report_ready','cancel_requested')`, now, sessionID); err != nil {
			return nil, domain.ArchaeologySession{}, mapErr(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='uncertain',error_code='canceled_during_ambiguous_start',updated_at=? WHERE session_id=? AND state='starting'`, now, sessionID); err != nil {
			return nil, domain.ArchaeologySession{}, mapErr(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_batches SET state='cancel_requested',updated_at=? WHERE session_id=? AND state IN ('queued','running')`, now, sessionID); err != nil {
			return nil, domain.ArchaeologySession{}, mapErr(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='cancel_requested',revision=revision+1,updated_at=? WHERE id=?`, now, sessionID); err != nil {
			return nil, domain.ArchaeologySession{}, mapErr(err)
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,batch_id,candidate_id,project_id,mode,state,thread_id,codex_session_id,turn_id FROM archaeology_native_jobs WHERE session_id=? AND state='cancel_requested' ORDER BY id LIMIT 2`, sessionID)
	if err != nil {
		return nil, domain.ArchaeologySession{}, err
	}
	var jobs []domain.ArchaeologyNativeJob
	for rows.Next() {
		var j domain.ArchaeologyNativeJob
		if err = rows.Scan(&j.ID, &j.BatchID, &j.CandidateID, &j.ProjectID, &j.Mode, &j.State, &j.ThreadID, &j.CodexSessionID, &j.TurnID); err != nil {
			rows.Close()
			return nil, domain.ArchaeologySession{}, err
		}
		jobs = append(jobs, j)
	}
	if err = rows.Close(); err != nil {
		return nil, domain.ArchaeologySession{}, err
	}
	if err = tx.Commit(); err != nil {
		return nil, domain.ArchaeologySession{}, err
	}
	value, err := s.ArchaeologySession(ctx, principal)
	return jobs, value, err
}
