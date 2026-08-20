package store

import (
	"context"

	"codex-commons/internal/domain"
)

// archaeologyEligibleOutcomePredicate is shared by the batch capability
// snapshot and selected-outcome reads. Keep the child checks here so a
// selected mutation cannot accidentally validate a weaker outcome shape than
// the batch-level predicate.
const archaeologyEligibleOutcomePredicate = `j.batch_id=b.id
           AND j.session_id=b.session_id
           AND j.state='completed'
           AND j.report_digest IS NOT NULL
           AND length(j.report_digest)=32
           AND o.project_id=j.project_id`

// archaeologyBatchEligibilitySQL is intentionally one store-owned predicate
// over the batch and all of its durable children. The principal binding is
// part of the same query as the batch binding so a caller cannot obtain a
// capability snapshot for another principal's batch.
const archaeologyBatchEligibilitySQL = `
SELECT b.state,
       b.policy_attested,
       (SELECT count(*)
          FROM archaeology_native_jobs j
         WHERE j.batch_id=b.id),
       (SELECT count(*)
          FROM archaeology_native_jobs j
         WHERE j.batch_id=b.id
           AND j.session_id=b.session_id
           AND j.state='completed'),
       (SELECT count(*)
          FROM archaeology_native_outcomes o
          JOIN archaeology_native_jobs j ON j.id=o.job_id
         WHERE j.batch_id=b.id),
       (SELECT count(*)
          FROM archaeology_native_outcomes o
          JOIN archaeology_native_jobs j ON j.id=o.job_id
         WHERE ` + archaeologyEligibleOutcomePredicate + `)
  FROM archaeology_native_batches b
  JOIN archaeology_sessions s ON s.id=b.session_id
 WHERE s.principal=?
   AND b.id=?`

func readArchaeologyBatchEligibility(ctx context.Context, q rowQuerier, principal, batchID string) (domain.ArchaeologyBatchEligibility, error) {
	if !boundedCoreText(principal, 200, true) || !boundedCoreText(batchID, 120, true) {
		return domain.ArchaeologyBatchEligibility{}, domain.ErrInvalid
	}

	var snapshot domain.ArchaeologyBatchEligibility
	var policyAttested int
	if err := q.QueryRowContext(ctx, archaeologyBatchEligibilitySQL, principal, batchID).Scan(
		&snapshot.State,
		&policyAttested,
		&snapshot.JobCount,
		&snapshot.CompletedJobCount,
		&snapshot.OutcomeCount,
		&snapshot.ValidOutcomeCount,
	); err != nil {
		return domain.ArchaeologyBatchEligibility{}, mapErr(err)
	}
	snapshot.PolicyAttested = policyAttested == 1
	snapshot.Eligible = snapshot.State == "completed" &&
		snapshot.PolicyAttested &&
		snapshot.JobCount > 0 &&
		snapshot.CompletedJobCount == snapshot.JobCount &&
		snapshot.OutcomeCount > 0 &&
		snapshot.ValidOutcomeCount == snapshot.OutcomeCount
	return snapshot, nil
}

// readArchaeologyEligibleSelectedOutcomes applies the batch capability
// predicate and then verifies every requested outcome against that same
// principal-owned batch and report-bearing completed job. Callers use this
// inside their existing transaction when the result gates a mutation.
func readArchaeologyEligibleSelectedOutcomes(ctx context.Context, q historicalQuerier, principal, batchID string, ids []string) ([]domain.ArchaeologyOutcome, error) {
	canonical, err := canonicalSelectedOutcomeIDs(ids)
	if err != nil {
		return nil, err
	}
	eligibility, err := readArchaeologyBatchEligibility(ctx, q, principal, batchID)
	if err != nil {
		return nil, err
	}
	if !eligibility.Eligible {
		return nil, domain.ErrConflict
	}

	marks, idArgs := selectedOutcomeQuery(canonical)
	args := make([]any, 0, len(idArgs)+2)
	args = append(args, principal, batchID)
	args = append(args, idArgs...)
	rows, err := q.QueryContext(ctx, `SELECT o.id,o.project_id,o.title,o.summary,o.source_count,o.proposal_json
FROM archaeology_native_outcomes o
JOIN archaeology_native_jobs j ON j.id=o.job_id
JOIN archaeology_native_batches b ON b.id=j.batch_id
JOIN archaeology_sessions s ON s.id=b.session_id
WHERE s.principal=? AND b.id=? AND `+archaeologyEligibleOutcomePredicate+` AND o.id IN (`+marks+`)
ORDER BY o.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ArchaeologyOutcome, 0, len(canonical))
	for rows.Next() {
		var outcome domain.ArchaeologyOutcome
		if err = rows.Scan(&outcome.ID, &outcome.ProjectID, &outcome.Title, &outcome.Summary, &outcome.SourceCount, &outcome.ProposalJSON); err != nil {
			return nil, err
		}
		out = append(out, outcome)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(canonical) {
		return nil, domain.ErrNotFound
	}
	return out, nil
}

// ArchaeologyBatchEligibility returns the authoritative read-side capability
// snapshot for one principal-owned native batch.
func (s *Store) ArchaeologyBatchEligibility(ctx context.Context, principal, batchID string) (domain.ArchaeologyBatchEligibility, error) {
	if s == nil || s.db == nil {
		return domain.ArchaeologyBatchEligibility{}, domain.ErrUnavailable
	}
	return readArchaeologyBatchEligibility(ctx, s.db, principal, batchID)
}
