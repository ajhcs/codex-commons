package store

import (
	"context"
	"database/sql"
	"strings"

	"codex-commons/internal/domain"
)

func (s *Store) ArchaeologyBatchHistory(ctx context.Context, principal string, query domain.ArchaeologyBatchHistoryQuery) (domain.ArchaeologyBatchHistoryPage, error) {
	if !boundedCoreText(principal, 200, true) || query.Limit < 1 || query.Limit > 50 {
		return domain.ArchaeologyBatchHistoryPage{}, domain.ErrInvalid
	}
	var revision int64
	if err := s.db.QueryRowContext(ctx, `SELECT revision FROM archaeology_sessions WHERE principal=?`, principal).Scan(&revision); err != nil {
		return domain.ArchaeologyBatchHistoryPage{}, mapErr(err)
	}
	offset, err := decodeArchaeologyCursor(query.Cursor, "history", "", "", revision)
	if err != nil {
		return domain.ArchaeologyBatchHistoryPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT b.id,b.state,b.mode,b.max_concurrency,b.depth,b.source_git,b.source_docs,b.source_codex_history,b.created_at,b.updated_at,
		count(j.id),sum(CASE WHEN j.state='queued' THEN 1 ELSE 0 END),sum(CASE WHEN j.state IN ('starting','active','report_ready','cancel_requested') THEN 1 ELSE 0 END),sum(CASE WHEN j.state='completed' THEN 1 ELSE 0 END),sum(CASE WHEN j.state IN ('attention','uncertain','failed','interrupted') THEN 1 ELSE 0 END),
		EXISTS(SELECT 1 FROM archaeology_native_outcomes o JOIN archaeology_native_jobs jo ON jo.id=o.job_id WHERE jo.batch_id=b.id)
		FROM archaeology_native_batches b JOIN archaeology_sessions s ON s.id=b.session_id LEFT JOIN archaeology_native_jobs j ON j.batch_id=b.id
		WHERE s.principal=? GROUP BY b.id ORDER BY b.created_at DESC,b.id DESC LIMIT ? OFFSET ?`, principal, query.Limit+1, offset)
	if err != nil {
		return domain.ArchaeologyBatchHistoryPage{}, err
	}
	defer rows.Close()
	var out domain.ArchaeologyBatchHistoryPage
	for rows.Next() {
		var item domain.ArchaeologyBatchSummary
		var git, docs, history int
		var created, updated string
		if err = rows.Scan(&item.ID, &item.State, &item.Mode, &item.MaxConcurrency, &item.Policy.Depth, &git, &docs, &history, &created, &updated, &item.SelectedTotal, &item.QueuedCount, &item.ActiveCount, &item.CompletedCount, &item.AttentionCount, &item.HasReport); err != nil {
			return out, err
		}
		item.Policy.Sources = domain.ArchaeologySources{Git: git == 1, Docs: docs == 1, CodexHistory: history == 1}
		item.CreatedAt, item.UpdatedAt = parseStamp(created), parseStamp(updated)
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > query.Limit {
		out.Items = out.Items[:query.Limit]
		out.NextCursor = encodeArchaeologyCursor("history", "", "", revision, offset+query.Limit)
	}
	return out, nil
}

func (s *Store) enrichArchaeologyOutcomePage(ctx context.Context, items []domain.ArchaeologyOutcome) error {
	if len(items) == 0 {
		return nil
	}
	positions := make(map[string]int, len(items))
	args := make([]any, len(items))
	marks := make([]string, len(items))
	for i := range items {
		positions[items[i].ID] = i
		args[i] = items[i].ID
		marks[i] = "?"
	}
	clause := strings.Join(marks, ",")
	rows, err := s.db.QueryContext(ctx, `SELECT outcome_id,kind,stable_id,digest,occurred_at FROM archaeology_native_provenance WHERE outcome_id IN (`+clause+`) ORDER BY outcome_id,position`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, at string
		var v domain.ArchaeologyProvenance
		if err = rows.Scan(&id, &v.Kind, &v.StableID, &v.Digest, &at); err != nil {
			rows.Close()
			return err
		}
		v.OccurredAt = parseStamp(at)
		items[positions[id]].Provenance = append(items[positions[id]].Provenance, v)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT outcome_id,session_id,contribution,demonstrated_strength,uncertainty,confidence FROM archaeology_native_outcome_contributors WHERE outcome_id IN (`+clause+`) ORDER BY outcome_id,session_id`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var v domain.ArchaeologyContributor
		if err = rows.Scan(&id, &v.SessionID, &v.Contribution, &v.DemonstratedStrength, &v.Uncertainty, &v.Confidence); err != nil {
			rows.Close()
			return err
		}
		items[positions[id]].Contributors = append(items[positions[id]].Contributors, v)
	}
	return rows.Close()
}

