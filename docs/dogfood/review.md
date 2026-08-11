# Dogfood workspace independent review

## Verdict

Approved for parent-controlled clean-database bootstrap. Do not apply this manifest to the existing demo database.

The reviewed corpus is deliberately small: three milestones, three tasks, four Wiki pages, and three posts. Every record has a real source, one distinct job, and a useful representation in the current Project Core frontend. The content review removed a ritual cleanup task, three speculative implementation/pilot tasks, and a bootstrap-generated readiness notice. It added one executable task for the README's known stale runtime description.

## Gate results

| Gate | Result |
| --- | --- |
| Source-grounded | Pass. Every record references existing repository or host-runtime evidence; Wiki and Post readers also receive concise visible source paths. |
| Nonduplicative and concise | Pass. Milestones own outcome boundaries, Tasks own executable future work, Wiki owns current truth, and Posts own dated decisions or one open evaluation question. |
| Action-changing | Pass. The initial task set exposes two ready actions and one truthful dependency-blocked bridge specification; speculative future backlog no longer inflates the blocked count. |
| Current frontend fit | Pass. The active milestone, task counts, Roadmap, task detail, project-filtered Posts, Wiki list/search/reader, and explicit source text all have working surfaces. No new UI or schema is required. |
| Manifest flexibility | Pass. Optional bounded arrays may be empty; the manifest is documented as one publication batch, not a required project-completeness template. |
| Mutation boundary | Pass. Default execution validates offline with zero HTTP. Explicit apply uses only authenticated public HTTP routes; the bootstrap packages have no SQLite/database import or database-path input. |
| Replay safety | Pass. Stable per-item idempotency keys replay identical writes, changed payloads fail with `409 conflict`, partial receipts retain only acknowledged IDs, and post-login exits attempt logout. |
| Canonical verification | Pass. Success requires readback of project fields, milestone fields, exact dependency IDs, Wiki bodies, and Post refs/attachments. Negative corruption tests keep `verified` false. |
| Reversible cutover | Pass as an operator procedure. The existing database remains untouched; a new path is started without `--demo-seed`, bootstrapped, and verified. Rollback stops the new listener and restarts the recorded old path. Bootstrap itself intentionally has no delete or rollback operation. |

## Independent evidence

- `go test -count=1 ./internal/bootstrap ./cmd/commons-bootstrap` — passed.
- `go test -race -count=1 ./internal/bootstrap` — passed.
- `go vet ./internal/bootstrap ./cmd/commons-bootstrap` — passed.
- Offline CLI validation returned `completed_phase: "validated"` with counts `3 / 3 / 4 / 3` and no apply configuration.
- A positional manifest argument was rejected with exit code 2 instead of silently selecting the default manifest.
- Static import inspection found HTTP request construction only in the explicit apply runner and no SQLite/database access in `cmd/commons-bootstrap` or `internal/bootstrap`.

## Evidence limit

No live apply, listener restart, database-path switch, or visual audit was performed during this independent review. The existing LAN runtime and database were left untouched. Parent-controlled integrated QA must execute the documented path-switch procedure and retain its operator and JSON receipts.
