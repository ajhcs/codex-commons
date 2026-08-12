package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"codex-commons/internal/domain"
)

func archaeologySessionID(principal string) string {
	return deterministicHistoricalID("AR-", principal)
}

func (s *Store) ArchaeologySession(ctx context.Context, principal string) (domain.ArchaeologySession, error) {
	if strings.TrimSpace(principal) == "" {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	var out domain.ArchaeologySession
	var discovered sql.NullString
	var updated string
	var git, docs, history int
	err := s.db.QueryRowContext(ctx, `SELECT id,principal,state,discovery_state,discovery_error,source_roots_scanned,depth,source_git,source_docs,source_codex_history,max_concurrency,revision,discovered_at,updated_at FROM archaeology_sessions WHERE principal=?`, principal).Scan(&out.ID, &out.Principal, &out.State, &out.DiscoveryState, &out.DiscoveryError, &out.SourceRootsScanned, &out.Config.Depth, &git, &docs, &history, &out.Config.MaxConcurrency, &out.Revision, &discovered, &updated)
	if err != nil {
		return out, mapErr(err)
	}
	out.MetadataOnly = true
	out.Config.Sources = domain.ArchaeologySources{Git: git == 1, Docs: docs == 1, CodexHistory: history == 1}
	out.UpdatedAt = parseStamp(updated)
	if discovered.Valid {
		out.DiscoveredAt = parseStamp(discovered.String)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,canonical_project_id,name,path_label,repository_label,last_activity_at,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,selected,from_codex_metadata,from_configured_root,codex_thread_count FROM archaeology_candidates WHERE session_id=? ORDER BY name,id`, out.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var c domain.ArchaeologyCandidate
		var last sql.NullString
		var a, b, d, selected, fromCodex, fromRoot int
		if err = rows.Scan(&c.ID, &c.CanonicalProjectID, &c.Name, &c.PathLabel, &c.RepositoryLabel, &last, &a, &b, &d, &c.DurationMinSeconds, &c.DurationMaxSeconds, &c.RelativeCost, &c.PrivacyNote, &selected, &fromCodex, &fromRoot, &c.CodexThreadCount); err != nil {
			rows.Close()
			return out, err
		}
		c.HasGit, c.HasDocs, c.HasCodexHistory, c.Selected = a == 1, b == 1, d == 1, selected == 1
		c.FromCodexMetadata, c.FromConfiguredRoot = fromCodex == 1, fromRoot == 1
		if last.Valid {
			c.LastActivityAt = parseStamp(last.String)
		}
		if c.Selected {
			out.Config.SelectedProjectIDs = append(out.Config.SelectedProjectIDs, c.ID)
		}
		out.Candidates = append(out.Candidates, c)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id,candidate_id,state,phase_label,completed_units,total_units,outcomes_found,sources_examined,error,runner_key,updated_at FROM archaeology_runs WHERE session_id=? ORDER BY created_at,id`, out.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var run domain.ArchaeologyRun
		var total sql.NullInt64
		var at string
		if err = rows.Scan(&run.ID, &run.ProjectID, &run.State, &run.PhaseLabel, &run.CompletedUnits, &total, &run.OutcomesFound, &run.SourcesExamined, &run.Error, &run.RunnerKey, &at); err != nil {
			rows.Close()
			return out, err
		}
		if total.Valid {
			v := int(total.Int64)
			run.TotalUnits = &v
		}
		run.UpdatedAt = parseStamp(at)
		out.Runs = append(out.Runs, run)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, "SELECT id,candidate_id,state,thread_id,codex_session_id,turn_id,client_message_id,error,grant_expires_at,created_at,updated_at FROM archaeology_task_launches WHERE session_id=? ORDER BY created_at,id", out.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var launch domain.ArchaeologyTaskLaunch
		var expires, created, launchUpdated string
		if err = rows.Scan(&launch.ID, &launch.ProjectID, &launch.State, &launch.ThreadID, &launch.CodexSessionID, &launch.TurnID, &launch.ClientMessageID, &launch.Error, &expires, &created, &launchUpdated); err != nil {
			rows.Close()
			return out, err
		}
		launch.GrantExpiresAt, launch.CreatedAt, launch.UpdatedAt = parseStamp(expires), parseStamp(created), parseStamp(launchUpdated)
		out.TaskLaunches = append(out.TaskLaunches, launch)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT o.id,o.project_id,o.title,o.summary,o.source_count,o.proposal_json FROM archaeology_outcomes o JOIN archaeology_runs r ON r.id=o.run_id WHERE r.session_id=? ORDER BY o.created_at,o.id`, out.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var o domain.ArchaeologyOutcome
		if err = rows.Scan(&o.ID, &o.ProjectID, &o.Title, &o.Summary, &o.SourceCount, &o.ProposalJSON); err != nil {
			rows.Close()
			return out, err
		}
		out.Outcomes = append(out.Outcomes, o)
	}
	rows.Close()
	for i := range out.Outcomes {
		item := &out.Outcomes[i]
		p, queryErr := s.db.QueryContext(ctx, `SELECT kind,stable_id,digest,occurred_at FROM archaeology_provenance WHERE outcome_id=? ORDER BY position`, item.ID)
		if queryErr != nil {
			return out, queryErr
		}
		for p.Next() {
			var source domain.ArchaeologyProvenance
			var at string
			if queryErr = p.Scan(&source.Kind, &source.StableID, &source.Digest, &at); queryErr != nil {
				p.Close()
				return out, queryErr
			}
			source.OccurredAt = parseStamp(at)
			item.Provenance = append(item.Provenance, source)
		}
		p.Close()
		c, queryErr := s.db.QueryContext(ctx, `SELECT session_id,contribution,demonstrated_strength,uncertainty,confidence FROM archaeology_outcome_contributors WHERE outcome_id=? ORDER BY session_id`, item.ID)
		if queryErr != nil {
			return out, queryErr
		}
		for c.Next() {
			var member domain.ArchaeologyContributor
			if queryErr = c.Scan(&member.SessionID, &member.Contribution, &member.DemonstratedStrength, &member.Uncertainty, &member.Confidence); queryErr != nil {
				c.Close()
				return out, queryErr
			}
			item.Contributors = append(item.Contributors, member)
		}
		c.Close()
	}
	var handoff domain.ArchaeologyHandoff
	var claimed sql.NullString
	var handoffCreated, handoffUpdated string
	err = s.db.QueryRowContext(ctx, `SELECT id,state,pack_json,claimed_by,claimed_at,failure,created_at,updated_at FROM archaeology_handoffs WHERE session_id=?`, out.ID).Scan(&handoff.ID, &handoff.State, &handoff.PackJSON, &handoff.ClaimedBy, &claimed, &handoff.Failure, &handoffCreated, &handoffUpdated)
	if err == nil {
		handoff.CreatedAt, handoff.UpdatedAt = parseStamp(handoffCreated), parseStamp(handoffUpdated)
		if claimed.Valid {
			handoff.ClaimedAt = parseStamp(claimed.String)
		}
		out.Handoff = &handoff
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out.NativeBatches, err = s.loadArchaeologyNative(ctx, out.ID)
	if err != nil {
		return out, err
	}
	nativeOutcomes, nativeErr := s.loadArchaeologyNativeOutcomes(ctx, out.ID)
	if nativeErr != nil {
		return out, nativeErr
	}
	out.Outcomes = append(out.Outcomes, nativeOutcomes...)
	return out, nil
}

func ensureArchaeologySession(ctx context.Context, tx *sql.Tx, principal string, now time.Time) (string, error) {
	id := archaeologySessionID(principal)
	at := stamp(now)
	_, err := tx.ExecContext(ctx, `INSERT INTO archaeology_sessions(id,principal,state,discovery_state,created_at,updated_at) VALUES(?,?,'draft','idle',?,?) ON CONFLICT(principal) DO NOTHING`, id, principal, at, at)
	return id, mapErr(err)
}
func archaeologyDigest(operation string, value any) [32]byte {
	body, _ := json.Marshal(struct {
		Operation string `json:"operation"`
		Value     any    `json:"value"`
	}{operation, value})
	return sha256.Sum256(body)
}
func claimArchaeologyRequest(ctx context.Context, tx *sql.Tx, principal, key, operation, sessionID string, digest [32]byte, now time.Time) (bool, error) {
	var prior []byte
	var op string
	err := tx.QueryRowContext(ctx, `SELECT operation,request_digest FROM archaeology_requests WHERE principal=? AND request_key=?`, principal, key).Scan(&op, &prior)
	if err == nil {
		if op != operation || string(prior) != string(digest[:]) {
			return false, domain.ErrConflict
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_requests(principal,request_key,operation,request_digest,session_id,recorded_at) VALUES(?,?,?,?,?,?)`, principal, key, operation, digest[:], sessionID, stamp(now))
	return false, mapErr(err)
}
func validArchaeologyCandidate(c domain.ArchaeologyCandidate) bool {
	return boundedCoreText(c.ID, 120, true) && projectIDPattern.MatchString(c.CanonicalProjectID) && c.CanonicalProjectID != domain.TopicGeneral && boundedCoreText(c.Name, 200, true) && boundedCoreText(c.PathLabel, 300, true) && boundedCoreText(c.RepositoryLabel, 300, false) && (c.RelativeCost == "low" || c.RelativeCost == "medium" || c.RelativeCost == "high") && c.DurationMinSeconds >= 0 && c.DurationMaxSeconds >= c.DurationMinSeconds && c.DurationMaxSeconds <= 86400 && boundedCoreText(c.PrivacyNote, 500, false)
}

func (s *Store) ReplaceArchaeologyDiscovery(ctx context.Context, m domain.ArchaeologyMutation, discovery domain.ArchaeologyDiscovery) (domain.ArchaeologySession, error) {
	if m.Principal == "" || m.RequestID == "" || len(discovery.Candidates) > 100 || discovery.SourceRootsScanned < 0 || discovery.SourceRootsScanned > 100 {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	seen := map[string]bool{}
	for index := range discovery.Candidates {
		c := &discovery.Candidates[index]
		if c.CanonicalProjectID == "" {
			c.CanonicalProjectID = c.ID
		}
		if !validArchaeologyCandidate(*c) || seen[c.ID] {
			return domain.ArchaeologySession{}, domain.ErrInvalid
		}
		seen[c.ID] = true
	}
	sort.Slice(discovery.Candidates, func(i, j int) bool { return discovery.Candidates[i].ID < discovery.Candidates[j].ID })
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	sid, err := ensureArchaeologySession(ctx, tx, m.Principal, now)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	replay, err := claimArchaeologyRequest(ctx, tx, m.Principal, m.RequestID, "discover", sid, archaeologyDigest("discover", nil), now)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	if !replay {
		var references int
		if err = tx.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM archaeology_runs WHERE session_id=?) +
(SELECT count(*) FROM archaeology_task_launches WHERE session_id=?) +
(SELECT count(*) FROM archaeology_candidate_projects WHERE session_id=?) +
(SELECT count(*) FROM archaeology_native_jobs WHERE session_id=?)`, sid, sid, sid, sid).Scan(&references); err != nil {
			return domain.ArchaeologySession{}, err
		}
		if references > 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE archaeology_candidates
SET selected=0,from_codex_metadata=0,from_configured_root=0,codex_thread_count=0,
privacy_note='Retained for historical audit; refresh metadata before selecting.'
WHERE session_id=?`, sid); err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
			for _, c := range discovery.Candidates {
				var prior string
				readErr := tx.QueryRowContext(ctx, `SELECT canonical_project_id FROM archaeology_candidates WHERE session_id=? AND id=?`, sid, c.ID).Scan(&prior)
				if readErr == nil && prior != "" && prior != c.CanonicalProjectID {
					return domain.ArchaeologySession{}, domain.ErrConflict
				}
				if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
					return domain.ArchaeologySession{}, readErr
				}
				var last any
				if !c.LastActivityAt.IsZero() {
					last = stamp(c.LastActivityAt)
				}
				_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_candidates(session_id,id,canonical_project_id,name,path_label,repository_label,last_activity_at,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,selected,from_codex_metadata,from_configured_root,codex_thread_count) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?)
ON CONFLICT(session_id,id) DO UPDATE SET canonical_project_id=excluded.canonical_project_id,name=excluded.name,path_label=excluded.path_label,repository_label=excluded.repository_label,last_activity_at=excluded.last_activity_at,has_git=excluded.has_git,has_docs=excluded.has_docs,has_codex_history=excluded.has_codex_history,duration_min_seconds=excluded.duration_min_seconds,duration_max_seconds=excluded.duration_max_seconds,relative_cost=excluded.relative_cost,privacy_note=excluded.privacy_note,selected=0,from_codex_metadata=excluded.from_codex_metadata,from_configured_root=excluded.from_configured_root,codex_thread_count=excluded.codex_thread_count`, sid, c.ID, c.CanonicalProjectID, c.Name, c.PathLabel, c.RepositoryLabel, last, c.HasGit, c.HasDocs, c.HasCodexHistory, c.DurationMinSeconds, c.DurationMaxSeconds, c.RelativeCost, c.PrivacyNote, c.FromCodexMetadata, c.FromConfiguredRoot, c.CodexThreadCount)
				if err != nil {
					return domain.ArchaeologySession{}, mapErr(err)
				}
			}
		} else {
			if _, err = tx.ExecContext(ctx, `DELETE FROM archaeology_candidates WHERE session_id=?`, sid); err != nil {
				return domain.ArchaeologySession{}, mapErr(err)
			}
			for _, c := range discovery.Candidates {
				var last any
				if !c.LastActivityAt.IsZero() {
					last = stamp(c.LastActivityAt)
				}
				_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_candidates(session_id,id,canonical_project_id,name,path_label,repository_label,last_activity_at,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,selected,from_codex_metadata,from_configured_root,codex_thread_count) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?)`, sid, c.ID, c.CanonicalProjectID, c.Name, c.PathLabel, c.RepositoryLabel, last, c.HasGit, c.HasDocs, c.HasCodexHistory, c.DurationMinSeconds, c.DurationMaxSeconds, c.RelativeCost, c.PrivacyNote, c.FromCodexMetadata, c.FromConfiguredRoot, c.CodexThreadCount)
				if err != nil {
					return domain.ArchaeologySession{}, mapErr(err)
				}
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET discovery_state='ready',source_roots_scanned=?,discovered_at=?,updated_at=?,revision=revision+1 WHERE id=?`, discovery.SourceRootsScanned, stamp(now), stamp(now), sid)
		if err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	return s.ArchaeologySession(ctx, m.Principal)
}

func validArchaeologyConfig(c domain.ArchaeologyConfig) bool {
	return (c.Depth == "quick" || c.Depth == "standard" || c.Depth == "deep") && c.MaxConcurrency >= 1 && c.MaxConcurrency <= 2 && (c.Sources.Git || c.Sources.Docs || c.Sources.CodexHistory)
}
func (s *Store) ConfigureArchaeology(ctx context.Context, m domain.ArchaeologyMutation) (domain.ArchaeologySession, error) {
	if m.Principal == "" || m.RequestID == "" || !validArchaeologyConfig(m.Config) || len(m.Config.SelectedProjectIDs) > 100 {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	ids := append([]string(nil), m.Config.SelectedProjectIDs...)
	sort.Strings(ids)
	for i, id := range ids {
		if !boundedCoreText(id, 120, true) || (i > 0 && ids[i-1] == id) {
			return domain.ArchaeologySession{}, domain.ErrInvalid
		}
	}
	m.Config.SelectedProjectIDs = ids
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	sid, err := ensureArchaeologySession(ctx, tx, m.Principal, now)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	replay, err := claimArchaeologyRequest(ctx, tx, m.Principal, m.RequestID, "configure", sid, archaeologyDigest("configure", struct {
		BaseRevision int64
		Config       domain.ArchaeologyConfig
	}{m.BaseRevision, m.Config}), now)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	if !replay {
		var revision int64
		var state string
		if err = tx.QueryRowContext(ctx, `SELECT revision,state FROM archaeology_sessions WHERE id=?`, sid).Scan(&revision, &state); err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
		if revision != m.BaseRevision || state != "draft" {
			return domain.ArchaeologySession{}, domain.ErrConflict
		}
		for _, id := range ids {
			var git, docs, history int
			if err = tx.QueryRowContext(ctx, `SELECT has_git,has_docs,has_codex_history FROM archaeology_candidates WHERE session_id=? AND id=?`, sid, id).Scan(&git, &docs, &history); err != nil {
				return domain.ArchaeologySession{}, domain.ErrInvalid
			}
			if !(m.Config.Sources.Git && git == 1 || m.Config.Sources.Docs && docs == 1 || m.Config.Sources.CodexHistory && history == 1) {
				return domain.ArchaeologySession{}, domain.ErrInvalid
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE archaeology_candidates SET selected=0 WHERE session_id=?`, sid); err != nil {
			return domain.ArchaeologySession{}, err
		}
		for _, id := range ids {
			if _, err = tx.ExecContext(ctx, `UPDATE archaeology_candidates SET selected=1 WHERE session_id=? AND id=?`, sid, id); err != nil {
				return domain.ArchaeologySession{}, err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET depth=?,source_git=?,source_docs=?,source_codex_history=?,max_concurrency=?,revision=revision+1,updated_at=? WHERE id=?`, m.Config.Depth, m.Config.Sources.Git, m.Config.Sources.Docs, m.Config.Sources.CodexHistory, m.Config.MaxConcurrency, stamp(now), sid)
		if err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	return s.ArchaeologySession(ctx, m.Principal)
}

func (s *Store) StartArchaeology(ctx context.Context, m domain.ArchaeologyMutation) (domain.ArchaeologySession, error) {
	return s.transitionArchaeology(ctx, m, "start")
}
func (s *Store) PauseArchaeology(ctx context.Context, m domain.ArchaeologyMutation) (domain.ArchaeologySession, error) {
	return s.transitionArchaeology(ctx, m, "pause")
}
func (s *Store) ResumeArchaeology(ctx context.Context, m domain.ArchaeologyMutation) (domain.ArchaeologySession, error) {
	return s.transitionArchaeology(ctx, m, "resume")
}
func (s *Store) CancelArchaeology(ctx context.Context, m domain.ArchaeologyMutation) (domain.ArchaeologySession, error) {
	return s.transitionArchaeology(ctx, m, "cancel")
}
func (s *Store) transitionArchaeology(ctx context.Context, m domain.ArchaeologyMutation, operation string) (domain.ArchaeologySession, error) {
	if m.Principal == "" || m.RequestID == "" {
		return domain.ArchaeologySession{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	sid, err := ensureArchaeologySession(ctx, tx, m.Principal, now)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	replay, err := claimArchaeologyRequest(ctx, tx, m.Principal, m.RequestID, operation, sid, archaeologyDigest(operation, m.BaseRevision), now)
	if err != nil {
		return domain.ArchaeologySession{}, err
	}
	if !replay {
		var state string
		var revision int64
		if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM archaeology_sessions WHERE id=?`, sid).Scan(&state, &revision); err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
		if revision != m.BaseRevision {
			return domain.ArchaeologySession{}, domain.ErrConflict
		}
		if operation != "start" {
			var exported int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_handoffs WHERE session_id=?`, sid).Scan(&exported); err != nil {
				return domain.ArchaeologySession{}, err
			}
			if exported > 0 {
				return domain.ArchaeologySession{}, domain.ErrUnavailable
			}
		}
		next := ""
		switch operation {
		case "start":
			if state != "draft" {
				return domain.ArchaeologySession{}, domain.ErrConflict
			}
			var selected int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_candidates WHERE session_id=? AND selected=1`, sid).Scan(&selected); err != nil {
				return domain.ArchaeologySession{}, err
			}
			if selected == 0 {
				return domain.ArchaeologySession{}, domain.ErrInvalid
			}
			next = "draft"
			handoffID := deterministicHistoricalID("ARH-", sid)
			var handoffState, claimedBy string
			var claimedAt sql.NullString
			handoffErr := tx.QueryRowContext(ctx, `SELECT state,claimed_by,claimed_at FROM archaeology_handoffs WHERE id=? AND session_id=?`, handoffID, sid).Scan(&handoffState, &claimedBy, &claimedAt)
			switch {
			case errors.Is(handoffErr, sql.ErrNoRows):
				_, err = tx.ExecContext(ctx, `INSERT INTO archaeology_handoffs(id,session_id,state,pack_json,created_at,updated_at) VALUES(?,?,'ready_to_claim','{}',?,?)`, handoffID, sid, stamp(now), stamp(now))
				if err != nil {
					return domain.ArchaeologySession{}, mapErr(err)
				}
			case handoffErr != nil:
				return domain.ArchaeologySession{}, handoffErr
			default:
				// Schema-12 upgrades may retain one never-claimed handoff from the
				// former copy-pack flow. Preserve that row byte-for-byte as audit
				// history, but allow the explicit new start to use direct tasks.
				var launches, outcomes, handoffRequests int
				if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_task_launches WHERE session_id=?`, sid).Scan(&launches); err != nil {
					return domain.ArchaeologySession{}, err
				}
				if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_outcomes o JOIN archaeology_runs r ON r.id=o.run_id WHERE r.session_id=?`, sid).Scan(&outcomes); err != nil {
					return domain.ArchaeologySession{}, err
				}
				if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM archaeology_handoff_requests WHERE handoff_id=?`, handoffID).Scan(&handoffRequests); err != nil {
					return domain.ArchaeologySession{}, err
				}
				if handoffState != "ready_to_claim" || claimedBy != "" || claimedAt.Valid || handoffRequests != 0 || launches != 0 || outcomes != 0 {
					return domain.ArchaeologySession{}, domain.ErrConflict
				}
			}
		case "pause":
			if state != "running" {
				return domain.ArchaeologySession{}, domain.ErrConflict
			}
			next = "pause_requested"
			_, err = tx.ExecContext(ctx, `UPDATE archaeology_runs SET state='pause_requested',updated_at=? WHERE session_id=? AND state IN ('queued','running')`, stamp(now), sid)
		case "resume":
			if state != "paused" && state != "pause_requested" {
				return domain.ArchaeologySession{}, domain.ErrConflict
			}
			next = "running"
			_, err = tx.ExecContext(ctx, `UPDATE archaeology_runs SET state='queued',updated_at=? WHERE session_id=? AND state IN ('paused','pause_requested')`, stamp(now), sid)
		case "cancel":
			if state == "completed" || state == "canceled" {
				return domain.ArchaeologySession{}, domain.ErrConflict
			}
			next = "cancel_requested"
			_, err = tx.ExecContext(ctx, `UPDATE archaeology_runs SET state='cancel_requested',updated_at=? WHERE session_id=? AND state NOT IN ('completed','failed','canceled')`, stamp(now), sid)
		}
		if err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state=?,revision=revision+1,updated_at=? WHERE id=?`, next, stamp(now), sid)
		if err != nil {
			return domain.ArchaeologySession{}, mapErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologySession{}, err
	}
	return s.ArchaeologySession(ctx, m.Principal)
}

func (s *Store) ReconcileArchaeology(ctx context.Context) error {
	now := stamp(s.now().UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_runs SET state='paused',phase_label='Paused after server restart',updated_at=? WHERE state IN ('running','pause_requested')`, now); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_runs SET state='canceled',phase_label='Canceled',updated_at=? WHERE state='cancel_requested'`, now); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='paused',revision=revision+1,updated_at=? WHERE state IN ('running','pause_requested')`, now); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_sessions SET state='canceled',revision=revision+1,updated_at=? WHERE state='cancel_requested'`, now); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_task_launches SET state='uncertain',error='Server restarted before Codex task creation was confirmed.',updated_at=? WHERE state='starting_codex'`, now); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_jobs SET state='uncertain',error_code='server_restarted_during_active_task',terminal_at=?,updated_at=? WHERE state IN ('starting','active','report_ready','cancel_requested')`, now, now); err != nil {
		return mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_native_batches SET state='attention',updated_at=? WHERE id IN (SELECT DISTINCT batch_id FROM archaeology_native_jobs WHERE state='uncertain')`, now); err != nil {
		return mapErr(err)
	}
	return tx.Commit()
}
