-- Durable repository-write intents for native archaeology jobs.
-- External launch/finalize/interrupt calls are deliberately not represented here.
CREATE TABLE archaeology_native_persistence_intents (
  id TEXT PRIMARY KEY CHECK (length(trim(id)) BETWEEN 1 AND 200),
  job_id TEXT NOT NULL REFERENCES archaeology_native_jobs(id) ON DELETE CASCADE,
  operation TEXT NOT NULL CHECK (operation IN ('fail_start','bind_identity','activate','lose_turn','complete_turn')),
  payload_digest BLOB NOT NULL CHECK (length(payload_digest)=32),
  payload_json TEXT NOT NULL CHECK (json_valid(payload_json) AND length(payload_json) BETWEEN 2 AND 65536),
  state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','leased','applied','superseded','blocked')),
  attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 1000),
  next_attempt_at TEXT CHECK (next_attempt_at IS NULL OR length(trim(next_attempt_at)) BETWEEN 1 AND 100),
  lease_owner TEXT NOT NULL DEFAULT '' CHECK (length(lease_owner) <= 200),
  lease_expires_at TEXT CHECK (lease_expires_at IS NULL OR length(trim(lease_expires_at)) BETWEEN 1 AND 100),
  last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 200),
  applied_at TEXT CHECK (applied_at IS NULL OR length(trim(applied_at)) BETWEEN 1 AND 100),
  created_at TEXT NOT NULL CHECK (length(trim(created_at)) BETWEEN 1 AND 100),
  updated_at TEXT NOT NULL CHECK (length(trim(updated_at)) BETWEEN 1 AND 100),
  UNIQUE(job_id,operation),
  CHECK (
    (state IN ('pending','leased') AND next_attempt_at IS NOT NULL AND length(trim(next_attempt_at)) BETWEEN 1 AND 100)
    OR
    (state IN ('applied','superseded','blocked') AND next_attempt_at IS NULL)
  ),
  CHECK (
    (state='applied' AND applied_at IS NOT NULL AND length(trim(applied_at)) BETWEEN 1 AND 100)
    OR
    (state!='applied' AND applied_at IS NULL)
  ),
  CHECK (
    (state='leased' AND length(trim(lease_owner)) BETWEEN 1 AND 200 AND lease_expires_at IS NOT NULL AND length(trim(lease_expires_at)) BETWEEN 1 AND 100)
    OR
    (state!='leased' AND lease_owner='' AND lease_expires_at IS NULL)
  )
) STRICT;

CREATE INDEX archaeology_native_persistence_intents_due
  ON archaeology_native_persistence_intents(state,next_attempt_at,created_at,id);
CREATE INDEX archaeology_native_persistence_intents_status
  ON archaeology_native_persistence_intents(state,updated_at,id);
