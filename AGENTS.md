# Codex Commons repository instructions

Codex Commons is a production-related repository. Treat server, deployment,
release, migration, authentication, and runtime work as operationally
sensitive even when the requested change is local or experimental.

## Scope and delegation

- These rules apply to the whole repository and are additive to any more
  specific instructions.
- Before working under `web/`, read and follow [`web/AGENTS.md`](web/AGENTS.md)
  for the web-specific design, prototype, asset, and release-gate rules.
- Do not treat fixtures, storyboards, screenshots, generated output, or model
  output as production authority. Production-shaped contracts and the
  documented release procedure remain the source of truth.

## Dirty-worktree preservation

- Preserve every pre-existing tracked and untracked change. Before editing,
  record `git status --short --branch`, the relevant diff/path inventory, and
  the current HEAD/upstream identity.
- Never use `git reset --hard`, `git checkout --`, broad cleanup, wholesale
  staging, or broad formatting to make this worktree convenient. Do not
  overwrite a user file to resolve an unrelated conflict.
- Do not stage, commit, push, deploy, or activate a candidate unless the user
  explicitly authorizes that separate action. Keep new work distinguishable
  from the baseline in the final report.

## Server and runtime safeguards

- Before any server, deployment, systemd, storage, networking,
  authentication, database, or runtime inspection or change, read the
  applicable notes under `/home/plumbob/.codex/memories/plumbob-server/`.
  Never inspect, copy, log, or disclose credentials, tokens, prompt bodies,
  personal data, or secret-file contents.
- Run `ss -tlnp` before binding any development or test listener. Choose a
  confirmed-free loopback port and do not reuse an existing production or
  control-plane listener. In particular, do not disturb the existing
  Codex Commons listener, the DeepSeek control-plane UI, the `/mnt/d` mount
  ordering, `/usr/local/bin/mediastack-watchdog.sh`, or
  `/usr/local/bin/plumbob-services.sh`.
- Do not start, stop, restart, reload, or reconfigure a live service; change
  Caddy, systemd, firewall, DNS, mounts, or host security policy; or bind a
  listener without an explicit operational plan and authorization.
- Do not deploy, switch a `current` release pointer, activate a candidate,
  apply a migration, run native or historical Apply, launch or cancel a live
  historian task, or mutate the live database in a documentation/governance
  task. Read-only health and readiness checks must not be represented as
  deployment approval.
- Keep candidate/release work immutable and separate from the dirty source
  tree. Package only from an explicitly reviewed, reproducible input set and
  the documented runbook; retain checksums and bounded evidence without
  recording secrets.

## Repository governance

- The critical release inputs currently called out by the Phase 0 baseline
  include `ops/`, `deploy/`, migrations `014` and `015`, selected native
  Apply/store files, and the Project Archaeology history/browser-gate files.
  They must be intentional tracked release inputs before packaging; do not
  solve that requirement by broadly unignoring secrets, databases, `.local/`,
  `node_modules/`, `dist/`, or other generated output.
- Keep provenance documents sanitized and source-grounded. Record identifiers,
  versions, checksums, statuses, and paths only when they are safe metadata;
  do not copy payloads, transcripts, credentials, or personal content into
  the repository.
- For governance-only changes, run the lightweight governance check when
  available, plus proportionate static tests, `git diff --check`, and an
  exact status/diff review. Do not expand Phase 0 into the Phase 1 release
  verifier or deployment workflow.
