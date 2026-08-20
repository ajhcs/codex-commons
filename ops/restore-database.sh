#!/bin/sh
set -eu
# Packaged atomic same-directory SQLite restore. Accept the exact verified
# backup and the exact DB destination. Child of deploy-release.sh; close any
# inherited copy of the deploy lock fd and leave fd 9 in the parent.
: "${1:?exact backup path required}"
: "${2:?exact database destination required}"
exec 9<&-
umask 077
LC_ALL=C
LANG=C
export LC_ALL LANG
backup=$1
dest=$2
ops_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
verify_restore=$ops_dir/verify-restore.sh
owned_restore_tmp=
nl='
'

reject_controls() {
	case "$1" in
	*[[:cntrl:]]*)
		echo "$2" >&2
		exit 64
		;;
	esac
}

require_abs_safe_path() {
	path=$1
	label=$2
	reject_controls "$path" "$label must not contain control characters"
	case "$path" in /*) ;; *)
		echo "$label must be an absolute path" >&2
		exit 64
		;;
	esac
	case "$path" in
	*/)
		echo "$label must not have a trailing slash" >&2
		exit 64
		;;
	esac
	case "$path" in
	*/.|*/..|*/./*|*/../*)
		echo "$label must not contain . or .. components" >&2
		exit 64
		;;
	esac
	case "$path" in
	*[!A-Za-z0-9_./:-]*)
		echo "$label contains unsafe characters" >&2
		exit 64
		;;
	esac
}

require_safe_leaf() {
	case "$1" in
	.|..|*/*|*[!A-Za-z0-9._-]*|'')
		echo "$2" >&2
		exit 64
		;;
	esac
}

require_regular_private() {
	path=$1
	label=$2
	if [ -L "$path" ]; then
		echo "$label must not be a symlink: $path" >&2
		exit 64
	fi
	if [ ! -f "$path" ]; then
		echo "$label is missing or not a regular file: $path" >&2
		exit 64
	fi
	effective_uid=$(id -u)
	effective_gid=$(id -g)
	file_uid=$(stat -c %u "$path")
	file_gid=$(stat -c %g "$path")
	file_mode=$(stat -c %a "$path")
	if [ "$file_uid" != "$effective_uid" ] || [ "$file_gid" != "$effective_gid" ] || [ "$file_mode" != 600 ]; then
		echo "$label must be mode 0600 and owned by the effective uid/gid: $path" >&2
		exit 64
	fi
}

file_sha256() {
	path=$1
	label=$2
	require_regular_private "$path" "$label"
	sum=$(/usr/bin/sha256sum -- "$path") || {
		echo "failed to hash $label: $path" >&2
		exit 64
	}
	digest=${sum%% *}
	case "$digest" in
	*[!0-9a-f]*|'')
		echo "malformed $label digest: $path" >&2
		exit 64
		;;
	esac
	if [ "${#digest}" -ne 64 ] || [ "$sum" != "$digest  $path" ]; then
		echo "$label digest is not bound to the exact path: $path" >&2
		exit 64
	fi
	printf '%s\n' "$digest"
}

validate_parent_dir() {
	parent=$1
	if [ -L "$parent" ]; then
		echo "database parent must not be a symlink: $parent" >&2
		exit 64
	fi
	if [ ! -d "$parent" ]; then
		echo "database parent is missing or not a directory: $parent" >&2
		exit 64
	fi
	canonical=$(readlink -f "$parent") || {
		echo "failed to canonicalize database parent" >&2
		exit 64
	}
	reject_controls "$canonical" "database parent must not contain control characters"
	test -n "$canonical"
	if [ -L "$canonical" ] || [ ! -d "$canonical" ]; then
		echo "database parent must be a real non-symlink directory: $canonical" >&2
		exit 64
	fi
	effective_uid=$(id -u)
	effective_gid=$(id -g)
	parent_uid=$(stat -c %u "$canonical")
	parent_gid=$(stat -c %g "$canonical")
	parent_mode=$(stat -c %a "$canonical")
	if [ "$parent_uid" != "$effective_uid" ] || [ "$parent_gid" != "$effective_gid" ]; then
		echo "database parent must be owned by the effective uid/gid: $canonical" >&2
		exit 64
	fi
	case "$parent_mode" in
	[0-7][0-7][0-7]|[0-7][0-7][0-7][0-7]) ;;
	*)
		echo "database parent mode is unsafe: $canonical" >&2
		exit 64
		;;
	esac
	if [ "$((0$parent_mode & 022))" -ne 0 ]; then
		echo "database parent must not be group or other writable: $canonical" >&2
		exit 64
	fi
	printf '%s\n' "$canonical"
}

