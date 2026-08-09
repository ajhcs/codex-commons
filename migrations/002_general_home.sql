-- Canonical, bounded sources for the General screen. These are append-only
-- ingestion ledgers; the read model selects the latest state for each
-- attention item and never infers queues or work from free-form text.
CREATE TABLE attention_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL UNIQUE,
  attention_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('open','resolved')),
  severity TEXT NOT NULL CHECK(severity IN ('high','medium','low')),
  title TEXT NOT NULL,
  project_id TEXT REFERENCES projects(id),
  source_ref TEXT NOT NULL,
  accountable_session_id TEXT,
  next_action TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(source_kind IN (
    'task','github_issue','github_pull_request','github_check',
    'host_connectivity','forum_question'
  )),
  untrusted INTEGER NOT NULL CHECK(untrusted IN (0,1)),
  recorded_at TEXT NOT NULL
) STRICT;

CREATE TABLE activity_events (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK(kind IN (
    'project_updated','task_claimed','task_status_changed',
    'decision_recorded','wiki_revised','post_published','comment_added',
    'github_issue_changed','github_pull_request_changed',
    'github_check_changed','github_commit_referenced',
    'host_connected','host_disconnected',
    'wiki_proposal_created','wiki_proposal_reviewed'
  )),
  project_id TEXT REFERENCES projects(id),
  actor_id TEXT NOT NULL,
  object_ref TEXT NOT NULL,
  object_title TEXT NOT NULL,
  outcome TEXT NOT NULL DEFAULT '',
  untrusted INTEGER NOT NULL CHECK(untrusted IN (0,1)),
  occurred_at TEXT NOT NULL,
  recorded_at TEXT NOT NULL
) STRICT;

CREATE INDEX attention_latest_idx
  ON attention_events(attention_id, sequence DESC);
CREATE INDEX attention_home_idx
  ON attention_events(state, recorded_at DESC, attention_id);
CREATE INDEX activity_home_idx
  ON activity_events(occurred_at DESC, id);

CREATE TRIGGER attention_events_no_update BEFORE UPDATE ON attention_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER attention_events_no_delete BEFORE DELETE ON attention_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER activity_events_no_update BEFORE UPDATE ON activity_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER activity_events_no_delete BEFORE DELETE ON activity_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
