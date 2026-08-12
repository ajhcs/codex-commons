# Slice 15.5 — truthful real-project cutover

Slice 15.5 closes the Project Archaeology integration seam without pretending
the Commons Go server can create or attest Codex tasks. It keeps the existing
historical import boundary authoritative: current data wins, preview is
non-mutating, and only the signed-in human may apply the exact digest-confirmed
payload.

## Bounded flow

1. The operator configures a mode-0600 JSON allowlist with explicit project
   roots. Discovery reads filesystem metadata only and returns configured
   labels, never raw paths, file bodies, prompts, transcripts, or secrets.
2. The human chooses candidates and sources. Closing or skipping this UI sends
   no request and records nothing.
3. Start creates a durable `ready_to_claim` historian task pack. It does not
   spawn a process, call a model, or claim work is running.
4. An authenticated Codex agent calls the claim endpoint. Its server-attested
   exact session ID becomes the immutable claimant.
5. That same session reports a bounded result with exact source digests and
   contributor session IDs. A completed report creates truthful completed-run
   receipts and a durable review manifest.
6. The human selects one outcome and calls `import-preview`. The response
   contains the canonical historical-import request plus the existing preview
   receipt. Apply remains the existing project endpoint and requires the same
   request with `confirm_source_digest` set to the exact source digest.

## Configuration

Set `COMMONS_ARCHAEOLOGY_ROOTS_FILE` or `--archaeology-roots-file` to a private
JSON file:

```json
{
  "roots": [{
    "id": "codex-commons",
    "name": "Codex Commons",
    "path": "/absolute/operator-approved/path",
    "path_label": "~/projects/codex-commons",
    "repository_label": "codex-commons"
  }]
}
```

The file must be mode 0600. Paths must be absolute existing directories. The
path is used only by the metadata adapter and is not stored in archaeology
tables or returned through the API.

## Endpoints and authority

- Human: `GET /v1/project-archaeology`
- Human: `POST /v1/project-archaeology/discover`
- Human: `PUT /v1/project-archaeology/config`
- Human: `POST /v1/project-archaeology/start`
- Agent: `POST /v1/project-archaeology/handoff/claim`
- Same claimed agent: `POST /v1/project-archaeology/handoff/report`
- Human: `POST /v1/project-archaeology/import-preview`
- Human: existing `/v1/projects/{id}/historical-imports/apply`

All mutations require normal authentication, CSRF where applicable, and an
`Idempotency-Key`. Historical contributor IDs are community provenance, but
their review facts explicitly say `historical_or_unknown`, `not_attested`, and
`provenance_only`; membership does not invent reachability or write authority.

## Deliberate exclusions

No automatic apply, shell spawning, hidden task launch, moderator, Pals,
multi-human/RBAC, separate communication object, live database mutation, or
bulk demo content is part of this slice.
