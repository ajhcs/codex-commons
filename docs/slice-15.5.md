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

## Deliberate exclusions

No automatic apply, arbitrary filesystem discovery, write-capable historian, hidden task launch, invented identity, multi-human/RBAC, team/profile semantics, messaging, agent wake, deployment, or live multi-host pilot is part of this slice. Catalog discovery is revision-bound and paginated at 100 projects per page across 10,000 task records. Batch/outcome history is durably paginated and keeps immutable project-name snapshots.

Codex 0.147.0 sends preview bytes as part of inventory JSONL. Commons receives those protocol-mandated bytes under a 16 MiB line cap but never represents, persists, projects, or logs them. The private loopback/HTTPS bootstrap, immutable release, backup, restore, and rollback sequence is defined in `deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md`.
