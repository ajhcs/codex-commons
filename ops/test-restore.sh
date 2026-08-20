#!/bin/sh
set -eu

# Phase 4 PR 4: offline atomic database restore fixtures. Disposable
# directories and fake sqlite/commands only. This suite never starts a
# service, binds a listener, or touches a live database.
repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
helper=$repo_root/ops/restore-database.sh
verify=$repo_root/ops/verify-restore.sh
deploy_script=$repo_root/ops/deploy-release.sh
stage_script=$repo_root/ops/stage-release.sh
runbook=$repo_root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md
test -f "$helper"
test -f "$verify"
test -f "$deploy_script"
test -f "$stage_script"
test -f "$runbook"

grep -Fq 'exec 9<&-' "$helper"
grep -Fq 'cp -P --' "$helper"
grep -Fq 'mv -Tf --' "$helper"
grep -Fq '/usr/bin/mktemp --' "$helper"
grep -Fq 'commons-restore.XXXXXX' "$helper"
grep -Fq 'verify-restore.sh' "$helper"
grep -Fq "sqlite3 --" "$helper"
grep -Fq "sqlite3 --" "$verify"
grep -Fq '/usr/bin/sha256sum --' "$verify"
grep -Fq '/usr/bin/sha256sum --' "$helper"
grep -Fq 'cp -P --' "$verify"
grep -Fq 'mode 0600' "$verify"
grep -Fq 'restore-database.sh' "$stage_script"
grep -Fq 'ops/restore-database.sh' "$runbook"
grep -Fq 'ops/test-restore.sh' "$runbook"
grep -Fq 'without_lock_fd /bin/sh "$staged/ops/restore-database.sh"' "$deploy_script"
grep -Fq 'capture /bin/sh "$staged/ops/backup.sh"' "$deploy_script"
if grep -Fq 'COMMONS_DB.rollback' "$helper" "$deploy_script"; then
	printf 'restore path must not use a predictable .rollback name\n' >&2
	exit 1
fi
if grep -Fq 'sort -nr' "$deploy_script"; then
	printf 'deploy-release.sh must not select a backup by mtime\n' >&2
	exit 1
fi

root=$(mktemp -d)
root=$(readlink -f "$root")
first_pid=
cleanup() {
	if [ -n "${first_pid:-}" ] && kill -0 "$first_pid" 2>/dev/null; then
		for child in $(ps -o pid= --ppid "$first_pid" 2>/dev/null); do
			kill "$child" 2>/dev/null || true
		done
		kill "$first_pid" 2>/dev/null || true
		wait "$first_pid" 2>/dev/null || true
	fi
	chmod -R u+w "$root" 2>/dev/null || true
	rm -rf -- "$root"
}
trap cleanup 0 1 2 15

parent=$root/dbparent
mkdir -p "$parent" "$root/backups" "$root/bin"
chmod 0755 "$parent"
dest=$parent/commons.sqlite3
backup=$root/backups/commons-20260101T000000Z.sqlite3
outside=$root/outside
printf 'outside-payload\n' > "$outside"
fake_bin=$root/bin

cat > "$fake_bin/sqlite3" <<'EOF'
#!/bin/sh
set -eu
if [ "${1:-}" = -- ]; then
	shift
fi
sql=${2:-}
case "$sql" in
'PRAGMA integrity_check')
	if [ -n "${FAKE_SQLITE_FAIL_SECOND_INTEGRITY:-}" ]; then
		if [ -f "${FAKE_SQLITE_FAIL_SECOND_INTEGRITY}" ]; then
			printf 'not ok\n'
			exit 0
		fi
		: > "${FAKE_SQLITE_FAIL_SECOND_INTEGRITY}"
	fi
	printf 'ok\n'
	;;
'PRAGMA foreign_key_check')
	;;
'SELECT COALESCE(max(version),0) FROM schema_migrations')
	printf '15\n'
	;;
"SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='archaeology_selected_imports'")
	printf '0\n'
	;;
"SELECT printf('%d,%d,%d', (SELECT count(*) FROM projects),(SELECT count(*) FROM tasks),(SELECT count(*) FROM archaeology_native_batches))")
	printf '0,0,0\n'
	;;
*group_concat*)
	printf '%s\n' 'fixture-schema'
	;;
*)
	printf 'unexpected sqlite3 sql: %s\n' "$sql" >&2
	exit 1
	;;
esac
EOF
chmod 0555 "$fake_bin/sqlite3"

schema_digest=$(printf '%s\n' 'fixture-schema' | /usr/bin/sha256sum | awk '{print $1}')
selected_digest=$(printf %s '' | /usr/bin/sha256sum | awk '{print $1}')

write_backup() {
	path=$1
	payload=$2
	printf '%s\n' "$payload" > "$path"
	chmod 0600 "$path"
	sha=$(/usr/bin/sha256sum -- "$path" | awk '{print $1}')
	printf '%s  %s\n' "$sha" "$path" > "$path.sha256"
	printf '{"file":"%s","sha256":"%s","verified_at":"20260101T000000Z","schema":15,"schema_digest":"%s","counts":"0,0,0,0","selected_digest":"%s","integrity":"ok","foreign_keys":0}\n' \
		"$(basename "$path")" "$sha" "$schema_digest" "$selected_digest" > "$path.receipt.json"
}

reset_dest() {
	rm -f -- "$dest" "$dest-wal" "$dest-shm"
	printf 'live-db-payload\n' > "$dest"
	chmod 0600 "$dest"
}

assert_no_restore_temp() {
	for leftover in "$parent"/commons-restore.*; do
		if [ -e "$leftover" ]; then
			printf 'owned restore temp leaked: %s\n' "$leftover" >&2
			exit 1
		fi
	done
}

run_helper() {
	PATH="$fake_bin:$PATH" /bin/sh "$helper" "$@"
}

expect_helper_failure() {
	case_name=$1
	expected_status=${2:-64}
	if PATH="$fake_bin:$PATH" /bin/sh "$helper" "$backup" "$dest" \
		>"$root/fail-$case_name.out" 2>"$root/fail-$case_name.err"; then
		printf 'helper %s unexpectedly succeeded\n' "$case_name" >&2
		exit 1
	else
		status=$?
	fi
	test "$status" -eq "$expected_status"
	assert_no_restore_temp
	printf 'REJECTED %s\n' "$case_name"
}

write_backup "$backup" 'backup-payload'
reset_dest

# Successful exact atomic replace and mode.
run_helper "$backup" "$dest"
test "$(cat "$dest")" = backup-payload
test "$(stat -c %a "$dest")" = 600
test "$(stat -c %u "$dest")" = "$(id -u)"
test "$(stat -c %g "$dest")" = "$(id -g)"
test ! -L "$dest"
assert_no_restore_temp
printf 'RESTORE_ATOMIC_REPLACE=pass\n'

# Missing destination first-deploy semantics: dest may be absent, then created.
rm -f -- "$dest"
run_helper "$backup" "$dest"
test -f "$dest"
test ! -L "$dest"
test "$(cat "$dest")" = backup-payload
test "$(stat -c %a "$dest")" = 600
assert_no_restore_temp
printf 'RESTORE_MISSING_DEST_FIRST_DEPLOY=pass\n'

# Stale predictable .rollback is ignored and never followed.
reset_dest
printf 'stale-outside\n' > "$root/stale-outside"
stale_before=$(/usr/bin/sha256sum "$root/stale-outside")
ln -s "$root/stale-outside" "$dest.rollback"
run_helper "$backup" "$dest"
test "$(cat "$dest")" = backup-payload
test -L "$dest.rollback"
test "$(/usr/bin/sha256sum "$root/stale-outside")" = "$stale_before"
rm -f -- "$dest.rollback"
printf 'RESTORE_STALE_ROLLBACK_IGNORED=pass\n'

