package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	"codex-commons/internal/domain"
)

func (s *Store) PrepareArchaeologyTaskLaunch(ctx context.Context, principal, projectID, requestID, clientMessageID string, grantDigest [32]byte, expiresAt time.Time) (domain.ArchaeologyTaskLaunch, bool, error) {
	s.archaeologyLaunchMu.Lock()
	defer s.archaeologyLaunchMu.Unlock()
	if principal == "" || !boundedCoreText(projectID, 120, true) || !boundedCoreText(requestID, 200, true) || !boundedCoreText(clientMessageID, 200, true) || grantDigest == [32]byte{} || !expiresAt.After(s.now().UTC()) {
		return domain.ArchaeologyTaskLaunch{}, false, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologyTaskLaunch{}, false, err
	}
	defer tx.Rollback()
	var sessionID string
	err = tx.QueryRowContext(ctx, "SELECT s.id FROM archaeology_sessions s JOIN archaeology_candidates c ON c.session_id=s.id WHERE s.principal=? AND c.id=? AND c.selected=1", principal, projectID).Scan(&sessionID)
	if err != nil {
		return domain.ArchaeologyTaskLaunch{}, false, mapErr(err)
	}
	id := deterministicHistoricalID("ARL-", sessionID, projectID)
	requestDigest := archaeologyDigest("launch", struct{ Principal, ProjectID, RequestID string }{principal, projectID, requestID})
	now := s.now().UTC()
	var prior []byte
	var existingClient string
	reserved := false
	err = tx.QueryRowContext(ctx, "SELECT request_digest,client_message_id FROM archaeology_task_launches WHERE id=?", id).Scan(&prior, &existingClient)
	if err == nil {
		if string(prior) != string(requestDigest[:]) || existingClient != clientMessageID {
			return domain.ArchaeologyTaskLaunch{}, false, domain.ErrConflict
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, "INSERT INTO archaeology_task_launches(id,session_id,candidate_id,state,client_message_id,request_digest,grant_digest,grant_expires_at,created_at,updated_at) VALUES(?,?,?,'starting_codex',?,?,?,?,?,?)", id, sessionID, projectID, clientMessageID, requestDigest[:], grantDigest[:], stamp(expiresAt), stamp(now), stamp(now))
		if err != nil {
			return domain.ArchaeologyTaskLaunch{}, false, mapErr(err)
		}
		reserved = true
	} else {
		return domain.ArchaeologyTaskLaunch{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return domain.ArchaeologyTaskLaunch{}, false, err
	}
	launch, err := s.archaeologyTaskLaunch(ctx, id)
	return launch, reserved, err
}

func (s *Store) CompleteArchaeologyTaskLaunch(ctx context.Context, launch domain.ArchaeologyLaunchResult) (domain.ArchaeologyTaskLaunch, error) {
	s.archaeologyLaunchMu.Lock()
	defer s.archaeologyLaunchMu.Unlock()
	if !boundedCoreText(launch.LaunchID, 120, true) || !boundedCoreText(launch.ProjectID, 120, true) || !boundedCoreText(launch.State, 40, true) || !boundedCoreText(launch.ThreadID, 120, false) || !boundedCoreText(launch.CodexSessionID, 120, false) || !boundedCoreText(launch.TurnID, 120, false) || !boundedCoreText(launch.Error, 500, false) {
		return domain.ArchaeologyTaskLaunch{}, domain.ErrInvalid
	}
	switch launch.State {
	case "task_created":
		if launch.ThreadID == "" || launch.CodexSessionID == "" || launch.TurnID == "" {
			return domain.ArchaeologyTaskLaunch{}, domain.ErrInvalid
		}
	case "failed", "uncertain":
	default:
		return domain.ArchaeologyTaskLaunch{}, domain.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "UPDATE archaeology_task_launches SET state=?,thread_id=?,codex_session_id=?,turn_id=?,error=?,updated_at=? WHERE id=? AND candidate_id=? AND state='starting_codex'", launch.State, launch.ThreadID, launch.CodexSessionID, launch.TurnID, launch.Error, stamp(s.now().UTC()), launch.LaunchID, launch.ProjectID)
	if err != nil {
		return domain.ArchaeologyTaskLaunch{}, mapErr(err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return domain.ArchaeologyTaskLaunch{}, domain.ErrConflict
	}
	return s.archaeologyTaskLaunch(ctx, launch.LaunchID)
}

func (s *Store) archaeologyTaskLaunch(ctx context.Context, id string) (domain.ArchaeologyTaskLaunch, error) {
	var out domain.ArchaeologyTaskLaunch
	var requestDigest, grantDigest, reportDigest, reportRequestDigest []byte
	var expires, created, updated string
	var consumed, reportExpires, reportConsumed sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT id,candidate_id,state,thread_id,codex_session_id,turn_id,client_message_id,request_digest,grant_digest,grant_expires_at,grant_consumed_at,report_digest,report_expires_at,report_consumed_at,report_request_digest,error,created_at,updated_at FROM archaeology_task_launches WHERE id=?", id).Scan(&out.ID, &out.ProjectID, &out.State, &out.ThreadID, &out.CodexSessionID, &out.TurnID, &out.ClientMessageID, &requestDigest, &grantDigest, &expires, &consumed, &reportDigest, &reportExpires, &reportConsumed, &reportRequestDigest, &out.Error, &created, &updated)
	if err != nil {
		return out, mapErr(err)
	}
	copy(out.RequestDigest[:], requestDigest)
	copy(out.GrantDigest[:], grantDigest)
	copy(out.ReportDigest[:], reportDigest)
	copy(out.ReportRequestDigest[:], reportRequestDigest)
	out.GrantExpiresAt, out.CreatedAt, out.UpdatedAt = parseStamp(expires), parseStamp(created), parseStamp(updated)
	if consumed.Valid {
		out.GrantConsumedAt = parseStamp(consumed.String)
	}
	if reportExpires.Valid {
		out.ReportExpiresAt = parseStamp(reportExpires.String)
	}
	if reportConsumed.Valid {
		out.ReportConsumedAt = parseStamp(reportConsumed.String)
	}
	return out, nil
}

func fixedDigestEqual(left, right [32]byte) bool {
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}
