package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"codex-commons/migrations"
)

const migration16At = "2026-08-15T00:00:00Z"

func applyMigration16Through(t *testing.T, db *sql.DB, through int) {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	must(t, err)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var version int
		_, err = fmt.Sscanf(entry.Name(), "%03d_", &version)
		must(t, err)
		if version > through {
			break
		}
		body, readErr := migrations.FS.ReadFile(entry.Name())
		must(t, readErr)
		_, err = db.Exec(string(body))
		must(t, err)
		_, err = db.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, version, entry.Name(), migration16At)
		must(t, err)
	}
}

func newMigration16Database(t *testing.T, through int) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migration16.sqlite3")
	db, err := sql.Open("sqlite", path)
	must(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL) STRICT`)
	must(t, err)
	applyMigration16Through(t, db, through)
	return db, path
}

func seedMigration16Job(t *testing.T, db *sql.DB, suffix string) string {
	t.Helper()
	sessionID := "m16-session-" + suffix
	principal := "human:m16-" + suffix
	projectID := "m16-project-" + suffix
	candidateID := "m16-candidate-" + suffix
	batchID := "m16-batch-" + suffix
	jobID := "m16-job-" + suffix
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO archaeology_sessions(id,principal,state,discovery_state,depth,source_git,source_docs,source_codex_history,max_concurrency,revision,created_at,updated_at)
		VALUES(?,?, 'completed','ready','standard',1,1,1,1,1,?,?)`, []any{sessionID, principal, migration16At, migration16At}},
		{`INSERT INTO projects(id,name,status,purpose,milestone,now_text,revision,created_at,updated_at)
		VALUES(?,?,'active','Migration fixture','','',1,?,?)`, []any{projectID, "Migration Project " + suffix, migration16At, migration16At}},
		{`INSERT INTO topics(id,project_id,name,created_at) VALUES(?,?,?,?)`, []any{projectID, projectID, "Migration Project " + suffix, migration16At}},
		{`INSERT INTO archaeology_candidates(session_id,id,name,path_label,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,selected,canonical_project_id)
		VALUES(?,?,?, ?,1,1,1,1,2,'low','',1,?)`, []any{sessionID, candidateID, "Migration Candidate " + suffix, "migration/" + suffix, projectID}},
		{`INSERT INTO archaeology_native_batches(id,session_id,request_key,request_digest,mode,state,max_concurrency,created_at,updated_at)
		VALUES(?,?,?,zeroblob(32),'app_server_dynamic_tools','completed',1,?,?)`, []any{batchID, sessionID, "batch-" + suffix, migration16At, migration16At}},
		{`INSERT INTO archaeology_native_jobs(id,batch_id,session_id,candidate_id,project_id,project_name,mode,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'app_server_dynamic_tools','completed',?,?)`, []any{jobID, batchID, sessionID, candidateID, projectID, "Migration Project " + suffix, migration16At, migration16At}},
	}
	for index, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed statement %d (%s): %v", index, suffix, err)
		}
	}
	return jobID
}

func insertMigration16Intent(db *sql.DB, id, jobID, operation string, digest []byte, payload, state string, attempts int, nextAttemptAt, leaseOwner, leaseExpiresAt, lastErrorCode, appliedAt string) error {
	return insertMigration16IntentAt(db, id, jobID, operation, digest, payload, state, attempts, nextAttemptAt, leaseOwner, leaseExpiresAt, lastErrorCode, appliedAt, migration16At, migration16At)
}

