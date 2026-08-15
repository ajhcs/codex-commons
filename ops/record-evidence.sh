#!/bin/sh
set -eu
: "${COMMONS_DB:?COMMONS_DB is required}"
: "${1:?mode-0600 evidence receipt required}"
receipt=$1
case "$receipt" in /*) ;; *) exit 64;; esac
test -f "$receipt"; test ! -L "$receipt"; test "$(stat -c %a "$receipt")" = 600
test -f "$receipt.sha256"; test ! -L "$receipt.sha256"
expected=$(awk 'NR==1 && $1 ~ /^[0-9a-f]{64}$/ {print $1}' "$receipt.sha256")
actual=$(sha256sum "$receipt" | awk '{print $1}')
test -n "$expected"; test "$expected" = "$actual"
test "$(sqlite3 :memory: "SELECT json_valid(readfile('$receipt'))")" = 1
kind=$(sqlite3 :memory: "SELECT json_extract(readfile('$receipt'),'$.kind')")
status=$(sqlite3 :memory: "SELECT json_extract(readfile('$receipt'),'$.status')")
violations=$(sqlite3 :memory: "SELECT json_extract(readfile('$receipt'),'$.violations')")
checked_at=$(sqlite3 :memory: "SELECT json_extract(readfile('$receipt'),'$.checked_at')")
scope_digest=$(sqlite3 :memory: "SELECT json_extract(readfile('$receipt'),'$.scope_digest')")
case "$kind" in report_recovery|duplicate_launch|repository_immutability|canonical_immutability) ;; *) exit 64;; esac
case "$status" in verified|attention) ;; *) exit 64;; esac
case "$violations" in ''|*[!0-9]*) exit 64;; esac
test "$violations" -le 10000
if [ "$status" = verified ]; then test "$violations" -eq 0; else test "$violations" -gt 0; fi
printf %s "$checked_at" | grep -Eq '^[0-9]{4}-(0[1-9]|1[0-2])-([0-2][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]Z$'
case "$scope_digest" in *[!0-9a-f]*|'') exit 64;; esac
test "${#scope_digest}" -eq 64
case "$COMMONS_DB" in /*) ;; *) exit 64;; esac
test -f "$COMMONS_DB"; test ! -L "$COMMONS_DB"
column=${kind}_status; violation_column=${kind}_violations; checked_column=${kind}_checked_at
digest_column=${kind}_receipt_digest
sqlite3 "$COMMONS_DB" "BEGIN IMMEDIATE;INSERT INTO installation_evidence_receipts(kind,status,violations,checked_at,scope_digest,receipt_digest,recorded_at) VALUES('$kind','$status',$violations,'$checked_at','$scope_digest','$actual',strftime('%Y-%m-%dT%H:%M:%fZ','now'));UPDATE installation_status SET $column='$status',$violation_column=$violations,$checked_column='$checked_at',$digest_column='$actual',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=1;COMMIT;"
test "$(sqlite3 "$COMMONS_DB" "SELECT $column||':'||$violation_column FROM installation_status WHERE id=1")" = "$status:$violations"
