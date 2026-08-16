# Slice 15.5 — controlled native Project Archaeology

Slice 15.5 connects Project Archaeology to the paired Codex App Server without making Commons an orchestrator or granting model output write authority. It is experimental trusted single-human dogfood, not share-ready.

## Bounded flow

1. Commons groups active and archived Codex-known workspace metadata into a bounded project catalog. Optional mode-0600 roots add operator-approved projects. Remote or co-located App Server metadata is in scope; a launch still requires an eligible working directory available to that App Server.
2. The human selects projects, depth, and admissible evidence kinds. Source selection governs evidence that may be cited; it does not provide filesystem isolation. Closing or skipping performs no write.
3. Start persists the exact policy, maps only empty project/topic shells, and queues one named `gpt-5.6-luna`/`max` read-only, never-approve task per project. Commons submits every manually confirmed task in the batch, up to 30 selections; Codex App Server governs execution capacity and allowance scheduling.
4. Commons durably binds each accepted Codex thread, session, and turn. A lost or malformed acceptance response is uncertain and is never retried blindly. Bounded metadata-only recovery requires one exact title-and-working-directory match across active and archived tasks; otherwise the uncertainty gate remains closed.
5. The exact bound task may report bounded progress and a source-grounded proposal through the two Project Archaeology tools. Disabled evidence kinds, invalid stable IDs, mismatched identities, oversized reports, excess rows, or inconsistent contributor/provenance claims are rejected.
6. Native output is retained for human review. Apply is default-off until both report acceptances pass. When enabled, every selected five-item preview page must be reviewed under a principal-bound session; the final completion token authorizes one stale-rejecting, append-only-audited SQLite transaction. Review stays bound to its exact source batch even while a newer batch is queued.

## Recovery and lifecycle

Queued cancellation is final without waiting for a callback. Active cancellation requests one exact turn interruption; a lost turn becomes uncertain. Human resolution requires the stored job, thread, and turn IDs and an append-only `confirmed_stopped` record. Identity-less recovery has no action and no retry. Native pause, resume, retry, and automatic restart are deliberately absent. Persisted native history never falls back to the legacy control plane when native execution is disabled.

## Import boundary

Native review never mutates Tasks, Wiki, Posts, or canonical historical-import tables. The feature-flagged selected Apply route is the only native import path; generic historical Apply rejects native proposals. The separate legacy bridge may apply eligible non-native proposals after complete diff review and exact digest confirmation. Current records win collisions.

## Phase 3 durability extension

Phase 3 keeps authority at the Store boundary. A native batch is eligible for preview, review, Apply, or exact replay only when the principal owns it, the batch is completed and policy-attested, every job is completed, and every outcome is attached to a report-bearing job with matching batch, session, and project identity. `can_apply` is a read-side explanation of that predicate, not a write capability.

Selected Apply is one SQLite writer transaction. It revalidates eligibility, canonicalizes the sorted outcome IDs, recomputes the selection and manifest digests, consumes the completed review token, applies every selected historical import, and inserts the append-only receipt. The replay tuple is the principal, idempotency key, batch, sorted outcome IDs, selection digest, and manifest digest. An exact tuple may return the immutable receipt after eligibility is rechecked; any changed field conflicts.

The scheduler also records a durable intent before each of five replay-safe Store mutations: `fail_start`, `bind_identity`, `activate`, `lose_turn`, and `complete_turn`. It applies the mutation and reads the row back; only `applied` permits the dependent lifecycle step. `pending`, `leased`, or `blocked` work raises persistence attention, degrades readiness, and closes the claim gate. Due work receives bounded leases and exponential retries from scheduler wakes and a one-second ticker. This persistence retry is not a historian-task retry.

On startup, Commons runs generic `ReconcileArchaeology` before `ReconcileArchaeologyNativePersistence`. Generic reconciliation first makes in-flight work uncertain; the second pass may then apply stronger exact completion or turn-loss evidence. Exact completion preserves its recorded state and duration. Exact loss remains uncertain with `codex_process_unavailable`. The ledger deliberately excludes external `Launch`, `Finalize`, and `Interrupt`, so startup recovery cannot replay any of them.

Repository acceptance uses disposable SQLite databases and a real Store plus scheduler. Tests force terminal persistence failures, close and reopen the database, drain the durable intent, verify the complete/lose evidence and healthy ledger, and assert zero external lifecycle replay after restart. This is local/offline evidence only. Candidate packaging, deployment, live restart/restore/rollback, and paired-App-Server acceptance are not proven here; they remain deferred through Phase 5 and to the Phase 9 live-acceptance gate.

## Deliberate exclusions

No automatic apply, arbitrary filesystem discovery, write-capable historian, hidden task launch, invented identity, multi-human/RBAC, team/profile semantics, messaging, agent wake, deployment, or live multi-host pilot is part of this slice. Store-write retries do not change those exclusions: they replay only deterministic local mutations and never create a second external control plane. Catalog discovery is revision-bound and paginated at 100 projects per page across 10,000 task records. Batch/outcome history is durably paginated and keeps immutable project-name snapshots.

Codex 0.147.0 sends preview bytes as part of inventory JSONL. Commons receives those protocol-mandated bytes under a 16 MiB line cap but never represents, persists, projects, or logs them. The private loopback/HTTPS bootstrap, immutable release, backup, restore, and rollback sequence is defined in `deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md`.
