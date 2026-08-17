# Continuous dogfood: Phase 2 baseline and Phase 3 source runbook

This is a junior-engineer-friendly operating guide for a reviewed, immutable
Codex Commons release. It explains what the supervisor is expected to do and
how to collect safe evidence. It is not an installer or an authorization to
change a live service.

The source tree is not a release. Never package or activate from a dirty
checkout, and never point an older binary at a newer database schema. Complete
the release gates and obtain explicit operational approval before changing
`current`, systemd, Caddy, DNS, the live database, or traffic.

The Phase 2 supervisor evidence remains the historical baseline. The verified
Phase 3 source boundary is `fe6cdba`; it adds centralized Apply eligibility,
transactional idempotent Apply, and a durable native-persistence ledger. That
commit is not a candidate identity or live proof. No candidate was activated,
no live database or historian was exercised, and the deployment decision
remains **NO-GO**.

## 1. The state model

There are two state machines to keep separate:

| Layer | States and meaning |
| --- | --- |
| systemd | `inactive` means not running; `activating (start)` means systemd is waiting for `READY=1`; `active (running)` means the service sent `READY=1`; `deactivating (stop)` means shutdown is in progress; `failed` means startup, watchdog, or process exit exhausted the unit's restart policy. |
| managed Codex supervisor | `starting` is opening a generation; `available` is usable; `degraded` is unavailable or unhealthy but recovery is still possible; `recovering` is the current bounded replacement attempt (the finite budget may permit later attempts); `exhausted` means the retry budget/cooldown was exhausted; `closed` is an intentional shutdown. |

The systemd unit is `Type=notify` and accepts notification messages from the
main process. Startup is deliberately finite: the unit allows 180 seconds for
migrations, reconciliation, the first readiness snapshot, and a cold managed
Codex probe before treating missing `READY=1` as a startup failure.

`WatchdogSec=60` is a liveness bound, not a readiness check. Commons sends a
heartbeat only while the shared core health snapshot permits it. Reconciliation
`attention` caused only by job uncertainty can remain core-ready and
watchdog-capable, but it disables scheduler claims. Native-persistence backlog
or a persistence fault is a degraded persistence component: readiness and
watchdog eligibility are false and claims remain blocked. During required Codex
recovery, a heartbeat may continue only inside the bounded grace window; after
grace or exhaustion the notifier stops heartbeats and signals fatal so systemd
can restart the service. On normal shutdown Commons sends `STOPPING=1`;
`deactivating` is therefore expected during an orderly stop.

The HTTP listener may bind while systemd is still `activating`. A bound local
socket is not `READY=1`, service-ready, routable through the approved proxy, or
deployment approval; use it only for bounded loopback diagnostics until the
first permitted readiness notification.

## 2. Optional and required Codex behavior

The example environment sets:

```text
COMMONS_REQUIRE_CODEX_READY=false
```

This is the safe optional default. With it, a Codex outage may leave the
Commons service ready for core/read-only and recovery views after database
migrations, reconciliation, and the first core-ready snapshot succeed. It must
still report Codex as unavailable and must not claim new historian work.

Set `COMMONS_REQUIRE_CODEX_READY=true` only for a deployment that intentionally
makes the managed Codex capability part of service readiness. In that mode,
`READY=1` and the healthy watchdog path require all of the following:

- the managed Codex process is available;
- the account is signed in;
- the configured model/effort is compatible;
- database and persistence checks are healthy; and
- reconciliation is not `failed` or `unknown`.

Reconciliation `attention` caused only by unresolved job uncertainty is
different from a failed core check: the service may remain ready and
watchdog-capable, but `SchedulerEligible` is false and no historian claims are
allowed until the attention is resolved. A native-persistence backlog is also
reported as attention, but its degraded persistence component keeps the service
non-ready and non-watchdog-eligible until the ledger is healthy.

An optional outage is therefore service-ready-but-Codex-unavailable. A required
outage is service-not-ready (or supported only during bounded recovery grace)
and should result in a controlled systemd restart after grace/exhaustion.
Never infer either state from a single `/v1/health` 200 response.

## 3. Startup and readiness

For an approved disposable rehearsal, the expected order is:

1. Verify the exact immutable release directory and its `SHA256SUMS` manifest.
2. Open the database and run migrations. Run generic archaeology reconciliation
   first, then native-persistence reconciliation, then re-read the persistence
   and uncertainty status before constructing or waking the scheduler.
