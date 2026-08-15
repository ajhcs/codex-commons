# Phase 0 remediation baseline

**Status:** Phase 0 baseline and governance foundation only. No live behavior
was changed by this baseline capture.

**Captured:** 2026-08-15 (UTC), before the Phase 0 files in this worktree were
created.

## Scope and purpose

This ledger is the durable handoff boundary for the validated Codex Commons
remediation program. Phase 0 records the repository and live-release facts
that can be safely established read-only, preserves the existing dirty
worktree, and adds repository safeguards. It does not repair the Project
Archaeology implementation, package a release, migrate a database, perform
Apply, or change live service behavior.

The target checkout is `/home/plumbob/codex-commons`. The writable execution
checkout is the linked worktree
`/home/plumbob/.codex/worktrees/a891/codex-commons`; both point at the same
commit at capture time. The source checkout and live runtime were not edited
by this work.

## Baseline identity

| Fact | Sanitized value |
| --- | --- |
| HEAD | `99b88c8fdf15b81718e615e8a7fd870b0ab39ffd` |
| Worktree branch | Detached (`HEAD`); no branch was created or changed |
| Source branch/upstream | `main` / `origin/main` |
| HEAD versus `origin/main` | `0` ahead, `0` behind |
| Remote | `origin` → `github.com/ajhcs/codex-commons` |
| Git tags at capture | None listed |
| Commit subject | Merge pull request #15, project-picker contract gate |
| Pre-existing status digest | `b0d4209403aa3950a06da862720c89a8a392799e516f919b1c52185bcee25859` |
| Pre-existing tracked-diff digest | `2460de5e5b7dfb961e547956c6d18ed174a58581778570ddc9946af82def6eea` |
| Pre-existing untracked-file-list digest | `090fb616d3c3c3146daf5d018e265360abaf6a7cfded2cccdd82308a5950edfd` |

The three digests are SHA-256 values over, respectively, the pre-edit
`git status --porcelain=v1`, `git diff --name-status`, and
`git ls-files --others --exclude-standard` output. They are boundary checks,
not substitutes for reviewing the paths and their content.

## Dirty-tree preservation boundary

The pre-edit worktree contained only unstaged modifications and untracked
paths: no staged entries, deletions, renames, commits, or pushes were made.

### Tracked modifications present before Phase 0

There were **85 modified tracked paths**. The compact path inventory below is
intentionally grouped rather than copied as a volatile diff blob:

| Surface | Count | Pre-existing scope |
| --- | ---: | --- |
| `.gitignore` | 1 | Existing ignore adjustment for `research/perspective-pilot/` |
| `cmd/` | 1 | `commons-server` entrypoint |
| `docs/` | 2 | Project Archaeology and Slice 15.5 contracts |
| `historical-import/` | 2 | Contract and manifest tests |
| `internal/appbackend/` | 4 | Home and Project Archaeology backend paths/tests |
| `internal/application/` | 7 | Archaeology scheduler/application paths/tests |
| `internal/codexauth/` | 5 | Archaeology client/process paths/tests |
| `internal/domain/` | 2 | Historical import and archaeology domain contracts |
| `internal/httpapi/` | 8 | Auth, routes, handler, and archaeology API paths/tests |
| `internal/server/` | 9 | Auth, mux, config, server, and archaeology paths/tests |
| `internal/store/` | 10 | Historical import, native archaeology, core, and store tests |
| `internal/storebackend/` | 2 | Store backend implementation/tests |
| `web/` | 32 | Existing web instructions, design/asset provenance, AppShell/auth, data adapters, Project Archaeology UI/state/tests, and Vite/package files |

The exact pre-edit tracked path set is reproducible with:

```sh
git diff --name-only 99b88c8fdf15b81718e615e8a7fd870b0ab39ffd
```

No Phase 0 edit overlaps those existing paths.

### Untracked production surfaces present before Phase 0

Git collapsed some untracked directories in short status output. The
pre-existing set expands to **41 untracked files across 25 short-status
entries**:

**Release, deployment, and operations (16 files):**

- `deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md`
- `deploy/caddy/commons.Caddyfile`
- `deploy/systemd/codex-commons-backup.service`
- `deploy/systemd/codex-commons-backup.timer`
- `deploy/systemd/codex-commons.service`
- `deploy/systemd/dogfood.env.example`
- `ops/backup.sh`
- `ops/build-release.sh`
- `ops/check-readiness.sh`
- `ops/deploy-release.sh`
- `ops/record-evidence.sh`
- `ops/seal-archive.sh`
- `ops/stage-release.sh`
- `ops/test-ops.sh`
- `ops/verify-release.sh`
- `ops/verify-restore.sh`

