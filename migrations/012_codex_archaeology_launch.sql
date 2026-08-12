-- Codex-native project catalog facts and durable historian task launches.
ALTER TABLE archaeology_candidates ADD COLUMN from_codex_metadata INTEGER NOT NULL DEFAULT 0 CHECK (from_codex_metadata IN (0,1));
ALTER TABLE archaeology_candidates ADD COLUMN from_configured_root INTEGER NOT NULL DEFAULT 1 CHECK (from_configured_root IN (0,1));
ALTER TABLE archaeology_candidates ADD COLUMN codex_thread_count INTEGER NOT NULL DEFAULT 0 CHECK (codex_thread_count BETWEEN 0 AND 1000000);

CREATE TABLE archaeology_task_launches (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES archaeology_sessions(id) ON DELETE CASCADE,
  candidate_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('preparing','starting_codex','task_created','claimed','running','report_ready','completed','failed','uncertain')),
  thread_id TEXT NOT NULL DEFAULT '' CHECK (length(thread_id) <= 120),
  codex_session_id TEXT NOT NULL DEFAULT '' CHECK (length(codex_session_id) <= 120),
  turn_id TEXT NOT NULL DEFAULT '' CHECK (length(turn_id) <= 120),
  client_message_id TEXT NOT NULL CHECK (length(trim(client_message_id)) BETWEEN 1 AND 200),
  request_digest BLOB NOT NULL CHECK (length(request_digest)=32),
  grant_digest BLOB NOT NULL CHECK (length(grant_digest)=32),
  grant_expires_at TEXT NOT NULL,
  grant_consumed_at TEXT,
  report_digest BLOB CHECK (report_digest IS NULL OR length(report_digest)=32),
  report_expires_at TEXT,
  report_consumed_at TEXT,
  report_request_digest BLOB CHECK (report_request_digest IS NULL OR length(report_request_digest)=32),
  error TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 500),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(session_id,candidate_id),
  FOREIGN KEY(session_id,candidate_id) REFERENCES archaeology_candidates(session_id,id)
) STRICT;

CREATE INDEX archaeology_task_launches_session_state ON archaeology_task_launches(session_id,state);
CREATE UNIQUE INDEX archaeology_task_launches_thread ON archaeology_task_launches(thread_id) WHERE thread_id != '';