# Exclusive temp is mktemp-shaped and does not replace a pre-existing name.
reset_dest
printf 'keep-me\n' > "$parent/commons-restore.AAAAAA"
chmod 0600 "$parent/commons-restore.AAAAAA"
run_helper "$backup" "$dest"
test "$(cat "$dest")" = backup-payload
test "$(cat "$parent/commons-restore.AAAAAA")" = keep-me
rm -f -- "$parent/commons-restore.AAAAAA"
assert_no_restore_temp
printf 'RESTORE_EXCLUSIVE_TEMP=pass\n'

reset_dest
dest_before=$(/usr/bin/sha256sum "$dest")
outside_before=$(/usr/bin/sha256sum "$outside")

# Source symlink.
rm -f -- "$root/backup-link"
ln -s "$backup" "$root/backup-link"
if PATH="$fake_bin:$PATH" /bin/sh "$helper" "$root/backup-link" "$dest" \
	>"$root/fail-source-symlink.out" 2>"$root/fail-source-symlink.err"; then
	printf 'source symlink unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
test "$(/usr/bin/sha256sum "$dest")" = "$dest_before"
assert_no_restore_temp
printf 'REJECTED source-symlink\n'

# Source directory.
rm -rf -- "$root/backup-dir"
mkdir "$root/backup-dir"
if PATH="$fake_bin:$PATH" /bin/sh "$helper" "$root/backup-dir" "$dest" \
	>"$root/fail-source-dir.out" 2>"$root/fail-source-dir.err"; then
	printf 'source directory unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
test "$(/usr/bin/sha256sum "$dest")" = "$dest_before"
printf 'REJECTED source-directory\n'

# Destination symlink.
rm -f -- "$dest"
ln -s "$outside" "$dest"
expect_helper_failure dest-symlink
test -L "$dest"
test "$(/usr/bin/sha256sum "$outside")" = "$outside_before"
reset_dest
dest_before=$(/usr/bin/sha256sum "$dest")

# Destination directory.
rm -f -- "$dest"
mkdir "$dest"
expect_helper_failure dest-directory
test -d "$dest"
rmdir "$dest"
reset_dest
dest_before=$(/usr/bin/sha256sum "$dest")

# Destination fifo / nonregular.
rm -f -- "$dest"
mkfifo "$dest"
expect_helper_failure dest-fifo
test -p "$dest"
rm -f -- "$dest"
reset_dest
dest_before=$(/usr/bin/sha256sum "$dest")

# Parent symlink.
real_parent=$root/real-parent
mkdir -p "$real_parent"
chmod 0755 "$real_parent"
ln -s "$real_parent" "$root/sym-parent"
sym_dest=$root/sym-parent/commons.sqlite3
printf 'live-db-payload\n' > "$real_parent/commons.sqlite3"
chmod 0600 "$real_parent/commons.sqlite3"
if PATH="$fake_bin:$PATH" /bin/sh "$helper" "$backup" "$sym_dest" \
	>"$root/fail-parent-symlink.out" 2>"$root/fail-parent-symlink.err"; then
	printf 'parent symlink unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
grep -Fq 'database parent must not be a symlink' "$root/fail-parent-symlink.err"
test "$(cat "$real_parent/commons.sqlite3")" = live-db-payload
rm -f -- "$root/sym-parent"
printf 'REJECTED parent-symlink\n'

# Group/other-writable parent.
chmod 0775 "$parent"
expect_helper_failure group-writable-parent
chmod 0777 "$parent"
expect_helper_failure world-writable-parent
chmod 0755 "$parent"
test "$(/usr/bin/sha256sum "$dest")" = "$dest_before"

# Wrong destination mode.
chmod 0644 "$dest"
expect_helper_failure dest-mode-0644
chmod 0600 "$dest"

