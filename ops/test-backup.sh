#!/bin/sh
set -eu
umask 077

# Disposable, offline backup-hardening matrix. Never reads a host database,
# environment file, prompt, or credential, and never binds a listener.
repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd -P)
backup_script=$repo_root/ops/backup.sh
verify_script=$repo_root/ops/verify-restore.sh
runbook=$repo_root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md
docs=$repo_root/docs/commons-ops.md
main=$repo_root/cmd/commons-ops/main.go
for path in "$backup_script" "$verify_script" "$runbook" "$docs" "$main"; do
	test -f "$path"; test ! -L "$path"
done

grep -Fq 'PATH=/usr/bin:/bin' "$backup_script"
grep -Fq 'umask 077' "$backup_script"
grep -Fq 'exec 9<&-' "$backup_script"
grep -Fq 'exec "$ops_bin" backup' "$backup_script"
if grep -RE 'COMMONS_BACKUP_DIR/\.backup.lock|exec 9>"\$COMMONS_BACKUP_DIR' \
	"$backup_script" "$main" "$repo_root/internal/opsbackup" "$repo_root/internal/opsfs" \
	"$repo_root/cmd/commons-ops"; then
	printf 'backup must not open a pathname lock file\n' >&2
	exit 1
fi
grep -Fq 'FlockExclusiveNonblock' "$repo_root/internal/opsfs/open_linux.go"
grep -Fq 'RENAME_NOREPLACE' "$repo_root/internal/opsfs/open_linux.go"
grep -Fq 'commons-ops backup' "$docs"
grep -Fq 'directory descriptor' "$runbook"
grep -Fq 'ops/test-backup.sh' "$runbook"

root=$(mktemp -d)
root=$(readlink -f "$root")
chmod 700 "$root"
cleanup() {
	chmod -R u+w "$root" 2>/dev/null || true
	rm -rf -- "$root"
}
trap cleanup 0 1 2 15

ops_bin=$root/commons-ops
(
	CDPATH= cd -- "$repo_root"
	CGO_ENABLED=0 go build -trimpath -o "$ops_bin" ./cmd/commons-ops
)
test -f "$ops_bin"; test ! -L "$ops_bin"; test -x "$ops_bin"
COMMONS_OPS_BIN=$ops_bin
export COMMONS_OPS_BIN

mkdir -m 700 "$root/src" "$root/backups" "$root/bin" "$root/status"
src=$root/src/commons.sqlite3
/usr/bin/sqlite3 "$src" <<'SQL'
CREATE TABLE schema_migrations(version INTEGER);
INSERT INTO schema_migrations VALUES(17);
CREATE TABLE projects(id INTEGER);
INSERT INTO projects VALUES(1);
CREATE TABLE tasks(id INTEGER);
INSERT INTO tasks VALUES(1);
CREATE TABLE archaeology_native_batches(id INTEGER);
CREATE TABLE installation_status(
  id INTEGER PRIMARY KEY CHECK (id=1),
  backup_status TEXT NOT NULL DEFAULT 'unknown',
  backup_verified_at TEXT,
  updated_at TEXT
);
INSERT INTO installation_status(id) VALUES(1);
SQL
chmod 600 "$src"
chmod 700 "$root/src" "$root/backups"

printf '#!/bin/sh\necho hijacked >&2\nexit 1\n' > "$root/bin/sqlite3"
printf '#!/bin/sh\necho hijacked >&2\nexit 1\n' > "$root/bin/date"
printf '#!/bin/sh\necho hijacked >&2\nexit 1\n' > "$root/bin/sha256sum"
chmod 700 "$root/bin/sqlite3" "$root/bin/date" "$root/bin/sha256sum"
PATH="$root/bin:$PATH"
export PATH

run_backup() {
	COMMONS_DB=$src COMMONS_BACKUP_DIR=$root/backups /bin/sh "$backup_script" 2>"$root/err.run"
}

positive_cases=0
rejections=0

expect_exit() {
	want=$1
	label=$2
	shift 2
	set +e
	"$@" >/dev/null 2>"$root/err.$label"
	got=$?
	set -e
	if test "$got" -ne "$want"; then
		printf 'exit %s want %s for %s\n' "$got" "$want" "$label" >&2
		cat "$root/err.$label" >&2
		exit 1
	fi
}

captured=$(
	run_backup
	printf x
)
captured=${captured%x}
case "$captured" in
*"
") captured=${captured%"
"} ;;
*)
	printf 'backup stdout missing terminator newline\n' >&2
	exit 1
	;;