**Server, migration, and native-store surfaces (18 files):**

- `migrations/014_archaeology_execution_policy.sql`
- `migrations/015_continuous_dogfood.sql`
- `internal/domain/human_session.go`
- `internal/httpapi/project_archaeology_response_budget_test.go`
- `internal/server/notify.go`
- `internal/server/project_archaeology_real_acceptance_test.go`
- `internal/store/human_sessions.go`
- `internal/store/human_sessions_test.go`
- `internal/store/installation_status.go`
- `internal/store/migration15_test.go`
- `internal/store/project_archaeology_apply.go`
- `internal/store/project_archaeology_apply_test.go`
- `internal/store/project_archaeology_catalog.go`
- `internal/store/project_archaeology_history.go`
- `internal/store/project_archaeology_policy.go`
- `internal/store/project_archaeology_policy_test.go`
- `internal/store/project_archaeology_review.go`
- `internal/store/project_archaeology_review_test.go`

**Project Archaeology history/browser and web release assets (7 files):**

- `web/scripts/run-project-archaeology-browser-gate.mjs`
- `web/src/features/project-archaeology/ProjectArchaeologyHistory.jsx`
- `web/tests/project-archaeology-production-browser.test.mjs`
- `web/src/assets/fonts/open-sans/OpenSans-Variable.woff2`
- `web/src/assets/identity/PROVENANCE.md`
- `web/src/assets/identity/commons-mark-connecting.png`
- `web/src/assets/identity/commons-mark-resolved.png`

These files were not staged or made release inputs by Phase 0. The listed
paths are the critical surfaces that Phase 1 must intentionally account for
before any packaging decision.

## Candidate and live-release status

No repository tag or new candidate was created by Phase 0. The dirty source
tree is not a reproducible release input and must not be treated as a
candidate.

The following facts were obtained read-only and are safe metadata only:

- The active immutable release is
  `continuous-dogfood-20260813.8`.
- Its `VERSION` matches the release directory name and the current release
  pointer resolves to that immutable directory.
- The release `SHA256SUMS` file has digest
  `bd24f786c0920ec6fb3d147447b61be4512a8f11e11339d85417c63a1185cb67`.
- `codex-commons.service` was active and the only relevant Commons listener
  observed was loopback `127.0.0.1:8088` owned by `commons-server`.
- With the configured public Host, `GET /v1/health` returned `status: ok` and
  version `continuous-dogfood-20260813.8`.
- The read-only internal readiness response reported release `.8`, schema
  version `15`, reconciliation `healthy`, Codex configured `true`, version
  `0.147.0`, and compatibility `compatible`, but Codex `available: false` and
  `account_state: unknown` at capture. This is not a fresh green readiness
  proof.

The server-memory record from the prior `.8` certification reported a
successful no-task preflight, schema 15, healthy backup/reconciliation, zero
active and uncertain scheduler counts, and Apply disabled. That historical
evidence remains useful provenance but was not re-certified here; the current
read-only readiness drift is a Phase 1 gate.

## Explicit NO-GO boundaries

The following boundaries are part of this baseline and remain in force until
a separately authorized phase changes them:

- **NO-GO: dirty-tree packaging.** Do not build, stage, seal, or deploy a
  release from this mixed pre-existing worktree.
- **NO-GO: live deployment or service control.** Do not start, stop, restart,
  reload, switch `current`, alter Caddy/systemd/host configuration, or bind a
  listener.
- **NO-GO: live database change.** Do not apply migrations `014` or `015`,
  mutate the live SQLite store, or run a live restore/rollback rehearsal.
- **NO-GO: candidate activation.** Do not create, activate, attach traffic
  to, or declare a candidate; do not infer approval from the `.8` health 200.
- **NO-GO: Apply or execution.** Do not launch/cancel historian tasks, submit
  reports, recover uncertain tasks, perform native or historical Apply, or
  publish canonical history.
- **NO-GO: evidence fabrication.** Do not copy prompts, transcripts, model
  reasoning, personal content, credentials, or secret-file contents into
  source, logs, receipts, or this ledger.
