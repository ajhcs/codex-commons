# Project Archaeology contract

Project Archaeology is an optional authenticated continuation after Codex onboarding. Skipping or closing it sends no request. It is an experimental, controlled, trusted single-human dogfood feature—not a share-ready or multi-user service.

## Discovery and authority

Discovery builds a bounded catalog from metadata for projects already known to the paired Codex App Server, including active and archived tasks. Optional mode-0600 configured roots are additive. This covers remote or co-located App Server catalogs; starting a task additionally requires an eligible project working directory available to the paired App Server. Candidate APIs and storage exclude raw paths, prompts, transcripts, file bodies, model reasoning, secrets, and private content.

Source choices govern which evidence a historian may cite. They are not filesystem isolation. Every report is checked against the selected depth, enabled evidence kinds, stable-ID rules, exact digests, byte limits, and cardinality limits.

Human reads and writes require the local principal. Writes also require same-origin, CSRF, an `Idempotency-Key`, and the current `base_revision`. Reusing a key with different input or acting on a stale revision conflicts.

Phase 3 makes the Store, rather than the HTTP or application layer, authoritative for native import eligibility. A principal-owned batch is eligible only when it is completed, its execution policy is attested, it has at least one job and outcome, every job is completed, and every outcome belongs to a report-bearing job bound to the same batch, session, and project. Preview, paged review, Apply, and exact Apply replay all recheck this same predicate at their Store boundary. A stale UI capability such as `can_apply` is therefore never sufficient authority for a write.

## Native execution

Start atomically maps or creates only empty Commons project/topic shells, snapshots the execution policy, and queues one named `gpt-5.6-luna` task at `max` reasoning per selected project. Historian tasks are non-ephemeral, read-only, and use `approvalPolicy: never`. Commons submits every manually confirmed task in the batch, up to the 30-project selection limit. Codex App Server governs execution capacity and allowance scheduling.

The native scheduler binds separate batch, job, candidate, project, Codex thread, session, and turn identities. It accepts progress and a bounded review report only from the exact bound thread and turn. It never applies history automatically.

An App Server error after launch begins is uncertain acceptance, not proof of failure. Commons does not retry. Recovery searches active and archived inventories by the exact deterministic task title and project working directory, re-reads the unique match, and accepts exactly one metadata-only turn. Zero, multiple, incomplete, or identity-less matches remain uncertain. A human may confirm a task stopped only by submitting the exact stored job, thread, and turn IDs; the resolution is append-only. One unresolved task blocks new launches globally.

Queued cancellation finishes synchronously. Active work receives one interrupt request. Loss of an exact active turn becomes uncertain. Native pause, resume, retry, and automatic restart are unsupported; legacy pause/resume records remain audit-only and never form a second control plane when native execution is unavailable or feature-disabled.

## Phase 3 durable scheduler writes

The native scheduler uses a SQLite persistence-intent ledger for the five repository mutations that can occur around an external task lifecycle:

| Operation | Store evidence retained |
| --- | --- |
| `fail_start` | The bounded launch result and whether acceptance is uncertain |
| `bind_identity` | The exact Codex thread, session, and turn IDs |
| `activate` | The exact thread and turn whose job may become active |
| `lose_turn` | The exact thread and turn known to be unavailable |
| `complete_turn` | The exact thread, turn, terminal status, and optional duration |

For each operation, the scheduler first ensures one deterministic `pending` row keyed by job and operation, then attempts the replay-safe Store mutation, and finally reads the ledger row back. Only an `applied` readback authorizes dependent lifecycle progress. A `blocked` or `superseded` readback never authorizes that progress. This makes a successful method return alone insufficient evidence when the durable row says otherwise.

The current retry worker wakes on scheduler signals and a one-second process ticker. Each durable pass leases at most 32 due rows for 30 seconds, uses exponential retry delays from 100 milliseconds up to 30 seconds, and blocks an intent after eight failed attempts. A separate process-local queue gives intents whose initial ledger write failed a bounded, fair retry pass. This queue is not represented as durable evidence; claims remain closed until both it and the durable ledger are clear.

Any durable `pending`, `leased`, or `blocked` row is persistence attention: readiness is degraded and the scheduler does not claim another job. Attention is distinct from an unexpected persistence fault, but neither condition permits a claim. Persistence retry only replays the five SQLite mutations above. External `Launch`, `Finalize`, and `Interrupt` calls are not ledger operations and are never replayed during startup recovery.

Startup ordering is part of the boundary. The server runs `ReconcileArchaeology` first, which conservatively marks an in-flight native job uncertain, and then runs `ReconcileArchaeologyNativePersistence`, which applies any stronger exact ledger evidence. An exact `complete_turn` may replace generic restart uncertainty with the recorded terminal state and duration. An exact `lose_turn` keeps the job uncertain but normalizes the reason to `codex_process_unavailable`. Neither path reopens the turn or contacts the App Server.

