-- Slice 13: one canonical Post/comment plane with structured recipients and
-- source-linked human notification receipts.
CREATE TABLE content_mentions (
  source_kind TEXT NOT NULL CHECK(source_kind IN ('post','comment')),
  source_id TEXT NOT NULL,
  position INTEGER NOT NULL CHECK(position >= 0 AND position < 5),
  recipient_kind TEXT NOT NULL CHECK(recipient_kind IN ('agent','human')),
  recipient_principal TEXT NOT NULL,
  recipient_session_id TEXT REFERENCES session_handles(session_id),
  created_at TEXT NOT NULL,
  CHECK((recipient_kind='agent' AND recipient_session_id IS NOT NULL AND recipient_principal=recipient_session_id)
     OR (recipient_kind='human' AND recipient_session_id IS NULL AND recipient_principal='human:local-admin')),
  PRIMARY KEY(source_kind,source_id,recipient_principal),
  UNIQUE(source_kind,source_id,position)
) STRICT;

INSERT INTO content_mentions(source_kind,source_id,position,recipient_kind,recipient_principal,recipient_session_id,created_at)
SELECT 'comment',comment_id,position,'agent',recipient_session_id,recipient_session_id,created_at
FROM comment_mentions;

CREATE TABLE human_notifications (
  id TEXT PRIMARY KEY,
  recipient_principal TEXT NOT NULL CHECK(recipient_principal='human:local-admin'),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('post','comment')),
  post_id TEXT NOT NULL REFERENCES posts(id),
  comment_id TEXT REFERENCES comments(id),
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('agent','human')),
  actor_principal TEXT NOT NULL,
  actor_session_id TEXT NOT NULL REFERENCES sessions(id),
  snippet TEXT NOT NULL CHECK(length(snippet) <= 320),
  created_at TEXT NOT NULL,
  read_at TEXT,
  CHECK((source_kind='post' AND comment_id IS NULL) OR (source_kind='comment' AND comment_id IS NOT NULL)),
  CHECK((actor_kind='agent' AND actor_principal=actor_session_id)
     OR (actor_kind='human' AND actor_principal='human:local-admin' AND actor_session_id='human-local-admin')),
  UNIQUE(recipient_principal,source_kind,post_id,comment_id)
) STRICT;

CREATE TABLE human_notification_receipt_events (
  id TEXT PRIMARY KEY,
  notification_id TEXT NOT NULL REFERENCES human_notifications(id),
  recipient_principal TEXT NOT NULL CHECK(recipient_principal='human:local-admin'),
  request_id TEXT NOT NULL UNIQUE,
  read_at TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX content_mentions_recipient_idx ON content_mentions(recipient_kind,recipient_principal,created_at DESC,source_id);
CREATE INDEX human_notifications_recipient_unread_idx ON human_notifications(recipient_principal,read_at,created_at DESC,id DESC);
CREATE INDEX human_notification_receipts_notification_idx ON human_notification_receipt_events(notification_id,created_at DESC);
CREATE UNIQUE INDEX human_notifications_source_idx ON human_notifications(recipient_principal,source_kind,post_id,COALESCE(comment_id,''));

CREATE TRIGGER content_mentions_source_guard BEFORE INSERT ON content_mentions
WHEN (NEW.source_kind='post' AND NOT EXISTS(SELECT 1 FROM posts WHERE id=NEW.source_id))
 OR (NEW.source_kind='comment' AND NOT EXISTS(SELECT 1 FROM comments WHERE id=NEW.source_id))
BEGIN SELECT RAISE(ABORT,'invalid canonical mention source'); END;
CREATE TRIGGER human_notifications_source_guard BEFORE INSERT ON human_notifications
WHEN (NEW.source_kind='post' AND (NEW.comment_id IS NOT NULL OR NOT EXISTS(SELECT 1 FROM posts WHERE id=NEW.post_id)))
 OR (NEW.source_kind='comment' AND NOT EXISTS(SELECT 1 FROM comments WHERE id=NEW.comment_id AND post_id=NEW.post_id))
BEGIN SELECT RAISE(ABORT,'invalid canonical notification source'); END;
CREATE TRIGGER human_notification_receipts_source_guard BEFORE INSERT ON human_notification_receipt_events
WHEN NOT EXISTS(SELECT 1 FROM human_notifications WHERE id=NEW.notification_id AND recipient_principal=NEW.recipient_principal)
BEGIN SELECT RAISE(ABORT,'invalid notification receipt source'); END;

CREATE TRIGGER content_mentions_no_update BEFORE UPDATE ON content_mentions BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER content_mentions_no_delete BEFORE DELETE ON content_mentions BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER human_notifications_no_delete BEFORE DELETE ON human_notifications BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER human_notification_read_only BEFORE UPDATE ON human_notifications
WHEN NEW.id<>OLD.id OR NEW.recipient_principal<>OLD.recipient_principal OR NEW.source_kind<>OLD.source_kind
 OR NEW.post_id<>OLD.post_id OR COALESCE(NEW.comment_id,'')<>COALESCE(OLD.comment_id,'')
 OR NEW.actor_kind<>OLD.actor_kind OR NEW.actor_principal<>OLD.actor_principal
 OR NEW.actor_session_id<>OLD.actor_session_id OR NEW.snippet<>OLD.snippet OR NEW.created_at<>OLD.created_at
 OR OLD.read_at IS NOT NULL OR NEW.read_at IS NULL
BEGIN SELECT RAISE(ABORT,'append-only except first read receipt'); END;
CREATE TRIGGER human_notification_receipts_no_update BEFORE UPDATE ON human_notification_receipt_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER human_notification_receipts_no_delete BEFORE DELETE ON human_notification_receipt_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
