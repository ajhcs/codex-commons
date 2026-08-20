#!/bin/sh
set -eu
: "${1:?staged release directory required}"
: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT is required}"
: "${COMMONS_DB:?COMMONS_DB is required}"
staged=$1
systemctl_cmd=${COMMONS_SYSTEMCTL:-systemctl}
case "$staged:$COMMONS_RELEASE_ROOT:$COMMONS_DB" in /*:/*:/*) ;; *) exit 64;; esac
test -d "$staged"; test ! -L "$staged"
test -d "$COMMONS_RELEASE_ROOT"
# Canonicalize the lock domain before any deploy mutation. Open that directory
# itself; do not create a .lock file or flock a symlink.
COMMONS_RELEASE_ROOT=$(readlink -f "$COMMONS_RELEASE_ROOT")
test -n "$COMMONS_RELEASE_ROOT"
test -d "$COMMONS_RELEASE_ROOT"
test ! -L "$COMMONS_RELEASE_ROOT"
staged=$(readlink -f "$staged")
test -n "$staged"
test -d "$staged"; test ! -L "$staged"; test "$(dirname "$staged")" = "$COMMONS_RELEASE_ROOT"
exec 9<"$COMMONS_RELEASE_ROOT"
if ! flock -n 9; then
	echo "another deploy already holds $COMMONS_RELEASE_ROOT" >&2
	exit 75
fi
# Grammar, digest, and receipt fields require the C locale. Do not inherit an
# operator locale into character classes or sha256sum text.
LC_ALL=C
LANG=C
export LC_ALL LANG
# flock(2) locks follow the open-file description. A child that inherited fd 9
# could `flock -u 9` and release this transaction while the parent is still
# active. Close the child's copy only; the parent keeps fd 9 until exit.
# Do not rely on backup.sh rebinding fd 9.
without_lock_fd() {
	"$@" 9<&-
}
# Capture GNU command stdout losslessly. POSIX $(...) strips every trailing
# newline; a non-newline sentinel survives that strip. Drop only the sentinel
# and exactly one GNU terminator newline so a value that itself ends in
# newline is still presented to later checks. Command failure fails closed.
# Child commands do not inherit a usable copy of the deploy lock fd.
nl='
'
capture() {
	captured=$(
		without_lock_fd "$@" || exit 1
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
require_release_id() {
	case "$1" in
	.|..|*/*|*[!A-Za-z0-9._-]*|'')
		echo "$2" >&2
		exit 64
		;;
	esac
}
require_manifest_digest() {
	case "$1" in
	*[!0-9a-f]*|'')
		echo "$2" >&2
		exit 64
		;;
	esac
	if [ "${#1}" -ne 64 ]; then
		echo "$2" >&2
		exit 64
	fi
}
# Hash the exact SHA256SUMS file of one canonical release directory. Require a
# regular non-symlink file and a lowercase 64-hex digest bound to that path.
# Identity evidence uses the trusted host hasher; do not honor PATH or
# EnvironmentFile substitutes.
manifest_digest() {
	release_dir=$1
	sums=$release_dir/SHA256SUMS
	if [ -L "$sums" ] || [ ! -f "$sums" ]; then
		echo "release manifest is missing, not a regular file, or symlink-shaped: $sums" >&2
		exit 64
	fi
	if ! capture /usr/bin/sha256sum -- "$sums"; then
		echo "failed to hash release manifest: $sums" >&2
		exit 64
	fi
	digest=${captured%% *}
	require_manifest_digest "$digest" "malformed release manifest digest"
	if [ "$captured" != "$digest  $sums" ]; then
		echo "manifest digest is not bound to the exact release manifest: $sums" >&2
		exit 64
	fi
	printf '%s\n' "$digest"
}
# Private operator-configured or default state directory for the sanitized
# deployment-attempt receipt. The canonical existing parent must be owned by
# the effective uid/gid and must not be group or other writable. The leaf must
# be a real non-symlink direct child, owned by the effective uid/gid, mode
# 0700. Child commands used here do not inherit the lock fd.
prepare_deploy_state_dir() {
	state_dir=${COMMONS_DEPLOY_STATE_DIR:-}
	if [ -z "$state_dir" ]; then
		: "${HOME:?HOME or COMMONS_DEPLOY_STATE_DIR is required}"
		reject_controls "$HOME" "HOME must not contain control characters"
		case "$HOME" in /*) ;; *)
			echo "HOME must be an absolute path" >&2
			exit 64
			;;
		esac
		state_dir=$HOME/.local/state/codex-commons/deploy
	fi
	reject_controls "$state_dir" "COMMONS_DEPLOY_STATE_DIR must not contain control characters"
	case "$state_dir" in /*) ;; *)
		echo "COMMONS_DEPLOY_STATE_DIR must be an absolute path" >&2
		exit 64
		;;
	esac
	case "$state_dir" in
	*/)
		echo "COMMONS_DEPLOY_STATE_DIR must not have a trailing slash" >&2
		exit 64
		;;
	esac
	case "$state_dir" in
	*/.|*/..|*/./*|*/../*)
		echo "COMMONS_DEPLOY_STATE_DIR must not contain . or .. components" >&2
		exit 64
		;;
	esac
	state_parent=$(without_lock_fd dirname "$state_dir")
	state_leaf=$(without_lock_fd basename "$state_dir")
	require_release_id "$state_leaf" "COMMONS_DEPLOY_STATE_DIR leaf must be a safe basename"
	if [ -L "$state_parent" ]; then
		echo "deploy state parent must not be a symlink: $state_parent" >&2
		exit 64
	fi
	if [ ! -d "$state_parent" ]; then
		echo "deploy state parent is missing or not a directory: $state_parent" >&2
		exit 64
	fi
	if ! capture readlink -f "$state_parent"; then
		echo "failed to canonicalize deploy state parent" >&2
		exit 64
	fi
	state_parent=$captured
	reject_controls "$state_parent" "deploy state parent must not contain control characters"
	test -n "$state_parent"
	test -d "$state_parent"
	test ! -L "$state_parent"
	effective_uid=$(without_lock_fd id -u)
	effective_gid=$(without_lock_fd id -g)
	parent_uid=$(without_lock_fd stat -c %u "$state_parent")
	parent_gid=$(without_lock_fd stat -c %g "$state_parent")
	parent_mode=$(without_lock_fd stat -c %a "$state_parent")
	if [ "$parent_uid" != "$effective_uid" ] || [ "$parent_gid" != "$effective_gid" ]; then
		echo "deploy state parent must be owned by the effective uid/gid: $state_parent" >&2
		exit 64
	fi
	case "$parent_mode" in
	[0-7][0-7][0-7]|[0-7][0-7][0-7][0-7]) ;;
	*)
		echo "deploy state parent mode is unsafe: $state_parent" >&2
		exit 64
		;;
	esac
	if [ "$((0$parent_mode & 022))" -ne 0 ]; then
		echo "deploy state parent must not be group or other writable: $state_parent" >&2
		exit 64
	fi
	state_dir=$state_parent/$state_leaf
	if [ -L "$state_dir" ]; then
		echo "deploy state directory must not be a symlink: $state_dir" >&2
		exit 64
	fi
	if [ -e "$state_dir" ]; then
		if [ ! -d "$state_dir" ]; then
			echo "deploy state path exists and is not a directory: $state_dir" >&2
			exit 64
		fi
	else
		without_lock_fd mkdir -m 0700 -- "$state_dir"
	fi
	validate_deploy_state_dir
}
validate_deploy_state_dir() {
	if [ -L "$state_dir" ]; then
		echo "deploy state directory must not be a symlink: $state_dir" >&2
		exit 64
	fi
	if [ ! -d "$state_dir" ]; then
		echo "deploy state directory is missing or not a directory: $state_dir" >&2
		exit 64
	fi
	if [ "$(without_lock_fd dirname "$state_dir")" != "$state_parent" ] || [ "$(without_lock_fd basename "$state_dir")" != "$state_leaf" ]; then
		echo "deploy state directory is not a canonical direct child of its parent: $state_dir" >&2
		exit 64
	fi
	effective_uid=$(without_lock_fd id -u)
	effective_gid=$(without_lock_fd id -g)
	dir_uid=$(without_lock_fd stat -c %u "$state_dir")
	dir_gid=$(without_lock_fd stat -c %g "$state_dir")
	dir_mode=$(without_lock_fd stat -c %a "$state_dir")
	if [ "$dir_uid" != "$effective_uid" ] || [ "$dir_gid" != "$effective_gid" ]; then
		echo "deploy state directory must be owned by the effective uid/gid: $state_dir" >&2
		exit 64
	fi
	if [ "$dir_mode" != 700 ]; then
		echo "deploy state directory must be mode 0700: $state_dir" >&2
		exit 64
	fi
}
reject_receipt_path() {
	echo "deploy receipt path must be a regular non-symlink file in the validated state directory" >&2
	exit 64
}
# Remove only the exclusive temp created by this invocation. Never follow or
# remove a substituted symlink or non-regular path.
cleanup_owned_receipt_tmp() {
	if [ -z "${owned_receipt_tmp:-}" ]; then
		return 0
	fi
	if [ -L "$owned_receipt_tmp" ]; then
		owned_receipt_tmp=
		return 0
	fi
	if [ -f "$owned_receipt_tmp" ]; then
		without_lock_fd rm -f -- "$owned_receipt_tmp"
	fi
	owned_receipt_tmp=
}
require_receipt_identity_fields() {
	require_release_id "$release_id" "candidate release id is unsafe"
	require_manifest_digest "$candidate_digest" "candidate manifest digest is malformed"
	case "$previous_state" in
	absent)
		if [ -n "$previous_id" ] || [ -n "$previous_digest" ]; then
			echo "absent previous state must not record an id or digest" >&2
			return 1
		fi
		;;
	validated)
		require_release_id "$previous_id" "previous release id is unsafe"
		require_manifest_digest "$previous_digest" "previous manifest digest is malformed"
		;;
	*)
		echo "unsupported deployment-attempt previous_state" >&2
		return 1
		;;
	esac
	return 0
}
# Publish one regular mode-0600 receipt through exclusive temp, owner/mode
# checks, file sync, mv -Tf, and directory sync. Callers decide whether a
# failure exits 64 or remains in the rollback machine. Never treat a failed
# publish as authorization to mutate or restart.
publish_receipt_body() {
	receipt_body=$1
	receipt=$state_dir/deployment-attempt
	if [ -L "$receipt" ]; then
		echo "deploy receipt path must be a regular non-symlink file in the validated state directory" >&2
		return 1
	fi
	if [ -e "$receipt" ] && [ ! -f "$receipt" ]; then
		echo "deploy receipt path must be a regular non-symlink file in the validated state directory" >&2
		return 1
	fi
	if ! capture /usr/bin/mktemp -- "$state_dir/deployment-attempt.XXXXXX"; then
		echo "failed to create private deployment-attempt temp" >&2
		return 1
	fi
	receipt_tmp=$captured
	if [ "$(without_lock_fd dirname "$receipt_tmp")" != "$state_dir" ]; then
		echo "private receipt temp is not in the validated state directory" >&2
		return 1
	fi
	owned_receipt_tmp=$receipt_tmp
	tmp_leaf=$(without_lock_fd basename "$receipt_tmp")
	case "$tmp_leaf" in
	deployment-attempt.??????) ;;
	*)
		echo "private receipt temp name is not exclusive-mktemp-shaped" >&2
		cleanup_owned_receipt_tmp
		return 1
		;;
	esac
	if [ -L "$receipt_tmp" ] || [ ! -f "$receipt_tmp" ]; then
		cleanup_owned_receipt_tmp
		echo "deploy receipt path must be a regular non-symlink file in the validated state directory" >&2
		return 1
	fi
	if ! without_lock_fd sh -c 'set -eu; umask 077; printf "%s\n" "$1" > "$2"; chmod 0600 -- "$2"' x "$receipt_body" "$receipt_tmp"; then
		echo "failed to write deployment-attempt receipt" >&2
		cleanup_owned_receipt_tmp
		return 1
	fi
	if [ -L "$receipt_tmp" ] || [ ! -f "$receipt_tmp" ]; then
		cleanup_owned_receipt_tmp
		echo "deploy receipt path must be a regular non-symlink file in the validated state directory" >&2
		return 1
	fi
	effective_uid=$(without_lock_fd id -u)
	effective_gid=$(without_lock_fd id -g)
	tmp_uid=$(without_lock_fd stat -c %u "$receipt_tmp")
	tmp_gid=$(without_lock_fd stat -c %g "$receipt_tmp")
	tmp_mode=$(without_lock_fd stat -c %a "$receipt_tmp")
	if [ "$tmp_uid" != "$effective_uid" ] || [ "$tmp_gid" != "$effective_gid" ] || [ "$tmp_mode" != 600 ]; then
		echo "deployment-attempt receipt temp must be mode 0600 and owned by the effective uid/gid" >&2
		cleanup_owned_receipt_tmp
		return 1
	fi
	if ! without_lock_fd sync -- "$receipt_tmp"; then
		echo "failed to sync deployment-attempt receipt" >&2
		cleanup_owned_receipt_tmp
		return 1
	fi
	if [ -L "$receipt" ]; then
		cleanup_owned_receipt_tmp
		echo "deploy receipt path must be a regular non-symlink file in the validated state directory" >&2
		return 1
	fi
	if [ -e "$receipt" ] && [ ! -f "$receipt" ]; then
		cleanup_owned_receipt_tmp
		echo "deploy receipt path must be a regular non-symlink file in the validated state directory" >&2
		return 1
	fi
	if ! without_lock_fd mv -Tf -- "$receipt_tmp" "$receipt"; then
		echo "failed to publish deployment-attempt receipt" >&2
		cleanup_owned_receipt_tmp
		return 1
	fi
	owned_receipt_tmp=
	if [ -L "$receipt" ] || [ ! -f "$receipt" ]; then
		echo "deploy receipt path must be a regular non-symlink file in the validated state directory" >&2
		return 1
	fi
	receipt_uid=$(without_lock_fd stat -c %u "$receipt")
	receipt_gid=$(without_lock_fd stat -c %g "$receipt")
	receipt_mode=$(without_lock_fd stat -c %a "$receipt")
	if [ "$receipt_uid" != "$effective_uid" ] || [ "$receipt_gid" != "$effective_gid" ] || [ "$receipt_mode" != 600 ]; then
		echo "deployment-attempt receipt must be mode 0600 and owned by the effective uid/gid" >&2
		return 1
	fi
	if ! without_lock_fd sync -- "$state_dir"; then
		echo "failed to sync deploy state directory" >&2
		return 1
	fi
	return 0
}
write_deployment_attempt_receipt() {
	prepare_deploy_state_dir
	receipt=$state_dir/deployment-attempt
	if [ -L "$receipt" ]; then
		reject_receipt_path
	fi
	if [ -e "$receipt" ] && [ ! -f "$receipt" ]; then
		reject_receipt_path
	fi
	if ! require_receipt_identity_fields; then
		exit 64
	fi
	receipt_body=$(printf '%s\n' \
		"kind=deployment-attempt" \
		"status=recorded" \
		"candidate_id=$release_id" \
		"candidate_digest=$candidate_digest" \
		"previous_state=$previous_state" \
		"previous_id=$previous_id" \
		"previous_digest=$previous_digest")
	if ! publish_receipt_body "$receipt_body"; then
		exit 64
	fi
}
# COMMONS_DB must be an absolute canonical-parent path. A dangling symlink is
# never treated as first-deploy absence. Child checks close the lock fd.
prepare_commons_db() {
	reject_controls "$COMMONS_DB" "COMMONS_DB must not contain control characters"
	case "$COMMONS_DB" in /*) ;; *)
		echo "COMMONS_DB must be an absolute path" >&2
		exit 64
		;;
	esac
	case "$COMMONS_DB" in
	*/)
		echo "COMMONS_DB must not have a trailing slash" >&2
		exit 64
		;;
	esac
	case "$COMMONS_DB" in
	*/.|*/..|*/./*|*/../*)
		echo "COMMONS_DB must not contain . or .. components" >&2
		exit 64
		;;
	esac
	case "$COMMONS_DB" in
	*[!A-Za-z0-9_./:-]*)
		echo "COMMONS_DB contains unsafe characters" >&2
		exit 64
		;;
	esac
	db_leaf=$(without_lock_fd basename "$COMMONS_DB")
	require_release_id "$db_leaf" "COMMONS_DB leaf must be a safe basename"
	db_parent=$(without_lock_fd dirname "$COMMONS_DB")
	if [ -L "$db_parent" ]; then
		echo "database parent must not be a symlink: $db_parent" >&2
		exit 64
	fi
	if [ ! -d "$db_parent" ]; then
		echo "database parent is missing or not a directory: $db_parent" >&2
		exit 64
	fi
	if ! capture readlink -f "$db_parent"; then
		echo "failed to canonicalize database parent" >&2
		exit 64
	fi
	db_parent=$captured
	reject_controls "$db_parent" "database parent must not contain control characters"
	test -n "$db_parent"
	test -d "$db_parent"
	test ! -L "$db_parent"
	effective_uid=$(without_lock_fd id -u)
	effective_gid=$(without_lock_fd id -g)
	parent_uid=$(without_lock_fd stat -c %u "$db_parent")
	parent_gid=$(without_lock_fd stat -c %g "$db_parent")
	parent_mode=$(without_lock_fd stat -c %a "$db_parent")
	if [ "$parent_uid" != "$effective_uid" ] || [ "$parent_gid" != "$effective_gid" ]; then
		echo "database parent must be owned by the effective uid/gid: $db_parent" >&2
		exit 64
	fi
	case "$parent_mode" in
	[0-7][0-7][0-7]|[0-7][0-7][0-7][0-7]) ;;
	*)
		echo "database parent mode is unsafe: $db_parent" >&2
		exit 64
		;;
	esac
	if [ "$((0$parent_mode & 022))" -ne 0 ]; then
		echo "database parent must not be group or other writable: $db_parent" >&2
		exit 64
	fi
	COMMONS_DB=$db_parent/$db_leaf
	if [ "$(without_lock_fd dirname "$COMMONS_DB")" != "$db_parent" ] || [ "$(without_lock_fd basename "$COMMONS_DB")" != "$db_leaf" ]; then
		echo "COMMONS_DB is not a canonical direct child of its parent: $COMMONS_DB" >&2
		exit 64
	fi
	if [ -L "$COMMONS_DB" ]; then
		echo "COMMONS_DB must not be a symlink; a dangling symlink is not absent: $COMMONS_DB" >&2
		exit 64
	fi
	had_db=false
	if [ -e "$COMMONS_DB" ]; then
		if [ ! -f "$COMMONS_DB" ]; then
			echo "COMMONS_DB exists and is not a regular file: $COMMONS_DB" >&2
			exit 64
		fi
		effective_uid=$(without_lock_fd id -u)
		effective_gid=$(without_lock_fd id -g)
		db_uid=$(without_lock_fd stat -c %u "$COMMONS_DB")
		db_gid=$(without_lock_fd stat -c %g "$COMMONS_DB")
		db_mode=$(without_lock_fd stat -c %a "$COMMONS_DB")
		if [ "$db_uid" != "$effective_uid" ] || [ "$db_gid" != "$effective_gid" ] || [ "$db_mode" != 600 ]; then
			echo "COMMONS_DB must be mode 0600 and owned by the effective uid/gid: $COMMONS_DB" >&2
			exit 64
		fi
		had_db=true
	fi
}
remove_validated_sqlite_sidecar() {
	sidecar=$1
	label=$2
	if [ -L "$sidecar" ]; then
		echo "refusing $label symlink: $sidecar" >&2
		return 1
	fi
	if [ ! -e "$sidecar" ]; then
		return 0
	fi
	if [ -d "$sidecar" ] || [ ! -f "$sidecar" ]; then
		echo "refusing non-regular $label: $sidecar" >&2
		return 1
	fi
	if ! without_lock_fd rm -f -- "$sidecar"; then
		echo "failed to remove $label" >&2
		return 1
	fi
	if [ -e "$sidecar" ] || [ -L "$sidecar" ]; then
		echo "failed to remove $label" >&2
		return 1
	fi
	return 0
}
cleanup_first_deploy_db() {
	if [ -L "$COMMONS_DB" ]; then
		echo "COMMONS_DB must not be a symlink; a dangling symlink is not absent: $COMMONS_DB" >&2
		return 1
	fi
	if [ -e "$COMMONS_DB" ]; then
		if [ ! -f "$COMMONS_DB" ]; then
			echo "COMMONS_DB exists and is not a regular file: $COMMONS_DB" >&2
			return 1
		fi
		if ! without_lock_fd rm -f -- "$COMMONS_DB"; then
			echo "failed to remove first-deploy database" >&2
			return 1
		fi
		if [ -e "$COMMONS_DB" ] || [ -L "$COMMONS_DB" ]; then
			echo "failed to remove first-deploy database" >&2
			return 1
		fi
	fi
	remove_validated_sqlite_sidecar "$COMMONS_DB-wal" "WAL" || return 1
	remove_validated_sqlite_sidecar "$COMMONS_DB-shm" "SHM" || return 1
	return 0
}
current="$COMMONS_RELEASE_ROOT/current"
previous=
previous_id=
previous_digest=
previous_state=absent
next="$COMMONS_RELEASE_ROOT/.current.next"
owned_next=
owned_receipt_tmp=
cleanup_owned_next() {
	# Remove only the temporary pointer created by this invocation. Never
	# follow it or rewrite current. The directory flock is released by
	# process exit closing fd 9.
	if [ -n "${owned_next:-}" ] && [ -L "$next" ]; then
		owned_target=$(without_lock_fd readlink -- "$next")
		if [ "$owned_target" = "$owned_next" ]; then
			without_lock_fd rm -f -- "$next"
		fi
	fi
}
cleanup_owned_paths() {
	cleanup_owned_receipt_tmp
	cleanup_owned_next
}
trap cleanup_owned_paths 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
reject_suspicious_next() {
	echo "refusing to follow or overwrite suspicious $next" >&2
	exit 66
}
if [ -L "$next" ]; then
	next_target=$(without_lock_fd readlink -- "$next")
	case "$next_target" in
	.|..|*/*|*[!A-Za-z0-9._-]*|'') reject_suspicious_next ;;
	esac
