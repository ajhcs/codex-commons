# `commons-ops` packaged boundary

Every immutable Commons release contains one dormant `commons-ops` executable
at its top level. It currently exposes only help, version, and the embedded
release identity; it performs no operational mutation. Future Phase 5
path-sensitive receipt, configuration, backup, and restore work must enter
through this packaged boundary rather than interpolating operator paths into
shell or SQLite commands.

The release builder embeds the same release ID in `commons-server` and
`commons-ops`. Staging copies the exact helper, and complete-tree verification
checks its executable mode, identity, and SHA256SUMS entry.

Source schema 17 adds a stable per-database `installation_id` and an empty
append-only `installation_restore_evidence` table. That identity is generated
once, is not derived from `review_secret`, is rejected if all-zero, and does
not change Beta-prerequisite truth. Packaged `ops/check-readiness.sh` still
pins live readiness to schema 15; updating that pin is later ops integration
together with backup/restore command work. This increment does not change
packaged readiness, backup, or restore shell behavior.

Restore evidence is recorded only after a strict bounded parse of a sanitized
receipt object. The parser rejects duplicate keys, unknown fields, malformed
JSON, trailing data, control characters, wrong types, oversized input/fields,
uppercase or non-hex SHA-256 digests, and invalid schema, release, drill, or
timestamp values. A validated receipt must match the live `installation_id`;
cross-installation identities are rejected. The store then derives a
deterministic domain-separated fingerprint (`codex-commons.installation.restore-evidence`
v1) that explicitly frames hash algorithm `sha256` with the domain and version,
then length-prefixed field framing over the bound identity, drill, timestamp,
backup digest, schema version, and release id. Exact replay is
idempotent; drill or digest collisions fail closed. Recording a valid receipt
does not set `restore_status` and does not make Beta prerequisites true.
Backup/restore CLI commands remain later work.