validate_destination() {
	path=$1
	# A dangling symlink is not absent. Presence checks must not follow.
	if [ -L "$path" ]; then
		echo "database destination must not be a symlink: $path" >&2
		exit 64
	fi
	if [ -e "$path" ]; then
		if [ ! -f "$path" ]; then
			echo "database destination exists and is not a regular file: $path" >&2
			exit 64
		fi
		require_regular_private "$path" "database destination"
	fi
}

# Remove only the exclusive temp created by this invocation. Never follow a
# substituted symlink, never remove the destination or source, and never
# unlink a stale predictable .rollback name.
cleanup_owned_restore_tmp() {
	if [ -z "${owned_restore_tmp:-}" ]; then
		return 0
	fi
	if [ -L "$owned_restore_tmp" ]; then
		owned_restore_tmp=
		return 0
	fi
	if [ -f "$owned_restore_tmp" ]; then
		rm -f -- "$owned_restore_tmp"
	fi
	owned_restore_tmp=
}

fail_restore() {
	echo "$1" >&2
	cleanup_owned_restore_tmp
	exit 64
}

fail_closed() {
	echo "$1" >&2
	exit 64
}

# Presence checks must not follow. A dangling sidecar symlink is not absent.
# $3 is an optional message suffix such as " after restore".
require_absent_or_safe_sidecar() {
	path=$1
	label=$2
	suffix=${3:-}
	if [ -L "$path" ]; then
		echo "refusing $label symlink${suffix}: $path" >&2
		return 1
	fi
	if [ ! -e "$path" ]; then
		return 0
	fi
	if [ -d "$path" ]; then
		echo "refusing $label directory${suffix}: $path" >&2
		return 1
	fi
	if [ ! -f "$path" ]; then
		echo "refusing non-regular $label${suffix}: $path" >&2
		return 1
	fi
	effective_uid=$(id -u)
	effective_gid=$(id -g)
	side_uid=$(stat -c %u "$path")
	side_gid=$(stat -c %g "$path")
	if [ "$side_uid" != "$effective_uid" ] || [ "$side_gid" != "$effective_gid" ]; then
		echo "$label must be owned by the effective uid/gid${suffix}: $path" >&2
		return 1
	fi
	return 0
}

# Pre-rename: reject unsafe WAL/SHM, but leave a safe sidecar in place so a
# failed mv cannot orphan the old destination.
validate_sidecar() {
	path=$1
	label=$2
	require_absent_or_safe_sidecar "$path" "$label" || {
		cleanup_owned_restore_tmp
		exit 64
	}
}

# Post-rename: revalidate, then unlink only a safe regular euid/egid-owned
# file. Any unsafe shape or removal failure is fail-closed and must not
# restart the service. Do not follow a substituted symlink.
remove_validated_sidecar() {
	path=$1
	label=$2
	require_absent_or_safe_sidecar "$path" "$label" " after restore" || exit 64
	if [ -L "$path" ]; then
		fail_closed "refusing $label symlink after restore: $path"
	fi
	if [ ! -e "$path" ]; then
		return 0
	fi
	if [ -d "$path" ] || [ ! -f "$path" ]; then
		fail_closed "refusing non-regular $label after restore: $path"
	fi
	rm -f -- "$path" || fail_closed "failed to remove $label after restore: $path"
	if [ -e "$path" ] || [ -L "$path" ]; then
		fail_closed "failed to remove $label after restore: $path"
	fi
}

verify_sqlite_copy() {
	path=$1
	label=$2
	require_regular_private "$path" "$label"
	# Pass the file as argv. Do not interpolate operator paths into SQL.
	integrity=$(sqlite3 -- "$path" 'PRAGMA integrity_check') || fail_restore "failed $label integrity check"
	if [ "$integrity" != ok ]; then
		fail_restore "$label failed integrity check"
	fi
	fk=$(sqlite3 -- "$path" 'PRAGMA foreign_key_check') || fail_restore "failed $label foreign key check"
	if [ -n "$fk" ]; then
		fail_restore "$label failed foreign key check"
	fi
}

trap cleanup_owned_restore_tmp 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15

