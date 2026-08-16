# Phase 2 completion ledger and operator checklist

**Scope:** Codex Commons managed-Codex supervision, readiness/watchdog
semantics, scheduler gating, and deployment documentation.

**Current disposition:** Source/documentation work may be complete while the
deployment decision remains **NO-GO**. This ledger deliberately records those
as separate facts. A passing test, a healthy disposable rehearsal, or a
read-only live check is not approval to activate a release.

This file preserves the Phase 2 checklist and evidence exactly as a historical
baseline. Phase 3 adds a durable native-persistence ledger, so the Phase 2
statements in Increments 09 and 10 that say there is no durable retry queue do
not describe the current source. See the Phase 3 addendum after the Phase 2
handoff for the current source behavior and its still-open release gates.

## How to use this ledger

Complete one increment at a time. For every increment, fill the evidence slots
with bounded, sanitized references—not credentials, prompts, transcripts, model
output, personal content, or raw database payloads.

- **Source/test complete** means the reviewed source or documentation exists
  and its listed static/unit/disposable checks pass.
- **Deployment approved** means an authorized operator approved one exact
  immutable release, database boundary, traffic boundary, maintenance window,
  and rollback target after all applicable release gates passed.
- The second status never follows automatically from the first. Leave the
  deployment field `NO-GO` until a separate approval packet exists.

Evidence slots remain blank until the corresponding source/test or approved
rehearsal actually runs. This ledger does not infer completion from a changed
file, a read-only health response, or a historical result. A blank evidence
slot means **unrecorded**, not pass or fail; populate it only with a bounded,
sanitized reference after the corresponding check runs.

## Increment checklist

### Increment 01 — Scope freeze and release identity

**Acceptance criteria**

- The source commit, branch, tracked diff, and untracked inventory are recorded.
- The candidate is built from a reviewed, clean input set rather than this
  dirty working tree.
- Existing live release/current/database state is treated as read-only.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `HEAD/branch: ____________________`.
- Checks: `Command/result: ____________________`.
- Candidate identity: `Release ID/manifest digest: ____________________`.
- Deployment approval: `NO-GO — exact approval packet: ____________________`.

### Increment 02 — Supervisor generation lifecycle

**Acceptance criteria**

- Each managed App Server process has a generation identity and sanitized
  start/exit/recovery records.
- Supervisor states are observable as `starting`, `available`, `degraded`,
  `recovering`, `exhausted`, or `closed`.
- Readiness observations do not opportunistically create replacement
  processes.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Files/tests: ____________________`.
- State transition evidence: `Receipt/log reference: ____________________`.
- Sanitization review: `No secret/prompt payloads: [ ]`.
- Deployment approval: `NO-GO — exact candidate review: ____________________`.

### Increment 03 — Bounded recovery and retry budget

**Acceptance criteria**

- Recovery has one owner per failed generation, bounded exponential backoff,
  cooldown, and a finite attempt budget.
- A successful managed call, not a poll or health request, is the documented
  reset point for the failure episode.
- Concurrent callers converge on one recovery result and cannot create a
  restart storm.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Focused test result: ____________________`.
- Recovery matrix: `first failure / repeated failure / recovery: __________`.
- Exhaustion evidence: `State, attempt count, timestamp: ________________`.
- Deployment approval: `NO-GO — exact candidate review: ____________________`.

### Increment 04 — Non-idempotent launch boundary

**Acceptance criteria**

- Launch, rename, and interrupt operations are not automatically replayed
  after an acceptance-ambiguous transport failure.
- Exact thread/session/turn identity is preserved when available.
- Uncertain work is retained in durable job state when possible, is never
  replaced implicitly, and blocks a new historian claim until the documented
  recovery path resolves it. A failed persistence write remains latched for
  startup reconciliation rather than being treated as durable success.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Focused test result: ____________________`.
- Uncertainty proof: `Job/thread/turn metadata-only reference: ____________`.
- Duplicate-launch proof: `Receipt/status: ____________________`.
- Deployment approval: `NO-GO — exact candidate review: ____________________`.

### Increment 05 — Shared core health snapshot

**Acceptance criteria**

- Readiness and watchdog decisions consume the same health snapshot.
- Core health includes database ping, reconciliation state, and the scheduler
  persistence gate. Reconciliation `attention`/uncertainty remains visible and
  can leave the core service ready and watchdog-capable, while making
  `SchedulerEligible=false`.
- Optional Codex outage is represented as Codex attention, not falsely healthy
  Codex capability.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Test/fixture: ____________________`.
