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

Startup begins at the stable launcher, not at the mutable `current` symlink.
The systemd unit invokes only
`~/.local/libexec/codex-commons/commons-launch.sh` (source:
`ops/commons-launch.sh`), which is installed outside release directories. That
script resolves `$COMMONS_RELEASE_ROOT/current` exactly once, requires the
target to be a canonical directory that is a direct child of the configured
release root, and exports exact `COMMONS_RELEASE_DIR`, `COMMONS_WEB_DIR`,
`COMMONS_CODEX_BIN`, and `COMMONS_RELEASE_IDENTITY_FILE` from that pin. It then
runs that directory's `ops/verify-release.sh`, `chdir`s into the same
directory, and `exec`s its `commons-server`. Missing, non-directory, nested,
outside-root, and unsafe/symlink-shaped targets fail closed. After the pin, a
later mutation or swap of `current` cannot redirect that startup. The unit
does not set `WorkingDirectory=current` and does not run `ExecStartPre`
against a separately resolved `current` path.

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
does not authorize live activation.

`ops/deploy-release.sh` serializes one deployment transaction by canonicalizing
`COMMONS_RELEASE_ROOT`, opening that canonical release-root directory itself,
and taking a nonblocking exclusive `flock` on the directory descriptor. It does
not create or follow a `.lock` file, and it does not lock through a symlink.
The descriptor stays open until the process exits, so the same lock covers
candidate verification, the previous-release preflight, receipt write, backup,
the `current` pointer switch, restart and readiness, and the captured-previous
rollback paths including atomic database restore. Staged scripts, the
configurable systemctl command, receipt helpers, the restore helper, and other
child processes do not receive a usable copy of that lock descriptor. A second concurrent deploy exits `75` (busy) before changing any
state. A suspicious pre-existing `.current.next` that is not a leftover
relative release-basename symlink is rejected without following or overwriting
it. If this invocation creates `.current.next` and is interrupted before the
rename, only that temporary symlink is removed; `current` is not rewritten by
the trap. The flock is released automatically when the process exits.

After candidate verification and while that flock is held, `ops/deploy-release.sh`
inspects `current` exactly once. First deployment with `current` absent is
allowed and records `previous_state=absent`. If `current` exists, it must be a
symlink whose target is a losslessly captured relative release basename using
the same safe grammar as `ops/commons-launch.sh`. Absolute, nested, traversal,
control, newline, and empty targets are rejected. The captured basename is
resolved to an exact canonical direct child of `COMMONS_RELEASE_ROOT` that is a
real non-symlink directory. That exact previous release is verified before
backup or pointer mutation, with `COMMONS_RELEASE_DIR`, `COMMONS_CODEX_BIN`,
`COMMONS_WEB_DIR`, and `COMMONS_RELEASE_IDENTITY_FILE` pinned to it;
`VERSION` must equal the captured basename. A lowercase SHA-256 digest of that
release's `SHA256SUMS` is bound to the exact manifest path and retained in the
transaction together with the exact previous path and release ID. If `current`
exists but is invalid, or previous verification or digest binding fails, the
deploy aborts before backup, pointer mutation, or `systemctl`; the invalid
pointer is not silently treated as absent.

A sanitized deployment-attempt receipt is then written under
`COMMONS_DEPLOY_STATE_DIR` (default `~/.local/state/codex-commons/deploy`). The
canonical existing parent must be a real non-symlink directory owned by the
effective uid/gid and not group or other writable. The deploy state directory
must be a real non-symlink direct child of that parent, owned by the effective
uid/gid, with exact mode 0700; a missing leaf is created `mkdir 0700` and
revalidated. Unsafe parent or directory is rejected before receipt mutation.
The receipt is one regular non-symlink file, mode 0600, owned by the effective
uid/gid. It is written to an exclusively created private temp in that
directory, verified, synced, and atomically `mv -Tf` onto the final path; then
the containing directory is synced. There is no sidecar digest file. Candidate
and previous identity use lowercase SHA-256 digests of each release's
`SHA256SUMS` from `/usr/bin/sha256sum`, bound to the exact manifest path. The
file contains only the fixed fields `kind=deployment-attempt`, `status=recorded`,
candidate id/digest, and previous state (`absent` or validated id/digest). It
must not record secrets, database paths, prompts, environment contents, or
arbitrary payloads. A successful candidate-ready deploy may retain that 7-line
`status=recorded` receipt. A rollback outcome receipt is published through the
same exclusive-temp, mode/owner, sync, `mv -Tf`, and directory-sync publisher
and may add only the allowlisted fields `deploy_outcome`, `service_state`, and
`database_state`. Required rollback receipt publish failure is fail-closed and
does not authorize later database, pointer, or restart mutation.

