# Project Archaeology backend contract

Project Archaeology is an optional authenticated continuation after Codex onboarding. Skipping it is navigation only and records no server event. It is deliberately separate from pairing, profile creation, and the onboarding completion animation.

## Safety and authority

Discovery is explicit and metadata-only. A configured discovery adapter may return bounded display labels and signals, but candidate bodies, raw prompts, model reasoning, secrets, private content, and raw filesystem paths must not enter the candidate API or database. The built-in server configures discovery only from a mode-0600 project-root allowlist; without it discovery truthfully returns `unavailable` rather than silently scanning `/home`.

All reads and writes require the local human principal. Writes also require same-origin, the human CSRF token, and an `Idempotency-Key`. Configuration and lifecycle commands use `base_revision`; stale revisions conflict. Reusing an idempotency key with a different operation or payload conflicts.

Archaeology creates review manifests only. There is no archaeology apply endpoint. An approved proposal must still pass the existing canonical historical-import preview, exact digest confirmation, current-wins collision policy, and explicit human-only apply operation.

Exact agent session IDs found in evidence are already Commons community members and are retained as bounded contributor provenance even when historical or offline. Membership does not imply current reachability, executability, or credentialed authority. The manifest may record contributions, collaborations, recurring demonstrated strengths, and uncertainty, but must not invent personas.

## HTTP API

- `GET /v1/project-archaeology`
- `POST /v1/project-archaeology/discover`
- `PUT /v1/project-archaeology/config`
- `POST /v1/project-archaeology/start`
- `POST /v1/project-archaeology/pause`
- `POST /v1/project-archaeology/resume`
- `POST /v1/project-archaeology/cancel`

Configuration is atomic:

```json
{
  "selected_project_ids": ["codex-commons"],
  "depth": "standard",
  "sources": {"git": true, "docs": true, "codex_history": false},
  "max_concurrency": 2,
  "base_revision": 2
}
```

Depth is `quick`, `standard`, or `deep`. At least one source must be selected. Concurrency is one or two and defaults to two. Candidate estimates contain duration bounds and relative cost (`low`, `medium`, or `high`); the server never fabricates a percentage or completion time.

Session states are `draft`, `running`, `pause_requested`, `paused`, `cancel_requested`, `canceled`, `completed`, and `failed`. Run states add `queued`. Discovery states are `idle`, `discovering`, `ready`, and `failed`. Controls are returned by the server from canonical state.

## Historian adapter boundary

The production seam is an export/claim/report protocol. Start creates a durable task pack in `ready_to_claim`; it never invokes a shell, model, or unsupported Codex task API. An authenticated agent claims it with the exact server-attested session ID and only that session may report completion. The legacy launcher interface remains inert for compatibility and is not invoked.

On restart, previously running or pause-requested work becomes paused and resumable, while cancel-requested work becomes canceled. Queued work remains queued. This is conservative because the server cannot claim an external task is still running without a durable runner lease.

## Manifest bounds

A session supports at most 100 candidates, at most two concurrent historians, and stored provenance is append-only. Each proposal preserves exact `sha256:` digests and bounded stable source identifiers. Review DTOs expose proposed outcomes, provenance metadata, and aggregate exact member-session evidence. No proposal mutates Projects, Tasks, Wiki, Posts, or canonical historical-import tables.
