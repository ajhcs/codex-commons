ALTER TABLE archaeology_sessions ADD COLUMN tasks_examined INTEGER NOT NULL DEFAULT 0 CHECK (tasks_examined BETWEEN 0 AND 10000);
ALTER TABLE archaeology_sessions ADD COLUMN projects_grouped INTEGER NOT NULL DEFAULT 0 CHECK (projects_grouped BETWEEN 0 AND 10000);
ALTER TABLE archaeology_sessions ADD COLUMN catalog_truncated INTEGER NOT NULL DEFAULT 0 CHECK (catalog_truncated IN (0,1));
ALTER TABLE archaeology_sessions ADD COLUMN app_server_identity TEXT NOT NULL DEFAULT '' CHECK (length(app_server_identity) <= 200);

ALTER TABLE archaeology_native_batches ADD COLUMN large_batch_acknowledged_at TEXT;
ALTER TABLE archaeology_native_batches ADD COLUMN large_batch_acknowledged_by TEXT NOT NULL DEFAULT '' CHECK (length(large_batch_acknowledged_by) <= 200);
ALTER TABLE archaeology_native_jobs ADD COLUMN project_name TEXT NOT NULL DEFAULT '' CHECK(length(project_name)<=200);
UPDATE archaeology_native_jobs SET project_name=coalesce((SELECT name FROM archaeology_candidates c WHERE c.session_id=archaeology_native_jobs.session_id AND c.id=archaeology_native_jobs.candidate_id),(SELECT name FROM projects p WHERE p.id=archaeology_native_jobs.project_id),archaeology_native_jobs.project_id);
CREATE INDEX archaeology_native_batches_history ON archaeology_native_batches(session_id,created_at DESC,id DESC);
CREATE INDEX archaeology_native_outcomes_batch_page ON archaeology_native_outcomes(job_id,created_at,id);

CREATE TABLE archaeology_selected_imports (
  id TEXT PRIMARY KEY,
  batch_id TEXT NOT NULL REFERENCES archaeology_native_batches(id),
  principal TEXT NOT NULL CHECK (length(trim(principal)) BETWEEN 1 AND 200),
  request_key TEXT NOT NULL CHECK (length(trim(request_key)) BETWEEN 1 AND 200),
  selection_digest TEXT NOT NULL CHECK (selection_digest GLOB 'sha256:*' AND length(selection_digest)=71),
  manifest_digest TEXT NOT NULL CHECK (manifest_digest GLOB 'sha256:*' AND length(manifest_digest)=71),
  outcome_ids_json TEXT NOT NULL CHECK (json_valid(outcome_ids_json)),
  result_json TEXT NOT NULL CHECK (json_valid(result_json)),
  created_at TEXT NOT NULL,
  UNIQUE(principal,request_key),
  UNIQUE(batch_id,selection_digest)
) STRICT;

