#!/bin/sh
set -eu
: "${1:?source database path required}"
: "${2:?archive database path required}"
source=$1; db=$2
case "$source:$db" in /*:/*) ;; *) exit 64;; esac
test -f "$source"; test ! -L "$source"; test ! -e "$db"
umask 077
sqlite3 "$source" ".backup '$db'"
test "$(sqlite3 "$db" 'PRAGMA integrity_check')" = ok
test "$(sqlite3 "$db" 'PRAGMA foreign_key_check' | wc -l)" -eq 0
chmod 0600 "$db"
receipt="$db.archive-receipt.json"
schema=$(sqlite3 "$db" 'SELECT COALESCE(max(version),0) FROM schema_migrations')
counts=$(sqlite3 "$db" "SELECT printf('%d,%d', (SELECT count(*) FROM projects),(SELECT count(*) FROM tasks))")
printf '{"file":"%s","sha256":"%s","schema":%s,"counts":"%s","integrity":"ok","foreign_keys":0}\n' "$(basename "$db")" "$(sha256sum "$db" | awk '{print $1}')" "$schema" "$counts" > "$receipt"
chmod 0600 "$receipt"
