#!/bin/sh
set -eu

# Pin a trusted PATH and C locale before any path or parsing utility runs.
# An EnvironmentFile-supplied PATH/locale must not select a substitute.
PATH=/usr/bin:/bin
export PATH
LC_ALL=C
LANG=C
export LC_ALL LANG

# Stable host launcher: resolve current once, pin that exact release directory,
# verify it, chdir into it, and exec its commons-server. Never re-read current
# after the pin. Intended for installation outside mutable release directories.

# Capture GNU command stdout losslessly. POSIX $(...) strips every trailing
# newline; a non-newline sentinel survives that strip. Drop only the sentinel
# and exactly one GNU terminator newline so a value that itself ends in
# newline is still presented to later checks. Command failure fails closed.
nl='
'
capture() {
	captured=$(
		"$@" || exit 1
		printf x
	) || return 1
	captured=${captured%x}
	case "$captured" in
	*"$nl") captured=${captured%"$nl"} ;;
	*) return 1 ;;
	esac
}

reject_controls() {
	case "$1" in
	*[[:cntrl:]]*)
		echo "$2" >&2
		exit 64
		;;
	esac
}

: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT is required}"
case "$COMMONS_RELEASE_ROOT" in /*) ;; *)
	echo "COMMONS_RELEASE_ROOT must be an absolute path" >&2
	exit 64
	;;
esac
reject_controls "$COMMONS_RELEASE_ROOT" "COMMONS_RELEASE_ROOT must not contain control characters"
test -d "$COMMONS_RELEASE_ROOT"
if ! capture readlink -f "$COMMONS_RELEASE_ROOT"; then
	echo "failed to canonicalize COMMONS_RELEASE_ROOT" >&2
	exit 64
fi
COMMONS_RELEASE_ROOT=$captured
test -n "$COMMONS_RELEASE_ROOT"
reject_controls "$COMMONS_RELEASE_ROOT" "COMMONS_RELEASE_ROOT must not contain control characters"
test -d "$COMMONS_RELEASE_ROOT"
test ! -L "$COMMONS_RELEASE_ROOT"

current="$COMMONS_RELEASE_ROOT/current"
if [ ! -L "$current" ]; then
	echo "current release pointer is missing or not a symlink: $current" >&2
	exit 64
fi

# Resolve the configured current pointer exactly once. Require a relative
# release-directory basename; do not follow nested, absolute, traversal, or
# other symlink-shaped targets. Capture losslessly before the ASCII grammar
# so a trailing newline cannot be stripped into a legal name.
if ! capture readlink -- "$current"; then
	echo "failed to read current release pointer" >&2
	exit 64
fi
current_target=$captured
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
if ! capture readlink -f "$release_dir"; then
	echo "failed to canonicalize current release directory" >&2
	exit 64
fi
release_dir=$captured
test -n "$release_dir"
reject_controls "$release_dir" "current release directory must not contain control characters"
test -d "$release_dir"
test ! -L "$release_dir"
# dirname/basename captures are safe after the grammar and control rejects.
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