3. Establish the first core readiness snapshot. A required Codex configuration
   also checks process availability, signed-in account, and compatibility here.
   A persistence failure, unresolved native-persistence backlog, or
   failed/unknown core observation blocks readiness. Job uncertainty remains
   visible and disables scheduler claims even when the other core checks still
   permit readiness and watchdog heartbeats.
4. The listener may bind locally before `READY=1`. While systemd is
   `activating`, that socket is for bounded loopback diagnostics only and is
   not service-ready or routable. Send `READY=1` only after the first permitted
   snapshot is published.
5. Start watchdog heartbeats only while the shared runtime decision allows it.
   Required Codex recovery may use the bounded grace exception; after grace or
   exhaustion heartbeats stop and the notifier signals fatal. A later readiness
   failure is not silently converted into healthy liveness.

The local readiness endpoint is read-only and loopback-only:

```sh
curl -fsS -H 'Host: 127.0.0.1:8088' \
  http://127.0.0.1:8088/v1/internal/readiness
```

Use the configured public Host only for the ordinary health check when the
release is already behind the approved private proxy. Readiness must never be
made public or checked through forwarded headers. The readiness payload is
runtime-only plus the service version; it is not an installation or schema
report. Collect the exact release identity, database schema, packaged digests,
and applicable Codex-policy evidence separately with the approved read-only
checker. A passing check is evidence, not deployment approval.

Interpret failures by layer:

- no `active (running)` and `activating (start)` for up to 180 seconds:
  inspect startup logs; systemd should fail the start rather than wait forever;
- `active (running)` with Codex unavailable in optional mode: core service is
  up, but historian claims are gated and the runtime response must show the
  Codex attention state;
- reconciliation `attention` caused only by job uncertainty: core service may
  stay ready and watchdog-capable, but `SchedulerEligible=false` and claims
  remain gated;
- required mode with Codex unavailable, incompatible, or unsigned-in: do not
  claim work; use bounded recovery grace, then let the notifier fail the unit;
- a pending, leased, or blocked native-persistence row, or a process-local
  persistence fault: readiness and watchdog eligibility are false and no new
  claim is permitted. Allow the bounded scheduler/startup reconciliation path
  to drain replay-safe Store work; do not replay an external Codex call;
- `failed` after a watchdog timeout: treat it as a failed health contract, not
  proof that the release is bad. Capture logs and readiness evidence before a
  separately approved retry.

## 4. Recovery, retry exhaustion, and non-idempotent launches

The supervisor owns process replacement. A readiness request is an observation;
it must not itself start a replacement process. Recovery is single-owner,
bounded, and uses exponential backoff/cooldown. Each attempt records sanitized
generation/attempt/result metadata, never credentials, prompts, transcripts, or
model output.

Only safe, idempotent observations (for example account status, model support,
or bounded inventory reads) may use the managed retry path. A transport failure
after a launch, rename, or interrupt may mean Codex already accepted the
mutation. Those operations are not replayed automatically:

- preserve the exact thread/session/turn identity when one exists;
- mark the launch uncertain when acceptance cannot be proved;
- persist that uncertainty through the durable Store-intent path; if the intent
  cannot yet be committed, fail closed and keep claims blocked;
- interrupt that exact task at most once when the recovery contract permits it;
- never create a replacement launch implicitly.

When the bounded retry budget is exhausted, the supervisor enters `exhausted`:

- no new historian claims are allowed;
- optional mode may keep core/read-only service available while exposing the
  Codex outage;
- required mode may heartbeat only during the bounded recovery grace window;
  after grace/exhaustion it must become fatal/non-ready and allow systemd's
  watchdog/restart policy to take over;
- do not reset the budget by polling, refreshing a browser, or repeatedly
  calling readiness. A sustained healthy managed call is the reset point.

## 5. Phase 3 Apply and durable scheduler persistence

### Apply boundary

The Store owns one eligibility predicate for capability projection, batch
detail, selected preview, review, and final Apply. Final Apply does not trust a
previous screen or preview: inside the serialized write transaction it
revalidates principal ownership, completed and policy-attested batch state,
terminal report-bearing jobs, selected outcome membership, proposal/selection
digests, manifest digest, and the completed review before mutation.