- **NO-GO: broad ignore changes.** Do not broadly unignore secrets, databases,
  `.local/`, `node_modules/`, `dist/`, or generated output. Required release
  inputs must become intentional tracked files through normal review.
- **NO-GO: Phase 1 scope expansion.** Do not turn this governance baseline
  into release-verifier implementation, candidate repair, or deployment work.

## Ignore and provenance audit

The existing root `.gitignore` and `web/.gitignore` do not ignore `ops/`,
`deploy/`, migrations `014`/`015`, the selected native Apply/store paths, the
Project Archaeology history/browser gates, or the required web assets. The
pre-existing `/research/perspective-pilot/` rule remains unchanged. Generated
outputs and local state remain ignored as intended. No broad ignore rule or
secret unignore was added in Phase 0.

`ops/test-governance.sh` provides the narrow durable check for this audit: it
asserts that the root and web governance files and this ledger exist, that
required NO-GO/preservation language remains present, and that the critical
release paths are not ignored. It does not build, bind, deploy, migrate, apply,
or inspect live data.

## Phase 1 handoff checklist

- [ ] Re-capture HEAD, branch/upstream, status, tracked diff, and the expanded
      untracked list after this Phase 0 change; retain a new boundary digest.
- [ ] Keep the 85 pre-existing tracked modifications separate from all Phase 1
      work; assign ownership and review status for each affected surface.
- [ ] Decide and document the intentional tracked release input set for all
      `ops/` and `deploy/` files, migrations `014`/`015`, selected Apply/store
      files, and Project Archaeology history/browser-gate files.
- [ ] Verify migration ordering, checksums, static contracts, and isolated
      fixture behavior without touching the live database.
- [ ] Create a clean, reproducible packaging checkout from an explicitly
      reviewed commit; do not package from this dirty snapshot.
- [ ] Run the Phase 1 release/readiness/backup/restore/inventory gates only
      under a separate authorization packet and preserve sanitized evidence.
- [ ] Re-prove Codex configured/available/signed-in/compatible, schema 15,
      healthy reconciliation, backup/restore state, and scheduler uncertainty
      state before any candidate decision.
- [ ] Keep Apply default-off and require explicit human review, exact digests,
      and a separate authorization before any canonical mutation.
- [ ] Run `ss -tlnp` before any new listener and record the selected port and
      owner without disclosing credentials or private payloads.

## Phase 0 review record

The local validation and required bounded DeepSeek review/fix loop completed
after implementation. These records describe the Phase 0 files only; the
pre-edit baseline above remains unchanged.

- Initial bounded review: DeepSeek job
  `dsh-agent-20260815021302-d8fbd6f5`, status `succeeded`, exit `0`.
  The reviewer independently confirmed the HEAD/upstream identity, 85
  modified tracked paths, 41 pre-existing untracked files, three boundary
  digests, safe `.8` evidence wording, web delegation, ignore audit, and
  Phase 0 scope. All of those review claims were validated and accepted as
  correct; no content fix was required.
- Accepted finding/fix: the new `ops/test-governance.sh` had mode `0644`
  despite its direct-execution shebang. It was changed to mode `0755` only;
  direct execution now passes. No pre-existing file was changed.
- Rejected/false-positive claims: none were raised as defects. The review’s
  possible concerns about digest drift, grouped counts, combined phrase
  matching, representative ignore samples, and Phase 1 omissions were
  independently checked and are intentional or correct for this phase.
- Final targeted verification: DeepSeek job
  `dsh-agent-20260815021758-ac7daf49`, status `succeeded`, exit `0`.
  It verified mode `0755`, direct execution, the three-file Phase 0 scope,
  and preservation of the 85/41 pre-existing baseline. It reported no issues.

### Local validation

- `./ops/test-governance.sh` — passed.
- `npm test` from `web/` — 8 tests passed.
- `GOCACHE=/tmp/codex-go-cache TMPDIR=/tmp/codex-commons-test-tmp go test ./...`
  — passed.
- `git diff --check` — passed for tracked changes. The new text files were
  additionally checked for trailing whitespace before the final review.
- `sh ops/test-ops.sh` — remains a pre-existing test failure after the
  release-manifest checks: its fixture adds a root-level unmanifested file,
  while `ops/verify-release.sh` only compares the declared release tree
  (`VERSION`, binaries, `web`, and packaged `ops`) and therefore accepts that
  outside file. This is recorded for Phase 1 release-test reconciliation; no
  Phase 1 verifier fix was made.