elif [ -e "$next" ]; then
	reject_suspicious_next
fi
COMMONS_RELEASE_DIR=$staged \
COMMONS_CODEX_BIN=$staged/bin/codex \
COMMONS_WEB_DIR=$staged/web \
COMMONS_RELEASE_IDENTITY_FILE=$staged/VERSION \
without_lock_fd /bin/sh "$staged/ops/verify-release.sh"
release_id=$(without_lock_fd sed -n '1p' "$staged/VERSION")
test "$release_id" = "$(without_lock_fd basename "$staged")"
require_release_id "$release_id" "candidate release id is unsafe"
candidate_digest=$(manifest_digest "$staged") || exit 64
require_manifest_digest "$candidate_digest" "candidate manifest digest is malformed"
# Inspect current exactly once after candidate verification and while the
# directory flock is held. First deployment with current absent is allowed.
# If current exists, a missing or unsafe target is not treated as absent.
if [ -e "$current" ] || [ -L "$current" ]; then
	if [ ! -L "$current" ]; then
		echo "current release pointer exists but is not a symlink: $current" >&2
		exit 64
	fi
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
	previous_dir=$COMMONS_RELEASE_ROOT/$current_target
	if [ ! -d "$previous_dir" ] || [ -L "$previous_dir" ]; then
		echo "previous release is missing, not a directory, or symlink-shaped" >&2
		exit 64
	fi
	if ! capture readlink -f "$previous_dir"; then
		echo "failed to canonicalize previous release directory" >&2
		exit 64
	fi
	previous=$captured
	reject_controls "$previous" "previous release directory must not contain control characters"
	test -n "$previous"
	test -d "$previous"
	test ! -L "$previous"
	if [ "$(without_lock_fd dirname "$previous")" != "$COMMONS_RELEASE_ROOT" ] || [ "$(without_lock_fd basename "$previous")" != "$current_target" ]; then
		echo "previous release is not a canonical direct child of COMMONS_RELEASE_ROOT" >&2
		exit 64
	fi
	if [ ! -f "$previous/ops/verify-release.sh" ] || [ -L "$previous/ops/verify-release.sh" ]; then
		echo "previous release verifier is missing or symlink-shaped" >&2
		exit 64
	fi
	COMMONS_RELEASE_DIR=$previous \
	COMMONS_CODEX_BIN=$previous/bin/codex \
	COMMONS_WEB_DIR=$previous/web \
	COMMONS_RELEASE_IDENTITY_FILE=$previous/VERSION \
	without_lock_fd /bin/sh "$previous/ops/verify-release.sh"
	previous_id=$(without_lock_fd sed -n '1p' "$previous/VERSION")
	if [ "$previous_id" != "$(without_lock_fd basename "$previous")" ] || [ "$previous_id" != "$current_target" ]; then
		echo "previous VERSION does not match the captured release basename" >&2
		exit 64
	fi
	require_release_id "$previous_id" "previous release id is unsafe"
	previous_digest=$(manifest_digest "$previous") || exit 64
	require_manifest_digest "$previous_digest" "previous manifest digest is malformed"
	previous_state=validated
