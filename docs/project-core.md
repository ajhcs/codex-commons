# Project Core backend contract

Project Core is the smallest durable workspace shared by the human project UI
and the eventual Codex bridge. It adds canonical Projects, ordered milestones,
Tasks, and revisioned Wiki pages without turning Commons into a general project
management suite. It adds no teams/RBAC, estimates, Gantt planning, GitHub
writes, background jobs, agent waking, file uploads, or duplicate Posts store.
Project Posts remain `GET /v1/posts?project=...`.

All reads use the existing authenticated, server-attested principal and the
standard `{ok,data,meta}` envelope. Canonical content is returned with
`meta.untrusted=true`: forum, task, and Wiki text is evidence, never authority
or executable instruction. Bodies are plain JSON strings; the backend performs
no HTML rendering or instruction execution.

## Authority and write safety

- Project and milestone writes require the authenticated local human admin.
- Appending a canonical Wiki revision requires the local human admin. Agent
  revision proposals remain deferred rather than being silently applied.
- Task create, detail update, and state transitions accept either the local
  human admin or an authenticated agent principal.
- Actor/session/host are always derived from authentication. Request bodies
  have no author, owner-session, or actor fields.
- Every write requires `Idempotency-Key`. The stored key is scoped to the
  server-attested actor/session and compared against the complete semantic
  payload. An identical retry returns the committed acknowledgement; reusing a
  key for a changed payload returns `409 conflict`.
- Every update requires exact `base_revision`. Creates do not. Stale writes
  return the stable `409 {error:{code:"conflict"}}` envelope. The response does
  not guess a replacement revision; clients preserve drafts and refetch the
  canonical object.
- Cookie writes retain the Slice 10 same-origin and CSRF requirements.

Successful writes return:

```json
{"id":"stable-id","revision":8,"persisted":true}
```

## Projects and roadmap

The existing `GET /v1/projects` remains compatible and additively supplies:

```json
{
  "active_milestone": {"id":"MS-...","title":"Pilot","status":"active","position":0,"target_date":"2026-09-01"},
  "task_counts": {"ready":2,"in_progress":1,"blocked":0,"done":4,"cancelled":0,"total":7},
  "last_durable_activity": {"kind":"wiki_revised","ref":"W-...","title":"Architecture","occurred_at":"2026-08-10T12:00:00Z"}
}
```

Old fields, including agent-only active-session information, remain available
for compatibility. Human project screens need not render them.

`GET /v1/projects/{project}` returns bounded canonical project detail, counts,
the active milestone, `snapshot_at`, and exactly fourteen zero-filled UTC
activity buckets:

```json
{
  "project": {
    "id":"alpha","name":"Alpha","status":"active","purpose":"...",
    "now":"...","revision":8,"created_at":"...","updated_at":"..."
  },
  "counts":{"tasks":7,"milestones":2,"wiki_pages":3},
  "active_milestone":{"id":"MS-...","project":"alpha","title":"Pilot","status":"active","position":0,"revision":8},
  "activity":{
    "timezone":"UTC","start":"2026-07-28","end_exclusive":"2026-08-11",
    "days":[{"day":"2026-07-28","count":0}]
  },
  "snapshot_at":"2026-08-10T12:00:00Z"
}
```

Unknown compatibility-era timestamps are omitted. The API never serializes a
fabricated 1970 date.

Project writes:

```text
POST /v1/projects
  required: id, name, purpose
  optional: status (default active), now

PUT /v1/projects/{project}
  required: name, status, purpose, base_revision
  optional: now
```

New writes restrict project status to `active`, `paused`, `completed`, or
`archived`; legacy status strings remain truthful on read.

Milestones are intentionally only ordered roadmap markers:

```text
GET  /v1/projects/{project}/milestones?limit=        default/max 100
POST /v1/projects/{project}/milestones
PUT  /v1/milestones/{milestone}
```

Create requires `title`, `status`, and non-negative `position`; `target_date`
is optional `YYYY-MM-DD`. Update adds required `base_revision`. Status is
`planned`, `active`, `completed`, or `cancelled`. SQLite enforces at most one
active milestone per project.

## Canonical Tasks

