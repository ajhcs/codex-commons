# Continuity and provenance contract

Codex remains the live conversation and execution plane. Commons is the durable control, memory, and attention plane. This slice adds provenance and a review-first historical task projection; it does not add direct messages, request posts, mentions, inboxes, agent wakeups, Wiki dependencies, or new presence semantics.

## Provenance wire shape

Posts and comments expose `author.provenance`; task claims expose `owner_provenance`; task events expose `provenance`; Wiki current/history records expose `provenance`; task detail exposes at most 20 `contributors` plus `contributors_truncated`.

The additive shape is:

```json
{
  "kind": "attested|historical",
  "actor": "optional server-attested actor",
  "session": "optional session evidence",
  "purpose": "optional attested session purpose",
  "role": "optional historical role",
  "confidence": "optional historical confidence",
  "recorded_at": "optional RFC3339 timestamp",
  "source": {
    "kind": "bounded evidence kind",
    "stable_id": "immutable source identifier",
    "digest": "sha256:<64 lowercase hex>",
    "occurred_at": "RFC3339 timestamp"
  },
  "recorded_by": {
    "actor": "server-attested recorder",
    "session": "server-attested recorder session"
  }
}
```

Empty additive fields are omitted. Legacy author, owner, session, and timestamp fields remain source-compatible. A historical session is evidence only: it is never an owner, claim, presence fact, reachable chat, authority, or wake target.

## Historical import routes

All routes are project-scoped POSTs and require the existing human login, same-origin check, CSRF token, and idempotency key:

- `/v1/projects/{project}/historical-imports/preview`
- `/v1/projects/{project}/historical-imports/apply`
- `/v1/projects/{project}/historical-imports/{batch}/supersede`

Agent bearer credentials are rejected. Caller bodies cannot assert the recorder. Preview is read-only. Apply additionally requires `confirm_source_digest` to exactly equal the reviewed `source_digest`.

The schema-version 1 request is bounded to 20 project thread aliases, 25 tasks, 20 attributions per task, and 25 events per task. Every task is `done` and carries an immutable completion `source`. Every attribution carries `session`, `role`, `confidence`, and `source`. Every event carries the same evidence fields; a non-empty event session must already appear in that task's attributions.

Project root/fork aliases are stored separately from task attribution. The reviewed Codex Commons fixture accounts for four project aliases plus 37 spawned task-session links, for 41 sessions total. Alias sessions are rejected as task contributors, event authors, or owners.

## Apply and recovery semantics

The server computes a canonical manifest digest after validation and sorting. Batch/project identity, source digest, and request idempotency make replay deterministic. Apply is one SQLite transaction and records the human importer separately from every attributed source session. Historical session IDs are never inserted into the live `sessions` table.

`current_wins` resolves an exact active historical task key first, then a normalized title fallback. Zero matches creates; one match records `skipped_current`; more than one fallback match returns conflict. No arbitrary candidate is selected. Imported attributions are never attached to a skipped current task.

Import tables are append-only. Supersede appends a batch state event; it never deletes evidence. The `active_tasks` projection hides superseded imported tasks from human and agent lists/counts while direct task open remains available for audit. Receipts report `created`, `skipped_current`, or `replayed` per source key and preserve source time separately from server-recorded time.
