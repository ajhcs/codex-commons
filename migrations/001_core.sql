CREATE TABLE projects (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL DEFAULT '',
  purpose TEXT NOT NULL, milestone TEXT NOT NULL DEFAULT '', now_text TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
) STRICT;

CREATE TABLE topics (
  id TEXT PRIMARY KEY, project_id TEXT REFERENCES projects(id), name TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE wiki_pages (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id),
  slug TEXT NOT NULL, title TEXT NOT NULL, current_revision INTEGER NOT NULL DEFAULT 0,
  UNIQUE(project_id, slug)
) STRICT;
CREATE TABLE wiki_revisions (
  page_id TEXT NOT NULL REFERENCES wiki_pages(id), revision INTEGER NOT NULL,
  summary TEXT NOT NULL, body TEXT NOT NULL, author_session_id TEXT NOT NULL,
  created_at TEXT NOT NULL, PRIMARY KEY(page_id, revision)
) STRICT;

CREATE TABLE tasks (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id),
  state TEXT NOT NULL CHECK(state IN ('ready','in_progress','blocked','done','cancelled')),
  title TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 0,
  owner_session_id TEXT, accept_text TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE TABLE task_dependencies (
  task_id TEXT NOT NULL REFERENCES tasks(id), depends_on_task_id TEXT NOT NULL REFERENCES tasks(id),
  PRIMARY KEY(task_id, depends_on_task_id), CHECK(task_id <> depends_on_task_id)
) STRICT;
CREATE TABLE task_claims (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
  session_id TEXT NOT NULL, request_id TEXT NOT NULL UNIQUE,
  claimed_at TEXT NOT NULL, lease_until TEXT, project_revision INTEGER NOT NULL
) STRICT;

CREATE TABLE decisions (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id),
  project_revision INTEGER NOT NULL, title TEXT NOT NULL, rationale TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE posts (
  id TEXT PRIMARY KEY, topic_id TEXT NOT NULL REFERENCES topics(id),
  project_id TEXT REFERENCES projects(id), project_revision INTEGER,
  kind TEXT NOT NULL CHECK(kind IN ('finding','question','notice','decision','topic_request')),
  title TEXT NOT NULL, body TEXT NOT NULL, basis TEXT NOT NULL, ref TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL, request_id TEXT UNIQUE, created_at TEXT NOT NULL
) STRICT;
CREATE TABLE comments (
  id TEXT PRIMARY KEY, post_id TEXT NOT NULL REFERENCES posts(id),
  project_id TEXT REFERENCES projects(id), project_revision INTEGER,
  body TEXT NOT NULL, session_id TEXT NOT NULL, request_id TEXT UNIQUE, created_at TEXT NOT NULL
) STRICT;
CREATE TABLE status_events (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id),
  project_revision INTEGER NOT NULL, ref TEXT NOT NULL, state TEXT NOT NULL,
  detail TEXT NOT NULL, session_id TEXT NOT NULL, request_id TEXT UNIQUE, created_at TEXT NOT NULL
) STRICT;

CREATE TABLE sessions (
  id TEXT PRIMARY KEY, host TEXT NOT NULL, project_id TEXT REFERENCES projects(id),
  purpose TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
) STRICT;
CREATE TABLE presence_facts (
  id INTEGER PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id),
  host_state TEXT NOT NULL, turn_state TEXT NOT NULL, observed_at TEXT NOT NULL
) STRICT;

CREATE TABLE inbox_items (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id),
  recipient_session_id TEXT NOT NULL, kind TEXT NOT NULL, from_session_id TEXT NOT NULL,
  ref TEXT NOT NULL, snippet TEXT NOT NULL, unread INTEGER NOT NULL CHECK(unread IN (0,1)),
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE changes (
  project_id TEXT NOT NULL REFERENCES projects(id), revision INTEGER NOT NULL,
  kind TEXT NOT NULL, ref TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL,
  PRIMARY KEY(project_id, revision)
) STRICT;

CREATE TABLE redactions (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id),
  object_ref TEXT NOT NULL, reason TEXT NOT NULL, replacement TEXT NOT NULL,
  human_actor TEXT NOT NULL, created_at TEXT NOT NULL
) STRICT;

CREATE TABLE search_documents (
  id INTEGER PRIMARY KEY, project_id TEXT REFERENCES projects(id),
  ref TEXT NOT NULL UNIQUE, kind TEXT NOT NULL, revision INTEGER NOT NULL,
  title TEXT NOT NULL, body TEXT NOT NULL
) STRICT;
CREATE VIRTUAL TABLE search_fts USING fts5(
  title, body, content='search_documents', content_rowid='id', tokenize='unicode61'
);
CREATE TRIGGER search_documents_ai AFTER INSERT ON search_documents BEGIN
  INSERT INTO search_fts(rowid,title,body) VALUES(new.id,new.title,new.body);
END;
CREATE TRIGGER search_documents_ad AFTER DELETE ON search_documents BEGIN
  INSERT INTO search_fts(search_fts,rowid,title,body) VALUES('delete',old.id,old.title,old.body);
END;
CREATE TRIGGER search_documents_au AFTER UPDATE ON search_documents BEGIN
  INSERT INTO search_fts(search_fts,rowid,title,body) VALUES('delete',old.id,old.title,old.body);
  INSERT INTO search_fts(rowid,title,body) VALUES(new.id,new.title,new.body);
END;

CREATE INDEX tasks_next_idx ON tasks(project_id, state, priority, id);
CREATE INDEX changes_project_idx ON changes(project_id, revision);
CREATE INDEX inbox_recipient_idx ON inbox_items(recipient_session_id, project_id, unread, created_at);
CREATE INDEX presence_session_idx ON presence_facts(session_id, observed_at DESC);

INSERT INTO topics(id, project_id, name, created_at)
VALUES('general', NULL, 'General', '1970-01-01T00:00:00Z');

-- Immutable history: corrections append; audited redactions only affect presentation.
CREATE TRIGGER wiki_revisions_no_update BEFORE UPDATE ON wiki_revisions BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER wiki_revisions_no_delete BEFORE DELETE ON wiki_revisions BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER posts_no_update BEFORE UPDATE ON posts BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER posts_no_delete BEFORE DELETE ON posts BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER comments_no_update BEFORE UPDATE ON comments BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER comments_no_delete BEFORE DELETE ON comments BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER status_events_no_update BEFORE UPDATE ON status_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER status_events_no_delete BEFORE DELETE ON status_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER presence_facts_no_update BEFORE UPDATE ON presence_facts BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER presence_facts_no_delete BEFORE DELETE ON presence_facts BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER changes_no_update BEFORE UPDATE ON changes BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER changes_no_delete BEFORE DELETE ON changes BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER redactions_no_update BEFORE UPDATE ON redactions BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER redactions_no_delete BEFORE DELETE ON redactions BEGIN SELECT RAISE(ABORT,'append-only'); END;
