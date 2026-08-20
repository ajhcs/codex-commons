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
# flock(2) locks follow the open-file description. A child that inherited fd 9
# could `flock -u 9` and release this transaction while the parent is still
# active. Close the child's copy only; the parent keeps fd 9 until exit.
# Do not rely on backup.sh rebinding fd 9.
without_lock_fd() {
	"$@" 9<&-
}
current="$COMMONS_RELEASE_ROOT/current"; previous=; next="$COMMONS_RELEASE_ROOT/.current.next"
owned_next=
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
trap cleanup_owned_next 0
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
COMMONS_RELEASE_DIR=$staged COMMONS_CODEX_BIN=$staged/bin/codex COMMONS_WEB_DIR=$staged/web without_lock_fd /bin/sh "$staged/ops/verify-release.sh"
release_id=$(without_lock_fd sed -n '1p' "$staged/VERSION")
test "$release_id" = "$(without_lock_fd basename "$staged")"
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
if [ -L "$current" ]; then
	previous=$(without_lock_fd readlink -f "$current" 2>/dev/null || true)
	test -z "$previous" || test -d "$previous" || previous=
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
	if [ -n "$previous" ]; then
		without_lock_fd ln -sfn "$(without_lock_fd basename "$previous")" "$current"
		without_lock_fd "$systemctl_cmd" --user restart codex-commons.service || true
		COMMONS_RELEASE_DIR="$previous" COMMONS_SYSTEMCTL="$systemctl_cmd" without_lock_fd /bin/sh "$previous/ops/check-readiness.sh" || true
	else
		without_lock_fd rm -f -- "$current"
	fi
	exit 1
fi