Exact concurrent retries use the same principal, request key, batch, selection
digest, manifest digest, and sorted outcome IDs. They return the same immutable
receipt. A request-key reuse with different identity conflicts, and any
eligibility drift before or during Apply rolls the whole transaction back. This
is source/offline behavior only; it is not permission to run Apply against a
live database.

### Scheduler claim gate and persistence ledger

Older Phase 2 evidence calls the fail-closed process state a `persistence latch`.
In current Phase 3 source, that phrase applies only to an unexpected
process-local persistence fault; replay-safe Store work is represented by the
durable intent rows below.

The native scheduler can claim work only when all gates below are true:

- Commons core readiness is healthy;
- Codex is available and compatible whenever historian execution is enabled;
- there is no unresolved `uncertain` historian job;
- the scheduler is configured for the native feature; and
- the native-persistence ledger is healthy and no persistence fault is set.

Scheduler eligibility is stricter than core service readiness. Reconciliation
attention caused only by unresolved uncertainty may leave the core service
ready and watchdog-capable, but it sets `SchedulerEligible=false` and blocks
claims. A persistence backlog is stricter: it also keeps readiness and watchdog
eligibility false.

`domain.ErrConflict` while claiming means that another worker won or that no
work is available; it is a normal no-work result. Other claim errors are
transient and receive bounded periodic retry. They never authorize a claim past
an unhealthy persistence gate.

Terminal persistence is part of correctness, not best-effort logging. The
durable ledger covers exactly these replay-safe Store transitions:

1. fail start;
2. bind exact thread/session/turn identity;
3. activate the exact thread/turn;
4. lose the exact thread/turn; and
5. complete the exact thread/turn with its terminal status and duration.

For each transition, the scheduler first ensures the durable intent, then
applies the Store mutation, then reads the row back. Lifecycle progress is
authorized only by an `applied` readback. Pending, leased, and blocked rows are
attention, stop claims, and stay visible to bounded retry or operator recovery.
The ledger never contains or replays external `LaunchNative`, `FinalizeNative`,
or `InterruptNative` calls.

Startup order matters. Generic archaeology reconciliation first moves stranded
starting/active work to a truthful uncertain state. Native-persistence
reconciliation then applies any exact durable terminal evidence and re-reads
the ledger before scheduler construction. A future-due, live-leased, or blocked
row may allow the process to start only in a non-green attention state; an
unexpected reconciliation error fails startup. Existing exact identities stay
authoritative, and unprovable work remains uncertain rather than being
relaunched.

The real-Store restart tests at the verified Phase 3 source boundary inject one
complete-turn or lose-turn persistence failure, close and reopen the Store,
perform that startup order, and recover the exact terminal evidence. The
restarted scheduler performs zero external launch, finalize, or interrupt
replays. This proves disposable Store behavior, not packaged-candidate or live
behavior.

## 6. Safe diagnostics

Diagnostics are read-only. Run them against the approved disposable or already
running instance; do not print environment files or secret values.

```sh
systemctl --user status codex-commons.service --no-pager
systemctl --user show codex-commons.service --no-pager \
  -p ActiveState -p SubState -p Result -p ExecMainStatus -p WatchdogUSec
journalctl --user-unit codex-commons.service --no-pager -n 200
readlink -f "$HOME/.local/lib/codex-commons/current"
ss -tlnp
```

Then use the loopback readiness request in Section 3 and, when the required
environment is already installed, run the read-only release/readiness checks.
The loopback response is runtime-only plus service version; collect release ID,
manifest digest, database schema, and scheduler active/uncertain evidence from
the separately approved checker/status queries. Capture timestamps, systemd
state, sanitized supervisor state, readiness status, and command exit codes.
Do not capture `EnvironmentFile` contents, binding keys, credentials, database
payloads, prompts, transcripts, or personal content.

Do not run `systemctl restart/stop`, `daemon-reload`, `reset-failed`, a release
pointer switch, database restore, migration, Caddy reload, or any listener
startup as a diagnostic. Those are operational changes and require an explicit
maintenance plan and authorization.

## 7. Disposable cutover sequence

The following is the gate sequence for a separately authorized rehearsal. It
does not authorize live activation:

Before any candidate or cutover work, run the Phase 4 source-only deployment
gate from the reviewed tree:

