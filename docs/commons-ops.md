# `commons-ops` packaged boundary

Every immutable Commons release contains one `commons-ops` executable at its
top level. Help, version, and the embedded release identity remain available.
This increment enables packaged `commons-ops backup`. Restore, archive, evidence, and
deployment commands stay disabled. Future Phase 5 path-sensitive restore work
must still enter through this packaged boundary rather than interpolating
operator paths into shell or SQLite commands.

The release builder embeds the same release ID in `commons-server` and
`commons-ops`. Staging copies the exact helper, and complete-tree verification
checks its executable mode, identity, and SHA256SUMS entry. Packaged
`ops/backup.sh` pins `PATH=/usr/bin:/bin`, sets umask 077, closes any inherited
copy of fd 9, and execs that helper. `COMMONS_DB` and `COMMONS_BACKUP_DIR`
remain the operator inputs. On success the helper writes exactly one absolute
backup path to stdout; diagnostics go to stderr. Deploy captures that single
line and must not select a different timer file by mtime.

Source schema 17 adds a stable per-database `installation_id` and an empty
append-only `installation_restore_evidence` table. That identity is generated
once, is not derived from `review_secret`, is rejected if all-zero, and does
not change Beta-prerequisite truth. Packaged `ops/check-readiness.sh` still
pins live readiness to schema 15; updating that pin is later ops integration
together with restore command work. This increment does not change packaged
readiness or restore state-machine behavior.

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

## Backup path boundary

Operator paths are absolute, slash-separated, and limited to `[A-Za-z0-9._-]`
components. Traversal, controls, whitespace, and SQL/shell punctuation fail
closed. Linux `openat(2)` with `O_NOFOLLOW` walks every component. The backup
root, `daily/`, and `monthly/` directories must be canonical, contained,
non-symlink directories owned by the effective uid/gid with exact mode 0700.
The live database must be a regular nlink=1 mode-0600 file whose parent is an
operator-owned directory that is not group or world writable. FIFOs, dangling
symlinks, hard links, and foreign owner/mode objects fail closed.

Backup serializes on a nonblocking exclusive `flock` of the verified backup-root
directory descriptor. It does not create, open, or trust a pathname lock file.
A second cooperating invocation exits `75` (busy). Creators set umask 077.

Publication prepares the SQLite copy plus checksum and sanitized receipt
sidecars in a random private mode-0700 directory inside the verified
destination, fsyncs file data and directories, then publishes each leaf with
`renameat2(RENAME_NOREPLACE)`. Parent directories are fsynced after each public
rename. Monthly publication uses the same no-follow/no-replace policy: an
already-valid monthly file is left in place; a symlink, FIFO, directory, or
other occupant of the monthly name fails closed. Successfully published leaves
are not unlinked on a later failure or signal.

Backup receipts contain only deterministic metadata (`file`, `sha256`,
`verified_at`, `schema`, `schema_digest`, `counts`, `selected_digest`,
`integrity`, `foreign_keys`). They do not copy secrets, transcripts, prompts,
or selected-import payloads. Checksums are one canonical GNU text-mode
`sha256sum` record for the published absolute path.

Retention inspects only direct children of the verified daily or monthly
directory. It may delete only validated regular files with expected owner,
mode 0600, and nlink=1. It never follows or deletes symlinks, directories, or
foreign owner/mode/hard-linked entries.

SQLite is opened through a pinned directory-fd URI
(`/proc/self/fd/<dirfd>/<leaf>`) so WAL/SHM sidecars are created next to the
real leaf. Directory flock plus inode/mode/nlink revalidation run as close as
possible to open and publish. This is not an absolute TOCTOU close against a
hostile same-uid actor: SQLite's own open does not take `O_NOFOLLOW`,
`flock(2)` is advisory, and unlink of a retained name is still a name-based
operation after an fd-relative regular-file check. Cooperating Commons
processes are serialized. Residual same-uid rename races are detected after
open when the inode or `/proc/self/fd` path drifts; they are not claimed
closed.
