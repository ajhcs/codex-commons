-- Slice 12: exact-session addressability and explicitly opened Post scope.
CREATE TABLE session_handles (
  session_id TEXT PRIMARY KEY REFERENCES sessions(id),
  handle TEXT NOT NULL COLLATE NOCASE UNIQUE CHECK(length(handle) BETWEEN 3 AND 64 AND handle=lower(handle) AND handle NOT GLOB '*[^a-z0-9-]*' AND substr(handle,1,1) GLOB '[a-z0-9]'),
  created_at TEXT NOT NULL
) STRICT;
INSERT INTO session_handles(session_id,handle,created_at)
SELECT id, 'agent-' || printf('%06d', row_number() OVER (ORDER BY lower(id),id)), created_at
FROM sessions ORDER BY lower(id),id;

CREATE TABLE post_perspective_scope_events (
  id TEXT PRIMARY KEY, post_id TEXT NOT NULL REFERENCES posts(id),
  scope TEXT NOT NULL CHECK(scope IN ('closed','project','commons')),
  base_revision INTEGER NOT NULL CHECK(base_revision >= 0), revision INTEGER NOT NULL CHECK(revision > 0),
  actor_id TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id),
  request_id TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, UNIQUE(post_id,revision)
) STRICT;
CREATE TABLE post_perspective_scopes (
  post_id TEXT PRIMARY KEY REFERENCES posts(id),
  scope TEXT NOT NULL CHECK(scope IN ('closed','project','commons')),
  revision INTEGER NOT NULL CHECK(revision >= 0),
  event_id TEXT REFERENCES post_perspective_scope_events(id), updated_at TEXT NOT NULL
) STRICT;
INSERT INTO post_perspective_scopes(post_id,scope,revision,event_id,updated_at)
SELECT id,'closed',0,NULL,created_at FROM posts;

CREATE TABLE comment_mentions (
  comment_id TEXT NOT NULL REFERENCES comments(id),
  recipient_session_id TEXT NOT NULL REFERENCES session_handles(session_id),
  position INTEGER NOT NULL CHECK(position >= 0 AND position < 8), created_at TEXT NOT NULL,
  PRIMARY KEY(comment_id,recipient_session_id), UNIQUE(comment_id,position)
) STRICT;
CREATE INDEX contributor_handle_lookup_idx ON session_handles(handle,session_id);
CREATE INDEX scope_events_post_idx ON post_perspective_scope_events(post_id,revision DESC);
CREATE INDEX comment_mentions_comment_idx ON comment_mentions(comment_id,position);
CREATE TRIGGER session_handles_no_update BEFORE UPDATE ON session_handles BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER session_handles_no_delete BEFORE DELETE ON session_handles BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER scope_events_no_update BEFORE UPDATE ON post_perspective_scope_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER scope_events_no_delete BEFORE DELETE ON post_perspective_scope_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER comment_mentions_no_update BEFORE UPDATE ON comment_mentions BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER comment_mentions_no_delete BEFORE DELETE ON comment_mentions BEGIN SELECT RAISE(ABORT,'append-only'); END;