## Review and import boundary

Native Apply is default-off and remains review-only until both report acceptances pass. While disabled, `can_apply` is false and the UI presents no Apply action. When explicitly enabled, the human may choose any completed proposals from one batch and must review every ordered five-item page. Each page uses a distinct idempotency key and a principal-bound 43-character review session; only the final page returns the completion token accepted by Apply. The server recomputes the exact ordered multi-project diff inside one SQLite transaction and imports every selected proposal plus append-only audit, or nothing. Generic historical Apply rejects native proposals.

For one project, `source_digest` identifies the reviewed evidence snapshot. Proposals with the same project and source digest are alternatives, not a combinable set: the UI permits choosing either one and the server rejects a combined preview before any review or canonical write. The durable uniqueness invariant remains `UNIQUE(project_id, source_digest)`.

A completed Apply is replayed before canonical preview recomputation only when principal, idempotency key, batch, sorted outcome IDs, selection digest, and manifest digest exactly match the append-only audit. This exact replay remains stable after restart or later canonical changes. Reusing the key with any different bound value conflicts.

The first Apply and its audit receipt commit in the same SQLite writer transaction. Inside that transaction, the Store reserves the writer boundary, revalidates eligibility and the exact selected outcomes, recomputes the selection and manifest digests against current canonical state, consumes the completed review token, applies every selected import, and inserts one append-only receipt. Any error rolls the whole transaction back. The receipt table rejects updates and deletes; an exact replay returns that recorded result only after eligibility is revalidated, while any tuple mismatch conflicts.

Review and history remain bound to their exact source batch. Starting a newer batch does not hide prior reports. Exact contributor session IDs are provenance only; membership does not imply reachability, execution, authority, or a persona.

The older canonical historical-import path remains separate. Where a legacy proposal is eligible, the human must inspect the complete task-and-evidence diff, type the exact server-derived manifest digest, and submit both exact manifest and source confirmations. Current Commons records win collisions.

## Human API

- `GET /v1/project-archaeology`
- `GET /v1/project-archaeology/catalog`
- `GET /v1/project-archaeology/batches`
- `GET /v1/project-archaeology/batches/{id}`
- `GET /v1/project-archaeology/batches/{id}/outcomes`
- `POST /v1/project-archaeology/discover`
- `PUT /v1/project-archaeology/config`
- `POST /v1/project-archaeology/start`
- `POST /v1/project-archaeology/cancel`
- `POST /v1/project-archaeology/resolve`
- `POST /v1/project-archaeology/import-preview` (legacy canonical bridge only)
- `POST /v1/project-archaeology/batches/{id}/import-preview`
- `POST /v1/project-archaeology/batches/{id}/import-preview-page`
- `POST /v1/project-archaeology/batches/{id}/import-apply` (feature-flagged)

Legacy pause/resume and task claim/report routes remain compatibility surfaces, not native controls.

Configuration selects 1–30 projects, `quick`, `standard`, or `deep` depth, and at least one of Git, documentation, or Codex history. The legacy `max_concurrency` field remains fixed at 2 for schema, wire, and audit compatibility; it is not a scheduling promise. More than five selections require a second server acknowledgement. Catalogs are revision-bound and cursor-paginated at 100 projects per page across a bounded 10,000-task inventory. Batch history and outcomes are durable cursor pages; immutable job snapshots retain understandable project names. Native reports are below 60 KiB, each proposal is below 32 KiB, and every stored provenance row is append-only.

Codex 0.147.0 necessarily includes preview bytes in inventory JSONL. Commons bounds an inbound line at 16 MiB, decodes only required workspace metadata, and immediately discards preview content without representation, persistence, API projection, or logging. Outbound and browser response budgets remain 1 MiB.

## Phase 3 acceptance status

Phase 3 implementation and disposable local evidence are present in this repository. Store and application tests cover the eligibility matrix, transactional Apply rollback and exact replay, persistence-ledger retry and claim gating, and real-Store scheduler restart cases for both exact completion and exact turn loss. The restart tests inject a terminal Store-write failure, preserve a pending intent across database close and reopen, run generic reconciliation before native-persistence reconciliation, verify the exact terminal evidence, and assert that restart performs zero external `Launch`, `Finalize`, or `Interrupt` calls.

This is offline/local proof, not candidate or production proof. It does not claim that an immutable candidate was packaged, deployed, restarted, restored, rolled back, or exercised against a live paired App Server. Those live acceptance and deployment gates remain deferred through Phase 5 and to the Phase 9 live-acceptance procedure.