- Optional-mode snapshot: `Status/fields: ____________________`.
- Degraded/recovered snapshot: `Before/after references: ________________`.
- Deployment approval: `NO-GO — exact candidate review: ____________________`.

### Increment 06 — Required versus optional Codex readiness

**Acceptance criteria**

- `COMMONS_REQUIRE_CODEX_READY=false` is the optional-safe example default.
- Optional mode can become service-ready after core startup checks while
  historian claims remain gated when Codex is unavailable.
- Required mode delays/fails readiness when Codex is unavailable, unsigned-in,
  incompatible, or blocked by failed/unknown core or persistence checks. A
  reconciliation `attention`/uncertainty state may remain service-ready and
  watchdog-capable, but it still blocks scheduler claims.

**Evidence slots**

- Source/config completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Env/config test: ____________________`.
- Optional outage evidence: `Readiness/systemd result: _________________`.
- Required outage evidence: `Readiness/systemd result: _________________`.
- Deployment approval: `NO-GO — explicit required-mode approval: __________`.

### Increment 07 — Type=notify startup contract

**Acceptance criteria**

- The service retains `Type=notify`, `NotifyAccess=main`, and `WatchdogSec=60`.
- `READY=1` is sent only after migrations/reconciliation and the first ready
  snapshot; required mode also waits for required Codex capability.
- `TimeoutStartSec=180` is finite and long enough for the documented cold-start
  work; a timeout is a failed start, never a green state.
- A listener may bind locally before `READY=1`; while systemd is activating it
  is not service-ready/routable or deployment approval. The readiness payload
  contains runtime state plus service version only; schema and packaged-digest
  evidence come from separate approved checks.
- Normal shutdown retains `STOPPING=1` semantics.

**Evidence slots**

- Unit/source completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Unit path/check: ____________________`.
- Startup evidence: `READY timing/result: ____________________`.
- Watchdog/stop evidence: `Heartbeat and STOPPING result: ________________`.
- Deployment approval: `NO-GO — systemd change approval: ________________`.

### Increment 08 — Watchdog health semantics

**Acceptance criteria**

- Watchdog heartbeats are emitted only while the shared health snapshot permits
  them.
- Required Codex recovery may heartbeat only during bounded grace; after grace
  or exhaustion it becomes fatal/non-ready. Reconciliation attention may remain
  watchdog-capable, while a persistence latch cannot leave the service green.
- A watchdog restart is reported as a failed health contract, not as proof of
  candidate correctness.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Test/fixture: ____________________`.
- Healthy heartbeat evidence: `Sanitized status/timestamps: _______________`.
- Degraded heartbeat evidence: `Sanitized status/timestamps: ______________`.
- Deployment approval: `NO-GO — exact restart authorization: ______________`.

### Increment 09 — Scheduler claim gating

**Acceptance criteria**

- Native scheduler claims require core health, required Codex capability (when
  configured), native feature enablement, no unresolved uncertainty, and no
  persistence latch.
- `domain.ErrConflict` means no work was available/another claimant won; it
  does not create an error storm.
- Other claim/persistence errors make one permitted attempt, fail closed with a
  persistence latch, and stop claims. Phase 2 has no durable retry queue;
  recovery requires startup reconciliation or an explicit verified clear.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Focused scheduler result: ____________________`.
- Claim allowed proof: `State/count/reference: ____________________________`.
- Claim blocked proof: `State/count/reference: ____________________________`.
- Deployment approval: `NO-GO — live scheduler authorization: _____________`.

### Increment 10 — Terminal persistence latch

**Acceptance criteria**

- Failures in terminal start/turn persistence, identity binding, or activation
  receive one best-effort persistence attempt; Phase 2 does not maintain a
  durable retry queue or replay non-idempotent mutations.
- A failed attempt sets a process-local persistence latch, reports failed
  persistence health, and stops new claims. Known durable job/identity state is
  retained when the write succeeded; otherwise startup reconciliation preserves
  uncertainty where possible.