fi
write_deployment_attempt_receipt
prebackup=
had_db=false
prepare_commons_db
if [ "$had_db" = true ]; then
	: "${COMMONS_BACKUP_DIR:?COMMONS_BACKUP_DIR is required}"
	reject_controls "$COMMONS_BACKUP_DIR" "COMMONS_BACKUP_DIR must not contain control characters"
	case "$COMMONS_BACKUP_DIR" in /*) ;; *)
		echo "COMMONS_BACKUP_DIR must be an absolute path" >&2
		exit 64
		;;
	esac
	if [ -n "${COMMONS_PUBLIC_ORIGIN:-}" ] && [ "${COMMONS_ALLOW_FIRST_CODEX_BIND_LAN:-false}" != true ]; then
		test "$(without_lock_fd sqlite3 -- "$COMMONS_DB" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='human_account_bindings'")" -eq 1
		test "$(without_lock_fd sqlite3 -- "$COMMONS_DB" 'SELECT count(*) FROM human_account_bindings')" -eq 1
	fi
	if ! capture /bin/sh "$staged/ops/backup.sh"; then
		echo "pre-upgrade backup failed or did not report an exact backup path" >&2
		exit 64
	fi
	prebackup=$captured
	reject_controls "$prebackup" "captured backup path must not contain control characters"
	case "$prebackup" in /*) ;; *)
		echo "captured backup path must be absolute" >&2
		exit 64
		;;
	esac
	case "$prebackup" in
	"$COMMONS_BACKUP_DIR"/*) ;;
	*)
		echo "captured backup path is outside COMMONS_BACKUP_DIR" >&2
		exit 64
		;;
	esac
	if [ -L "$prebackup" ] || [ ! -f "$prebackup" ]; then
		echo "captured backup path is missing, not a regular file, or symlink-shaped: $prebackup" >&2
		exit 64
	fi
fi
if [ "$had_db" = false ] && [ -n "${COMMONS_PUBLIC_ORIGIN:-}" ]; then
	echo "bootstrap a durable Codex account binding on direct loopback before enabling COMMONS_PUBLIC_ORIGIN" >&2
	exit 78
fi
if [ -L "$next" ]; then
	without_lock_fd rm -f -- "$next"
fi
if [ -e "$next" ] || [ -L "$next" ]; then
	reject_suspicious_next
fi
owned_next=$(without_lock_fd basename "$staged")
without_lock_fd ln -s "$owned_next" "$next"
without_lock_fd mv -Tf "$next" "$current"
owned_next=
require_allowlisted() {
	value=$1
	label=$2
	shift 2
	for allowed in "$@"; do
		if [ "$value" = "$allowed" ]; then
			return 0
		fi
	done
	echo "unsupported $label" >&2
	return 1
}
recorded_attempt_receipt_body() {
	printf '%s\n' \
		"kind=deployment-attempt" \
		"status=recorded" \
		"candidate_id=$release_id" \
		"candidate_digest=$candidate_digest" \
		"previous_state=$previous_state" \
		"previous_id=$previous_id" \
		"previous_digest=$previous_digest"
}
# Query ActiveState exactly and losslessly. Do not infer active or stopped
# from a generic command exit. Only inactive or failed proves stopped.
# active/reloading/activating/deactivating are not stopped. Query failure,
# empty, multiline, control, or unknown output is unknown and fails closed.
query_unit_active_state() {
	unit_active_state=
	unit_active_state_class=unknown
	if ! capture "$systemctl_cmd" --user show --property=ActiveState --value codex-commons.service; then
		return 1
	fi
	case "$captured" in
	*[[:cntrl:]]*|'')
		return 1
		;;
	esac
	case "$captured" in
	inactive|failed)
		unit_active_state=$captured
		unit_active_state_class=stopped
		return 0
		;;
	active|reloading|activating|deactivating)
		unit_active_state=$captured
		unit_active_state_class=active
		return 0
		;;
	esac
	return 1
}
prove_service_stopped() {
	if query_unit_active_state && [ "$unit_active_state_class" = stopped ]; then
		return 0
	fi
	echo "codex-commons.service is not proven stopped" >&2
	return 1
}
recorded_service_state() {
	case "${unit_active_state_class:-unknown}" in
	stopped)
		printf '%s\n' stopped
		;;
	active)
		printf '%s\n' active
		;;
	*)
		printf '%s\n' unknown
		;;
	esac
}
# One stop attempt and the same exact ActiveState proof. If that final stop
# or proof fails, record previous_stop_failed with active/unknown and never
# retry. Do not leave a known active previous service.
stop_and_prove_previous() {
	context=$1
	if ! without_lock_fd "$systemctl_cmd" --user stop codex-commons.service; then
		echo "failed to stop codex-commons.service" >&2
		query_unit_active_state || true
		finish_rollback previous_stop_failed "$(recorded_service_state)" "$rollback_database_state" \
			"$context; final stop failed"
	fi
	if ! prove_service_stopped; then
		finish_rollback previous_stop_failed "$(recorded_service_state)" "$rollback_database_state" \
			"$context; service remained unproven after stop"
	fi
}
publish_rollback_outcome() {
	deploy_outcome=$1
	service_state=$2
	database_state=$3
	if ! require_receipt_identity_fields; then
		echo "rollback receipt identity fields are invalid" >&2
		return 1
	fi
	if ! require_allowlisted "$deploy_outcome" "deploy_outcome" \
		candidate_failed stop_failed receipt_mismatch previous_reverify_failed \
		backup_verify_failed restore_failed first_deploy_cleanup_failed \
		pointer_switch_failed previous_restart_failed previous_readiness_failed \
		previous_stop_failed previous_ready no_previous; then
		return 1
	fi
	if ! require_allowlisted "$service_state" "service_state" stopped active unknown ready; then
		return 1
	fi
	if ! require_allowlisted "$database_state" "database_state" unchanged restored absent uncertain; then
		return 1
	fi
	if [ -z "${state_dir:-}" ] || [ -z "${state_parent:-}" ] || [ -z "${state_leaf:-}" ]; then
		echo "deploy state directory is not prepared for a rollback receipt" >&2
		return 1
	fi
	if ! validate_deploy_state_dir; then
		return 1
	fi
	receipt_body=$(printf '%s\n' \
		"kind=deployment-attempt" \
		"status=failed" \
		"candidate_id=$release_id" \
		"candidate_digest=$candidate_digest" \
		"previous_state=$previous_state" \
		"previous_id=$previous_id" \
		"previous_digest=$previous_digest" \
		"deploy_outcome=$deploy_outcome" \
		"service_state=$service_state" \
		"database_state=$database_state")
	if ! publish_receipt_body "$receipt_body"; then
		echo "failed to publish rollback outcome receipt" >&2
		return 1
	fi
	return 0
}
finish_rollback() {
	deploy_outcome=$1
	service_state=$2
	database_state=$3
	message=$4
	echo "$message" >&2
	if ! publish_rollback_outcome "$deploy_outcome" "$service_state" "$database_state"; then
		echo "failed to publish rollback outcome receipt" >&2
	fi
	exit 1
}
# Publishing a rollback outcome is safe only when the state directory and the
# receipt path are a regular non-symlink file, owned by the effective uid/gid,
# mode 0600. Unsafe shape must be left untouched.
receipt_is_publish_safe() {
	if [ -z "${state_dir:-}" ] || [ -z "${state_parent:-}" ] || [ -z "${state_leaf:-}" ]; then
		echo "deploy state directory is not prepared for receipt revalidation" >&2
		return 1
	fi
	if ! validate_deploy_state_dir; then
		return 1
	fi
	receipt=$state_dir/deployment-attempt
	if [ -L "$receipt" ] || [ ! -f "$receipt" ]; then
		echo "deployment-attempt receipt is missing or not a regular file" >&2
		return 1
	fi
	effective_uid=$(without_lock_fd id -u)
	effective_gid=$(without_lock_fd id -g)
	receipt_uid=$(without_lock_fd stat -c %u "$receipt")
	receipt_gid=$(without_lock_fd stat -c %g "$receipt")
	receipt_mode=$(without_lock_fd stat -c %a "$receipt")
	if [ "$receipt_uid" != "$effective_uid" ] || [ "$receipt_gid" != "$effective_gid" ] || [ "$receipt_mode" != 600 ]; then
		echo "deployment-attempt receipt must be mode 0600 and owned by the effective uid/gid" >&2
		return 1
	fi
	return 0
}
receipt_content_matches_recorded() {
	expected=$(recorded_attempt_receipt_body)
	actual=$(without_lock_fd cat -- "$receipt") || {
		echo "failed to read deployment-attempt receipt" >&2
		return 1
	}
	if [ "$actual" != "$expected" ]; then
		echo "deployment-attempt receipt does not match the captured attempt identity" >&2
		return 1
	fi
	if [ "$(without_lock_fd wc -l < "$receipt")" -ne 7 ]; then
		echo "deployment-attempt receipt is not the exact recorded 7-line contract" >&2
		return 1
	fi
	return 0
}
revalidate_captured_previous() {
	if [ "$previous_state" = absent ]; then
		if [ -n "${previous:-}" ] || [ -n "$previous_id" ] || [ -n "$previous_digest" ]; then
			echo "absent previous state must not have a captured previous identity" >&2
			return 1
		fi
		return 0
	fi
	if [ "$previous_state" != validated ]; then
		echo "unsupported previous_state during rollback" >&2
		return 1
	fi
	if [ -z "${previous:-}" ] || [ -z "$previous_id" ] || [ -z "$previous_digest" ]; then
		echo "captured previous identity is missing" >&2
		return 1
	fi
	if [ -L "$previous" ] || [ ! -d "$previous" ]; then
		echo "captured previous release is missing, not a directory, or symlink-shaped" >&2
		return 1
	fi
	if [ "$(without_lock_fd dirname "$previous")" != "$COMMONS_RELEASE_ROOT" ] || [ "$(without_lock_fd basename "$previous")" != "$previous_id" ]; then
		echo "captured previous release is not a canonical direct child of COMMONS_RELEASE_ROOT" >&2
		return 1
	fi
	if [ ! -f "$previous/ops/verify-release.sh" ] || [ -L "$previous/ops/verify-release.sh" ]; then
		echo "previous release verifier is missing or symlink-shaped" >&2
		return 1
	fi
	COMMONS_RELEASE_DIR=$previous \
	COMMONS_CODEX_BIN=$previous/bin/codex \
	COMMONS_WEB_DIR=$previous/web \
	COMMONS_RELEASE_IDENTITY_FILE=$previous/VERSION \
	without_lock_fd /bin/sh "$previous/ops/verify-release.sh" || {
		echo "captured previous release failed re-verification" >&2
		return 1
	}
	reverify_id=$(without_lock_fd sed -n '1p' "$previous/VERSION")
	if [ "$reverify_id" != "$previous_id" ] || [ "$reverify_id" != "$(without_lock_fd basename "$previous")" ]; then
		echo "previous VERSION does not match the captured release basename" >&2
		return 1
	fi
	reverify_digest=$(manifest_digest "$previous") || return 1
	if [ "$reverify_digest" != "$previous_digest" ]; then
		echo "previous manifest digest does not match the captured digest" >&2
		return 1
	fi
	return 0
}
# First-deploy/no-previous removal reads current only to verify it still names
# the exact candidate release_id. That read is never used as target selection.
# A swapped pointer is left in place.
safe_remove_current() {
	if [ -L "$current" ]; then
		if ! capture readlink -- "$current"; then
			echo "failed to read current before first-deploy removal" >&2
			return 1
		fi
		if [ "$captured" != "$release_id" ]; then
			echo "current pointer is not the candidate release; refusing to unlink a substituted pointer" >&2
			return 1
		fi
		if ! without_lock_fd rm -f -- "$current"; then
			echo "failed to remove current after first-deploy failure" >&2
			return 1
		fi
	elif [ -e "$current" ]; then
		echo "refusing to remove non-symlink current pointer" >&2
		return 1
	fi
	if [ -e "$current" ] || [ -L "$current" ]; then
		echo "current pointer still present after first-deploy rollback" >&2
		return 1
	fi
	return 0
}
# Switch current only to the captured previous id. The post-switch readlink
# verifies that exact id; it does not choose a new target by re-reading current.
atomic_switch_current_to_previous() {
	require_release_id "$previous_id" "rollback target id is unsafe"
	if [ -L "$next" ]; then
		if ! without_lock_fd rm -f -- "$next"; then
			echo "failed to remove leftover rollback pointer temp" >&2
			return 1
		fi
	fi
	if [ -e "$next" ] || [ -L "$next" ]; then
		echo "refusing to follow or overwrite suspicious $next" >&2
		return 1
	fi
	owned_next=$previous_id
	if ! without_lock_fd ln -s "$owned_next" "$next"; then
		echo "failed to create rollback pointer temp" >&2
		owned_next=
		return 1
	fi
	if ! capture readlink -- "$next"; then
		echo "failed to read rollback pointer temp" >&2
		cleanup_owned_next
		return 1
	fi
	if [ "$captured" != "$previous_id" ]; then
		echo "rollback pointer temp does not match the captured previous id" >&2
		cleanup_owned_next
		return 1
	fi
	if ! without_lock_fd mv -Tf -- "$next" "$current"; then
		echo "failed to switch current to the captured previous release" >&2
		cleanup_owned_next
		return 1
	fi
	owned_next=
	if ! capture readlink -- "$current"; then
		echo "failed to read current after rollback pointer switch" >&2
		return 1
	fi
	if [ "$captured" != "$previous_id" ]; then
		echo "current readback does not match the captured previous id" >&2
		return 1
	fi
	return 0
}
# Candidate restart/readiness failure always exits 1, even if previous becomes
# ready. Stop is proven with the exact ActiveState query before any restore,
# cleanup, or pointer mutation.
run_fail_closed_rollback() {
	if ! without_lock_fd "$systemctl_cmd" --user stop codex-commons.service; then
		echo "failed to stop codex-commons.service" >&2
		query_unit_active_state || true
		finish_rollback stop_failed "$(recorded_service_state)" unchanged \
			"candidate failed; service stop failed"
	fi
	if ! prove_service_stopped; then
		finish_rollback stop_failed "$(recorded_service_state)" unchanged \
			"candidate failed; service remained unproven after stop"
	fi
	if ! receipt_is_publish_safe; then
		echo "candidate failed; receipt is unsafe to replace; service left stopped" >&2
		exit 1
	fi
	if ! receipt_content_matches_recorded; then
		echo "candidate failed; receipt content mismatch; service left stopped" >&2
		if ! publish_rollback_outcome receipt_mismatch stopped unchanged; then
			echo "failed to publish receipt_mismatch rollback receipt" >&2
		fi
		exit 1
	fi
	if ! revalidate_captured_previous; then
		finish_rollback previous_reverify_failed stopped unchanged \
			"candidate failed; captured previous revalidation failed"
	fi
	if ! publish_rollback_outcome candidate_failed stopped unchanged; then
		echo "required rollback receipt publish failed; refusing later mutation or restart" >&2
		exit 1
	fi
	rollback_database_state=unchanged
	if [ -n "$prebackup" ]; then
		if ! without_lock_fd /bin/sh "$staged/ops/verify-restore.sh" "$prebackup" >/dev/null; then
			finish_rollback backup_verify_failed stopped unchanged \
				"pre-upgrade backup failed restore verification"
		fi
		if ! without_lock_fd /bin/sh "$staged/ops/restore-database.sh" "$prebackup" "$COMMONS_DB"; then
			finish_rollback restore_failed stopped uncertain \
				"atomic database restore failed"
		fi
		rollback_database_state=restored
	elif [ "$had_db" = false ]; then
		if ! cleanup_first_deploy_db; then
			finish_rollback first_deploy_cleanup_failed stopped uncertain \
				"first-deploy database cleanup failed"
		fi
		rollback_database_state=absent
	fi
	if [ -n "$previous_id" ]; then
		if ! atomic_switch_current_to_previous; then
			finish_rollback pointer_switch_failed stopped "$rollback_database_state" \
				"failed to switch current to the captured previous release"
		fi
	else
		if ! safe_remove_current; then
			finish_rollback pointer_switch_failed stopped "$rollback_database_state" \
				"failed to remove current after first-deploy failure"
		fi
		finish_rollback no_previous stopped "$rollback_database_state" \
			"candidate failed; no previous release; service left stopped"
	fi
	if ! without_lock_fd "$systemctl_cmd" --user restart codex-commons.service; then
		echo "previous release failed to restart" >&2
		stop_and_prove_previous "candidate failed; previous process failed to restart"
		finish_rollback previous_restart_failed stopped "$rollback_database_state" \
			"candidate failed; previous process failed to restart"
	fi
	if ! COMMONS_RELEASE_DIR="$previous" COMMONS_SYSTEMCTL="$systemctl_cmd" without_lock_fd /bin/sh "$previous/ops/check-readiness.sh"; then
		echo "previous release failed readiness" >&2
		stop_and_prove_previous "candidate failed; previous readiness failed"
		finish_rollback previous_readiness_failed stopped "$rollback_database_state" \
			"candidate failed; previous readiness failed"
	fi
	if ! publish_rollback_outcome previous_ready ready "$rollback_database_state"; then
		echo "failed to publish previous_ready rollback receipt" >&2
		stop_and_prove_previous "candidate failed; previous_ready receipt publish failed"
		echo "candidate failed; previous_ready receipt publish failed; service left stopped" >&2
		exit 1
	fi
	echo "candidate failed; previous release restored and ready" >&2
	exit 1
}
if ! without_lock_fd "$systemctl_cmd" --user restart codex-commons.service || ! COMMONS_RELEASE_DIR="$staged" COMMONS_SYSTEMCTL="$systemctl_cmd" without_lock_fd /bin/sh "$staged/ops/check-readiness.sh"; then
	run_fail_closed_rollback
	exit 1
fi
