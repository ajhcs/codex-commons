-- Posts are append-only. Attachments are immutable URL metadata written in
-- the same transaction as the post; Commons does not fetch or host the media.
CREATE TABLE post_attachments (
  post_id TEXT NOT NULL REFERENCES posts(id),
  position INTEGER NOT NULL CHECK(position >= 0 AND position < 8),
  kind TEXT NOT NULL CHECK(kind IN ('link','github','image','video')),
  url TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(post_id, position)
) STRICT;

-- State changes append; the latest event is the current presentation state.
-- No event means open. A superseded record must point at its replacement.
CREATE TABLE post_state_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  post_id TEXT NOT NULL REFERENCES posts(id),
  project_id TEXT REFERENCES projects(id),
  project_revision INTEGER,
  state TEXT NOT NULL CHECK(state IN ('open','resolved','superseded')),
  superseded_by TEXT REFERENCES posts(id),
  session_id TEXT NOT NULL,
  request_id TEXT UNIQUE,
  created_at TEXT NOT NULL,
  CHECK((state = 'superseded' AND superseded_by IS NOT NULL) OR
        (state <> 'superseded' AND superseded_by IS NULL))
) STRICT;

CREATE INDEX posts_feed_idx ON posts(created_at DESC, id);
CREATE INDEX posts_topic_feed_idx ON posts(topic_id, created_at DESC, id);
CREATE INDEX posts_project_feed_idx ON posts(project_id, created_at DESC, id);
CREATE INDEX posts_kind_feed_idx ON posts(kind, created_at DESC, id);
CREATE INDEX comments_post_feed_idx ON comments(post_id, created_at, id);
CREATE INDEX post_state_latest_idx ON post_state_events(post_id, sequence DESC);

CREATE TRIGGER post_attachments_no_update BEFORE UPDATE ON post_attachments
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER post_attachments_no_delete BEFORE DELETE ON post_attachments
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER post_state_events_no_update BEFORE UPDATE ON post_state_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER post_state_events_no_delete BEFORE DELETE ON post_state_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;
