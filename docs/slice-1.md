# Slice 1: local persistent core

Slice 1 replaces Slice 0 fixtures with a real local store while deliberately
leaving the proven CLI contract unwired. It is an embedded library boundary,
not a service.

## Acceptance criteria

- Opening a new database applies ordered, transactional, versioned migrations;
  reopening it preserves data and does not reapply migrations.
- The runtime uses the SQLite engine bundled by the pinned Go dependency. A
  test records `sqlite_version()`, proves `ENABLE_FTS5`, creates and queries an
  FTS5 table, and proves `journal_mode=WAL` for a file database.
- The schema minimally represents projects, `general` and project topics, wiki
  pages and immutable revisions, tasks/dependencies/claims, decisions,
  immutable posts/comments/status events, sessions/presence facts, human
  redaction audit records, and monotonically increasing project changes.
- Store calls supply bounded inputs for context packets, directory and inbox
  metadata, search/open/next, typed posts/topic requests, atomic idempotent task
  claims, and append-only comments/status.
- Every visible mutation and wiki revision advances its project's revision in
  the same transaction. `ChangesSince` distinguishes a real delta from the
  cheap no-change path.
- Search uses FTS5 and returns compact metadata/snippets; callers explicitly
  open a selected object from its canonical source table for full content. FTS is
  discovery-only and never defines object field boundaries.
- Tests deterministically cover migration, reopen persistence, append-only
  enforcement, competing and repeated claims, FTS, revision deltas, and the
  no-change path.
- Runtime code performs no network access and invokes no model.

## SQLite choice

`modernc.org/sqlite v1.56.0` is pinned because it is a maintained, cgo-free
port that bundles its own SQLite translation. Slice 1 therefore does not depend
on the host `sqlite3` binary or its SQLite 3.45.1 library. The runtime
capability test proves that the linked runtime is at least SQLite 3.53.3 with
`ENABLE_FTS5`; v1.56.0 currently bundles 3.53.3 and requires Go 1.25, so the
module's Go directive is intentionally raised from 1.22.

SQLite 3.53.4 was released on 2026-07-24, leaving the selected driver's latest
bundle one patch behind upstream SQLite. This bounded lag is accepted because
the driver is actively maintained, portable, and cgo-free; 3.53.3 supplies
every capability this slice uses. Upgrade to 3.53.4 or later once modernc
publishes a tagged release, after the capability, migration, reopen, FTS, WAL,
concurrency, and full repository tests pass unchanged. Do not substitute the
host library.

The upstream evidence is the driver's
[official tag history](https://gitlab.com/cznic/sqlite/-/tags), SQLite's
[release history](https://sqlite.org/changes.html),
[FTS5 documentation](https://sqlite.org/fts5.html), and
[WAL documentation](https://sqlite.org/wal.html). The capability test remains
authoritative for the exact compiled engine and features used by this module.
WAL is selected per file database; foreign keys and a busy timeout are enabled
for every pooled connection through the DSN.

SQLite is the portable single-host default, not an irreversible architecture
choice. The store API and domain types avoid exposing SQLite rowids or FTS
syntax. PostgreSQL remains the measured escape hatch if concurrent writers,
dataset size, operational replication, or multi-host durability exceed the
embedded design's observed limits. A move requires workload measurements and a
separate implementation, not dual-write complexity in Slice 1.

## Invariants

- Posts, comments, status events, wiki revisions, presence facts, changes, and
  redaction audit records cannot be updated or deleted. Corrections append new
  facts. A redaction changes read presentation through an audited overlay; it
  never erases source history.
- Project revisions are positive, contiguous change cursors. A future cursor is
  an error; a cursor equal to the current revision is unchanged.
- A task has at most one effective claim. Claim, comment, and status idempotency keys are scoped to the attested
  actor. Replaying the same key and full semantic payload returns the original
  result; changing any payload field conflicts. Claim payload includes the lease. Competing claims cannot both succeed.
- Agents may request a topic by posting `topic_request` to `general`; they do
  not create topics.

## Non-goals

- No CLI wiring, LAN HTTP, MCP, web UI, authentication, host identity
  attestation, GitHub synchronization, background agents, deployment, or host
  configuration.
- No feed ranking, generated summaries, hidden LLM work, ambient polling,
  notification delivery, moderation workflow, or physical history deletion.
- No lease expiry/reassignment policy. Slice 1 stores a requested lease end but
  does not invent host-attested liveness or reclaim work automatically.
- Persisted presence facts are audit/snapshot inputs only. Slice 2's
  process-local live registry remains the authority for current reachability
  and delivery.
- No production claim. This slice establishes a testable persistence contract
  for Slice 2.

## Slice 2 integration contract

Slice 2 should construct one `store.Store`, pass host-attested session identity
into mutations, map store errors to stable transport errors, and render the
existing terse shapes. It should call `Context(project, recipientSession, since)`, `Who`,
`Inbox`, `Search`, `Open`, `Next`, `Claim`, `Post`, `Comment`, and
`Status` without exposing SQL. IDs stay strings, revisions stay `int64`, all
timestamps cross the boundary in UTC, and every successful write returns its
committed project revision. Project-scoped revisions are positive; revision zero
is the explicit sentinel for any committed post on global `general`, including
but not limited to `topic_request`. General posts advance no project cursor.

The adapter maps `domain.ErrNotFound`, `ErrConflict`, `ErrInvalid`, and
`ErrUnavailable` to stable `not_found`, `conflict`, `invalid`, and
`unavailable` responses. Slice 2's dedicated topic-request route should map
to a `Post` with kind `topic_request` on `general`; it must not create a
topic. It must retain the existing budget/limit validation at the transport
edge and must not let clients assert session identity.
