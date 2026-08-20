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
