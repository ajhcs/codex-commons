package store

import (
	"context"
	"database/sql"
	"errors"

	"codex-commons/internal/domain"
)

func claimHandoffRequest(ctx context.Context, tx *sql.Tx, handoffID, key, operation, sessionID string, digest [32]byte, now string) (bool, error) {
	var prior []byte
	var priorOperation, priorSession string
	err := tx.QueryRowContext(ctx, `SELECT operation,session_id,request_digest FROM archaeology_handoff_requests WHERE handoff_id=? AND request_key=?`, handoffID, key).Scan(&priorOperation, &priorSession, &prior)
	if err == nil {
		if priorOperation != operation || priorSession != sessionID || string(prior) != string(digest[:]) {
			return false, domain.ErrConflict
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_handoff_requests(handoff_id,request_key,operation,session_id,request_digest,recorded_at) VALUES(?,?,?,?,?,?)`, handoffID, key, operation, sessionID, digest[:], now)
	return false, mapErr(err)
}

func (s *Store) ClaimArchaeologyHandoff(ctx context.Context, command domain.ArchaeologyHandoffClaim) (domain.ArchaeologySession, error) {
	if !boundedCoreText(command.HandoffID, 120, true) || !boundedCoreText(command.RequestID, 200, true) || !boundedCoreText(command.SessionID, 200, true) {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	now := stamp(s.now().UTC())
	replay, err := claimHandoffRequest(ctx, tx, command.HandoffID, command.RequestID, "claim", command.SessionID, archaeologyDigest("claim", command.SessionID), now)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	var principal, state, claimedBy string
	err = tx.QueryRowContext(ctx, `SELECT s.principal,h.state,h.claimed_by FROM archaeology_handoffs h JOIN archaeology_sessions s ON s.id=h.session_id WHERE h.id=?`, command.HandoffID).Scan(&principal, &state, &claimedBy)
	if err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	if !replay {
		if state != "ready_to_claim" {
			return domain.ArchaeologySession{}, domain.ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_handoffs SET state='claimed',claimed_by=?,claimed_at=?,updated_at=? WHERE id=?`, command.SessionID, now, now, command.HandoffID); err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='running',revision=revision+1,updated_at=? WHERE principal=?`, now, principal); err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
	} else if claimedBy != command.SessionID {
		return domain.ArchaeologySession{}, domain.ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	return s.ArchaeologySession(ctx, principal)
}

func (s *Store) ReportArchaeologyHandoff(ctx context.Context, command domain.ArchaeologyHandoffReport) (domain.ArchaeologySession, error) {
	if !boundedCoreText(command.HandoffID, 120, true) || !boundedCoreText(command.RequestID, 200, true) || !boundedCoreText(command.SessionID, 200, true) || len(command.Outcomes) < 1 || len(command.Outcomes) > 25 {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	nowTime := s.now().UTC()
	now := stamp(nowTime)
	for _, outcome := range command.Outcomes {
		if !validArchaeologyOutcome(outcome, now) {
			return domain.ArchaeologySession{}, domain.ErrInvalid
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	replay, err := claimHandoffRequest(ctx, tx, command.HandoffID, command.RequestID, "report", command.SessionID, archaeologyDigest("report", command.Outcomes), now)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	var principal, archaeologySessionID, state, claimedBy string
	err = tx.QueryRowContext(ctx, `SELECT s.principal,s.id,h.state,h.claimed_by FROM archaeology_handoffs h JOIN archaeology_sessions s ON s.id=h.session_id WHERE h.id=?`, command.HandoffID).Scan(&principal, &archaeologySessionID, &state, &claimedBy)
	if err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	if !replay {
		if state != "claimed" || claimedBy != command.SessionID {
			return domain.ArchaeologySession{}, domain.ErrConflict
		}
		rows, queryErr := tx.QueryContext(ctx, `SELECT id FROM archaeology_candidates WHERE session_id=? AND selected=1 ORDER BY id`, archaeologySessionID)
		if queryErr != nil {
			return domain.ArchaeologySession{}, queryErr
		}
		var selectedIDs []string
		selected := map[string]bool{}
		for rows.Next() {
			var candidateID string
			if queryErr = rows.Scan(&candidateID); queryErr != nil {
				rows.Close()
				return domain.ArchaeologySession{}, queryErr
			}
			selectedIDs = append(selectedIDs, candidateID)
			selected[candidateID] = true
		}
		if queryErr = rows.Close(); queryErr != nil {
			return domain.ArchaeologySession{}, queryErr
		}
		type projectCounts struct{ outcomes, sources int }
		counts := map[string]projectCounts{}
		for _, outcome := range command.Outcomes {
			if !selected[outcome.ProjectID] {
				return domain.ArchaeologySession{}, domain.ErrInvalid
			}
			current := counts[outcome.ProjectID]
			current.outcomes++
			current.sources += outcome.SourceCount
			counts[outcome.ProjectID] = current
		}
		for _, candidateID := range selectedIDs {
			count := counts[candidateID]
			runID := deterministicHistoricalID("ARR-", archaeologySessionID, candidateID)
			if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_runs(id,session_id,candidate_id,state,phase_label,completed_units,total_units,outcomes_found,sources_examined,runner_key,created_at,updated_at) VALUES(?,?,?,'completed','Historian report received',?,?,?,?,?,?,?)`, runID, archaeologySessionID, candidateID, count.sources, count.sources, count.outcomes, count.sources, command.SessionID, now, now); err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
		}
		for _, outcome := range command.Outcomes {
			runID := deterministicHistoricalID("ARR-", archaeologySessionID, outcome.ProjectID)
			outcomeID := outcome.ID
			if outcomeID == "" {
				outcomeID = deterministicHistoricalID("ARO-", command.HandoffID, outcome.ProjectID, outcome.Title)
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_outcomes(id,run_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, outcomeID, runID, outcome.ProjectID, outcome.Title, outcome.Summary, outcome.SourceCount, outcome.ProposalJSON, now); err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
			for position, source := range outcome.Provenance {
				if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_provenance(outcome_id,position,kind,stable_id,digest,occurred_at) VALUES(?,?,?,?,?,?)`, outcomeID, position, source.Kind, source.StableID, source.Digest, stamp(source.OccurredAt)); err != nil {
					return domain.ArchaeologySession{}, mapErr(err)
				}
			}
			for _, member := range outcome.Contributors {
				if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_outcome_contributors(outcome_id,session_id,contribution,demonstrated_strength,uncertainty,confidence) VALUES(?,?,?,?,?,?)`, outcomeID, member.SessionID, member.Contribution, member.DemonstratedStrength, member.Uncertainty, member.Confidence); err != nil {
					return domain.ArchaeologySession{}, mapErr(err)
				}
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_handoffs SET state='completed',updated_at=? WHERE id=?`, now, command.HandoffID); err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='completed',revision=revision+1,updated_at=? WHERE id=?`, now, archaeologySessionID); err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	return s.ArchaeologySession(ctx, principal)
}
