-- Historical task reconstruction is a bounded, project-scoped projection. It
-- preserves the human recorder separately from attributed source sessions.
-- Historical session IDs never enter sessions, claims, presence, or inbox data.

CREATE TABLE historical_import_batches (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  batch_id TEXT NOT NULL,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  source_digest TEXT NOT NULL,
  manifest_digest TEXT NOT NULL,
  collision_policy TEXT NOT NULL CHECK(collision_policy = 'current_wins'),
  request_key TEXT NOT NULL UNIQUE,
  recorded_by_actor TEXT NOT NULL,
  recorded_by_session TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  UNIQUE(project_id, batch_id),
  UNIQUE(project_id, source_digest)
) STRICT;

CREATE TABLE historical_project_thread_aliases (
  id TEXT PRIMARY KEY,
  batch_record_id TEXT NOT NULL REFERENCES historical_import_batches(id),
  alias TEXT NOT NULL,
  session_id TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(length(source_kind) BETWEEN 1 AND 40),
  source_stable_id TEXT NOT NULL,
  source_digest TEXT NOT NULL,
  source_occurred_at TEXT NOT NULL,
  recorded_by_actor TEXT NOT NULL,
  recorded_by_session TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  UNIQUE(batch_record_id, alias),
  UNIQUE(batch_record_id, session_id)
) STRICT;

CREATE INDEX historical_project_thread_aliases_batch
  ON historical_project_thread_aliases(batch_record_id, alias);

CREATE TABLE historical_import_batch_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  batch_record_id TEXT NOT NULL REFERENCES historical_import_batches(id),
  state TEXT NOT NULL CHECK(state IN ('applied','superseded')),
  reason TEXT NOT NULL DEFAULT '',
  request_key TEXT UNIQUE,
  recorded_by_actor TEXT NOT NULL,
  recorded_by_session TEXT NOT NULL,
  recorded_at TEXT NOT NULL
) STRICT;

CREATE INDEX historical_import_batch_events_latest
  ON historical_import_batch_events(batch_record_id, sequence DESC);

CREATE TABLE historical_import_tasks (
  id TEXT PRIMARY KEY,
  batch_record_id TEXT NOT NULL REFERENCES historical_import_batches(id),
  source_key TEXT NOT NULL,
  canonical_task_id TEXT NOT NULL,
  disposition TEXT NOT NULL CHECK(disposition IN ('created','skipped_current')),
  source_kind TEXT NOT NULL CHECK(length(source_kind) BETWEEN 1 AND 40),
  source_stable_id TEXT NOT NULL,
  source_digest TEXT NOT NULL,
  source_occurred_at TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  UNIQUE(batch_record_id, source_key)
) STRICT;

CREATE UNIQUE INDEX historical_import_tasks_created_task
  ON historical_import_tasks(canonical_task_id) WHERE disposition='created';

CREATE TABLE historical_task_attributions (
  id TEXT PRIMARY KEY,
  import_task_id TEXT NOT NULL REFERENCES historical_import_tasks(id),
  task_id TEXT NOT NULL REFERENCES tasks(id),
  session_id TEXT NOT NULL,
  role TEXT NOT NULL CHECK(role IN ('originator','implementer','reviewer','evaluator')),
  confidence TEXT NOT NULL CHECK(confidence IN ('verified','supported','uncertain')),
  source_kind TEXT NOT NULL CHECK(length(source_kind) BETWEEN 1 AND 40),
  source_stable_id TEXT NOT NULL,
  source_digest TEXT NOT NULL,
  source_occurred_at TEXT NOT NULL,
  recorded_by_actor TEXT NOT NULL,
  recorded_by_session TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  UNIQUE(import_task_id,session_id,role)
) STRICT;

CREATE INDEX historical_task_attributions_task
  ON historical_task_attributions(task_id, recorded_at, id);

CREATE TABLE historical_task_events (
  id TEXT PRIMARY KEY,
  import_task_id TEXT NOT NULL REFERENCES historical_import_tasks(id),
  task_id TEXT NOT NULL REFERENCES tasks(id),
  event_key TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('completed','reviewed','failed','retried','remediated','evaluated')),
  summary TEXT NOT NULL,
  source_session_id TEXT NOT NULL DEFAULT '',
  confidence TEXT NOT NULL CHECK(confidence IN ('verified','supported','uncertain')),
  occurred_at TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(length(source_kind) BETWEEN 1 AND 40),
  source_stable_id TEXT NOT NULL,
  source_digest TEXT NOT NULL,
  recorded_by_actor TEXT NOT NULL,
  recorded_by_session TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  UNIQUE(import_task_id,event_key)
) STRICT;

CREATE INDEX historical_task_events_task_recent
  ON historical_task_events(task_id, occurred_at DESC, id DESC);

CREATE VIEW active_tasks AS
SELECT tasks.* FROM tasks
WHERE NOT EXISTS (
  SELECT 1 FROM historical_import_tasks imported
  JOIN historical_import_batches batch ON batch.id=imported.batch_record_id
  WHERE imported.canonical_task_id=tasks.id AND imported.disposition='created'
  AND (SELECT state FROM historical_import_batch_events event
       WHERE event.batch_record_id=batch.id
       ORDER BY sequence DESC LIMIT 1)='superseded'
);

CREATE TRIGGER historical_import_batches_no_update BEFORE UPDATE ON historical_import_batches
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_import_batches_no_delete BEFORE DELETE ON historical_import_batches
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_project_thread_aliases_no_update BEFORE UPDATE ON historical_project_thread_aliases
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_project_thread_aliases_no_delete BEFORE DELETE ON historical_project_thread_aliases
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_import_batch_events_no_update BEFORE UPDATE ON historical_import_batch_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_import_batch_events_no_delete BEFORE DELETE ON historical_import_batch_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_import_tasks_no_update BEFORE UPDATE ON historical_import_tasks
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_import_tasks_no_delete BEFORE DELETE ON historical_import_tasks
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_task_attributions_no_update BEFORE UPDATE ON historical_task_attributions
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_task_attributions_no_delete BEFORE DELETE ON historical_task_attributions
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_task_events_no_update BEFORE UPDATE ON historical_task_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER historical_task_events_no_delete BEFORE DELETE ON historical_task_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;
