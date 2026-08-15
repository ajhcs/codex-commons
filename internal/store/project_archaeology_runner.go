package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"codex-commons/internal/domain"
)

// ClaimArchaeologyRun atomically enforces the user-selected concurrency cap.
// ErrConflict means there is currently no claimable capacity or queued run.
func (s *Store) ClaimArchaeologyRun(ctx context.Context, principal, runnerKey string) (domain.ArchaeologyRun, error) {
	if !boundedCoreText(principal, 200, true) || !boundedCoreText(runnerKey, 200, true) {
		return domain.ArchaeologyRun{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologyRun{}, err
	}
	defer tx.Rollback()
	var sessionID string
	var maximum int
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT id,max_concurrency,state FROM archaeology_sessions WHERE principal=?`, principal).Scan(&sessionID, &maximum, &state); err != nil {
		return domain.ArchaeologyRun{}, mapErr(err)
	}
	if state != "running" {
		return domain.ArchaeologyRun{}, domain.ErrConflict
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_runs WHERE session_id=? AND state='running'`, sessionID).Scan(&active); err != nil {
		return domain.ArchaeologyRun{}, err
	}
	if active >= maximum {
		return domain.ArchaeologyRun{}, domain.ErrConflict
	}
	var out domain.ArchaeologyRun
	var total sql.NullInt64
	var updated string
	err = tx.QueryRowContext(ctx, `SELECT id,candidate_id,state,phase_label,completed_units,total_units,outcomes_found,sources_examined,error,updated_at FROM archaeology_runs WHERE session_id=? AND state='queued' ORDER BY created_at,id LIMIT 1`, sessionID).Scan(&out.ID, &out.ProjectID, &out.State, &out.PhaseLabel, &out.CompletedUnits, &total, &out.OutcomesFound, &out.SourcesExamined, &out.Error, &updated)
	if err != nil {
		return domain.ArchaeologyRun{}, mapErr(err)
	}
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE archaeology_runs SET state='running',runner_key=?,phase_label='Historian examining selected sources',updated_at=? WHERE id=? AND state='queued'`, runnerKey, stamp(now), out.ID)
	if err != nil {
		return domain.ArchaeologyRun{}, mapErr(err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return domain.ArchaeologyRun{}, domain.ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologyRun{}, err
	}
	out.State = "running"
	out.RunnerKey = runnerKey
	out.PhaseLabel = "Historian examining selected sources"
	out.UpdatedAt = now
	if total.Valid {
		value := int(total.Int64)
		out.TotalUnits = &value
	}
	return out, nil
}

func validArchaeologyOutcome(outcome domain.ArchaeologyOutcome, now string) bool {
	if !boundedCoreText(outcome.ID, 120, false) || !boundedCoreText(outcome.ProjectID, 120, true) || !boundedCoreText(outcome.Title, 300, true) || !boundedCoreText(outcome.Summary, 4000, true) || outcome.SourceCount < 1 || outcome.SourceCount > 1000 || len(outcome.Provenance) < 1 || len(outcome.Provenance) > 100 || len(outcome.Provenance) > outcome.SourceCount || len(outcome.Contributors) > 100 || len(outcome.ProposalJSON) > domain.ArchaeologyNativeProposalMaxBytes || !json.Valid([]byte(outcome.ProposalJSON)) || !strings.HasPrefix(strings.TrimSpace(outcome.ProposalJSON), "{") {
		return false
	}
	lowered := strings.ToLower(outcome.ProposalJSON)
	for _, forbidden := range []string{`"prompt"`, `"reasoning"`, `"transcript"`, `"secret"`, `"private_data"`} {
		if strings.Contains(lowered, forbidden) {
			return false
		}
	}
	type provenanceKey struct{ kind, stableID, digest string }
	seenSources := make(map[provenanceKey]bool, len(outcome.Provenance))
	for _, source := range outcome.Provenance {
		if source.Kind != "git" && source.Kind != "docs" && source.Kind != "codex_history" {
			return false
		}
		if !validHistoricalSource(domain.HistoricalSource{Kind: source.Kind, StableID: source.StableID, Digest: source.Digest, OccurredAt: source.OccurredAt}, parseStamp(now)) {
			return false
		}
		key := provenanceKey{source.Kind, source.StableID, source.Digest}
		if seenSources[key] {
			return false
		}
		seenSources[key] = true
	}
	seenMembers := make(map[string]bool, len(outcome.Contributors))
	for _, member := range outcome.Contributors {
		if !boundedCoreText(member.SessionID, 200, true) || !boundedCoreText(member.Contribution, 1000, true) || !boundedCoreText(member.DemonstratedStrength, 300, false) || !boundedCoreText(member.Uncertainty, 500, false) || !domain.HistoricalConfidences[member.Confidence] || seenMembers[member.SessionID] {
			return false
		}
		seenMembers[member.SessionID] = true
	}
	return true
}

// UpdateArchaeologyRun is the durable reporter seam used by a supported task
// adapter. It stores truthful units and review proposals, never percentages.
func (s *Store) UpdateArchaeologyRun(ctx context.Context, update domain.ArchaeologyRunUpdate) (domain.ArchaeologySession, error) {
	if !boundedCoreText(update.Principal, 200, true) || !boundedCoreText(update.RunID, 120, true) || !boundedCoreText(update.RunnerKey, 200, true) || !boundedCoreText(update.PhaseLabel, 120, false) || !boundedCoreText(update.Error, 500, false) || update.CompletedUnits < 0 || update.SourcesExamined < 0 {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	if update.TotalUnits != nil && *update.TotalUnits < update.CompletedUnits {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	if update.State != "running" && update.State != "paused" && update.State != "canceled" && update.State != "completed" && update.State != "failed" {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	if update.State != "completed" && len(update.Outcomes) > 0 {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	now := s.now().UTC()
	for _, outcome := range update.Outcomes {
		if !validArchaeologyOutcome(outcome, stamp(now)) {
			return domain.ArchaeologySession{}, domain.ErrInvalid
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	var sessionID, currentProject, currentState, currentRunner string
	if err = tx.QueryRowContext(ctx, `SELECT r.session_id,r.candidate_id,r.state,r.runner_key FROM archaeology_runs r JOIN archaeology_sessions s ON s.id=r.session_id WHERE r.id=? AND s.principal=?`, update.RunID, update.Principal).Scan(&sessionID, &currentProject, &currentState, &currentRunner); err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	if currentRunner != update.RunnerKey || currentState != "running" && currentState != "pause_requested" && currentState != "cancel_requested" {
		return domain.ArchaeologySession{}, domain.ErrConflict
	}
	if currentState == "pause_requested" && update.State != "paused" && update.State != "canceled" && update.State != "failed" {
		return domain.ArchaeologySession{}, domain.ErrConflict
	}
	if currentState == "cancel_requested" && update.State != "canceled" && update.State != "failed" {
		return domain.ArchaeologySession{}, domain.ErrConflict
	}
	if update.State == "completed" {
		for _, outcome := range update.Outcomes {
			if outcome.ProjectID != currentProject {
				return domain.ArchaeologySession{}, domain.ErrInvalid
			}
			id := outcome.ID
			if id == "" {
				id = deterministicHistoricalID("ARO-", update.RunID, outcome.Title)
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_outcomes(id,run_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, update.RunID, outcome.ProjectID, outcome.Title, outcome.Summary, outcome.SourceCount, outcome.ProposalJSON, stamp(now))
			if err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
			for position, source := range outcome.Provenance {
				_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_provenance(outcome_id,position,kind,stable_id,digest,occurred_at) VALUES(?,?,?,?,?,?)`, id, position, source.Kind, source.StableID, source.Digest, stamp(source.OccurredAt))
				if err != nil {
					return domain.ArchaeologySession{}, mapErr(err)
				}
			}
			for _, member := range outcome.Contributors {
				_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_outcome_contributors(outcome_id,session_id,contribution,demonstrated_strength,uncertainty,confidence) VALUES(?,?,?,?,?,?)`, id, member.SessionID, member.Contribution, member.DemonstratedStrength, member.Uncertainty, member.Confidence)
				if err != nil {
					return domain.ArchaeologySession{}, mapErr(err)
				}
			}
		}
	}
	var total any
	if update.TotalUnits != nil {
		total = *update.TotalUnits
	}
	_, err = tx.ExecContext(ctx, `UPDATE archaeology_runs SET state=?,phase_label=?,completed_units=?,total_units=?,outcomes_found=?,sources_examined=?,error=?,updated_at=? WHERE id=?`, update.State, update.PhaseLabel, update.CompletedUnits, total, len(update.Outcomes), update.SourcesExamined, update.Error, stamp(now), update.RunID)
	if err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	var unfinished, failed, paused, canceled int
	if err = tx.QueryRowContext(ctx, `SELECT sum(state NOT IN ('completed','failed','canceled')),sum(state='failed'),sum(state='paused'),sum(state='canceled') FROM archaeology_runs WHERE session_id=?`, sessionID).Scan(&unfinished, &failed, &paused, &canceled); err != nil {
		return domain.ArchaeologySession{}, err
	}
	sessionState := "running"
	if unfinished == 0 {
		if failed > 0 {
			sessionState = "failed"
		} else if canceled > 0 {
			sessionState = "canceled"
		} else {
			sessionState = "completed"
		}
	} else if paused == unfinished {
		sessionState = "paused"
	}
	_, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state=?,revision=revision+1,updated_at=? WHERE id=?`, sessionState, stamp(now), sessionID)
	if err != nil {
		return domain.ArchaeologySession{}, mapErr(err)
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	return s.ArchaeologySession(ctx, update.Principal)
}
