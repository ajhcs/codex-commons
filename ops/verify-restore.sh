#!/bin/sh
set -eu
# Isolated restore verification of one exact backup. The operator backup path
# is never interpolated into SQL; sqlite3 receives the work copy as argv.
: "${1:?backup path required}"
exec 9<&-
umask 077
LC_ALL=C
LANG=C
export LC_ALL LANG
backup=$1
record=${2:-}
case "$record" in ''|--record-drill) ;; *) exit 64;; esac
case "$backup" in
*[[:cntrl:]]*)
	echo "backup path must not contain control characters" >&2
	exit 64
	;;
esac
case "$backup" in /*) ;; *)
	echo "backup path must be an absolute path" >&2
	exit 64
	;;
esac
if [ -L "$backup" ]; then
	echo "backup path must not be a symlink: $backup" >&2
	exit 64
fi
if [ ! -f "$backup" ]; then
	echo "backup path is missing or not a regular file: $backup" >&2
	exit 64
fi
effective_uid=$(id -u)
effective_gid=$(id -g)
backup_uid=$(stat -c %u "$backup")
backup_gid=$(stat -c %g "$backup")
backup_mode=$(stat -c %a "$backup")
if [ "$backup_uid" != "$effective_uid" ] || [ "$backup_gid" != "$effective_gid" ] || [ "$backup_mode" != 600 ]; then
	echo "backup path must be mode 0600 and owned by the effective uid/gid: $backup" >&2
	exit 64
fi
checksum=$backup.sha256
receipt=$backup.receipt.json
if [ -L "$checksum" ] || [ ! -f "$checksum" ]; then
	echo "backup checksum is missing, not a regular file, or symlink-shaped" >&2
	exit 64
fi
if [ -L "$receipt" ] || [ ! -f "$receipt" ]; then
	echo "backup receipt is missing, not a regular file, or symlink-shaped" >&2
	exit 64
fi
expected_sha=$(awk 'NR==1 && $1 ~ /^[0-9a-f]{64}$/ {print $1}' "$checksum")
test -n "$expected_sha"
sum=$(/usr/bin/sha256sum -- "$backup") || {
	echo "failed to hash backup path: $backup" >&2
	exit 64
}
actual_sha=${sum%% *}
if [ "$actual_sha" != "$expected_sha" ] || [ "$sum" != "$actual_sha  $backup" ]; then
	echo "backup checksum does not match the exact source file" >&2
	exit 64
fi
work=
cleanup_verify_work() {
	if [ -n "${work:-}" ] && [ -d "$work" ] && [ ! -L "$work" ]; then
		rm -rf -- "$work"
	fi
	work=
}
trap cleanup_verify_work EXIT HUP INT TERM
work=$(mktemp -d)
cp -P -- "$backup" "$work/restore.sqlite3"
if [ -L "$work/restore.sqlite3" ] || [ ! -f "$work/restore.sqlite3" ]; then
	echo "restore work copy must be a regular non-symlink file" >&2
	exit 64
fi
chmod 0600 -- "$work/restore.sqlite3"
# SQL strings are literals. The operator backup path is not substituted here.
test "$(sqlite3 -- "$work/restore.sqlite3" 'PRAGMA integrity_check')" = ok
test "$(sqlite3 -- "$work/restore.sqlite3" 'PRAGMA foreign_key_check' | wc -l)" -eq 0
grep -Fq '"sha256":"'"$actual_sha"'"' "$receipt"
schema=$(sqlite3 -- "$work/restore.sqlite3" 'SELECT COALESCE(max(version),0) FROM schema_migrations')
schema_digest=$(sqlite3 -- "$work/restore.sqlite3" "SELECT COALESCE(group_concat(type||':'||name||':'||tbl_name||':'||coalesce(sql,''),char(10)),'') FROM (SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name,tbl_name)" | /usr/bin/sha256sum | awk '{print $1}')
selected_count=0; selected=''
if test "$(sqlite3 -- "$work/restore.sqlite3" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='archaeology_selected_imports'")" -eq 1; then selected_count=$(sqlite3 -- "$work/restore.sqlite3" 'SELECT count(*) FROM archaeology_selected_imports'); selected=$(sqlite3 -- "$work/restore.sqlite3" "SELECT COALESCE(group_concat(id||':'||batch_id||':'||principal||':'||request_key||':'||selection_digest||':'||manifest_digest||':'||outcome_ids_json||':'||result_json||':'||created_at,'|'),'') FROM (SELECT * FROM archaeology_selected_imports ORDER BY id)"); fi
counts=$(sqlite3 -- "$work/restore.sqlite3" "SELECT printf('%d,%d,%d', (SELECT count(*) FROM projects),(SELECT count(*) FROM tasks),(SELECT count(*) FROM archaeology_native_batches))")
counts="$counts,$selected_count"
selected_digest=$(printf %s "$selected" | /usr/bin/sha256sum | awk '{print $1}')
grep -Fq '"schema":'"$schema" "$receipt"
grep -Fq '"schema_digest":"'"$schema_digest"'"' "$receipt"
grep -Fq '"counts":"'"$counts"'"' "$receipt"
grep -Fq '"selected_digest":"'"$selected_digest"'"' "$receipt"
if [ "$record" = --record-drill ]; then
	: "${COMMONS_RESTORE_STATUS_DB:?COMMONS_RESTORE_STATUS_DB is required with --record-drill}"
	case "$COMMONS_RESTORE_STATUS_DB" in /*) ;; *) exit 64;; esac
	if [ -L "$COMMONS_RESTORE_STATUS_DB" ] || [ ! -f "$COMMONS_RESTORE_STATUS_DB" ]; then
		echo "restore status database is missing, not a regular file, or symlink-shaped" >&2
		exit 64
	fi
	test "$(sqlite3 -- "$COMMONS_RESTORE_STATUS_DB" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='installation_status'")" -eq 1
	sqlite3 -- "$COMMONS_RESTORE_STATUS_DB" "UPDATE installation_status SET restore_status='verified',restore_verified_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1"
	test "$(sqlite3 -- "$COMMONS_RESTORE_STATUS_DB" 'SELECT restore_status FROM installation_status WHERE id=1')" = verified
fi
