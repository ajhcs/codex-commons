package store

import (
	"context"

	"codex-commons/internal/domain"
)

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
         WHERE j.batch_id=b.id
           AND j.session_id=b.session_id
           AND j.state='completed'
           AND j.report_digest IS NOT NULL
           AND length(j.report_digest)=32
           AND o.project_id=j.project_id)
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

// ArchaeologyBatchEligibility returns the authoritative read-side capability
// snapshot for one principal-owned native batch.
func (s *Store) ArchaeologyBatchEligibility(ctx context.Context, principal, batchID string) (domain.ArchaeologyBatchEligibility, error) {
	if s == nil || s.db == nil {
		return domain.ArchaeologyBatchEligibility{}, domain.ErrUnavailable
	}
	return readArchaeologyBatchEligibility(ctx, s.db, principal, batchID)
}
