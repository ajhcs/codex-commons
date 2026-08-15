# Continuous dogfood cutover

This is an operator checklist, not an automatic installer. Run every validation
against disposable paths first. Never point an older release at a newer schema.

1. Stop the temporary Commons process and verify `127.0.0.1:8088` is free.
2. Use `ops/seal-archive.sh SOURCE ARCHIVE` to create a consistent mode-0600
   schema-13 evidence snapshot and receipt. Preserve SOURCE and any WAL/SHM.
3. Create a fresh mode-0700 state directory. Leave the production database
   absent so the schema-15 binary creates it.
4. Build the server with `ops/build-release.sh RELEASE_ID /absolute/output`.
   Set `COMMONS_CODEX_BUNDLE_SOURCE` to the exact native Codex vendor directory,
   then stage with `ops/stage-release.sh RELEASE_ID`. The immutable release keeps
   the vendor-relative layout: `bin/codex`, `bin/codex-code-mode-host`,
   `codex-resources/bwrap`, `codex-resources/zsh/bin/zsh`, `codex-path/rg`, and
   `codex-package.json`. Verify the complete `SHA256SUMS`, exact 0.147.0 version,
   six pinned checksums, regular-file/non-symlink types, executable/read-only
   modes, and absence of unmanifested files with `ops/verify-release.sh`.
   Missing, linked, extra, or hash-mismatched runtime content fails before
   service start. The manifest-bound `VERSION` file is authoritative; the
   service reads it through `COMMONS_RELEASE_IDENTITY_FILE`.
5. Before deployment, add an AppArmor attachment for the exact immutable
   release path `RELEASE/codex-resources/bwrap`; never use a wildcard or the
   mutable `current` symlink. Parse the proposed profile offline first, load it
   only after the release manifest is verified, and retain the prior profile for
   rollback. Run the explicitly gated no-task runtime preflight to exercise the
   packaged bwrap, zsh, and rg and managed App Server account/model inventory.
   It must not create a Codex thread/turn or a Commons batch/job/report.
6. Bootstrap with direct loopback HTTP, no `COMMONS_PUBLIC_ORIGIN`, no Caddy,
   and `COMMONS_ALLOW_FIRST_CODEX_BIND_LAN=false`. Bind and verify exactly one
   durable `human_account_binding`, then stop Commons. Never expose an unbound
   installation through the loopback reverse proxy.
7. Install the environment/key mode 0600 and user units/Caddy template mode
   0644. Enable user linger, daemon-reload, the service, and backup timer only
   in the approved maintenance window. The service listener stays loopback.
8. Set `COMMONS_PUBLIC_ORIGIN=https://commons.plumbob.lan`, keep first-LAN-bind
   false, then deploy. Confirm Type=notify READY, watchdog health, exact release
   version, schema 15, pinned Codex/account/compatibility, and Host/origin/CSRF.
9. Add the private DNS A/AAAA record and Caddy site with `tls internal`; validate
   certificate trust, HSTS/security headers, no arbitrary Host, and no public
   internet exposure. Authelia remains out of this path.
10. Run one manual Luna Max historian for exactly one project. Verify one exact final
   Codex title, repeated durable report reads, repository immutability, no
   canonical mutation, then record the four acceptance receipt results with
   `COMMONS_DB=/absolute/live.sqlite3 ops/record-evidence.sh RECEIPT.json`.
   The receipt must be mode 0600, carry a matching `.sha256`, and bind its kind,
   status, violations, checked time, and acceptance scope digest. Verified
   requires zero violations; attention requires a positive bounded count.
   Never toggle evidence from the browser or without that receipt.
11. Review every selected preview page, apply only via the final completion
    token, restart Commons/Caddy, and prove session/history/report/import recovery.
12. Run `ops/backup.sh`, then `ops/verify-restore.sh BACKUP` in isolation. After
    the schema, integrity, FK, counts, and selected-audit digests match, record
    the successful drill with `COMMONS_RESTORE_STATUS_DB=/absolute/live.sqlite3
    ops/verify-restore.sh BACKUP --record-drill`. Never record a rollback
    preflight as a restore drill.
13. For rollback: stop the restart-on-failure service, validate the matching
    pre-upgrade receipt, remove only the exact DB WAL/SHM while stopped, restore
    the matching database atomically, switch `current` to its matching release,
    then restart and re-run readiness. A first-release failure removes `current`
    and remains stopped.

Beta remains a human decision. The dashboard must keep its recommendation false
while any verification is unknown/attention, any report is lost, uncertainty is
unresolved, or backup/restore/compatibility/reconciliation is not verified.
