#!/bin/sh
set -eu
: "${1:?staged release directory required}"
: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT is required}"
: "${COMMONS_DB:?COMMONS_DB is required}"
staged=$1
systemctl_cmd=${COMMONS_SYSTEMCTL:-systemctl}
case "$staged:$COMMONS_RELEASE_ROOT" in /*:/*) ;; *) exit 64;; esac
test -d "$staged"; test ! -L "$staged"; test "$(dirname "$staged")" = "$COMMONS_RELEASE_ROOT"
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
current="$COMMONS_RELEASE_ROOT/current"; previous=; next="$COMMONS_RELEASE_ROOT/.current.next"
if [ -L "$current" ]; then
	previous=$(readlink -f "$current" 2>/dev/null || true)
	test -z "$previous" || test -d "$previous" || previous=
fi
rm -f -- "$next"; ln -s "$(basename "$staged")" "$next"; mv -Tf "$next" "$current"
if ! "$systemctl_cmd" --user restart codex-commons.service || ! COMMONS_RELEASE_DIR="$staged" COMMONS_SYSTEMCTL="$systemctl_cmd" /bin/sh "$staged/ops/check-readiness.sh"; then
	"$systemctl_cmd" --user stop codex-commons.service || true
	if [ -n "$prebackup" ]; then
		/bin/sh "$staged/ops/verify-restore.sh" "$prebackup" >/dev/null
		rm -f -- "$COMMONS_DB-wal" "$COMMONS_DB-shm"
		cp -- "$prebackup" "$COMMONS_DB.rollback"; mv -f "$COMMONS_DB.rollback" "$COMMONS_DB"
	elif [ "$had_db" = false ]; then
		rm -f -- "$COMMONS_DB" "$COMMONS_DB-wal" "$COMMONS_DB-shm"
	fi
	if [ -n "$previous" ]; then
		ln -sfn "$(basename "$previous")" "$current"
		"$systemctl_cmd" --user restart codex-commons.service || true
		COMMONS_RELEASE_DIR="$previous" COMMONS_SYSTEMCTL="$systemctl_cmd" /bin/sh "$previous/ops/check-readiness.sh" || true
	else
		rm -f -- "$current"
	fi
	exit 1
fi
