package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"codex-commons/internal/domain"
)

func reviewToken(secret []byte, value string) (string, []byte) {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte(value))
	raw := h.Sum(nil)
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, digest[:]
}

func (s *Store) AdvanceArchaeologySelectedReview(ctx context.Context, command domain.ArchaeologySelectedReviewCommand) (domain.ArchaeologySelectedReviewReceipt, error) {
	if !boundedCoreText(command.Principal, 200, true) || !boundedCoreText(command.BatchID, 120, true) || !boundedCoreText(command.RequestID, 200, true) || !historicalDigestPattern.MatchString(command.SelectionDigest) || !historicalDigestPattern.MatchString(command.ManifestDigest) || command.Page < 0 || command.PageCount < 1 || command.PageCount > 12 || command.Page >= command.PageCount {
		return domain.ArchaeologySelectedReviewReceipt{}, domain.ErrInvalid
	}
	idsJSON, _ := json.Marshal(command.OutcomeIDs)
	now := s.now().UTC()
	expires := now.Add(30 * time.Minute)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ArchaeologySelectedReviewReceipt{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM archaeology_selected_reviews WHERE expires_at<=?`, stamp(now)); err != nil {
		return domain.ArchaeologySelectedReviewReceipt{}, err
	}
	out := domain.ArchaeologySelectedReviewReceipt{}
	var secret []byte
	if err = tx.QueryRowContext(ctx, `SELECT review_secret FROM installation_status WHERE id=1`).Scan(&secret); err != nil {
		return out, err
	}
	id := deterministicHistoricalID("ASV-", command.Principal, command.BatchID, command.SelectionDigest, command.ManifestDigest)
	sessionToken, sessionDigest := reviewToken(secret, "session\x00"+id)
	if command.Page == 0 && command.SessionToken == "" {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO archaeology_selected_reviews(id,principal,batch_id,selection_digest,manifest_digest,outcome_ids_json,session_token_digest,page_size,page_count,next_page,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,0,?,?)`, id, command.Principal, command.BatchID, command.SelectionDigest, command.ManifestDigest, string(idsJSON), sessionDigest, 5, command.PageCount, stamp(expires), stamp(now))
		if err != nil {
			return out, mapErr(err)
		}
	} else if command.SessionToken != sessionToken || len(command.SessionToken) != 43 {
		return out, domain.ErrInvalid
	}
	var selection, manifest, outcomes, expiry string
	var next, count int
	var consumed sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT selection_digest,manifest_digest,outcome_ids_json,next_page,page_count,expires_at,consumed_at FROM archaeology_selected_reviews WHERE id=? AND principal=? AND batch_id=? AND session_token_digest=?`, id, command.Principal, command.BatchID, sessionDigest).Scan(&selection, &manifest, &outcomes, &next, &count, &expiry, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return out, domain.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out = domain.ArchaeologySelectedReviewReceipt{SessionToken: sessionToken, NextPage: command.Page + 1, ExpiresAt: parseStamp(expiry)}
	if selection != command.SelectionDigest || manifest != command.ManifestDigest || outcomes != string(idsJSON) || count != command.PageCount || !out.ExpiresAt.After(now) {
		return out, domain.ErrConflict
	}
	var storedRequest string
	pageErr := tx.QueryRowContext(ctx, `SELECT request_key FROM archaeology_selected_review_pages WHERE review_id=? AND page=?`, id, command.Page).Scan(&storedRequest)
	if pageErr == nil && storedRequest != command.RequestID {
		return out, domain.ErrConflict
	}
	if pageErr == nil {
		if command.Page+1 == command.PageCount {
			out.CompletionToken, _ = reviewToken(secret, "completion\x00"+command.Principal+"\x00"+command.BatchID+"\x00"+command.SelectionDigest+"\x00"+command.ManifestDigest)
		}
		if err = tx.Commit(); err != nil {
			return out, mapErr(err)
		}
		return out, nil
	}
	if pageErr != nil && !errors.Is(pageErr, sql.ErrNoRows) {
		return out, pageErr
	}
	if consumed.Valid || next != command.Page {
		return out, domain.ErrConflict
	}
	responseDigest := sha256.Sum256([]byte(sessionToken))
	if _, err = tx.ExecContext(ctx, `INSERT INTO archaeology_selected_review_pages(review_id,page,request_key,response_token_digest,viewed_at) VALUES(?,?,?,?,?)`, id, command.Page, command.RequestID, responseDigest[:], stamp(now)); err != nil {
		return out, mapErr(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE archaeology_selected_reviews SET next_page=next_page+1 WHERE id=? AND next_page=?`, id, command.Page); err != nil {
		return out, err
	}
	if out.NextPage == command.PageCount {
		completion, digest := reviewToken(secret, "completion\x00"+command.Principal+"\x00"+command.BatchID+"\x00"+command.SelectionDigest+"\x00"+command.ManifestDigest)
		out.CompletionToken = completion
		sessionDigest := sha256.Sum256([]byte(out.SessionToken))
		result, updateErr := tx.ExecContext(ctx, `UPDATE archaeology_selected_reviews SET completion_token_digest=COALESCE(completion_token_digest,?),completed_at=COALESCE(completed_at,?) WHERE principal=? AND batch_id=? AND session_token_digest=? AND next_page=page_count AND (completion_token_digest IS NULL OR completion_token_digest=?)`, digest, stamp(now), command.Principal, command.BatchID, sessionDigest[:], digest)
		if updateErr != nil {
			return out, updateErr
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return out, domain.ErrConflict
		}
	}
	if err = tx.Commit(); err != nil {
		return out, mapErr(err)
	}
	return out, nil
}

func consumeSelectedReview(ctx context.Context, tx *sql.Tx, command domain.ArchaeologySelectedApplyCommand, outcomeIDs []string, now time.Time) error {
	if command.ReviewCompletionToken == "" {
		return domain.ErrInvalid
	}
	digest := sha256.Sum256([]byte(command.ReviewCompletionToken))
	idsJSON, _ := json.Marshal(outcomeIDs)
	result, err := tx.ExecContext(ctx, `UPDATE archaeology_selected_reviews SET consumed_at=? WHERE principal=? AND batch_id=? AND selection_digest=? AND manifest_digest=? AND outcome_ids_json=? AND completion_token_digest=? AND completed_at IS NOT NULL AND consumed_at IS NULL AND expires_at>?`, stamp(now), command.Principal, command.BatchID, command.SelectionDigest, command.ManifestDigest, string(idsJSON), digest[:], stamp(now))
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return domain.ErrConflict
	}
	return nil
}
