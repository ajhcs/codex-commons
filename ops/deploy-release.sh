#!/bin/sh
set -eu
: "${1:?staged release directory required}"
: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT is required}"
: "${COMMONS_DB:?COMMONS_DB is required}"
staged=$1
systemctl_cmd=${COMMONS_SYSTEMCTL:-systemctl}
case "$staged:$COMMONS_RELEASE_ROOT" in /*:/*) ;; *) exit 64;; esac
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
write_deployment_attempt_receipt() {
	prepare_deploy_state_dir
	receipt=$state_dir/deployment-attempt
	if [ -L "$receipt" ]; then
		reject_receipt_path
	fi
	if [ -e "$receipt" ] && [ ! -f "$receipt" ]; then
		reject_receipt_path
	fi
	require_release_id "$release_id" "candidate release id is unsafe"
	require_manifest_digest "$candidate_digest" "candidate manifest digest is malformed"
	case "$previous_state" in
	absent)
		if [ -n "$previous_id" ] || [ -n "$previous_digest" ]; then
			echo "absent previous state must not record an id or digest" >&2
			exit 64
		fi
		;;
	validated)
		require_release_id "$previous_id" "previous release id is unsafe"
		require_manifest_digest "$previous_digest" "previous manifest digest is malformed"
		;;
	*)
		echo "unsupported deployment-attempt previous_state" >&2
		exit 64
		;;
	esac
	receipt_body=$(printf '%s\n' \
		"kind=deployment-attempt" \
		"status=recorded" \
		"candidate_id=$release_id" \
		"candidate_digest=$candidate_digest" \
		"previous_state=$previous_state" \
		"previous_id=$previous_id" \
		"previous_digest=$previous_digest")
	if ! capture /usr/bin/mktemp -- "$state_dir/deployment-attempt.XXXXXX"; then
		echo "failed to create private deployment-attempt temp" >&2
		exit 64
	fi
	receipt_tmp=$captured
	if [ "$(without_lock_fd dirname "$receipt_tmp")" != "$state_dir" ]; then
		echo "private receipt temp is not in the validated state directory" >&2
		exit 64
	fi
	owned_receipt_tmp=$receipt_tmp
	tmp_leaf=$(without_lock_fd basename "$receipt_tmp")
	case "$tmp_leaf" in
	deployment-attempt.??????) ;;
	*)
		echo "private receipt temp name is not exclusive-mktemp-shaped" >&2
		cleanup_owned_receipt_tmp
		exit 64
		;;
	esac
	if [ -L "$receipt_tmp" ] || [ ! -f "$receipt_tmp" ]; then
		cleanup_owned_receipt_tmp
		reject_receipt_path
	fi
	if ! without_lock_fd sh -c 'set -eu; umask 077; printf "%s\n" "$1" > "$2"; chmod 0600 -- "$2"' x "$receipt_body" "$receipt_tmp"; then
		echo "failed to write deployment-attempt receipt" >&2
		cleanup_owned_receipt_tmp
		exit 64
	fi
	if [ -L "$receipt_tmp" ] || [ ! -f "$receipt_tmp" ]; then
		cleanup_owned_receipt_tmp
		reject_receipt_path
	fi
	effective_uid=$(without_lock_fd id -u)
	effective_gid=$(without_lock_fd id -g)
	tmp_uid=$(without_lock_fd stat -c %u "$receipt_tmp")
	tmp_gid=$(without_lock_fd stat -c %g "$receipt_tmp")
	tmp_mode=$(without_lock_fd stat -c %a "$receipt_tmp")
	if [ "$tmp_uid" != "$effective_uid" ] || [ "$tmp_gid" != "$effective_gid" ] || [ "$tmp_mode" != 600 ]; then
		echo "deployment-attempt receipt temp must be mode 0600 and owned by the effective uid/gid" >&2
		cleanup_owned_receipt_tmp
		exit 64
	fi
	if ! without_lock_fd sync -- "$receipt_tmp"; then
		echo "failed to sync deployment-attempt receipt" >&2
		cleanup_owned_receipt_tmp
		exit 64
	fi
	if [ -L "$receipt" ]; then
		cleanup_owned_receipt_tmp
		reject_receipt_path
	fi
	if [ -e "$receipt" ] && [ ! -f "$receipt" ]; then
		cleanup_owned_receipt_tmp
		reject_receipt_path
	fi
	if ! without_lock_fd mv -Tf -- "$receipt_tmp" "$receipt"; then
		echo "failed to publish deployment-attempt receipt" >&2
		cleanup_owned_receipt_tmp
		exit 64
	fi
	owned_receipt_tmp=
	if [ -L "$receipt" ] || [ ! -f "$receipt" ]; then
		reject_receipt_path
	fi
	receipt_uid=$(without_lock_fd stat -c %u "$receipt")
	receipt_gid=$(without_lock_fd stat -c %g "$receipt")
	receipt_mode=$(without_lock_fd stat -c %a "$receipt")
	if [ "$receipt_uid" != "$effective_uid" ] || [ "$receipt_gid" != "$effective_gid" ] || [ "$receipt_mode" != 600 ]; then
		echo "deployment-attempt receipt must be mode 0600 and owned by the effective uid/gid" >&2
		exit 64
	fi
	if ! without_lock_fd sync -- "$state_dir"; then
		echo "failed to sync deploy state directory" >&2
		exit 64
	fi
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
if [ -f "$COMMONS_DB" ]; then
	had_db=true
	if [ -n "${COMMONS_PUBLIC_ORIGIN:-}" ] && [ "${COMMONS_ALLOW_FIRST_CODEX_BIND_LAN:-false}" != true ]; then
		test "$(without_lock_fd sqlite3 "$COMMONS_DB" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='human_account_bindings'")" -eq 1
		test "$(without_lock_fd sqlite3 "$COMMONS_DB" 'SELECT count(*) FROM human_account_bindings')" -eq 1
	fi
	without_lock_fd /bin/sh "$staged/ops/backup.sh"
	prebackup=$(without_lock_fd find "$COMMONS_BACKUP_DIR/daily" -maxdepth 1 -type f -name 'commons-*.sqlite3' -printf '%T@ %p\n' | without_lock_fd sort -nr | without_lock_fd awk 'NR==1 {print $2}')
	test -n "$prebackup"
fi
if [ ! -f "$COMMONS_DB" ] && [ -n "${COMMONS_PUBLIC_ORIGIN:-}" ]; then
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
if ! without_lock_fd "$systemctl_cmd" --user restart codex-commons.service || ! COMMONS_RELEASE_DIR="$staged" COMMONS_SYSTEMCTL="$systemctl_cmd" without_lock_fd /bin/sh "$staged/ops/check-readiness.sh"; then
	without_lock_fd "$systemctl_cmd" --user stop codex-commons.service || true
	if [ -n "$prebackup" ]; then
		without_lock_fd /bin/sh "$staged/ops/verify-restore.sh" "$prebackup" >/dev/null
		without_lock_fd rm -f -- "$COMMONS_DB-wal" "$COMMONS_DB-shm"
		without_lock_fd cp -- "$prebackup" "$COMMONS_DB.rollback"; without_lock_fd mv -f "$COMMONS_DB.rollback" "$COMMONS_DB"
	elif [ "$had_db" = false ]; then
		without_lock_fd rm -f -- "$COMMONS_DB" "$COMMONS_DB-wal" "$COMMONS_DB-shm"
	fi
	# Rollback uses only the captured exact previous path and id. Do not
	# re-read current or resolve a new target.
	if [ -n "$previous_id" ]; then
		without_lock_fd ln -sfn -- "$previous_id" "$current"
		without_lock_fd "$systemctl_cmd" --user restart codex-commons.service || true
		COMMONS_RELEASE_DIR="$previous" COMMONS_SYSTEMCTL="$systemctl_cmd" without_lock_fd /bin/sh "$previous/ops/check-readiness.sh" || true
	else
		without_lock_fd rm -f -- "$current"
	fi
	exit 1
fi