- The latch clears only after startup reconciliation or an explicit verified
  persistence reconcile/clear; no terminal event is fabricated or silently
  dropped.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Fault-injection result: ____________________`.
- Latch-set evidence: `State/timestamp/sanitized error: _________________`.
- Latch-clear evidence: `Committed receipt/reconciliation: ______________`.
- Deployment approval: `NO-GO — exact candidate review: ____________________`.

### Increment 11 — Startup reconciliation and uncertainty latch

**Acceptance criteria**

- Startup preserves/marks stranded `starting`, `active`, and report-ready jobs
  as uncertain. Existing exact Codex identities remain authoritative; exact
  identity recovery is attempted only where an identity is missing and the
  provider can prove it.
- Unprovable terminal status remains uncertainty, not an indefinitely active job
  and not an implicit replacement launch.
- One unresolved uncertain job blocks new historian work globally until exact
  human resolution.

**Evidence slots**

- Source/test completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Reconciliation test/result: ____________________`.
- Before/after job-state evidence: `Metadata-only reference: ______________`.
- Claim-block evidence: `Scheduler result: ________________________________`.
- Deployment approval: `NO-GO — recovery authorization: ____________________`.

### Increment 12 — Safe diagnostics and rollback

**Acceptance criteria**

- Operators can inspect systemd state, bounded journal lines, exact release
  identity, loopback readiness, and listeners without reading secrets or
  mutating service/database state.
- Readiness output is runtime-only plus service version; release/schema/digest
  evidence is collected separately by approved static/read-only checks.
- Rollback validates the matching backup and exact previous release before
  changing `current` or the database.
- Any rollback restart/readiness failure leaves the service stopped and
  preserves evidence. Cleanup may be best effort, but every failure—including
  a suppressed `|| true` command—is reported and is not treated as success.

**Evidence slots**

