package store

import (
	"context"
	"database/sql"

	"codex-commons/internal/domain"
)

func claimByRequestQuery(ctx context.Context, q rowQuerier, storageKey, requestID string) (domain.Claim, error) {
	var claim domain.Claim
	var claimedAt string
	var leaseUntil sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id,task_id,session_id,request_id,project_revision,claimed_at,lease_until
FROM task_claims WHERE request_id=?`, storageKey).Scan(
		&claim.ID, &claim.TaskID, &claim.SessionID, &claim.RequestID, &claim.Revision, &claimedAt, &leaseUntil,
	)
	if err != nil {
		return claim, mapErr(err)
	}
	claim.RequestID = requestID
	claim.ClaimedAt = parseStamp(claimedAt)
	if leaseUntil.Valid {
		value := parseStamp(leaseUntil.String)
		claim.LeaseUntil = &value
	}
	return claim, nil
}
