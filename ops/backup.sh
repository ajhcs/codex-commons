#!/bin/sh
set -eu
# Compatibility wrapper for packaged commons-ops backup. Preserve the deploy
# contract: stdout is exactly one absolute backup path. Diagnostics go to
# stderr. Do not open a pathname .backup.lock and do not rebind fd 9.
umask 077
PATH=/usr/bin:/bin
export PATH
LC_ALL=C
LANG=C
export LC_ALL LANG
exec 9<&-
: "${COMMONS_DB:?COMMONS_DB is required}"
: "${COMMONS_BACKUP_DIR:?COMMONS_BACKUP_DIR is required}"
if test -n "${COMMONS_OPS_BIN:-}"; then
	ops_bin=$COMMONS_OPS_BIN
else
	ops_bin=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)/commons-ops
fi
case "$ops_bin" in /*) ;; *)
	echo "commons-ops path must be absolute" >&2
	exit 64
	;;
esac
if [ -L "$ops_bin" ] || [ ! -f "$ops_bin" ] || [ ! -x "$ops_bin" ]; then
	echo "commons-ops must be a regular executable: $ops_bin" >&2
	exit 64
fi
exec "$ops_bin" backup
