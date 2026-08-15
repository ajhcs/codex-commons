ALTER TABLE archaeology_native_batches ADD COLUMN depth TEXT NOT NULL DEFAULT '' CHECK (depth IN ('','quick','standard','deep'));
ALTER TABLE archaeology_native_batches ADD COLUMN source_git INTEGER NOT NULL DEFAULT 0 CHECK (source_git IN (0,1));
ALTER TABLE archaeology_native_batches ADD COLUMN source_docs INTEGER NOT NULL DEFAULT 0 CHECK (source_docs IN (0,1));
ALTER TABLE archaeology_native_batches ADD COLUMN source_codex_history INTEGER NOT NULL DEFAULT 0 CHECK (source_codex_history IN (0,1));
ALTER TABLE archaeology_native_batches ADD COLUMN policy_attested INTEGER NOT NULL DEFAULT 0 CHECK (policy_attested IN (0,1));
CREATE TABLE archaeology_native_resolutions (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES archaeology_sessions(id) ON DELETE CASCADE,
  job_id TEXT NOT NULL REFERENCES archaeology_native_jobs(id),
  principal TEXT NOT NULL CHECK (length(trim(principal)) BETWEEN 1 AND 200),
  request_key TEXT NOT NULL CHECK (length(trim(request_key)) BETWEEN 1 AND 200),
  request_digest BLOB NOT NULL CHECK (length(request_digest)=32),
  resolution TEXT NOT NULL CHECK (resolution='confirmed_stopped'),
  thread_id TEXT NOT NULL DEFAULT '' CHECK (length(thread_id) <= 120),
  turn_id TEXT NOT NULL DEFAULT '' CHECK (length(turn_id) <= 120),
  created_at TEXT NOT NULL,
  UNIQUE(session_id,request_key),
  UNIQUE(job_id)
) STRICT;
CREATE TRIGGER archaeology_native_resolutions_no_update BEFORE UPDATE ON archaeology_native_resolutions
BEGIN SELECT RAISE(ABORT,'append-only native resolution'); END;
CREATE TRIGGER archaeology_native_resolutions_no_delete BEFORE DELETE ON archaeology_native_resolutions
BEGIN SELECT RAISE(ABORT,'append-only native resolution'); END;
UPDATE archaeology_native_jobs
SET state='uncertain',error_code='execution_policy_unattested',terminal_at=coalesce(terminal_at,updated_at)
WHERE state IN ('starting','active','report_ready','cancel_requested');
UPDATE archaeology_native_jobs
SET state='interrupted',error_code='execution_policy_unattested_never_launched',terminal_at=coalesce(terminal_at,updated_at)
WHERE state='queued';
UPDATE archaeology_native_batches
SET state='attention'
WHERE state NOT IN ('completed','canceled')
  AND id IN (SELECT DISTINCT batch_id FROM archaeology_native_jobs WHERE state='uncertain');
UPDATE archaeology_native_batches
SET state='canceled'
WHERE state NOT IN ('completed','canceled')
  AND id NOT IN (SELECT DISTINCT batch_id FROM archaeology_native_jobs WHERE state='uncertain');
UPDATE archaeology_sessions
SET state='draft',revision=revision+1
WHERE id IN (SELECT DISTINCT session_id FROM archaeology_native_batches WHERE policy_attested=0)
  AND NOT EXISTS (
    SELECT 1 FROM archaeology_native_jobs
    WHERE archaeology_native_jobs.session_id=archaeology_sessions.id
      AND archaeology_native_jobs.state='uncertain'
  );
