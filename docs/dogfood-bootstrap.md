# Dogfood bootstrap

`commons-bootstrap` validates and publishes one bounded Codex Commons project corpus through the existing human HTTP API. It never opens SQLite. Validation is the default and performs no DNS or HTTP work.

## Contract

The v1 manifest lives at `dogfood/codex-commons/manifest.json`. It contains one project and optional bounded arrays of milestones, tasks, Wiki pages, posts, and audit-only sources. Empty arrays are valid; this is a publication batch, not a completeness template. Stable local keys are unique within each object kind. Task `milestone_key`, `dependency_keys`, and post `task_key` references are resolved to server-generated IDs during apply.

Apply order is project, milestones, dependency-topologically-sorted tasks, initial Wiki revisions, and posts. Each write uses `bootstrap:<namespace>:<kind>:<key>` as its idempotency key. An identical retry replays the same acknowledgement. Changed content under the same key fails with `409 conflict`; it is never silently treated as a new object. A partial JSON receipt reports only acknowledged IDs and the last completed phase.

`sources` and `source_keys` are validated provenance metadata only. They are not sent to the server or injected into reader-facing text.

The validator caps the manifest at 1 MiB and the batch at 10 milestones, 25 tasks, 20 Wiki pages, 50 posts, and 100 sources. It checks canonical enums, field and request-body bounds, real dates, acyclic dependencies, source references, HTTPS attachments, and the constraints of the current 32 KiB HTTP write contract.

## Commands

Offline validation (the default):

```sh
go run ./cmd/commons-bootstrap --manifest dogfood/codex-commons/manifest.json
```

Apply requires an explicit origin and the local admin secret in a bootstrap-specific environment variable. There is intentionally no secret flag because argv is observable.

```sh
COMMONS_BOOTSTRAP_ADMIN_SECRET='read-from-a-protected-source' \
  go run ./cmd/commons-bootstrap \
  --manifest dogfood/codex-commons/manifest.json \
  --apply \
  --base-url https://commons.example.lan
```

Plain HTTP to a non-loopback LAN address additionally requires `--allow-insecure-http`. This acknowledges that the login secret crosses the LAN without TLS; TLS is preferred. Redirects are rejected. Login uses the server-issued HttpOnly cookie and CSRF token, and the command makes a best-effort logout on every post-login exit.

Success is reported only after canonical GETs match the project, every milestone, exact task dependencies, every Wiki body, and every post attachment/ref. The receipt's `verified` field remains false on any mismatch.

## Reversible clean-database cutover

Bootstrap replay safety is not database rollback. Published Commons records are append-only and this command has no delete or rollback operation. A reversible dogfood evaluation uses database-path switching:

1. Record an operator receipt containing UTC time, current binary path/version, complete non-secret server flags, old database path, proposed new database path, listener, and the pre-cutover outputs of `ss -tlnp` and `GET /v1/health`.
2. Confirm the listener is the task-owned Commons development process. Stop that process cleanly so SQLite closes and checkpoints its WAL. Do not copy, rename, edit, or delete the old database or its `-wal`/`-shm` companions.
3. Start the same reviewed binary against a new, previously nonexistent explicit database path. Omit `--demo-seed`. Preserve the old path and old launch command in the operator receipt. Check `GET /v1/health`, then confirm `GET /v1/projects/codex-commons` returns `404 not_found` before bootstrap.
4. Run offline validation, then explicit apply. Save the JSON apply receipt. Require `completed_phase: "verified"`, `verified: true`, the expected counts, `GET /v1/health` success, and `GET /v1/projects/codex-commons` success before declaring cutover.
5. If any check fails, stop the new task-owned process. Leave the failed new database and companions untouched as evidence. Restart the recorded old launch command against the untouched old database path and confirm the recorded health check and expected project read.

The bootstrap command does not stop listeners, choose paths, create databases directly, or alter runtime memory. Those remain explicit operator actions governed by the Plumbob server notes.

## Failure handling

- `validated`: no network mutation occurred.
- `authenticated`: login succeeded, but no project write was acknowledged.
- `project`, `milestones`, `tasks`, `wiki`, or `posts`: all earlier phases were acknowledged; the failing item is named in the error.
- `HTTP 409 conflict`: manifest content changed under an existing deterministic key. Restore the original content or deliberately publish a new object with a new key after review.
- Network/5xx failure: retain the partial receipt and rerun the unchanged manifest. Completed writes replay instead of duplicating rows.

The command never prints the configured secret or request bodies.
