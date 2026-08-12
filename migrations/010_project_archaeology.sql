-- User-controlled Project Archaeology. Candidate bodies and raw paths are never
-- persisted; manifests are proposals and cannot directly mutate canonical data.
CREATE TABLE archaeology_sessions (
  id TEXT PRIMARY KEY,
  principal TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL CHECK (state IN ('draft','running','pause_requested','paused','cancel_requested','canceled','completed','failed')),
  discovery_state TEXT NOT NULL CHECK (discovery_state IN ('idle','discovering','ready','failed')),
  discovery_error TEXT NOT NULL DEFAULT '' CHECK (length(discovery_error) <= 500),
  source_roots_scanned INTEGER NOT NULL DEFAULT 0 CHECK (source_roots_scanned BETWEEN 0 AND 100),
  depth TEXT NOT NULL DEFAULT 'standard' CHECK (depth IN ('quick','standard','deep')),
  source_git INTEGER NOT NULL DEFAULT 1 CHECK (source_git IN (0,1)),
  source_docs INTEGER NOT NULL DEFAULT 1 CHECK (source_docs IN (0,1)),
  source_codex_history INTEGER NOT NULL DEFAULT 0 CHECK (source_codex_history IN (0,1)),
  max_concurrency INTEGER NOT NULL DEFAULT 2 CHECK (max_concurrency BETWEEN 1 AND 2),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
  discovered_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE archaeology_candidates (
  session_id TEXT NOT NULL REFERENCES archaeology_sessions(id) ON DELETE CASCADE,
  id TEXT NOT NULL,
  name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 200),
  path_label TEXT NOT NULL CHECK (length(trim(path_label)) BETWEEN 1 AND 300),
  repository_label TEXT NOT NULL DEFAULT '' CHECK (length(repository_label) <= 300),
  last_activity_at TEXT,
  has_git INTEGER NOT NULL CHECK (has_git IN (0,1)),
  has_docs INTEGER NOT NULL CHECK (has_docs IN (0,1)),
  has_codex_history INTEGER NOT NULL CHECK (has_codex_history IN (0,1)),
  duration_min_seconds INTEGER NOT NULL CHECK (duration_min_seconds >= 0),
  duration_max_seconds INTEGER NOT NULL CHECK (duration_max_seconds >= duration_min_seconds),
  relative_cost TEXT NOT NULL CHECK (relative_cost IN ('low','medium','high')),
  privacy_note TEXT NOT NULL CHECK (length(privacy_note) <= 500),
  selected INTEGER NOT NULL DEFAULT 0 CHECK (selected IN (0,1)),
  PRIMARY KEY(session_id,id)
) STRICT;

CREATE TABLE archaeology_runs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES archaeology_sessions(id) ON DELETE CASCADE,
  candidate_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('queued','running','pause_requested','paused','cancel_requested','canceled','completed','failed')),
  phase_label TEXT NOT NULL DEFAULT '' CHECK (length(phase_label) <= 120),
  completed_units INTEGER NOT NULL DEFAULT 0 CHECK (completed_units >= 0),
  total_units INTEGER CHECK (total_units IS NULL OR total_units >= completed_units),
  outcomes_found INTEGER NOT NULL DEFAULT 0 CHECK (outcomes_found >= 0),
  sources_examined INTEGER NOT NULL DEFAULT 0 CHECK (sources_examined >= 0),
  error TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 500),
  runner_key TEXT NOT NULL DEFAULT '' CHECK (length(runner_key) <= 200),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(session_id,candidate_id),
  FOREIGN KEY(session_id,candidate_id) REFERENCES archaeology_candidates(session_id,id)
) STRICT;

CREATE TABLE archaeology_outcomes (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES archaeology_runs(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL,
  title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 300),
  summary TEXT NOT NULL CHECK (length(summary) <= 4000),
  source_count INTEGER NOT NULL CHECK (source_count BETWEEN 1 AND 1000),
  proposal_json TEXT NOT NULL CHECK (json_valid(proposal_json)),
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE archaeology_provenance (
  outcome_id TEXT NOT NULL REFERENCES archaeology_outcomes(id) ON DELETE CASCADE,
  position INTEGER NOT NULL CHECK (position BETWEEN 0 AND 99),
  kind TEXT NOT NULL CHECK (kind IN ('git','docs','codex_history')),
  stable_id TEXT NOT NULL CHECK (length(trim(stable_id)) BETWEEN 1 AND 300),
  digest TEXT NOT NULL CHECK (digest GLOB 'sha256:*' AND length(digest)=71),
  occurred_at TEXT NOT NULL,
  PRIMARY KEY(outcome_id,position)
) STRICT;

CREATE TABLE archaeology_outcome_contributors (
  outcome_id TEXT NOT NULL REFERENCES archaeology_outcomes(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL CHECK (length(trim(session_id)) BETWEEN 1 AND 200),
  contribution TEXT NOT NULL CHECK (length(trim(contribution)) BETWEEN 1 AND 1000),
  demonstrated_strength TEXT NOT NULL DEFAULT '' CHECK (length(demonstrated_strength) <= 300),
  uncertainty TEXT NOT NULL DEFAULT '' CHECK (length(uncertainty) <= 500),
  confidence TEXT NOT NULL CHECK (confidence IN ('verified','supported','uncertain')),
  PRIMARY KEY(outcome_id,session_id)
) STRICT;

CREATE TABLE archaeology_requests (
  principal TEXT NOT NULL,
  request_key TEXT NOT NULL,
  operation TEXT NOT NULL,
  request_digest BLOB NOT NULL CHECK (length(request_digest)=32),
  session_id TEXT NOT NULL REFERENCES archaeology_sessions(id),
  recorded_at TEXT NOT NULL,
  PRIMARY KEY(principal,request_key)
) STRICT;

CREATE INDEX archaeology_runs_session_state ON archaeology_runs(session_id,state);
CREATE INDEX archaeology_outcomes_run ON archaeology_outcomes(run_id);

CREATE TRIGGER archaeology_provenance_no_update BEFORE UPDATE ON archaeology_provenance
BEGIN SELECT RAISE(ABORT,'append-only provenance'); END;
CREATE TRIGGER archaeology_provenance_no_delete BEFORE DELETE ON archaeology_provenance
BEGIN SELECT RAISE(ABORT,'append-only provenance'); END;