func (s *Store) ArchaeologyBatch(ctx context.Context, principal, batchID string) (domain.ArchaeologyBatchDetail, error) {
	if !boundedCoreText(principal, 200, true) || !boundedCoreText(batchID, 120, true) {
		return domain.ArchaeologyBatchDetail{}, domain.ErrInvalid
	}
	var out domain.ArchaeologyBatchDetail
	var git, docs, history int
	var created, updated string
	var ack sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT b.id,b.state,b.mode,b.max_concurrency,b.depth,b.source_git,b.source_docs,b.source_codex_history,b.policy_attested,b.large_batch_acknowledged_at,b.large_batch_acknowledged_by,b.created_at,b.updated_at FROM archaeology_native_batches b JOIN archaeology_sessions s ON s.id=b.session_id WHERE s.principal=? AND b.id=?`, principal, batchID).Scan(&out.Batch.ID, &out.Batch.State, &out.Batch.Mode, &out.Batch.MaxConcurrency, &out.Batch.Policy.Depth, &git, &docs, &history, &out.Batch.PolicyAttested, &ack, &out.Batch.LargeBatchAcknowledgedBy, &created, &updated)
	if err != nil {
		return out, mapErr(err)
	}
	out.Batch.Policy.Sources = domain.ArchaeologySources{Git: git == 1, Docs: docs == 1, CodexHistory: history == 1}
	out.Batch.CreatedAt, out.Batch.UpdatedAt = parseStamp(created), parseStamp(updated)
	if ack.Valid {
		out.Batch.LargeBatchAcknowledgedAt = parseStamp(ack.String)
	}
	out.Batch.Eligibility, err = readArchaeologyBatchEligibility(ctx, s.db, principal, batchID)
	if err != nil {
		return out, err
	}
	jobs, err := s.db.QueryContext(ctx, `SELECT j.id,j.batch_id,j.candidate_id,j.project_id,j.project_name,j.mode,j.state,j.thread_id,j.codex_session_id,j.turn_id,j.phase_label,j.sources_examined,j.duration_ms,j.error_code,j.created_at,j.started_at,j.reported_at,j.terminal_at,j.updated_at FROM archaeology_native_jobs j WHERE j.batch_id=? ORDER BY j.created_at,j.id LIMIT 30`, batchID)
	if err != nil {
		return out, err
	}
	for jobs.Next() {
		var j domain.ArchaeologyNativeJob
		var duration sql.NullInt64
		var c, u string
		var started, reported, terminal sql.NullString
		if err = jobs.Scan(&j.ID, &j.BatchID, &j.CandidateID, &j.ProjectID, &j.ProjectName, &j.Mode, &j.State, &j.ThreadID, &j.CodexSessionID, &j.TurnID, &j.PhaseLabel, &j.SourcesExamined, &duration, &j.ErrorCode, &c, &started, &reported, &terminal, &u); err != nil {
			jobs.Close()
			return out, err
		}
		j.CreatedAt, j.UpdatedAt = parseStamp(c), parseStamp(u)
		j.Policy = out.Batch.Policy
		if duration.Valid {
			v := duration.Int64
			j.DurationMS = &v
		}
		if started.Valid {
			j.StartedAt = parseStamp(started.String)
		}
		if reported.Valid {
			j.ReportedAt = parseStamp(reported.String)
		}
		if terminal.Valid {
			j.TerminalAt = parseStamp(terminal.String)
		}
		out.Batch.Jobs = append(out.Batch.Jobs, j)
	}
	if err = jobs.Close(); err != nil {
		return out, err
	}
	page, err := s.ArchaeologyBatchOutcomes(ctx, principal, batchID, domain.ArchaeologyOutcomePageQuery{Limit: 5})
	out.Outcomes, out.OutcomesNextCursor = page.Items, page.NextCursor
	return out, err
}

func (s *Store) ArchaeologyBatchOutcomes(ctx context.Context, principal, batchID string, query domain.ArchaeologyOutcomePageQuery) (domain.ArchaeologyOutcomePage, error) {
	if !boundedCoreText(principal, 200, true) || !boundedCoreText(batchID, 120, true) || query.Limit < 1 || query.Limit > 5 {
		return domain.ArchaeologyOutcomePage{}, domain.ErrInvalid
	}
	var revision int64
	if err := s.db.QueryRowContext(ctx, `SELECT s.revision FROM archaeology_native_batches b JOIN archaeology_sessions s ON s.id=b.session_id WHERE s.principal=? AND b.id=?`, principal, batchID).Scan(&revision); err != nil {
		return domain.ArchaeologyOutcomePage{}, mapErr(err)
	}
	offset, err := decodeArchaeologyCursor(query.Cursor, "batch-outcomes:"+batchID, "", "", revision)
	if err != nil {
		return domain.ArchaeologyOutcomePage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT o.id,o.project_id,o.title,o.summary,o.source_count,o.proposal_json FROM archaeology_native_outcomes o JOIN archaeology_native_jobs j ON j.id=o.job_id WHERE j.batch_id=? ORDER BY o.created_at,o.id LIMIT ? OFFSET ?`, batchID, query.Limit+1, offset)
	if err != nil {
		return domain.ArchaeologyOutcomePage{}, err
	}
	defer rows.Close()
	var out domain.ArchaeologyOutcomePage
	for rows.Next() {
		var item domain.ArchaeologyOutcome
		if err = rows.Scan(&item.ID, &item.ProjectID, &item.Title, &item.Summary, &item.SourceCount, &item.ProposalJSON); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > query.Limit {
		out.Items = out.Items[:query.Limit]
		out.NextCursor = encodeArchaeologyCursor("batch-outcomes:"+batchID, "", "", revision, offset+query.Limit)
	}
	if err = s.enrichArchaeologyOutcomePage(ctx, out.Items); err != nil {
		return domain.ArchaeologyOutcomePage{}, err
	}
	return out, nil
}