CREATE TABLE archaeology_selected_reviews (
  id TEXT PRIMARY KEY,
  principal TEXT NOT NULL CHECK(length(trim(principal)) BETWEEN 1 AND 200),
  batch_id TEXT NOT NULL REFERENCES archaeology_native_batches(id),
  selection_digest TEXT NOT NULL CHECK(selection_digest GLOB 'sha256:*' AND length(selection_digest)=71),
  manifest_digest TEXT NOT NULL CHECK(manifest_digest GLOB 'sha256:*' AND length(manifest_digest)=71),
  outcome_ids_json TEXT NOT NULL CHECK(json_valid(outcome_ids_json)),
  session_token_digest BLOB NOT NULL UNIQUE CHECK(length(session_token_digest)=32),
  completion_token_digest BLOB UNIQUE CHECK(completion_token_digest IS NULL OR length(completion_token_digest)=32),
  page_size INTEGER NOT NULL CHECK(page_size=5),
  page_count INTEGER NOT NULL CHECK(page_count BETWEEN 1 AND 12),
  next_page INTEGER NOT NULL DEFAULT 0 CHECK(next_page BETWEEN 0 AND page_count),
  completed_at TEXT,
  consumed_at TEXT,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;
CREATE INDEX archaeology_selected_reviews_principal_expiry ON archaeology_selected_reviews(principal,expires_at);
CREATE TABLE archaeology_selected_review_pages (
  review_id TEXT NOT NULL REFERENCES archaeology_selected_reviews(id) ON DELETE CASCADE,
  page INTEGER NOT NULL CHECK(page BETWEEN 0 AND 11),
  request_key TEXT NOT NULL CHECK(length(trim(request_key)) BETWEEN 1 AND 200),
  response_token_digest BLOB NOT NULL CHECK(length(response_token_digest)=32),
  viewed_at TEXT NOT NULL,
  PRIMARY KEY(review_id,page),
  UNIQUE(review_id,request_key)
) STRICT;
CREATE TRIGGER archaeology_selected_imports_no_update BEFORE UPDATE ON archaeology_selected_imports
BEGIN SELECT RAISE(ABORT,'append-only selected import'); END;
CREATE TRIGGER archaeology_selected_imports_no_delete BEFORE DELETE ON archaeology_selected_imports
BEGIN SELECT RAISE(ABORT,'append-only selected import'); END;

CREATE TABLE installation_status (
  id INTEGER PRIMARY KEY CHECK (id=1),
  backup_status TEXT NOT NULL DEFAULT 'unknown' CHECK (backup_status IN ('unknown','verified','failed')),
  backup_verified_at TEXT,
  compatibility_status TEXT NOT NULL DEFAULT 'unknown' CHECK (compatibility_status IN ('unknown','compatible','incompatible','unavailable')),
  compatibility_checked_at TEXT,
  reconciliation_status TEXT NOT NULL DEFAULT 'unknown' CHECK (reconciliation_status IN ('unknown','healthy','attention','failed')),
  reconciliation_checked_at TEXT,
  restore_status TEXT NOT NULL DEFAULT 'unknown' CHECK(restore_status IN ('unknown','verified','failed')),
  restore_verified_at TEXT,
  report_recovery_status TEXT NOT NULL DEFAULT 'unknown' CHECK(report_recovery_status IN ('unknown','verified','attention')),
  report_recovery_violations INTEGER NOT NULL DEFAULT 0 CHECK(report_recovery_violations BETWEEN 0 AND 10000),
  report_recovery_checked_at TEXT,
  duplicate_launch_status TEXT NOT NULL DEFAULT 'unknown' CHECK(duplicate_launch_status IN ('unknown','verified','attention')),
  duplicate_launch_violations INTEGER NOT NULL DEFAULT 0 CHECK(duplicate_launch_violations BETWEEN 0 AND 10000),
  duplicate_launch_checked_at TEXT,
  repository_immutability_status TEXT NOT NULL DEFAULT 'unknown' CHECK(repository_immutability_status IN ('unknown','verified','attention')),
  repository_immutability_violations INTEGER NOT NULL DEFAULT 0 CHECK(repository_immutability_violations BETWEEN 0 AND 10000),
  repository_immutability_checked_at TEXT,
  canonical_immutability_status TEXT NOT NULL DEFAULT 'unknown' CHECK(canonical_immutability_status IN ('unknown','verified','attention')),
  canonical_immutability_violations INTEGER NOT NULL DEFAULT 0 CHECK(canonical_immutability_violations BETWEEN 0 AND 10000),
  canonical_immutability_checked_at TEXT,
  report_recovery_receipt_digest TEXT NOT NULL DEFAULT '' CHECK(report_recovery_receipt_digest='' OR length(report_recovery_receipt_digest)=64),
  duplicate_launch_receipt_digest TEXT NOT NULL DEFAULT '' CHECK(duplicate_launch_receipt_digest='' OR length(duplicate_launch_receipt_digest)=64),
  repository_immutability_receipt_digest TEXT NOT NULL DEFAULT '' CHECK(repository_immutability_receipt_digest='' OR length(repository_immutability_receipt_digest)=64),
  canonical_immutability_receipt_digest TEXT NOT NULL DEFAULT '' CHECK(canonical_immutability_receipt_digest='' OR length(canonical_immutability_receipt_digest)=64),
  codex_session_revocation_pending INTEGER NOT NULL DEFAULT 0 CHECK(codex_session_revocation_pending IN (0,1)),
  review_secret BLOB NOT NULL CHECK(length(review_secret)=32),
  updated_at TEXT NOT NULL
) STRICT;
INSERT INTO installation_status(id,review_secret,updated_at) VALUES(1,randomblob(32),strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE TABLE installation_evidence_receipts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL CHECK(kind IN ('report_recovery','duplicate_launch','repository_immutability','canonical_immutability')),
  status TEXT NOT NULL CHECK(status IN ('verified','attention')),
  violations INTEGER NOT NULL CHECK(violations BETWEEN 0 AND 10000),
  checked_at TEXT NOT NULL,
  scope_digest TEXT NOT NULL CHECK(length(scope_digest)=64),
  receipt_digest TEXT NOT NULL UNIQUE CHECK(length(receipt_digest)=64),
  recorded_at TEXT NOT NULL
) STRICT;
CREATE TRIGGER installation_evidence_receipts_no_update BEFORE UPDATE ON installation_evidence_receipts BEGIN SELECT RAISE(ABORT,'append-only evidence receipt'); END;
CREATE TRIGGER installation_evidence_receipts_no_delete BEFORE DELETE ON installation_evidence_receipts BEGIN SELECT RAISE(ABORT,'append-only evidence receipt'); END;

CREATE TABLE human_browser_sessions (
  token_digest BLOB PRIMARY KEY CHECK(length(token_digest)=32),
  csrf_digest BLOB NOT NULL CHECK(length(csrf_digest)=32),
  principal TEXT NOT NULL CHECK(length(trim(principal)) BETWEEN 1 AND 200),
  auth_method TEXT NOT NULL CHECK(auth_method IN ('codex','recovery')),
  binding_revision INTEGER NOT NULL CHECK(binding_revision >= 0),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  revoked_at TEXT
) STRICT;
CREATE INDEX human_browser_sessions_expiry ON human_browser_sessions(expires_at);

CREATE TRIGGER archaeology_native_outcomes_no_update BEFORE UPDATE ON archaeology_native_outcomes
BEGIN SELECT RAISE(ABORT,'append-only native outcome'); END;
CREATE TRIGGER archaeology_native_outcomes_no_delete BEFORE DELETE ON archaeology_native_outcomes
BEGIN SELECT RAISE(ABORT,'append-only native outcome'); END;
