#!/bin/sh
set -eu

# Stable host launcher: resolve current once, pin that exact release directory,
# verify it, chdir into it, and exec its commons-server. Never re-read current
# after the pin. Intended for installation outside mutable release directories.

: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT is required}"
case "$COMMONS_RELEASE_ROOT" in /*) ;; *)
	echo "COMMONS_RELEASE_ROOT must be an absolute path" >&2
	exit 64
	;;
esac
test -d "$COMMONS_RELEASE_ROOT"
COMMONS_RELEASE_ROOT=$(readlink -f "$COMMONS_RELEASE_ROOT")
test -n "$COMMONS_RELEASE_ROOT"
test -d "$COMMONS_RELEASE_ROOT"
test ! -L "$COMMONS_RELEASE_ROOT"

current="$COMMONS_RELEASE_ROOT/current"
if [ ! -L "$current" ]; then
	echo "current release pointer is missing or not a symlink: $current" >&2
	exit 64
fi

# Resolve the configured current pointer exactly once. Require a relative
# release-directory basename; do not follow nested, absolute, traversal, or
# other symlink-shaped targets.
current_target=$(readlink -- "$current")
case "$current_target" in
.|..|*/*|*[!A-Za-z0-9._-]*|'')
	echo "refusing unsafe current release target" >&2
	exit 64
	;;
esac

release_dir="$COMMONS_RELEASE_ROOT/$current_target"
if [ ! -d "$release_dir" ] || [ -L "$release_dir" ]; then
	echo "current release is missing, not a directory, or symlink-shaped" >&2
	exit 64
fi
release_dir=$(readlink -f "$release_dir")
test -n "$release_dir"
test -d "$release_dir"
test ! -L "$release_dir"
test "$(dirname "$release_dir")" = "$COMMONS_RELEASE_ROOT"
test "$(basename "$release_dir")" = "$current_target"

COMMONS_RELEASE_DIR=$release_dir
COMMONS_WEB_DIR="$COMMONS_RELEASE_DIR/web"
COMMONS_CODEX_BIN="$COMMONS_RELEASE_DIR/bin/codex"
COMMONS_RELEASE_IDENTITY_FILE="$COMMONS_RELEASE_DIR/VERSION"
export COMMONS_RELEASE_ROOT COMMONS_RELEASE_DIR COMMONS_WEB_DIR COMMONS_CODEX_BIN COMMONS_RELEASE_IDENTITY_FILE

if [ ! -f "$COMMONS_RELEASE_DIR/ops/verify-release.sh" ] || [ -L "$COMMONS_RELEASE_DIR/ops/verify-release.sh" ]; then
	echo "release verifier is missing or symlink-shaped" >&2
	exit 64
fi
/bin/sh "$COMMONS_RELEASE_DIR/ops/verify-release.sh"

cd -- "$COMMONS_RELEASE_DIR" || exit 64
if [ ! -f "$COMMONS_RELEASE_DIR/commons-server" ] || [ -L "$COMMONS_RELEASE_DIR/commons-server" ] || [ ! -x "$COMMONS_RELEASE_DIR/commons-server" ]; then
	echo "release server is missing, not executable, or symlink-shaped" >&2
	exit 64
fi
exec "$COMMONS_RELEASE_DIR/commons-server" "$@"
