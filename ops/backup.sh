#!/bin/sh
set -eu
: "${COMMONS_DB:?COMMONS_DB is required}"
: "${COMMONS_BACKUP_DIR:?COMMONS_BACKUP_DIR is required}"
umask 077
case "$COMMONS_DB:$COMMONS_BACKUP_DIR" in *[!A-Za-z0-9_./:-]*|*..*) exit 64;; esac
case "$COMMONS_DB:$COMMONS_BACKUP_DIR" in /*:/*) ;; *) exit 64;; esac
test -f "$COMMONS_DB"
test ! -L "$COMMONS_DB"
mkdir -p "$COMMONS_BACKUP_DIR" "$COMMONS_BACKUP_DIR/daily" "$COMMONS_BACKUP_DIR/monthly"
test ! -L "$COMMONS_BACKUP_DIR"
exec 9>"$COMMONS_BACKUP_DIR/.backup.lock"
flock -n 9
stamp=$(date -u +%Y%m%dT%H%M%SZ)
target="$COMMONS_BACKUP_DIR/daily/commons-$stamp.sqlite3"
test ! -e "$target"
has_table() { test "$(sqlite3 "$COMMONS_DB" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='$1'")" -eq 1; }
status() { if has_table installation_status; then sqlite3 "$COMMONS_DB" "UPDATE installation_status SET backup_status='$1',backup_verified_at=$2,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1"; fi; }
trap 'status failed NULL' HUP INT TERM EXIT
sqlite3 "$COMMONS_DB" ".backup '$target'" >/dev/null
test "$(sqlite3 "$target" 'PRAGMA integrity_check')" = ok
test "$(sqlite3 "$target" 'PRAGMA foreign_key_check' | wc -l)" -eq 0
sha256sum "$target" > "$target.sha256"
schema=$(sqlite3 "$target" 'SELECT COALESCE(max(version),0) FROM schema_migrations')
selected_count=0; selected=''
if test "$(sqlite3 "$target" "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='archaeology_selected_imports'")" -eq 1; then selected_count=$(sqlite3 "$target" 'SELECT count(*) FROM archaeology_selected_imports'); selected=$(sqlite3 "$target" "SELECT COALESCE(group_concat(id||':'||batch_id||':'||principal||':'||request_key||':'||selection_digest||':'||manifest_digest||':'||outcome_ids_json||':'||result_json||':'||created_at,'|'),'') FROM (SELECT * FROM archaeology_selected_imports ORDER BY id)"); fi
counts=$(sqlite3 "$target" "SELECT printf('%d,%d,%d', (SELECT count(*) FROM projects),(SELECT count(*) FROM tasks),(SELECT count(*) FROM archaeology_native_batches))")
counts="$counts,$selected_count"
selected_digest=$(printf %s "$selected" | sha256sum | awk '{print $1}')
schema_digest=$(sqlite3 "$target" "SELECT COALESCE(group_concat(type||':'||name||':'||tbl_name||':'||coalesce(sql,''),char(10)),'') FROM (SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name,tbl_name)" | sha256sum | awk '{print $1}')
printf '{"file":"%s","sha256":"%s","verified_at":"%s","schema":%s,"schema_digest":"%s","counts":"%s","selected_digest":"%s","integrity":"ok","foreign_keys":0}\n' "$(basename "$target")" "$(sha256sum "$target" | awk '{print $1}')" "$stamp" "$schema" "$schema_digest" "$counts" "$selected_digest" > "$target.receipt.json"
status verified "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
trap - HUP INT TERM EXIT
find "$COMMONS_BACKUP_DIR/daily" -maxdepth 1 -type f -name 'commons-*.sqlite3' -printf '%T@ %p\n' | sort -nr | awk 'NR>30 {print $2}' | while IFS= read -r old; do rm -f -- "$old" "$old.sha256" "$old.receipt.json"; done
month=$(date -u +%Y-%m)
monthly="$COMMONS_BACKUP_DIR/monthly/commons-$month.sqlite3"
if [ ! -e "$monthly" ]; then cp --reflink=auto "$target" "$monthly"; sha256sum "$monthly" > "$monthly.sha256"; sed "s/$(basename "$target")/$(basename "$monthly")/;s/$(sha256sum "$target" | awk '{print $1}')/$(sha256sum "$monthly" | awk '{print $1}')/" "$target.receipt.json" > "$monthly.receipt.json"; fi
find "$COMMONS_BACKUP_DIR/monthly" -maxdepth 1 -type f -name 'commons-*.sqlite3' -printf '%T@ %p\n' | sort -nr | awk 'NR>12 {print $2}' | while IFS= read -r old; do rm -f -- "$old" "$old.sha256" "$old.receipt.json"; done
# Exact backup created by this invocation. Deploy captures this single line
# and must not select a different timer file by mtime.
if [ -L "$target" ] || [ ! -f "$target" ]; then
	echo "backup target is missing, not a regular file, or symlink-shaped" >&2
	exit 64
fi
printf '%s\n' "$target"