- Documentation/static completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Runbook review: ____________________`.
- Safe-diagnostic transcript digest: `Sanitized reference: ________________`.
- Rollback rehearsal: `Candidate/previous result: _________________________`.
- Deployment approval: `NO-GO — rollback packet: __________________________`.

### Increment 13 — Release gates and explicit activation boundary

**Acceptance criteria**

- The runbook states that source/test completion, read-only readiness, and
  disposable acceptance do not approve live deployment.
- Before activation, the exact candidate passes manifest, AppArmor/runtime
  preflight, backup/restore, readiness, scheduler, and applicable Phase 0–5
  gates; an authorized operator records the release/database/traffic/rollback
  boundary.
- No live `current` switch, service restart, Caddy/DNS change, Apply, live
  historian, or Beta promotion occurs from this source-only increment.

**Evidence slots**

- Source/doc completion: `Status: [ ] pending  [ ] pass  [ ] fail`;
  `Files/checks: ____________________`.
- Static/test evidence: `Command/result: _________________________________`.
- Disposable acceptance packet: `Release ID/receipt digest: ______________`.
- Deployment approval: **`NO-GO — explicit approval packet: _____________`**.

### Increment 14 — Disposable managed-supervisor acceptance

**Acceptance criteria**

- A disposable end-to-end test starts with a healthy managed supervisor,
  publishes HTTP readiness `200`, and performs exactly one accepted scheduler
  launch.
- A managed-child exit publishes supervisor degradation/recovery, returns HTTP
  readiness `503`, gates the scheduler, and does not create a duplicate claim
  or launch.
- Bounded recovery installs the replacement generation, returns HTTP readiness
  `200`, and preserves the single accepted claim/active job.
- A second exit exhausts the bounded recovery budget, returns readiness `503`,
  causes notifier fatal, stops further watchdog messages, and does not replay
  the accepted launch or claim.
- This is disposable source/test evidence only; it does not approve a
  candidate, release, live service, database, traffic path, or Beta promotion.

**Evidence slots**

- Source/test completion: **`PASS`** —
  `go test ./internal/server -run '^TestPhase2DisposableAcceptanceManagedRecoveryAndExhaustion$' -count=20 -timeout=120s`.
- State sequence: `healthy/200 → degraded/503/gated → recovered/200 →
  exhausted/503/fatal/no-watchdog/no-duplicate-claim: PASS`.
- Final full-race evidence: `PASS` — `go test -race ./... -count=1` on the
  frozen post-remediation tree.
- Deployment approval: **`NO-GO — no candidate or live approval`**.

## Phase 2 handoff

At handoff, report the changed source paths, exact test commands and results,
open evidence slots, and any supervisor/scheduler risks. The current
source/test verification summary is:

- Full Go suite: `go test ./...` — **PASS**.
- Full race suite: `go test -race ./... -count=1` — **PASS** on the frozen
  post-remediation tree.
- Focused disposable acceptance: the Increment 14 command — **PASS** at
  `-count=20`.
- Static Go analysis: `go vet ./...` — **PASS**.
- Ops artifact suite:
  `GOTOOLCHAIN=go1.25.0 GOCACHE=/tmp/codex-go-cache sh ops/test-ops.sh` —
  **PASS**, with 54 rejected cases, 54 restorations, 4 positive cases, and
  `PHASE13_FINAL_PRISTINE=1`.
- Governance: `sh ops/test-governance.sh` — **PASS**.
- Shell syntax: `sh -n ops/check-readiness.sh ops/test-ops.sh` — **PASS**.
- Systemd unit verification:
  `systemd-analyze verify deploy/systemd/codex-commons.service` — **PASS**.

These results establish source/test completion only. They do not establish a
candidate identity, release digest, deployment approval, or live readiness;
those evidence slots remain open and the deployment decision remains
**NO-GO**. Do not mark a row `deployment approved` merely because a local or
historical runtime was healthy. The next operator must start from a clean,
reviewed candidate and a new read-only readiness capture.

## Phase 3 source/offline completion addendum

**Verified source boundary:** `fe6cdba` on the Phase 3 verified branch. This is
source and disposable-test evidence only. It is not a packaged candidate,
release manifest, deployment approval, live database result, or live historian
result.

The Phase 3 change sequence is intentionally small and ordered:

| Commit | Source/offline result |
| --- | --- |
| `0761ab1` | Centralizes repository-owned batch and outcome eligibility used by capability projection and final enforcement. |
| `56ccd97` | Adds migration 016 and the durable native-persistence intent schema. |
| `1c353c1` | Applies the centralized eligibility gate to selected preview and review without weakening store enforcement. |
| `702c0a1` | Adds the Store intent ledger and replay-safe persistence operations. |
| `3cfe26b` | Serializes selected Apply at the SQLite writer boundary and makes exact concurrent replays return one immutable receipt. |
| `f082b28` | Makes the scheduler ensure, apply, and read back durable intents; adds bounded retry, a periodic wake, and a claim gate while persistence needs attention. |
| `7e09420` | Runs generic archaeology reconciliation before native-persistence reconciliation during startup and exposes unresolved ledger work as non-green attention. |
| `fe6cdba` | Proves complete-turn and lose-turn persistence across Store close/reopen, including exact terminal evidence and no replay of external launch, finalize, or interrupt calls. |

`cafa666` is retained prerequisite evidence aligning the sandboxed operations
test with the documented Go toolchain. It is not a Phase 3 product-behavior
change.

### What the durable ledger does

The ledger is deliberately narrow. It records only five replay-safe repository
transitions: fail start, bind identity, activate, lose turn, and complete turn.
For each transition, the scheduler commits the intent first, applies the Store
mutation, reads the durable row back, and advances lifecycle state only after
the row is `applied`. Pending, leased, or blocked work keeps persistence in an
attention state and prevents a new historian claim.

The ledger never replays external `LaunchNative`, `FinalizeNative`, or
`InterruptNative` calls. Those remain acceptance-sensitive boundaries. The
restart tests use a real Store, inject a terminal persistence failure, close
and reopen the database, run generic reconciliation followed by native
persistence reconciliation, and verify the exact complete/lose result without
any external call replay.

### Evidence and remaining operational gates

- Eligibility and Apply evidence is source-grounded in
  `internal/store/project_archaeology_eligibility_test.go`,
  `internal/store/project_archaeology_selected_gating_test.go`, and
  `internal/store/project_archaeology_apply_test.go`.
- Durable scheduler and startup evidence is source-grounded in
  `internal/store/project_archaeology_persistence_test.go`,
  `internal/application/project_archaeology_scheduler_phase3_test.go`,
  `internal/application/project_archaeology_scheduler_restart_test.go`,
  `internal/server/server_startup_persistence_test.go`, and
  `internal/server/startup_reconciliation_test.go`.
- Source/offline Phase 3 completion: **PASS** at the verified source boundary.
- Candidate build, manifest, AppArmor/runtime preflight, backup/restore packet,
  and disposable production-composition acceptance: **unrecorded**.
- Deployment, candidate activation, live database mutation, live Apply, and
  live historian execution: **not performed**.
- Deployment approval: **NO-GO**.

Per the current delivery direction, finish through Phase 5 before beginning
live tests. Phase 9's disposable gates, new-candidate construction, explicit
live approval, and live recovery/evidence work also remain outstanding. No
source or offline result in this addendum may be promoted into those evidence
slots.
