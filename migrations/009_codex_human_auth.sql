-- Slice 14: managed Codex account binding, public human profile, and
-- append-only security outcomes. No provider email, token, browser cookie,
-- device code, or verification URL is persisted here.
CREATE TABLE human_account_bindings (
  principal TEXT PRIMARY KEY
    CHECK (principal = 'human:local-admin'),
  provider TEXT NOT NULL
    CHECK (provider = 'chatgpt'),
  provider_subject_digest BLOB NOT NULL
    CHECK (length(provider_subject_digest) = 32),
  display_name TEXT NOT NULL
    CHECK (length(trim(display_name)) BETWEEN 1 AND 200),
  handle TEXT NOT NULL COLLATE NOCASE UNIQUE
    CHECK (
      length(handle) BETWEEN 3 AND 64
      AND handle = lower(handle)
      AND handle NOT GLOB '*[^a-z0-9-]*'
      AND substr(handle, 1, 1) GLOB '[a-z0-9]'
      AND substr(handle, -1, 1) GLOB '[a-z0-9]'
    ),
  revision INTEGER NOT NULL DEFAULT 1
    CHECK (revision >= 1),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE human_auth_events (
  id TEXT PRIMARY KEY,
  principal TEXT NOT NULL
    CHECK (principal = 'human:local-admin'),
  event_type TEXT NOT NULL
    CHECK (event_type IN ('account_bound', 'profile_updated', 'recovery_login')),
  binding_revision INTEGER NOT NULL
    CHECK (binding_revision >= 0),
  request_key TEXT NOT NULL DEFAULT '',
  request_digest BLOB NOT NULL DEFAULT X''
    CHECK (length(request_digest) IN (0, 32)),
  recorded_at TEXT NOT NULL
) STRICT;

CREATE UNIQUE INDEX human_auth_events_request_key
  ON human_auth_events(principal, event_type, request_key)
  WHERE request_key <> '';

CREATE TRIGGER human_auth_events_no_update
BEFORE UPDATE ON human_auth_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;

CREATE TRIGGER human_auth_events_no_delete
BEFORE DELETE ON human_auth_events
BEGIN SELECT RAISE(ABORT,'append-only'); END;
