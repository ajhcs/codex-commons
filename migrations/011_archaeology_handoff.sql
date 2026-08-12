-- Durable export/claim/report transport for Codex-owned historian tasks.
CREATE TABLE archaeology_handoffs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL UNIQUE REFERENCES archaeology_sessions(id) ON DELETE CASCADE,
  state TEXT NOT NULL CHECK (state IN ('ready_to_claim','claimed','completed','failed')),
  pack_json TEXT NOT NULL CHECK (json_valid(pack_json) AND length(pack_json) <= 65536),
  claimed_by TEXT NOT NULL DEFAULT '' CHECK (length(claimed_by) <= 200),
  claimed_at TEXT,
  failure TEXT NOT NULL DEFAULT '' CHECK (length(failure) <= 500),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE archaeology_handoff_requests (
  handoff_id TEXT NOT NULL REFERENCES archaeology_handoffs(id) ON DELETE CASCADE,
  request_key TEXT NOT NULL,
  operation TEXT NOT NULL CHECK (operation IN ('claim','report')),
  session_id TEXT NOT NULL CHECK (length(trim(session_id)) BETWEEN 1 AND 200),
  request_digest BLOB NOT NULL CHECK (length(request_digest)=32),
  recorded_at TEXT NOT NULL,
  PRIMARY KEY(handoff_id,request_key)
) STRICT;