func (s *Store) ArchaeologySelectedOutcomes(ctx context.Context, principal, batchID string, ids []string) ([]domain.ArchaeologyOutcome, error) {
	if !boundedCoreText(principal, 200, true) || !boundedCoreText(batchID, 120, true) || len(ids) < 1 || len(ids) > domain.ArchaeologyNativeMaxProjects*2 {
		return nil, domain.ErrInvalid
	}
	marks := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, principal, batchID)
	seen := map[string]bool{}
	for i, id := range ids {
		if !boundedCoreText(id, 120, true) || seen[id] {
			return nil, domain.ErrInvalid
		}
		seen[id] = true
		marks[i] = "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT o.id,o.project_id,o.title,o.summary,o.source_count,o.proposal_json FROM archaeology_native_outcomes o JOIN archaeology_native_jobs j ON j.id=o.job_id JOIN archaeology_native_batches b ON b.id=j.batch_id JOIN archaeology_sessions s ON s.id=b.session_id WHERE s.principal=? AND b.id=? AND o.id IN (`+strings.Join(marks, ",")+`) ORDER BY o.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ArchaeologyOutcome, 0, len(ids))
	for rows.Next() {
		var v domain.ArchaeologyOutcome
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Summary, &v.SourceCount, &v.ProposalJSON); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(ids) {
		return nil, domain.ErrNotFound
	}
	return out, nil
}

func (s *Store) loadArchaeologyOutcomesForBatch(ctx context.Context, batchID string) ([]domain.ArchaeologyOutcome, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.id,o.project_id,o.title,o.summary,o.source_count,o.proposal_json FROM archaeology_native_outcomes o JOIN archaeology_native_jobs j ON j.id=o.job_id WHERE j.batch_id=? ORDER BY o.created_at,o.id LIMIT ?`, batchID, domain.ArchaeologyNativeMaxProjects*2)
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
	positions := map[string]int{}
	for i := range out {
		positions[out[i].ID] = i
	}
	p, err := s.db.QueryContext(ctx, `SELECT p.outcome_id,p.kind,p.stable_id,p.digest,p.occurred_at FROM archaeology_native_provenance p JOIN archaeology_native_outcomes o ON o.id=p.outcome_id JOIN archaeology_native_jobs j ON j.id=o.job_id WHERE j.batch_id=? ORDER BY p.outcome_id,p.position`, batchID)
	if err != nil {
		return nil, err
	}
	for p.Next() {
		var id, occurred string
		var source domain.ArchaeologyProvenance
		if err = p.Scan(&id, &source.Kind, &source.StableID, &source.Digest, &occurred); err != nil {
			p.Close()
			return nil, err
		}
		if i, ok := positions[id]; ok {
			source.OccurredAt = parseStamp(occurred)
			out[i].Provenance = append(out[i].Provenance, source)
		}
	}
	if err = p.Close(); err != nil {
		return nil, err
	}
	c, err := s.db.QueryContext(ctx, `SELECT c.outcome_id,c.session_id,c.contribution,c.demonstrated_strength,c.uncertainty,c.confidence FROM archaeology_native_outcome_contributors c JOIN archaeology_native_outcomes o ON o.id=c.outcome_id JOIN archaeology_native_jobs j ON j.id=o.job_id WHERE j.batch_id=? ORDER BY c.outcome_id,c.session_id`, batchID)
	if err != nil {
		return nil, err
	}
	for c.Next() {
		var id string
		var member domain.ArchaeologyContributor
		if err = c.Scan(&id, &member.SessionID, &member.Contribution, &member.DemonstratedStrength, &member.Uncertainty, &member.Confidence); err != nil {
			c.Close()
			return nil, err
		}
		if i, ok := positions[id]; ok {
			out[i].Contributors = append(out[i].Contributors, member)
		}
	}
	if err = c.Close(); err != nil {
		return nil, err
	}
	return out, nil
}
