#!/bin/sh
set -eu
: "${1:?staged release directory required}"
: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT is required}"
: "${COMMONS_DB:?COMMONS_DB is required}"
systemctl_cmd=${COMMONS_SYSTEMCTL:-systemctl}
case "$COMMONS_RELEASE_ROOT" in
/*) ;;
*) exit 64 ;;
esac
test -d "$COMMONS_RELEASE_ROOT"
release_root=$(readlink -f -- "$COMMONS_RELEASE_ROOT")
test -n "$release_root"
case "$release_root" in
/*) ;;
*) exit 64 ;;
esac
test -d "$release_root"
test ! -L "$release_root"
COMMONS_RELEASE_ROOT=$release_root
export COMMONS_RELEASE_ROOT

# Open and lock the canonical release-root directory before inspecting or
# verifying the candidate. The descriptor stays open until this process exits,
# including while restart/readiness and rollback paths run. A symlink or
# lexical alias for the root therefore shares the same lock domain.
exec 9<"$release_root"
if ! flock -n 9; then
	echo "another release transaction already owns $release_root" >&2
	exit 75
fi

current="$release_root/current"; previous=
if [ -e "$current" ] || [ -L "$current" ]; then
	if [ ! -L "$current" ]; then
		echo "$current must be a symlink to a release directory" >&2
		exit 78
	fi
	previous=$(readlink -f -- "$current" 2>/dev/null || true)
	if [ -z "$previous" ] ||
		[ ! -d "$previous" ] ||
		[ -L "$previous" ] ||
		[ "$(dirname -- "$previous")" != "$release_root" ]; then
		echo "$current does not resolve to a release directory under $release_root" >&2
		exit 78
	fi
fi

staged=$1
case "$staged" in
/*) ;;
*) exit 64 ;;
esac
test -d "$staged"
test ! -L "$staged"
staged=$(readlink -f -- "$staged")
test -n "$staged"
test ! -L "$staged"
test "$(dirname -- "$staged")" = "$release_root"
COMMONS_RELEASE_DIR=$staged COMMONS_CODEX_BIN=$staged/bin/codex COMMONS_WEB_DIR=$staged/web /bin/sh "$staged/ops/verify-release.sh"
release_id=$(sed -n '1p' "$staged/VERSION")
test "$release_id" = "$(basename "$staged")"
prebackup=
had_db=false
if [ -f "$COMMONS_DB" ]; then
	had_db=true
	if [ -n "${COMMONS_PUBLIC_ORIGIN:-}" ] && [ "${COMMONS_ALLOW_FIRST_CODEX_BIND_LAN:-false}" != true ]; then
		test "$(sqlite3 "$COMMONS_DB" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='human_account_bindings'")" -eq 1
		test "$(sqlite3 "$COMMONS_DB" 'SELECT count(*) FROM human_account_bindings')" -eq 1
	fi
	/bin/sh "$staged/ops/backup.sh"
	prebackup=$(find "$COMMONS_BACKUP_DIR/daily" -maxdepth 1 -type f -name 'commons-*.sqlite3' -printf '%T@ %p\n' | sort -nr | awk 'NR==1 {print $2}')
	test -n "$prebackup"
fi
if [ ! -f "$COMMONS_DB" ] && [ -n "${COMMONS_PUBLIC_ORIGIN:-}" ]; then
	echo "bootstrap a durable Codex account binding on direct loopback before enabling COMMONS_PUBLIC_ORIGIN" >&2
	exit 78
fi
pointer_temp=
cleanup_pointer_temp() {
	if [ -n "${pointer_temp:-}" ] && {
		test -e "$pointer_temp" || test -L "$pointer_temp"
	}; then
		rm -f -- "$pointer_temp"
	fi
}
trap cleanup_pointer_temp 0 1 2 15

switch_current() {
	target=$1
	pointer_temp=$(mktemp "$release_root/.current.next.XXXXXX")
	# mktemp gives this transaction an exclusive, owned pathname. Replace the
	# regular placeholder with our symlink, then atomically move that exact path.
	rm -f -- "$pointer_temp"
	ln -s -- "$(basename "$target")" "$pointer_temp"
	mv -Tf -- "$pointer_temp" "$current"
	pointer_temp=
}

switch_current "$staged"
if ! "$systemctl_cmd" --user restart codex-commons.service || ! \
	COMMONS_RELEASE_DIR="$staged" \
	COMMONS_RELEASE_IDENTITY_FILE="$staged/VERSION" \
	COMMONS_CODEX_BIN="$staged/bin/codex" \
	COMMONS_WEB_DIR="$staged/web" \
	COMMONS_SYSTEMCTL="$systemctl_cmd" \
	/bin/sh "$staged/ops/check-readiness.sh"; then
	"$systemctl_cmd" --user stop codex-commons.service || true
	if [ -n "$prebackup" ]; then
		/bin/sh "$staged/ops/verify-restore.sh" "$prebackup" >/dev/null
		rm -f -- "$COMMONS_DB-wal" "$COMMONS_DB-shm"
		cp -- "$prebackup" "$COMMONS_DB.rollback"; mv -f "$COMMONS_DB.rollback" "$COMMONS_DB"
	elif [ "$had_db" = false ]; then
		rm -f -- "$COMMONS_DB" "$COMMONS_DB-wal" "$COMMONS_DB-shm"
	fi
	if [ -n "$previous" ]; then
		switch_current "$previous"
		"$systemctl_cmd" --user restart codex-commons.service || true
		COMMONS_RELEASE_DIR="$previous" \
		COMMONS_RELEASE_IDENTITY_FILE="$previous/VERSION" \
		COMMONS_CODEX_BIN="$previous/bin/codex" \
		COMMONS_WEB_DIR="$previous/web" \
		COMMONS_SYSTEMCTL="$systemctl_cmd" \
		/bin/sh "$previous/ops/check-readiness.sh" || true
	else
		rm -f -- "$current"
	fi
	exit 1
fi
