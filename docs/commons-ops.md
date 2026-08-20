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