After the receipt, `COMMONS_DB` is validated as an absolute path whose
canonical parent is a real non-symlink directory owned by the effective
uid/gid and not group or other writable. The destination is either safely
absent for first-deploy cleanup or a regular non-symlink file, mode 0600,
owned by that uid/gid. A dangling symlink is never treated as absent. When a
database is present, `ops/backup.sh` prints the exact backup file created by
this invocation as a single stdout line; deploy captures that regular path and
does not glob-pick a newer timer file by mtime.

Prove this increment offline with disposable directories and fake commands:

```sh
sh ops/test-deploy.sh
sh ops/test-restore.sh
sh ops/test-launch.sh
```

Those fixtures are not authorization to switch a live `current`, restart a
unit, or mutate a live database. `ops/test-launch.sh` never calls systemd; it
uses disposable directories and fake verify/server commands to prove that
verify, `chdir`, and `exec` stay on the originally pinned tree after `current`
is mutated or swapped.

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
7. Install the environment/key with mode 0600, the stable launcher outside
   the release directories (for example
   `~/.local/libexec/codex-commons/commons-launch.sh`), and units/templates
   with mode 0644. The service unit must invoke only that launcher. Enable user
   linger, the service, and backup timer only in the approved maintenance
   window; keep the app listener on loopback.
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

Rollback is a separate operational decision and, when invoked from
`ops/deploy-release.sh`, still runs under the same release-root directory
flock. Rollback uses only the captured exact previous path and release ID from
the earlier preflight; it never re-reads `current` and never resolves a new
target. Candidate restart or readiness failure always exits `1`, even when the
previous release becomes ready.

The fail-closed machine is one-shot. After candidate failure it runs
`systemctl --user stop` and then proves the unit is stopped with an exact,
lossless `systemctl --user show --property=ActiveState --value
codex-commons.service` query. Only `ActiveState=inactive` or `failed` proves
stopped. `active`, `reloading`, `activating`, and `deactivating` are not
stopped. Query failure, empty, multiline, control, or unknown output is
`unknown` and fail-closed. Do not infer active or stopped from a generic
`is-active` exit. Stop failure or an unproven stopped state records
`deploy_outcome=stop_failed` with accurate `service_state` (`active` for known
non-stopped states, `unknown` for command/parse failure) and performs zero
database or `current` mutation. It then revalidates the exact 7-line
`status=recorded` deployment-attempt receipt and the captured previous path,
release ID, and manifest digest. If that receipt is a structurally safe
regular non-symlink file, owned by the effective uid/gid, mode 0600, but the
7-line content does not match the captured attempt identity, publish a
sanitized fixed-field `deploy_outcome=receipt_mismatch` receipt and still
perform zero database or `current` mutation. If the receipt path, state
directory, owner, or mode is unsafe so publishing is not safe, leave the
receipt untouched and exit stopped with zero mutation. Previous re-verification
failure stays stopped with zero database or `current` mutation. A required
rollback receipt with `deploy_outcome=candidate_failed` and
`service_state=stopped` must publish before later mutation; publish
write/sync/`mv -Tf` failure refuses restore, pointer switch, and restart.

If a database restore is required, `ops/deploy-release.sh` invokes packaged
`ops/verify-restore.sh` and `ops/restore-database.sh` through `without_lock_fd`
while fd 9 stays in the parent. Backup verify failure stays stopped with no
restore, pointer switch, or restart. Restore helper failure, including after
the destination has already been replaced, records `database_state=uncertain`,
does not retry, and does not switch `current` or restart. First-deploy cleanup
failure stays stopped. The helper copies the exact verified backup with `cp -P`
into an exclusive private temp in the validated database parent, re-verifies
that temp, and syncs it. It then validates any pre-existing WAL/SHM without deleting them,
atomically `mv -Tf` onto the exact destination, and only afterwards revalidates
each sidecar and removes only safe regular, effective-uid/gid-owned files.
Service is already stopped during that sidecar cleanup. A stale predictable
`.rollback` name is not used. Copy, verify, sync, mv, post-rename sidecar
revalidation/removal, or source/destination/parent validation failure is
fail-closed with no silent fallback. A post-rename sidecar or directory-sync
failure must not restart the service.