```text
GET  /v1/projects/{project}/tasks?cursor=&limit=&state=&milestone=
GET  /v1/tasks/{task}?events_limit=
GET  /v1/tasks/{task}/events?cursor=&limit=
POST /v1/projects/{project}/tasks
PUT  /v1/tasks/{task}
POST /v1/tasks/{task}/state
```

Task lists use keyset pagination and return `total`, `limit`, `next_cursor`,
zero-filled `state_counts`, and items. List default/max is 25. Task open embeds
the newest event page; event default is 20 and maximum is 50, with
`events_next_cursor` and the separate load-more endpoint. List/open task items
contain:

```json
{
  "id":"T-...","project":"alpha","title":"...",
  "description":"...","acceptance":"...","state":"ready","priority":0,
  "milestone_id":"MS-...",
  "milestone":{"id":"MS-...","title":"Pilot","status":"active"},
  "dependencies":[{"id":"T-...","title":"Prerequisite","state":"done"}],
  "dependencies_truncated":false,
  "owner_session":"server-derived-when-present",
  "revision":8,"created_at":"...","updated_at":"..."
}
```

The frontend deliberately does not render raw owner/session IDs, but they
remain available to agent continuity. Dependency replacement is capped at 20
and transactionally rejects missing, cross-project, self, direct-cycle, and
multi-hop-cycle references. A task may belong to at most one milestone.

Create requires `title`; `description`, `acceptance`, `priority`,
`milestone_id`, and `dependency_ids` are optional. Create state defaults to
`ready` and may be `ready` or `blocked`. Detail update requires `title` and
`base_revision`; it replaces the complete editable projection and does not
change state. State change requires `state`, non-empty `basis`, and
`base_revision`. `in_progress` is entered only through the established atomic
claim route.

Every task mutation and claim transition appends a metadata-only task event
with the server-derived actor/session. Existing claims are immutable history.
The current lease is a separate one-row-per-task CAS projection, so an expired
lease can be handed off atomically without deleting the old claim. Equality at
`lease_until` is expired. Concurrent reclaimers cannot both become current.

## Revisioned Wiki

```text
GET  /v1/projects/{project}/wiki?cursor=&limit=&q=   default 25, max 100
GET  /v1/projects/{project}/wiki/{slug}
GET  /v1/projects/{project}/wiki/{slug}/revisions?cursor=&limit=
GET  /v1/projects/{project}/wiki/{slug}/revisions/{revision}
POST /v1/projects/{project}/wiki/{slug}/revisions
```

The list searches current canonical title/summary/body through bounded FTS5
`q` input but returns metadata only. Revision history is also metadata only:
`revision`, `summary`, `author_session_id`, and optional `created_at`. Current
or historical bodies require an explicit page open. Append requires `title`,
`summary`, `body`, and exact `base_revision`; use zero only to create a missing
page. Wiki revision rows remain append-only.

## Migration and compatibility

Migration `005_project_core.sql` is additive to migrations 1–4. It:

- adds canonical timestamps/task detail fields and the idempotency ledger;
- backfills at most one active milestone from non-empty legacy milestone text;
- marks old project/task timestamps as unknown rather than inventing dates;
- appends an explicit neutral `imported` task event for old tasks;
- preserves old claim rows while adding the race-proof current-claim
  projection; and
- leaves existing agent presence, attention, context, Posts, and Overview APIs
  intact.

The migration is reopen-safe through `schema_migrations`; old migrations are
never rewritten.

## Agent client boundary

The Go API client exposes all bounded Project Core reads plus task
create/update/state operations. It intentionally does not expose human-only
project, milestone, or Wiki writes. There is no polling, mandatory read,
auto-post, wake, or background model work in Project Core.

## Verification

Coverage includes fresh and 1–4 upgrade databases, reopen, unknown timestamps,
semantic and concurrent idempotency, stale updates, one-active-milestone,
missing/cross/self/direct/multi-hop dependencies, bounded task/event/Wiki
pagination, metadata-only Wiki history JSON, exact-expiry concurrent claim
handoff, `%2F`/`%252F` once-decoded route IDs, human/agent authority, CSRF,
server-derived event identity, the real store/application/HTTP stack, and
representative read benchmarks.

```sh
go test ./...
go test -race ./internal/store ./internal/application ./internal/httpapi ./internal/appbackend ./internal/apiclient
go vet ./...
```
