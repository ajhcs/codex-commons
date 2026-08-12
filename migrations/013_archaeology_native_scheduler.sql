CREATE TABLE archaeology_native_batches (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES archaeology_sessions(id) ON DELETE CASCADE,
  request_key TEXT NOT NULL,
  request_digest BLOB NOT NULL CHECK (length(request_digest)=32),
  mode TEXT NOT NULL CHECK (mode='app_server_dynamic_tools'),
  state TEXT NOT NULL CHECK (state IN ('queued','running','cancel_requested','canceled','completed','attention')),
  max_concurrency INTEGER NOT NULL CHECK (max_concurrency BETWEEN 1 AND 2),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(session_id,request_key)
) STRICT;
ALTER TABLE archaeology_candidates ADD COLUMN canonical_project_id TEXT NOT NULL DEFAULT '' CHECK (length(canonical_project_id) <= 120);
CREATE TABLE archaeology_candidate_projects (
  session_id TEXT NOT NULL,
  candidate_id TEXT NOT NULL,
  project_id TEXT NOT NULL REFERENCES projects(id),
  mapped_by_principal TEXT NOT NULL CHECK (length(trim(mapped_by_principal)) BETWEEN 1 AND 200),
  purpose TEXT NOT NULL CHECK (length(trim(purpose)) BETWEEN 1 AND 500),
  created_project INTEGER NOT NULL CHECK (created_project IN (0,1)),
  mapped_at TEXT NOT NULL,
  PRIMARY KEY(session_id,candidate_id),
  UNIQUE(session_id,project_id),
  FOREIGN KEY(session_id,candidate_id) REFERENCES archaeology_candidates(session_id,id)
) STRICT;
CREATE TABLE archaeology_native_jobs (
  id TEXT PRIMARY KEY,
  batch_id TEXT NOT NULL REFERENCES archaeology_native_batches(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES archaeology_sessions(id) ON DELETE CASCADE,
  candidate_id TEXT NOT NULL,
  project_id TEXT NOT NULL REFERENCES projects(id),
  mode TEXT NOT NULL CHECK (mode='app_server_dynamic_tools'),
  state TEXT NOT NULL CHECK (state IN ('queued','starting','active','report_ready','cancel_requested','completed','failed','interrupted','uncertain','attention')),
  thread_id TEXT NOT NULL DEFAULT '' CHECK (length(thread_id) <= 120),
  codex_session_id TEXT NOT NULL DEFAULT '' CHECK (length(codex_session_id) <= 120),
  turn_id TEXT NOT NULL DEFAULT '' CHECK (length(turn_id) <= 120),
  phase_label TEXT NOT NULL DEFAULT '' CHECK (length(phase_label) <= 120),
  sources_examined INTEGER NOT NULL DEFAULT 0 CHECK (sources_examined BETWEEN 0 AND 10000),
  duration_ms INTEGER CHECK (duration_ms IS NULL OR duration_ms BETWEEN 0 AND 604800000),
  report_digest BLOB CHECK (report_digest IS NULL OR length(report_digest)=32),
  error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 80),
  created_at TEXT NOT NULL,
  started_at TEXT,
  reported_at TEXT,
  terminal_at TEXT,
  updated_at TEXT NOT NULL,
  UNIQUE(batch_id,candidate_id),
  UNIQUE(batch_id,project_id),
  FOREIGN KEY(session_id,candidate_id) REFERENCES archaeology_candidates(session_id,id)
) STRICT;
CREATE INDEX archaeology_native_batches_session_created ON archaeology_native_batches(session_id,created_at DESC);
CREATE INDEX archaeology_native_jobs_batch_state ON archaeology_native_jobs(batch_id,state,created_at,id);
CREATE UNIQUE INDEX archaeology_native_jobs_thread ON archaeology_native_jobs(thread_id) WHERE thread_id != '';
CREATE UNIQUE INDEX archaeology_native_jobs_turn ON archaeology_native_jobs(turn_id) WHERE turn_id != '';
CREATE TABLE archaeology_native_outcomes (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES archaeology_native_jobs(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL,
  title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 300),
  summary TEXT NOT NULL CHECK (length(summary) <= 4000),
  source_count INTEGER NOT NULL CHECK (source_count BETWEEN 1 AND 1000),
  proposal_json TEXT NOT NULL CHECK (json_valid(proposal_json)),
  created_at TEXT NOT NULL
) STRICT;
CREATE TABLE archaeology_native_provenance (
  outcome_id TEXT NOT NULL REFERENCES archaeology_native_outcomes(id) ON DELETE CASCADE,
  position INTEGER NOT NULL CHECK (position BETWEEN 0 AND 99),
  kind TEXT NOT NULL CHECK (kind IN ('git','docs','codex_history')),
  stable_id TEXT NOT NULL CHECK (length(trim(stable_id)) BETWEEN 1 AND 300),
  digest TEXT NOT NULL CHECK (digest GLOB 'sha256:*' AND length(digest)=71),
  occurred_at TEXT NOT NULL,
  PRIMARY KEY(outcome_id,position)
) STRICT;
CREATE TABLE archaeology_native_outcome_contributors (
  outcome_id TEXT NOT NULL REFERENCES archaeology_native_outcomes(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL CHECK (length(trim(session_id)) BETWEEN 1 AND 200),
  contribution TEXT NOT NULL CHECK (length(trim(contribution)) BETWEEN 1 AND 1000),
  demonstrated_strength TEXT NOT NULL DEFAULT '' CHECK (length(demonstrated_strength) <= 300),
  uncertainty TEXT NOT NULL DEFAULT '' CHECK (length(uncertainty) <= 500),
  confidence TEXT NOT NULL CHECK (confidence IN ('verified','supported','uncertain')),
  PRIMARY KEY(outcome_id,session_id)
) STRICT;
CREATE INDEX archaeology_native_outcomes_job ON archaeology_native_outcomes(job_id);
CREATE TRIGGER archaeology_native_provenance_no_update BEFORE UPDATE ON archaeology_native_provenance
BEGIN SELECT RAISE(ABORT,'append-only native provenance'); END;
CREATE TRIGGER archaeology_native_provenance_no_delete BEFORE DELETE ON archaeology_native_provenance
BEGIN SELECT RAISE(ABORT,'append-only native provenance'); END;