func insertMigration16IntentAt(db *sql.DB, id, jobID, operation string, digest []byte, payload, state string, attempts int, nextAttemptAt, leaseOwner, leaseExpiresAt, lastErrorCode, appliedAt, createdAt, updatedAt string) error {
	var next any
	if nextAttemptAt != "" {
		next = nextAttemptAt
	}
	var leaseExpiry any
	if leaseExpiresAt != "" {
		leaseExpiry = leaseExpiresAt
	}
	var applied any
	if appliedAt != "" {
		applied = appliedAt
	}
	_, err := db.Exec(`INSERT INTO archaeology_native_persistence_intents(
id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,lease_owner,lease_expires_at,last_error_code,applied_at,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, jobID, operation, digest, payload, state, attempts, next, leaseOwner, leaseExpiry, lastErrorCode, applied, createdAt, updatedAt)
	return err
}

func mustMigration16Reject(t *testing.T, err error, description string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted invalid persistence intent: %s", description)
	}
}

func TestMigration16FreshSchemaAndPersistenceIntentConstraints(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fresh.sqlite3")
	store, err := Open(ctx, path)
	must(t, err)
	defer store.Close()

	var version int
	must(t, store.DB().QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version))
	if version != 16 {
		t.Fatalf("schema version=%d", version)
	}
	jobID := seedMigration16Job(t, store.DB(), "constraints")
	digest := make([]byte, 32)
	payload := `{"job_id":"m16-job-constraints","operation":"fail_start"}`
	must(t, insertMigration16Intent(store.DB(), "m16-intent", jobID, "fail_start", digest, payload, "pending", 0, migration16At, "", "", "", ""))

	for _, operation := range []string{"bind_identity", "activate", "lose_turn", "complete_turn"} {
		operationJob := seedMigration16Job(t, store.DB(), operation)
		if err := insertMigration16Intent(store.DB(), "m16-"+operation, operationJob, operation, digest, `{"v":1}`, "pending", 0, migration16At, "", "", "", ""); err != nil {
			t.Fatalf("operation %s: %v", operation, err)
		}
	}
	zeroJSONJob := seedMigration16Job(t, store.DB(), "json-zero")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-json-zero", zeroJSONJob, "fail_start", digest, `0`, "pending", 0, migration16At, "", "", "", ""), "one-character JSON payload")
	boundaryPayloadJob := seedMigration16Job(t, store.DB(), "json-boundary")
	boundaryPayload := `"` + strings.Repeat("x", 65534) + `"`
	if len(boundaryPayload) != 65536 {
		t.Fatalf("boundary payload length=%d", len(boundaryPayload))
	}
	if err := insertMigration16Intent(store.DB(), "m16-json-boundary", boundaryPayloadJob, "fail_start", digest, boundaryPayload, "pending", 0, migration16At, "", "", "", ""); err != nil {
		t.Fatalf("65536-byte JSON payload: %v", err)
	}
	supersededJob := seedMigration16Job(t, store.DB(), "superseded")
	if err := insertMigration16Intent(store.DB(), "m16-superseded", supersededJob, "fail_start", digest, payload, "superseded", 0, "", "", "", "", ""); err != nil {
		t.Fatalf("valid superseded state: %v", err)
	}
	blockedJob := seedMigration16Job(t, store.DB(), "blocked")
	if err := insertMigration16Intent(store.DB(), "m16-blocked", blockedJob, "fail_start", digest, payload, "blocked", 0, "", "", "", "", ""); err != nil {
		t.Fatalf("valid blocked state: %v", err)
	}

	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-bad-operation", jobID, "report", digest, payload, "pending", 0, migration16At, "", "", "", ""), "operation allowlist")
	for _, operation := range []string{"launch", "finalize", "interrupt"} {
		mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-external-"+operation, jobID, operation, digest, payload, "pending", 0, migration16At, "", "", "", ""), "external operation "+operation)
	}
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-bad-state", jobID, "activate", digest, payload, "unknown", 0, migration16At, "", "", "", ""), "state allowlist")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-bad-digest", jobID, "activate", make([]byte, 31), payload, "pending", 0, migration16At, "", "", "", ""), "digest length")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-bad-json", jobID, "activate", digest, `{`, "pending", 0, migration16At, "", "", "", ""), "payload JSON")
	longPayload := `"` + strings.Repeat("x", 65535) + `"`
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-long-json", jobID, "activate", digest, longPayload, "pending", 0, migration16At, "", "", "", ""), "payload bound")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-negative-attempts", jobID, "activate", digest, payload, "pending", -1, migration16At, "", "", "", ""), "negative attempts")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-large-attempts", jobID, "activate", digest, payload, "pending", 1001, migration16At, "", "", "", ""), "bounded attempts")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-missing-job", "missing-job", "activate", digest, payload, "pending", 0, migration16At, "", "", "", ""), "job foreign key")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), " ", jobID, "activate", digest, payload, "pending", 0, migration16At, "", "", "", ""), "trimmed id")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), strings.Repeat("i", 201), jobID, "activate", digest, payload, "pending", 0, migration16At, "", "", "", ""), "oversized id")
	pendingNoScheduleJob := seedMigration16Job(t, store.DB(), "pending-no-schedule")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-pending-no-schedule", pendingNoScheduleJob, "fail_start", digest, payload, "pending", 0, "", "", "", "", ""), "pending NULL schedule")
	terminalScheduleJob := seedMigration16Job(t, store.DB(), "terminal-schedule")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-terminal-schedule", terminalScheduleJob, "fail_start", digest, payload, "blocked", 0, migration16At, "", "", "", ""), "terminal non-NULL schedule")
	supersededScheduleJob := seedMigration16Job(t, store.DB(), "superseded-schedule")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-superseded-schedule", supersededScheduleJob, "fail_start", digest, payload, "superseded", 0, migration16At, "", "", "", ""), "superseded non-NULL schedule")
	appliedNoTimestampJob := seedMigration16Job(t, store.DB(), "applied-no-timestamp")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-applied-no-timestamp", appliedNoTimestampJob, "fail_start", digest, payload, "applied", 0, "", "", "", "", ""), "applied NULL timestamp")
	appliedScheduleJob := seedMigration16Job(t, store.DB(), "applied-schedule")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-applied-schedule", appliedScheduleJob, "fail_start", digest, payload, "applied", 0, migration16At, "", "", "", migration16At), "applied schedule")
	pendingAppliedTimestampJob := seedMigration16Job(t, store.DB(), "pending-applied-timestamp")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-pending-applied-timestamp", pendingAppliedTimestampJob, "fail_start", digest, payload, "pending", 0, migration16At, "", "", "", migration16At), "pending applied timestamp")
	terminalAppliedTimestampJob := seedMigration16Job(t, store.DB(), "terminal-applied-timestamp")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-terminal-applied-timestamp", terminalAppliedTimestampJob, "fail_start", digest, payload, "blocked", 0, "", "", "", "", migration16At), "terminal applied timestamp")

	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-pending-lease", jobID, "activate", digest, payload, "pending", 0, migration16At, "worker", migration16At, "", ""), "pending lease")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-leased-no-schedule", jobID, "activate", digest, payload, "leased", 0, "", "worker", migration16At, "", ""), "leased NULL schedule")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-leased-owner", jobID, "activate", digest, payload, "leased", 0, migration16At, "", migration16At, "", ""), "leased owner")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-leased-expiry", jobID, "activate", digest, payload, "leased", 0, migration16At, "worker", "", "", ""), "leased expiry")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-empty-owner", jobID, "activate", digest, payload, "leased", 0, migration16At, " ", migration16At, "", ""), "empty lease owner")
	longOwnerJob := seedMigration16Job(t, store.DB(), "long-owner")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-long-owner", longOwnerJob, "fail_start", digest, payload, "leased", 0, migration16At, strings.Repeat("w", 201), migration16At, "", ""), "oversized lease owner")
	longErrorJob := seedMigration16Job(t, store.DB(), "long-error")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-long-error", longErrorJob, "fail_start", digest, payload, "pending", 0, migration16At, "", "", strings.Repeat("e", 201), ""), "oversized error code")
	longNextJob := seedMigration16Job(t, store.DB(), "long-next")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-long-next", longNextJob, "fail_start", digest, payload, "pending", 0, strings.Repeat("t", 101), "", "", "", ""), "oversized next-attempt timestamp")
	longExpiryJob := seedMigration16Job(t, store.DB(), "long-expiry")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-long-expiry", longExpiryJob, "fail_start", digest, payload, "leased", 0, migration16At, "worker", strings.Repeat("t", 101), "", ""), "oversized lease timestamp")
	longAppliedJob := seedMigration16Job(t, store.DB(), "long-applied")
	mustMigration16Reject(t, insertMigration16Intent(store.DB(), "m16-long-applied", longAppliedJob, "fail_start", digest, payload, "applied", 0, "", "", "", "", strings.Repeat("t", 101)), "oversized applied timestamp")
	longCreatedJob := seedMigration16Job(t, store.DB(), "long-created")
	mustMigration16Reject(t, insertMigration16IntentAt(store.DB(), "m16-long-created", longCreatedJob, "fail_start", digest, payload, "pending", 0, migration16At, "", "", "", "", strings.Repeat("t", 101), migration16At), "oversized created timestamp")
	longUpdatedJob := seedMigration16Job(t, store.DB(), "long-updated")
	mustMigration16Reject(t, insertMigration16IntentAt(store.DB(), "m16-long-updated", longUpdatedJob, "fail_start", digest, payload, "pending", 0, migration16At, "", "", "", "", migration16At, strings.Repeat("t", 101)), "oversized updated timestamp")
	if err := insertMigration16Intent(store.DB(), "m16-leased", jobID, "activate", digest, payload, "leased", 1, migration16At, "worker-1", "2026-08-15T00:05:00Z", "", ""); err != nil {
		t.Fatalf("valid lease: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `INSERT INTO archaeology_native_persistence_intents(id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,created_at,updated_at) VALUES(?,?,?,zeroblob(32),?,'pending',0,?,?,?)`, "m16-duplicate", jobID, "fail_start", payload, migration16At, migration16At); err == nil {
		t.Fatal("accepted duplicate job/operation intent")
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO archaeology_native_persistence_intents(id,job_id,operation,payload_digest,payload_json,state,attempts,next_attempt_at,created_at,updated_at) VALUES(?,?,?,zeroblob(32),?,'pending',0,?,?,?)`, "m16-duplicate-payload", jobID, "fail_start", `{"changed":true}`, migration16At, migration16At); err == nil {
		t.Fatal("accepted changed payload for existing job/operation")
	}

	if _, err := store.DB().ExecContext(ctx, `UPDATE archaeology_native_persistence_intents SET state='applied',next_attempt_at=NULL,lease_owner='',lease_expires_at=NULL,applied_at=?,updated_at=? WHERE id='m16-leased'`, migration16At, migration16At); err != nil {
		t.Fatalf("mutable retry state update: %v", err)
	}
	var cascadeBefore, cascadeAfter int
	cascadeJob := seedMigration16Job(t, store.DB(), "cascade")
	if err := insertMigration16Intent(store.DB(), "m16-cascade", cascadeJob, "fail_start", digest, payload, "pending", 0, migration16At, "", "", "", ""); err != nil {
		t.Fatalf("cascade intent: %v", err)
	}
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_persistence_intents WHERE job_id=?`, cascadeJob).Scan(&cascadeBefore))
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM archaeology_native_jobs WHERE id=?`, cascadeJob); err != nil {
		t.Fatalf("job delete cascade: %v", err)
	}
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_persistence_intents WHERE job_id=?`, cascadeJob).Scan(&cascadeAfter))
	if cascadeBefore != 1 || cascadeAfter != 0 {
		t.Fatalf("job cascade before=%d after=%d", cascadeBefore, cascadeAfter)
	}

	indexes, err := store.DB().QueryContext(ctx, `PRAGMA index_list('archaeology_native_persistence_intents')`)
	must(t, err)
	defer indexes.Close()
	seen := map[string]bool{}
	for indexes.Next() {
		var seq int
		var name string
		var unique, partial int
		var origin string
		must(t, indexes.Scan(&seq, &name, &unique, &origin, &partial))
		seen[name] = true
	}
	must(t, indexes.Err())
	for _, name := range []string{"archaeology_native_persistence_intents_due", "archaeology_native_persistence_intents_status"} {
		if !seen[name] {
			t.Fatalf("missing index %s", name)
		}
	}

	var integrity string
	must(t, store.DB().QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity))
	if integrity != "ok" {
		t.Fatalf("integrity_check=%q", integrity)
	}
	var violations int
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations))
	if violations != 0 {
		t.Fatalf("foreign_key_check=%d", violations)
	}
}

func TestMigration16Schema15UpgradePreservesNativeAndSelectedImportData(t *testing.T) {
	ctx := context.Background()
	db, path := newMigration16Database(t, 15)
	jobID := seedMigration16Job(t, db, "upgrade")
	_, err := db.Exec(`INSERT INTO archaeology_native_outcomes(id,job_id,project_id,title,summary,source_count,proposal_json,created_at) VALUES(?,?,?,?,?,1,?,?)`, "m16-outcome-upgrade", jobID, "m16-project-upgrade", "Upgrade outcome", "Preserved", `{"batch_id":"m16-historical"}`, migration16At)
	must(t, err)
	_, err = db.Exec(`INSERT INTO archaeology_selected_imports(id,batch_id,principal,request_key,selection_digest,manifest_digest,outcome_ids_json,result_json,created_at) VALUES(?,?,?,?,?,?,?, ?,?)`,
		"m16-import-upgrade", "m16-batch-upgrade", "human:m16-upgrade", "m16-request", "sha256:"+strings.Repeat("1", 64), "sha256:"+strings.Repeat("2", 64), `["m16-outcome-upgrade"]`, `{"applied":true}`, migration16At)
	must(t, err)
	must(t, db.Close())

	store, err := Open(ctx, path)
	must(t, err)
	defer store.Close()
	var version int
	must(t, store.DB().QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version))
	if version != 16 {
		t.Fatalf("schema version=%d", version)
	}
	var jobs, outcomes, imports, intents int
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_jobs WHERE id=?`, jobID).Scan(&jobs))
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_outcomes WHERE id='m16-outcome-upgrade'`).Scan(&outcomes))
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_selected_imports WHERE id='m16-import-upgrade'`).Scan(&imports))
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_native_persistence_intents`).Scan(&intents))
	if jobs != 1 || outcomes != 1 || imports != 1 || intents != 0 {
		t.Fatalf("preserved jobs=%d outcomes=%d imports=%d intents=%d", jobs, outcomes, imports, intents)
	}
	var outcomeTitle, resultJSON string
	must(t, store.DB().QueryRowContext(ctx, `SELECT o.title,i.result_json FROM archaeology_native_outcomes o JOIN archaeology_selected_imports i ON i.outcome_ids_json=json_array(o.id) WHERE o.id='m16-outcome-upgrade'`).Scan(&outcomeTitle, &resultJSON))
	if outcomeTitle != "Upgrade outcome" || resultJSON != `{"applied":true}` {
		t.Fatalf("preserved values title=%q result=%q", outcomeTitle, resultJSON)
	}
	var integrity string
	must(t, store.DB().QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity))
	if integrity != "ok" {
		t.Fatalf("integrity_check=%q", integrity)
	}
	var violations int
	must(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations))
	if violations != 0 {
		t.Fatalf("foreign_key_check=%d", violations)
	}
}