# Wrong source mode.
chmod 0644 "$backup"
expect_helper_failure source-mode-0644
chmod 0600 "$backup"

# Unowned parent when feasible.
if [ "$(stat -c %u /usr)" != "$(id -u)" ]; then
	if PATH="$fake_bin:$PATH" /bin/sh "$helper" "$backup" /usr/codex-commons-restore-should-not-exist \
		>"$root/fail-unowned-parent.out" 2>"$root/fail-unowned-parent.err"; then
		printf 'unowned parent unexpectedly succeeded\n' >&2
		exit 1
	else
		test "$?" -eq 64
	fi
	test ! -e /usr/codex-commons-restore-should-not-exist
	grep -Fq 'database parent must be owned by the effective uid/gid' "$root/fail-unowned-parent.err"
	printf 'REJECTED unowned-parent\n'
fi

# WAL symlink must not be followed or removed as a target.
reset_dest
printf 'outside-wal\n' > "$root/outside-wal"
wal_before=$(/usr/bin/sha256sum "$root/outside-wal")
ln -s "$root/outside-wal" "$dest-wal"
expect_helper_failure wal-symlink
test -L "$dest-wal"
test "$(/usr/bin/sha256sum "$root/outside-wal")" = "$wal_before"
test "$(cat "$dest")" = live-db-payload
rm -f -- "$dest-wal"

# WAL directory.
reset_dest
mkdir "$dest-wal"
expect_helper_failure wal-directory
test -d "$dest-wal"
rmdir "$dest-wal"

# SHM symlink.
reset_dest
printf 'outside-shm\n' > "$root/outside-shm"
shm_before=$(/usr/bin/sha256sum "$root/outside-shm")
ln -s "$root/outside-shm" "$dest-shm"
expect_helper_failure shm-symlink
test -L "$dest-shm"
test "$(/usr/bin/sha256sum "$root/outside-shm")" = "$shm_before"
test "$(cat "$dest")" = live-db-payload
rm -f -- "$dest-shm"

# Copy failure.
reset_dest
dest_before=$(/usr/bin/sha256sum "$dest")
cp_bin=$root/cp-bin
mkdir -p "$cp_bin"
cat > "$cp_bin/cp" <<'EOF'
#!/bin/sh
set -eu
for arg; do
	case "$arg" in
	*/commons-restore.*)
		echo "forced copy failure" >&2
		exit 1
		;;
	esac
done
exec /bin/cp "$@"
EOF
chmod 0555 "$cp_bin/cp"
if PATH="$cp_bin:$fake_bin:$PATH" /bin/sh "$helper" "$backup" "$dest" \
	>"$root/fail-copy.out" 2>"$root/fail-copy.err"; then
	printf 'copy failure unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
grep -Fq 'failed to copy backup into private restore temp' "$root/fail-copy.err"
test "$(/usr/bin/sha256sum "$dest")" = "$dest_before"
assert_no_restore_temp
printf 'RESTORE_COPY_FAIL_CLOSED=pass\n'

# Temp verify/integrity failure after a successful source verify.
reset_dest
dest_before=$(/usr/bin/sha256sum "$dest")
integrity_marker=$root/integrity.second
rm -f -- "$integrity_marker"
if FAKE_SQLITE_FAIL_SECOND_INTEGRITY=$integrity_marker \
	PATH="$fake_bin:$PATH" /bin/sh "$helper" "$backup" "$dest" \
	>"$root/fail-verify.out" 2>"$root/fail-verify.err"; then
	printf 'temp verify failure unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
grep -Fq 'failed integrity check' "$root/fail-verify.err"
test "$(/usr/bin/sha256sum "$dest")" = "$dest_before"
assert_no_restore_temp
printf 'RESTORE_VERIFY_FAIL_CLOSED=pass\n'

