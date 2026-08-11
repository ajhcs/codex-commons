-- Small durable project core: ordered milestones, canonical task state with an
-- append-only event trail, optimistic revisions, and actor-scoped write
-- idempotency. Existing project milestone text and all prior APIs remain.

ALTER TABLE projects ADD COLUMN created_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z';
ALTER TABLE projects ADD COLUMN updated_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z';

CREATE TABLE milestones (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  title TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('planned','active','completed','cancelled')),
  position INTEGER NOT NULL CHECK(position >= 0),
  target_date TEXT,
  project_revision INTEGER NOT NULL CHECK(project_revision >= 0),
  created_at TEXT,
  updated_at TEXT
) STRICT;

CREATE UNIQUE INDEX milestones_one_active_per_project
  ON milestones(project_id) WHERE status='active';
CREATE INDEX milestones_project_order
  ON milestones(project_id, position, id);

INSERT INTO milestones(id,project_id,title,status,position,target_date,project_revision,created_at,updated_at)
SELECT 'MS-legacy-' || id,id,milestone,'active',0,NULL,revision,NULL,NULL
FROM projects
WHERE trim(milestone)<>'';

ALTER TABLE tasks ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN milestone_id TEXT REFERENCES milestones(id);
ALTER TABLE tasks ADD COLUMN project_revision INTEGER NOT NULL DEFAULT 0 CHECK(project_revision >= 0);
ALTER TABLE tasks ADD COLUMN created_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z';
ALTER TABLE tasks ADD COLUMN updated_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z';

CREATE INDEX tasks_project_updated
  ON tasks(project_id, updated_at DESC, id DESC);
CREATE INDEX tasks_project_milestone
  ON tasks(project_id, milestone_id, state, updated_at DESC, id DESC);

-- Slice 1 treated a claim as one-per-task forever. Preserve every historical
-- claim while allowing a later atomic handoff after its lease expires.
ALTER TABLE task_claims RENAME TO task_claims_single_lease;
CREATE TABLE task_claims (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id),
  session_id TEXT NOT NULL,
  request_id TEXT NOT NULL UNIQUE,
  claimed_at TEXT NOT NULL,
  lease_until TEXT,
  project_revision INTEGER NOT NULL
) STRICT;
INSERT INTO task_claims(id,task_id,session_id,request_id,claimed_at,lease_until,project_revision)
SELECT id,task_id,session_id,request_id,claimed_at,lease_until,project_revision
FROM task_claims_single_lease;
DROP TABLE task_claims_single_lease;
CREATE INDEX task_claims_task_recent
  ON task_claims(task_id, claimed_at DESC, id DESC);

CREATE TABLE task_current_claims (
  task_id TEXT PRIMARY KEY REFERENCES tasks(id),
  claim_id TEXT NOT NULL UNIQUE REFERENCES task_claims(id),
  lease_until TEXT,
  updated_at TEXT NOT NULL
) STRICT;
INSERT INTO task_current_claims(task_id,claim_id,lease_until,updated_at)
SELECT task_id,id,lease_until,claimed_at FROM task_claims;

CREATE TRIGGER task_claims_no_update BEFORE UPDATE ON task_claims
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER task_claims_no_delete BEFORE DELETE ON task_claims
BEGIN SELECT RAISE(ABORT,'append-only'); END;

CREATE TABLE task_events (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id),
  project_id TEXT NOT NULL REFERENCES projects(id),
  project_revision INTEGER NOT NULL CHECK(project_revision >= 0),
  kind TEXT NOT NULL CHECK(kind IN ('imported','created','updated','state_changed','claimed','reclaimed')),
  summary TEXT NOT NULL,
  from_state TEXT,
  to_state TEXT,
  actor_id TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX task_events_task_recent
  ON task_events(task_id, created_at DESC, id DESC);

CREATE TRIGGER task_events_no_update BEFORE UPDATE ON task_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER task_events_no_delete BEFORE DELETE ON task_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;

-- One ledger covers Project Core writes. request_key is already scoped by the
-- server-attested actor using the store's existing length-prefixed key scheme.
CREATE TABLE project_core_requests (
  request_key TEXT PRIMARY KEY,
  operation TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  result_id TEXT NOT NULL,
  result_revision INTEGER NOT NULL CHECK(result_revision >= 0),
  project_revision INTEGER NOT NULL CHECK(project_revision >= 0),
  created_at TEXT NOT NULL
) STRICT;

-- Existing tasks have truthful unknown-era metadata and an explicit imported
-- event rather than a fabricated author or modern creation timestamp.
INSERT INTO task_events(
  id,task_id,project_id,project_revision,kind,summary,
  from_state,to_state,actor_id,session_id,created_at
)
SELECT
  'TE-import-' || id,id,project_id,project_revision,'imported','Imported by compatibility migration',
  NULL,state,'','',created_at
FROM tasks;
