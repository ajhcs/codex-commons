package store

import (
	"context"
	"crypto/subtle"
	"database/sql"

	"codex-commons/internal/domain"
)

func (s *Store) ClaimArchaeologyTaskLaunch(ctx context.Context, command domain.ArchaeologyTaskClaim) (domain.ArchaeologyTaskClaimResult, error) {
	if !boundedCoreText(command.LaunchID, 120, true) || !boundedCoreText(command.ProjectID, 120, true) ||
		!boundedCoreText(command.ThreadID, 120, true) || !boundedCoreText(command.CodexSessionID, 120, true) ||
		command.GrantDigest == [32]byte{} || command.ReportDigest == [32]byte{} || !command.ReportExpiresAt.After(s.now().UTC()) {
		return domain.ArchaeologyTaskClaimResult{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologyTaskClaimResult{}, err
	}
	defer tx.Rollback()
	var projectID, threadID, sessionID, state, grantExpiry string
	var grantDigest []byte
	var consumed sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT candidate_id,thread_id,codex_session_id,state,grant_digest,grant_expires_at,grant_consumed_at FROM archaeology_task_launches WHERE id=?`, command.LaunchID).Scan(&projectID, &threadID, &sessionID, &state, &grantDigest, &grantExpiry, &consumed)
	if err != nil {
		return domain.ArchaeologyTaskClaimResult{}, mapErr(err)
	}
	var stored [32]byte
	copy(stored[:], grantDigest)
	if projectID != command.ProjectID || threadID != command.ThreadID || sessionID != command.CodexSessionID ||
		!fixedDigestEqual(stored, command.GrantDigest) || !parseStamp(grantExpiry).After(s.now().UTC()) {
		return domain.ArchaeologyTaskClaimResult{}, domain.ErrForbidden
	}
	if state != "task_created" || consumed.Valid {
		return domain.ArchaeologyTaskClaimResult{}, domain.ErrConflict
	}
	now := s.now().UTC()
	_, err = tx.ExecContext(ctx, `UPDATE archaeology_task_launches SET state='claimed',grant_consumed_at=COALESCE(grant_consumed_at,?),report_digest=?,report_expires_at=?,report_consumed_at=NULL,report_request_digest=NULL,updated_at=? WHERE id=?`, stamp(now), command.ReportDigest[:], stamp(command.ReportExpiresAt), stamp(now), command.LaunchID)
	if err != nil {
		return domain.ArchaeologyTaskClaimResult{}, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologyTaskClaimResult{}, err
	}
	return domain.ArchaeologyTaskClaimResult{LaunchID: command.LaunchID, ProjectID: projectID, ThreadID: threadID, CodexSessionID: sessionID, ReportExpiresAt: command.ReportExpiresAt}, nil
}

func (s *Store) ReportArchaeologyTaskLaunch(ctx context.Context, command domain.ArchaeologyTaskReport) (domain.ArchaeologySession, error) {
	if !boundedCoreText(command.LaunchID, 120, true) || !boundedCoreText(command.ProjectID, 120, true) || !boundedCoreText(command.ThreadID, 120, true) ||
		!boundedCoreText(command.CodexSessionID, 120, true) || !boundedCoreText(command.RequestID, 200, true) || command.ReportDigest == [32]byte{} || len(command.Outcomes) < 1 || len(command.Outcomes) > 25 {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	nowTime := s.now().UTC()
	for _, outcome := range command.Outcomes {
		if outcome.ProjectID != command.ProjectID || !validArchaeologyOutcome(outcome, stamp(nowTime)) {
			return domain.ArchaeologySession{}, domain.ErrInvalid
		}
	}
	requestDigest := archaeologyDigest("task-report", struct {
		RequestID string
		Outcomes  []domain.ArchaeologyOutcome
	}{command.RequestID, command.Outcomes})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	var archaeologySessionID, principal, projectID, threadID, sessionID, state string
	var reportDigest, priorRequest []byte
	var reportExpiry sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT l.session_id,s.principal,l.candidate_id,l.thread_id,l.codex_session_id,l.state,l.report_digest,l.report_expires_at,l.report_request_digest FROM archaeology_task_launches l JOIN archaeology_sessions s ON s.id=l.session_id WHERE l.id=?`, command.LaunchID).Scan(&archaeologySessionID, &principal, &projectID, &threadID, &sessionID, &state, &reportDigest, &reportExpiry, &priorRequest)
	if err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	var stored [32]byte
	copy(stored[:], reportDigest)
	if projectID != command.ProjectID || threadID != command.ThreadID || sessionID != command.CodexSessionID || !fixedDigestEqual(stored, command.ReportDigest) {
		return domain.ArchaeologySession{}, domain.ErrForbidden
	}
	if state == "completed" {
		if len(priorRequest) == 32 && subtle.ConstantTimeCompare(priorRequest, requestDigest[:]) == 1 {
			if err = tx.Commit(); err != nil {
				return domain.ArchaeologySession{}, err
			}
			return s.ArchaeologySession(ctx, principal)
		}
		return domain.ArchaeologySession{}, domain.ErrConflict
	}
	if (state != "claimed" && state != "running") || !reportExpiry.Valid || !parseStamp(reportExpiry.String).After(nowTime) {
		return domain.ArchaeologySession{}, domain.ErrForbidden
	}
	runID := deterministicHistoricalID("ARR-", archaeologySessionID, projectID)
	var sources int
	for _, outcome := range command.Outcomes {
		sources += outcome.SourceCount
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_runs(id,session_id,candidate_id,state,phase_label,completed_units,total_units,outcomes_found,sources_examined,runner_key,created_at,updated_at) VALUES(?,?,?,'completed','Historian report received',?,?,?,?,?,?,?)`, runID, archaeologySessionID, projectID, sources, sources, len(command.Outcomes), sources, command.CodexSessionID, stamp(nowTime), stamp(nowTime))
	if err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	for _, outcome := range command.Outcomes {
		outcomeID := outcome.ID
		if outcomeID == "" {
			outcomeID = deterministicHistoricalID("ARO-", command.LaunchID, projectID, outcome.Title)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_outcomes(id,run_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, outcomeID, runID, projectID, outcome.Title, outcome.Summary, outcome.SourceCount, outcome.ProposalJSON, stamp(nowTime))
		if err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
		for position, source := range outcome.Provenance {
			_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_provenance(outcome_id,position,kind,stable_id,digest,occurred_at) VALUES(?,?,?,?,?,?)`, outcomeID, position, source.Kind, source.StableID, source.Digest, stamp(source.OccurredAt))
			if err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
		}
		for _, member := range outcome.Contributors {
			_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_outcome_contributors(outcome_id,session_id,contribution,demonstrated_strength,uncertainty,confidence) VALUES(?,?,?,?,?,?)`, outcomeID, member.SessionID, member.Contribution, member.DemonstratedStrength, member.Uncertainty, member.Confidence)
			if err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE archaeology_task_launches SET state='completed',report_consumed_at=?,report_request_digest=?,updated_at=? WHERE id=?`, stamp(nowTime), requestDigest[:], stamp(nowTime), command.LaunchID)
	if err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	var remaining int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_task_launches WHERE session_id=? AND state!='completed'`, archaeologySessionID).Scan(&remaining); err != nil {
		return domain.ArchaeologySession{}, err
	}
	if remaining == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='completed',revision=revision+1,updated_at=? WHERE id=?`, stamp(nowTime), archaeologySessionID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE archaeology_handoffs SET state='completed',updated_at=? WHERE session_id=?`, stamp(nowTime), archaeologySessionID)
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='running',revision=revision+1,updated_at=? WHERE id=?`, stamp(nowTime), archaeologySessionID)
	}
	if err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	return s.ArchaeologySession(ctx, principal)
}