# File-sync failure.
reset_dest
dest_before=$(/usr/bin/sha256sum "$dest")
fail_bin=$root/fail-bin
mkdir -p "$fail_bin"
cat > "$fail_bin/sync" <<'EOF'
#!/bin/sh
set -eu
for arg; do
	case "$arg" in
	--|-d|-f) ;;
	*/commons-restore.*)
		echo "forced file-sync failure" >&2
		exit 1
		;;
	esac
done
exec /bin/sync "$@"
EOF
chmod 0555 "$fail_bin/sync"
if PATH="$fail_bin:$fake_bin:$PATH" /bin/sh "$helper" "$backup" "$dest" \
	>"$root/fail-file-sync.out" 2>"$root/fail-file-sync.err"; then
	printf 'file-sync failure unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
grep -Fq 'failed to sync private restore temp' "$root/fail-file-sync.err"
test "$(/usr/bin/sha256sum "$dest")" = "$dest_before"
assert_no_restore_temp
printf 'RESTORE_FILE_SYNC_FAIL_CLOSED=pass\n'

# Directory-sync failure after a successful rename.
reset_dest
rm -f -- "$fail_bin/sync"
cat > "$fail_bin/sync" <<EOF
#!/bin/sh
set -eu
for arg; do
	case "\$arg" in
	--|-d|-f) ;;
	*/commons-restore.*)
		exec /bin/sync "\$@"
		;;
	$parent)
		echo "forced dir-sync failure" >&2
		exit 1
		;;
	esac
done
exec /bin/sync "\$@"
EOF
chmod 0555 "$fail_bin/sync"
if PATH="$fail_bin:$fake_bin:$PATH" /bin/sh "$helper" "$backup" "$dest" \
	>"$root/fail-dir-sync.out" 2>"$root/fail-dir-sync.err"; then
	printf 'dir-sync failure unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
grep -Fq 'failed to sync database parent directory' "$root/fail-dir-sync.err"
test "$(cat "$dest")" = backup-payload
assert_no_restore_temp
printf 'RESTORE_DIR_SYNC_FAIL_CLOSED=pass\n'

# mv failure.
reset_dest
dest_before=$(/usr/bin/sha256sum "$dest")
rm -f -- "$fail_bin/mv" "$fail_bin/sync"
cat > "$fail_bin/mv" <<'EOF'
#!/bin/sh
set -eu
for arg; do
	case "$arg" in
	*/commons.sqlite3)
		echo "forced mv failure" >&2
		exit 1
		;;
	esac
done
exec /bin/mv "$@"
EOF
chmod 0555 "$fail_bin/mv"
rm -f -- "$fail_bin/sync"
if PATH="$fail_bin:$fake_bin:$PATH" /bin/sh "$helper" "$backup" "$dest" \
	>"$root/fail-mv.out" 2>"$root/fail-mv.err"; then
	printf 'mv failure unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
grep -Fq 'failed to atomically replace database destination' "$root/fail-mv.err"
test "$(/usr/bin/sha256sum "$dest")" = "$dest_before"
assert_no_restore_temp
printf 'RESTORE_MV_FAIL_CLOSED=pass\n'
rm -f -- "$fail_bin/mv"

# Destination swap after precheck: dest becomes a symlink before rename.
reset_dest
printf 'decoy-payload\n' > "$root/decoy-db"
chmod 0600 "$root/decoy-db"
decoy_before=$(/usr/bin/sha256sum "$root/decoy-db")
rm -f -- "$fail_bin/sync" "$fail_bin/mv"
cat > "$fail_bin/sync" <<EOF
#!/bin/sh
set -eu
for arg; do
	case "\$arg" in
	--|-d|-f) ;;
	*/commons-restore.*)
		rm -f -- "$dest"
		ln -s -- "$root/decoy-db" "$dest"
		exec /bin/sync "\$@"
		;;
	esac
done
exec /bin/sync "\$@"
EOF
chmod 0555 "$fail_bin/sync"
if PATH="$fail_bin:$fake_bin:$PATH" /bin/sh "$helper" "$backup" "$dest" \
	>"$root/fail-dest-swap.out" 2>"$root/fail-dest-swap.err"; then
	printf 'destination swap unexpectedly succeeded\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