require_abs_safe_path "$backup" "backup path"
require_abs_safe_path "$dest" "database destination"
require_regular_private "$backup" "backup path"
if [ ! -f "$verify_restore" ] || [ -L "$verify_restore" ]; then
	echo "restore verifier is missing or symlink-shaped: $verify_restore" >&2
	exit 64
fi
/bin/sh "$verify_restore" "$backup" >/dev/null

dest_leaf=$(basename "$dest")
require_safe_leaf "$dest_leaf" "database destination leaf must be a safe basename"
dest_parent=$(dirname "$dest")
require_abs_safe_path "$dest_parent" "database parent"
dest_parent=$(validate_parent_dir "$dest_parent") || exit 64
dest_parent=${dest_parent%"$nl"}
dest=$dest_parent/$dest_leaf
if [ "$(dirname "$dest")" != "$dest_parent" ] || [ "$(basename "$dest")" != "$dest_leaf" ]; then
	echo "database destination is not a canonical direct child of its parent: $dest" >&2
	exit 64
fi
validate_destination "$dest"

backup_digest=$(file_sha256 "$backup" "backup path") || exit 64
backup_digest=${backup_digest%"$nl"}

restore_tmp=$(/usr/bin/mktemp -- "$dest_parent/commons-restore.XXXXXX") || {
	echo "failed to create private restore temp" >&2
	exit 64
}
if [ "$(dirname "$restore_tmp")" != "$dest_parent" ]; then
	rm -f -- "$restore_tmp"
	echo "private restore temp is not in the validated database parent" >&2
	exit 64
fi
owned_restore_tmp=$restore_tmp
tmp_leaf=$(basename "$restore_tmp")
case "$tmp_leaf" in
commons-restore.??????) ;;
*)
	fail_restore "private restore temp name is not exclusive-mktemp-shaped"
	;;
esac
if [ -L "$restore_tmp" ] || [ ! -f "$restore_tmp" ]; then
	fail_restore "private restore temp must be a regular non-symlink file"
fi

require_regular_private "$backup" "backup path"
if ! cp -P -- "$backup" "$restore_tmp"; then
	fail_restore "failed to copy backup into private restore temp"
fi
if [ -L "$restore_tmp" ]; then
	fail_restore "restore temp became a symlink during copy"
fi
if [ ! -f "$restore_tmp" ]; then
	fail_restore "restore temp is missing or not a regular file after copy"
fi
if ! chmod 0600 -- "$restore_tmp"; then
	fail_restore "failed to chmod private restore temp"
fi
require_regular_private "$restore_tmp" "private restore temp"
tmp_digest=$(file_sha256 "$restore_tmp" "private restore temp") || {
	cleanup_owned_restore_tmp
	exit 64
}
tmp_digest=${tmp_digest%"$nl"}
if [ "$tmp_digest" != "$backup_digest" ]; then
	fail_restore "restore temp digest does not match the exact backup"
fi
verify_sqlite_copy "$restore_tmp" "private restore temp"

if ! sync -- "$restore_tmp"; then
	fail_restore "failed to sync private restore temp"
fi

validate_parent_dir "$dest_parent" >/dev/null || {
	cleanup_owned_restore_tmp
	exit 64
}
validate_destination "$dest"
validate_sidecar "$dest-wal" "WAL"
validate_sidecar "$dest-shm" "SHM"

if [ -L "$dest" ]; then
	fail_restore "database destination must not be a symlink: $dest"
fi
if [ -e "$dest" ] && [ ! -f "$dest" ]; then
	fail_restore "database destination exists and is not a regular file: $dest"
fi
if ! mv -Tf -- "$restore_tmp" "$dest"; then
	fail_restore "failed to atomically replace database destination"
fi
owned_restore_tmp=

if [ -L "$dest" ] || [ ! -f "$dest" ]; then
	fail_closed "database destination must be a regular non-symlink file after restore: $dest"
fi
require_regular_private "$dest" "database destination"
final_digest=$(file_sha256 "$dest" "database destination") || exit 64
final_digest=${final_digest%"$nl"}
if [ "$final_digest" != "$backup_digest" ]; then
	fail_closed "restored database digest does not match the exact backup"
fi
remove_validated_sidecar "$dest-wal" "WAL"
remove_validated_sidecar "$dest-shm" "SHM"
if ! sync -- "$dest_parent"; then
	fail_closed "failed to sync database parent directory"
fi
