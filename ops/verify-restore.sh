#!/bin/sh
set -eu
: "${1:?backup path required}"
backup=$1
record=${2:-}
case "$record" in ''|--record-drill) ;; *) exit 64;; esac
case "$backup" in /*) ;; *) exit 64;; esac
test -f "$backup"
test ! -L "$backup"
expected_sha=$(awk 'NR==1 && $1 ~ /^[0-9a-f]{64}$/ {print $1}' "$backup.sha256")
test -n "$expected_sha"
actual_sha=$(sha256sum "$backup" | awk '{print $1}')
test "$actual_sha" = "$expected_sha"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT HUP INT TERM
cp -- "$backup" "$work/restore.sqlite3"
test "$(sqlite3 "$work/restore.sqlite3" 'PRAGMA integrity_check')" = ok
test "$(sqlite3 "$work/restore.sqlite3" 'PRAGMA foreign_key_check' | wc -l)" -eq 0
receipt="$backup.receipt.json"
test -f "$receipt"
grep -Fq '"sha256":"'"$actual_sha"'"' "$receipt"
schema=$(sqlite3 "$work/restore.sqlite3" 'SELECT COALESCE(max(version),0) FROM schema_migrations')
schema_digest=$(sqlite3 "$work/restore.sqlite3" "SELECT COALESCE(group_concat(type||':'||name||':'||tbl_name||':'||coalesce(sql,''),char(10)),'') FROM (SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name,tbl_name)" | sha256sum | awk '{print $1}')
selected_count=0; selected=''
if test "$(sqlite3 "$work/restore.sqlite3" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='archaeology_selected_imports'")" -eq 1; then selected_count=$(sqlite3 "$work/restore.sqlite3" 'SELECT count(*) FROM archaeology_selected_imports'); selected=$(sqlite3 "$work/restore.sqlite3" "SELECT COALESCE(group_concat(id||':'||batch_id||':'||principal||':'||request_key||':'||selection_digest||':'||manifest_digest||':'||outcome_ids_json||':'||result_json||':'||created_at,'|'),'') FROM (SELECT * FROM archaeology_selected_imports ORDER BY id)"); fi
counts=$(sqlite3 "$work/restore.sqlite3" "SELECT printf('%d,%d,%d', (SELECT count(*) FROM projects),(SELECT count(*) FROM tasks),(SELECT count(*) FROM archaeology_native_batches))")
counts="$counts,$selected_count"
selected_digest=$(printf %s "$selected" | sha256sum | awk '{print $1}')
grep -Fq '"schema":'"$schema" "$receipt"
grep -Fq '"schema_digest":"'"$schema_digest"'"' "$receipt"
grep -Fq '"counts":"'"$counts"'"' "$receipt"
grep -Fq '"selected_digest":"'"$selected_digest"'"' "$receipt"
if [ "$record" = --record-drill ]; then
	: "${COMMONS_RESTORE_STATUS_DB:?COMMONS_RESTORE_STATUS_DB is required with --record-drill}"
	case "$COMMONS_RESTORE_STATUS_DB" in /*) ;; *) exit 64;; esac
	test -f "$COMMONS_RESTORE_STATUS_DB"; test ! -L "$COMMONS_RESTORE_STATUS_DB"
	test "$(sqlite3 "$COMMONS_RESTORE_STATUS_DB" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='installation_status'")" -eq 1
	sqlite3 "$COMMONS_RESTORE_STATUS_DB" "UPDATE installation_status SET restore_status='verified',restore_verified_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1"
	test "$(sqlite3 "$COMMONS_RESTORE_STATUS_DB" 'SELECT restore_status FROM installation_status WHERE id=1')" = verified
fi