test -L "$dest"
test "$(readlink "$dest")" = "$root/decoy-db"
test "$(/usr/bin/sha256sum "$root/decoy-db")" = "$decoy_before"
test "$(cat "$root/decoy-db")" = decoy-payload
assert_no_restore_temp
grep -Fq 'database destination must not be a symlink' "$root/fail-dest-swap.err"
printf 'RESTORE_DEST_SWAP_AFTER_PRECHECK=pass\n'
rm -f -- "$fail_bin/sync" "$dest"
reset_dest

# Interrupt before rename must clean only the owned temp.
sync_hold=$root/sync.hold
: > "$sync_hold"
rm -f -- "$fail_bin/sync"
cat > "$fail_bin/sync" <<EOF
#!/bin/sh
set -eu
hold=0
for arg; do
	case "\$arg" in
	--|-d|-f) ;;
	*/commons-restore.*)
		hold=1
		;;
	esac
done
if [ "\$hold" -eq 1 ] && [ -n "\${FAKE_SYNC_HOLD:-}" ]; then
	while [ -f "\$FAKE_SYNC_HOLD" ]; do
		sleep 0.01
	done
	exit 1
fi
exec /bin/sync "\$@"
EOF
chmod 0555 "$fail_bin/sync"
dest_before=$(/usr/bin/sha256sum "$dest")
PATH="$fail_bin:$fake_bin:$PATH" \
FAKE_SYNC_HOLD=$sync_hold \
/bin/sh "$helper" "$backup" "$dest" \
	>"$root/interrupt.out" 2>"$root/interrupt.err" &
first_pid=$!
attempt=0
while true; do
	found_tmp=
	for path in "$parent"/commons-restore.*; do
		if [ -f "$path" ] && [ ! -L "$path" ]; then
			found_tmp=$path
			break
		fi
	done
	if [ -n "$found_tmp" ]; then
		break
	fi
	attempt=$((attempt + 1))
	test "$attempt" -lt 500
	sleep 0.01
done
for child in $(ps -o pid= --ppid "$first_pid" 2>/dev/null); do
	kill -TERM "$child" 2>/dev/null || true
done
kill -TERM "$first_pid" 2>/dev/null || true
rm -f -- "$sync_hold"
if wait "$first_pid"; then
	printf 'restore-interrupt helper exited 0\n' >&2
	exit 1
else
	interrupt_status=$?
fi
first_pid=
test "$interrupt_status" -ne 0
test "$(/usr/bin/sha256sum "$dest")" = "$dest_before"
assert_no_restore_temp
printf 'RESTORE_INTERRUPT_CLEANUP=pass\n'

# verify-restore.sh rejects unsafe exact sources without interpolating paths.
reset_dest
write_backup "$backup" 'backup-payload'
rm -f -- "$root/verify-link"
ln -s "$backup" "$root/verify-link"
if PATH="$fake_bin:$PATH" /bin/sh "$verify" "$root/verify-link" \
	>"$root/verify-symlink.out" 2>"$root/verify-symlink.err"; then
	printf 'verify-restore accepted a symlink source\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
grep -Fq 'backup path must not be a symlink' "$root/verify-symlink.err"
chmod 0644 "$backup"
if PATH="$fake_bin:$PATH" /bin/sh "$verify" "$backup" \
	>"$root/verify-mode.out" 2>"$root/verify-mode.err"; then
	printf 'verify-restore accepted mode 0644 source\n' >&2
	exit 1
else
	test "$?" -eq 64
fi
grep -Fq 'mode 0600' "$root/verify-mode.err"
chmod 0600 "$backup"
printf 'VERIFY_RESTORE_EXACT_SOURCE=pass\n'

printf 'PHASE4_PR4_RESTORE_HELPER=pass\n'
