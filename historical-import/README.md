# Historical import preview

This area reconstructs the completed Codex Commons work as a reviewable historical batch. It does not write a database, start a server, or call the network.

The current v1 corpus intentionally contains:

- 20 outcome-level completed tasks, rather than one task per message or final response.
- 37 distinct spawned Codex sessions attached once to the outcome they helped implement, review, or evaluate.
- Four user-owned root/fork session IDs recorded separately as project thread aliases.
- 13 review, interruption, retry, remediation, or evaluation events.
- The three current dogfood tasks as an offline collision snapshot; they remain authoritative and are never rewritten by this batch.

The accounting invariant is therefore `4 project aliases + 37 task sessions = 41 sessions`. A project thread alias is lineage only. It is never an owner, contributor, event author, presence assertion, reachable task, chat target, or wake control.

## Preview

From the repository root:

```sh
go run ./historical-import/cmd
```

The default command reads only:

- `historical-import/manifests/codex-commons.v1.json`
- `historical-import/snapshots/codex-commons-current.v1.json`
- The repository-relative evidence files declared by the manifest

It prints deterministic JSON. The report includes source and manifest digests, exact counts, per-task idempotency keys, collision decisions, redaction findings, source time, and an intentionally empty recorded time. It always reports `network_calls: 0`.

For a gate that fails when any evidence, privacy, or schema blocker remains:

```sh
go run ./historical-import/cmd --require-apply-eligible
```

Tests:

```sh
go test ./historical-import/...
```

## Evidence boundary

The sanitized evidence inventory contains identifiers, outcome labels, turn status, and UTC timestamps only. It excludes:

- User prompts and assistant bodies
- Hidden reasoning or chain-of-thought
- Tool stdout and command transcripts
- Secrets, credentials, and email addresses
- Full private filesystem paths
- Absolute generated-image paths

Every evidence file is bounded to 2 MiB, must be a regular file under the chosen review root, and is checked against its declared SHA-256 digest. Symlink escapes, digest mismatches, duplicate source references, and unsafe locators block eligibility.

The selected completion time for each task comes from an immutable completed Codex turn after corroborating the outcome with repository documentation and later verification. Thread `updated_at` is never used as completion evidence. Four project alias times come from their UUIDv7 creation timestamps and do not represent task completion.

`source.occurred_at` answers when the original evidence occurred. `recorded_at` must be supplied later by the server-attested import receipt. The importer never backdates the recording event.

## Collision and replay policy

The offline snapshot is deliberately inspectable and currently names the three authoritative dogfood tasks. Exact stable-key matches are checked first and normalized-title matches second. A match is reported as `skip_current`; the current canonical object wins.

Normalized-title matches are collected as candidates, never stored in a last-write-wins map. If more than one current task has the same normalized title, preview is blocked unless one exact stable-key match disambiguates it.

The future server preview must repeat collision detection against current storage immediately before apply. The checked-in snapshot is not a substitute for that canonical check. A non-empty event session must also be attributed to the same task.
The dormant apply request retains every reviewed task, including offline `skip_current` candidates. The canonical server, not the client snapshot, chooses `created`, `skipped_current`, or `replayed` and accounts for each item in one receipt.


Idempotency keys include the batch, object key, and source-digest prefix. Replaying the same batch and digest returns the prior receipt. Reusing a batch ID with changed evidence is a conflict.

Apply is atomic. A non-applied receipt containing a canonical task ID or created count is rejected as an impossible partial write.

## Dormant apply seam

`contract.go` matches the human-only canonical routes:

- `POST /v1/projects/{project}/historical-imports/preview`
- `POST /v1/projects/{project}/historical-imports/apply`

The apply request requires the exact reviewed `confirm_source_digest`. The transport additionally requires the existing same-origin human cookie, CSRF token, and `Idempotency-Key`. Import identity is never caller supplied: the server attests the recorder as `historical-import`.

This area intentionally contains no HTTP client and the CLI has no apply flag. A later human approval must review the fresh server preview and explicitly authorize the canonical HTTP apply. There is no direct SQLite path.

## Files

- `manifests/codex-commons.v1.json`: the exact proposed historical batch
- `sources/verified-thread-inventory.v1.json`: sanitized thread and turn evidence
- `snapshots/codex-commons-current.v1.json`: offline current-object collision input
- `previews/codex-commons.v1.preview.json`: deterministic review artifact; regenerate after any manifest, source, or snapshot change
- `manifest.go`: bounded schema, privacy checks, and digest generation
- `sources.go`: offline evidence-file verification
- `preview.go`: deterministic current-wins preview
- `contract.go`: dormant apply DTO, replay rules, and atomic receipt validator
- `manifest_test.go`: exact corpus and safety regressions