Switch `current` only to the captured previous release ID through an owned
`.current.next` temp and `mv -Tf`, then read back that exact ID. Temp, rename,
or readback failure stays stopped and does not restart previous. Absent
previous reads `current` only to verify it still names the exact candidate
`release_id` and then removes that candidate pointer; that read is never used
as target selection. If `current` was swapped, fail stopped without unlinking
the substituted pointer. Previous restart command failure performs exactly one
stop attempt and the same exact ActiveState stopped proof before finishing
with `deploy_outcome=previous_restart_failed` and `service_state=stopped`. If
that final stop or proof fails, record `deploy_outcome=previous_stop_failed`
with `active` or `unknown`, never retry, and do not leave a known active
previous service. Previous readiness failure performs one stop, proves the
final stopped state with the same query, and does not retry; a failed final
stop records `deploy_outcome=previous_stop_failed`. Previous ready publishes
durable `deploy_outcome=previous_ready` and still exits `1`. If that final
`previous_ready` publish fails after previous has started, perform one stop
attempt and the same exact stopped proof before exit; do not restart or retry.

Rollback receipts remain fixed-field only: candidate and previous IDs plus
manifest digests, and allowlisted `status` (`recorded` or `failed`),
`deploy_outcome`, `service_state`, and `database_state`. They must not record
paths, secrets, database names, environment contents, or payloads. An ignored
command, including `|| true`, is not a successful stop, restore, cleanup,
pointer switch, restart, or readiness result.

| From | Event | `deploy_outcome` | `service_state` | `database_state` | `current` | Exit |
| --- | --- | --- | --- | --- | --- | --- |
| Candidate restart/readiness failed | `systemctl stop` or exact ActiveState proof fails | `stop_failed` | `active` or `unknown` | `unchanged` | candidate | 1 |
| Proven stopped | 7-line receipt content mismatch; path/owner/mode 0600 | `receipt_mismatch` | `stopped` | `unchanged` | candidate | 1 |
| Proven stopped | receipt path/state dir/owner/mode unsafe to replace | receipt left in place | `stopped` | `unchanged` | candidate | 1 |
| Proven stopped, receipt valid | captured previous re-verify fails | `previous_reverify_failed` | `stopped` | `unchanged` | candidate | 1 |
| Proven stopped, identities valid | required rollback receipt write/sync/mv fails | original 7-line `recorded` remains | `stopped` | `unchanged` | candidate | 1 |
| Required `candidate_failed` receipt published | backup verify fails | `backup_verify_failed` | `stopped` | `unchanged` | candidate | 1 |
| Backup verified | restore helper fails, possibly after dest replace | `restore_failed` | `stopped` | `uncertain` | candidate | 1 |
| No pre-upgrade DB | first-deploy cleanup fails | `first_deploy_cleanup_failed` | `stopped` | `uncertain` | candidate | 1 |
| DB restored or absent | pointer temp/mv/readback fails | `pointer_switch_failed` | `stopped` | `restored` or `absent` | candidate or unverified | 1 |
| No captured previous, `current` still the candidate | candidate pointer removed | `no_previous` | `stopped` | `absent` or `restored` | absent | 1 |
| No captured previous, `current` swapped | refuse unlink of substituted pointer | `pointer_switch_failed` | `stopped` | `absent` or `restored` | substituted | 1 |
| Pointer switched to captured previous | previous `restart` fails; one stop proves stopped | `previous_restart_failed` | `stopped` | `restored` or `absent` | captured previous | 1 |
| Previous restart failed | final stop or ActiveState proof fails | `previous_stop_failed` | `active` or `unknown` | `restored` or `absent` | captured previous | 1 |
| Previous restarted | previous readiness fails; one stop proves stopped | `previous_readiness_failed` | `stopped` | `restored` or `absent` | captured previous | 1 |
| Previous readiness failed | final stop or ActiveState proof fails | `previous_stop_failed` | `active` or `unknown` | `restored` or `absent` | captured previous | 1 |
| Previous ready, `previous_ready` receipt publish fails | one stop proves stopped; no restart/retry | last published outcome remains | `stopped` | `restored` or `absent` | captured previous | 1 |
| Previous readiness passes | durable `previous_ready` receipt | `previous_ready` | `ready` | `restored` or `absent` | captured previous | 1 |

Classify the result from that table. For any rollback restart/readiness
failure, leave Commons stopped, preserve the restored database/release
metadata and evidence, and escalate. Rollback helpers may use best-effort
temp cleanup; every failure is reported and is not treated as success. Do not
keep retrying a live service, and do not delete the last known-good release
or backup.

## 9. Release gates and approval boundary

Source edits, static checks, Go tests, browser tests, the Phase 4
`sh ops/test-deploy.sh` and `sh ops/test-launch.sh` gates, and a disposable
acceptance run prove the candidate contract only. They do not approve a live deployment. Live
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