esac
case "$captured" in /*) ;; *)
	printf 'backup stdout is not absolute: %s\n' "$captured" >&2
	exit 1
	;;
esac
nl='
'
case "$captured" in
*"$nl"*)
	printf 'backup stdout contained extra lines: %s\n' "$captured" >&2
	exit 1
	;;
esac
test -f "$captured"; test ! -L "$captured"
test "$(stat -c %a "$captured")" = 600
test -f "$captured.sha256"; test ! -L "$captured.sha256"
test -f "$captured.receipt.json"; test ! -L "$captured.receipt.json"
test ! -e "$root/backups/.backup.lock"
test ! -L "$root/backups/.backup.lock"
grep -Fq '"integrity":"ok"' "$captured.receipt.json"
grep -Fq '"schema":17' "$captured.receipt.json"
if grep -Eq 'result_json|prompt|secret|transcript|review_secret' "$captured.receipt.json"; then
	printf 'receipt leaked a payload field\n' >&2
	exit 1
fi
test "$(/usr/bin/sqlite3 "$src" 'SELECT backup_status FROM installation_status WHERE id=1')" = verified
PATH=/usr/bin:/bin /bin/sh "$verify_script" "$captured"
positive_cases=$((positive_cases + 1))
printf 'VALID wrapper-stdout-and-verify\n'

# Hostile PATH binaries did not run; the packaged helper is used.
if grep -Fq hijacked "$root/err."* 2>/dev/null; then
	printf 'PATH hijack reached a shell helper\n' >&2
	exit 1
fi

ln -s "$root/backups" "$root/backups-link"
rejections=$((rejections + 1))
expect_exit 64 symlink-root env COMMONS_DB="$src" COMMONS_BACKUP_DIR="$root/backups-link" /bin/sh "$backup_script"
printf 'REJECTED symlink-root\n'

chmod 755 "$root/backups"
rejections=$((rejections + 1))
expect_exit 64 wrong-mode env COMMONS_DB="$src" COMMONS_BACKUP_DIR="$root/backups" /bin/sh "$backup_script"
chmod 700 "$root/backups"
printf 'REJECTED wrong-mode\n'

if test -d "$root/backups/daily"; then
	mv "$root/backups/daily" "$root/daily.real"
	mkfifo "$root/backups/daily"
	rejections=$((rejections + 1))
	expect_exit 64 fifo-daily env COMMONS_DB="$src" COMMONS_BACKUP_DIR="$root/backups" /bin/sh "$backup_script"
	rm -f -- "$root/backups/daily"
	mv "$root/daily.real" "$root/backups/daily"
	printf 'REJECTED fifo-daily\n'
fi

# Existing regular target is preserved. Re-run immediately may collide on
# the same UTC second; either way the original file must remain intact.
keep=$captured
keep_body=$(cat "$keep")
set +e
COMMONS_DB=$src COMMONS_BACKUP_DIR=$root/backups /bin/sh "$backup_script" >"$root/second.out" 2>"$root/second.err"
second_status=$?
set -e
if test "$second_status" -eq 0; then
	second=$(cat "$root/second.out")
	test "$second" != "$keep"
	test "$(cat "$keep")" = "$keep_body"
	positive_cases=$((positive_cases + 1))
	printf 'VALID distinct-second-backup\n'
else
	test "$second_status" -eq 64
	test "$(cat "$keep")" = "$keep_body"
	rejections=$((rejections + 1))
	printf 'REJECTED existing-target\n'
fi

# Concurrent invocation while the first holds the directory flock.
hold=$root/status/hold
hold_status=$root/status/point
printf x > "$hold"
chmod 600 "$hold"
COMMONS_OPS_HOLD=$hold
COMMONS_OPS_HOLD_POINT=pre-open
COMMONS_OPS_HOLD_STATUS=$hold_status
export COMMONS_OPS_HOLD COMMONS_OPS_HOLD_POINT COMMONS_OPS_HOLD_STATUS
COMMONS_DB=$src COMMONS_BACKUP_DIR=$root/backups /bin/sh "$backup_script" >"$root/held.out" 2>"$root/held.err" &
held_pid=$!
i=0
while test "$i" -lt 50; do
	test -s "$hold_status" && break
	sleep 0.05
	i=$((i + 1))
done
test -s "$hold_status"
set +e
COMMONS_DB=$src COMMONS_BACKUP_DIR=$root/backups /bin/sh "$backup_script" >"$root/busy.out" 2>"$root/busy.err"
busy_status=$?
set -e
test "$busy_status" -eq 75
test ! -s "$root/busy.out"
rm -f -- "$hold"
wait "$held_pid" || true
unset COMMONS_OPS_HOLD COMMONS_OPS_HOLD_POINT COMMONS_OPS_HOLD_STATUS
rejections=$((rejections + 1))
printf 'REJECTED concurrent-busy\n'

# Retention must leave a planted symlink, directory, hard link, and 0644 file.
daily=$root/backups/daily
ln -s "$(basename "$captured")" "$daily/commons-symlink.sqlite3"
mkdir -m 700 "$daily/commons-dir.sqlite3"
ln -- "$captured" "$daily/commons-hard.sqlite3"
printf wide > "$daily/commons-wide.sqlite3"
chmod 644 "$daily/commons-wide.sqlite3"
sleep 1
COMMONS_DB=$src COMMONS_BACKUP_DIR=$root/backups /bin/sh "$backup_script" >/dev/null
test -L "$daily/commons-symlink.sqlite3"
test -d "$daily/commons-dir.sqlite3"
test -f "$daily/commons-hard.sqlite3"
test -f "$daily/commons-wide.sqlite3"
test "$(stat -c %a "$daily/commons-wide.sqlite3")" = 644
positive_cases=$((positive_cases + 1))
printf 'VALID retention-skips-unsafe-entries\n'

printf 'backup checks passed positives=%s rejections=%s\n' "$positive_cases" "$rejections"