```sh
sh ops/test-deploy-release.sh
```

This test uses only disposable release directories and fake systemctl,
readiness, and verification scripts. It proves the canonical release-root
lock domain, contention fail-closed behavior, and atomic forward/rollback
pointer cleanup; it does not restart a service, switch a live `current`, or
mutate a live database.

1. Confirm the working tree is clean and the source commit is recorded. Stop
   any temporary process only under its task owner and verify the chosen
   loopback port is free.
2. Seal a consistent, mode-0600 source/evidence snapshot with
   `ops/seal-archive.sh`; preserve matching WAL/SHM files.
3. Create a fresh mode-0700 disposable state directory. Do not point a new
   schema binary at an old database.
4. Build and stage from the reviewed commit with `ops/build-release.sh` and
   `ops/stage-release.sh`. Keep the release immutable and verify its complete
   manifest, exact Codex version, pinned checksums, modes, ownership, and
   absence of extra files with `ops/verify-release.sh`.
5. Preflight AppArmor against the exact immutable release path. Never attach a
   wildcard or the mutable `current` symlink. Run the gated no-task runtime
   preflight; it must create no thread, turn, batch, job, report, or import.
6. Bootstrap through direct loopback HTTP with no public origin and first-LAN
   bind disabled. Verify one durable human account binding, then stop the
   disposable process.
7. Install the environment/key with mode 0600 and units/templates with mode
   0644. Enable user linger, the service, and backup timer only in the approved
   maintenance window; keep the app listener on loopback.
8. Verify Type=notify `READY=1` timing (a local listener may exist before it),
   watchdog grace/fatal behavior, the runtime-only readiness payload, exact
   release identity, schema/digests, compatibility, Host/origin/CSRF rules,
   and optional/required Codex semantics. A read-only readiness pass is not a
   deployment approval.
9. Validate the private Caddy/TLS path and trust without bypass flags. Do not
   expose the service publicly.
10. Run exactly one non-applying, human-approved Luna Max historian for one
    project. Verify the exact final title, repeated durable report reads,
    repository immutability, canonical immutability, and the four evidence
    receipts. Never toggle evidence from a browser.
11. Run backup and isolated restore verification. Record a restore drill only
    after schema, integrity, FK, counts, and audit digests match.

## 8. Rollback and failure boundaries

Rollback is a separate operational decision. First capture the candidate
failure and stop the restart-on-failure service. Validate the matching
pre-upgrade receipt and exact previous release before touching the database or
`current`. If a database restore is required, remove only the matching WAL/SHM
while the service is stopped, restore through a verified temporary file in the
same directory, and atomically replace the exact target. Switch `current` only
to the validated release directory, then restart and rerun readiness.

Classify the result explicitly:

- candidate failed; previous release and database restored and ready;
- candidate failed; database restore failed;
- candidate failed; previous process failed to restart; or
- candidate failed; previous readiness failed.

For any rollback restart/readiness failure, leave Commons stopped, preserve the
restored database/release metadata and evidence, and escalate. Rollback helpers
may use best-effort cleanup; an ignored command (including `|| true`) is not a
successful cleanup. Record and report each cleanup, restore, restart, and
readiness failure, verify the final stopped state, do not keep retrying a live
service, and do not delete the last known-good release or backup.

## 9. Release gates and approval boundary

Source edits, static checks, Go tests, browser tests, the Phase 4
`sh ops/test-deploy-release.sh` gate, and a disposable acceptance run prove the
candidate contract only. They do not approve a live deployment. Live
activation is prohibited until the applicable Phase 0–5
release gates are complete, the candidate is built from an explicitly reviewed
commit, its manifest/AppArmor/runtime/backup/restore evidence is recorded, and
an authorized operator approves the exact release ID, database boundary,
traffic boundary, maintenance window, and rollback target.

The current delivery direction is to finish through Phase 5 before beginning
live tests. Phase 9's disposable gates, new-candidate construction, explicit
live approval, and live recovery/evidence work also remain outstanding.

Do not activate `current`, restart a live unit, change Caddy/DNS, run Apply,
launch or cancel a live historian, or promote Beta from this runbook alone.
Beta remains a human decision and stays NO-GO while any required readiness,
reconciliation, backup/restore, compatibility, recovery, uncertainty, or
immutability evidence is unknown or attention.
