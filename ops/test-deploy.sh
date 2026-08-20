#!/bin/sh
set -eu

# Phase 4 PR 1: offline deploy-lock fixtures. Disposable directories and fake
# commands only. This suite never starts a service, binds a listener, or
# touches a live release pointer or database.
repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
deploy_script=$repo_root/ops/deploy-release.sh
runbook=$repo_root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md
test -f "$deploy_script"
test -f "$runbook"

grep -Fq 'exec 9<"$COMMONS_RELEASE_ROOT"' "$deploy_script"
grep -Fq 'flock -n 9' "$deploy_script"
grep -Fq 'exit 75' "$deploy_script"
grep -Fq '.current.next' "$deploy_script"
grep -Fq 'ops/test-deploy.sh' "$runbook"
grep -Fq 'canonical release-root directory' "$runbook"
if grep -Fq 'exec 9>' "$deploy_script"; then
	printf 'deploy-release.sh must open the release-root directory, not a lock file\n' >&2
	exit 1
fi

root=$(mktemp -d)
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

release_root=$root/releases
mkdir -p "$release_root"
current=$release_root/current
db=$root/missing.sqlite3
verify_started=$root/verify.started
verify_release=$root/verify.release
verify_marker=$root/verify.marker
mv_hold=$root/mv.hold
systemctl_log=$root/systemctl.log
fake_bin=$root/bin
mv_bin=$root/mv-bin
mkdir -p "$fake_bin" "$mv_bin"

make_release() {
	release_id=$1
	hold_verify=${2:-0}
	release_dir=$release_root/$release_id
	mkdir -p "$release_dir/ops"
	printf '%s\n' "$release_id" > "$release_dir/VERSION"
	if [ "$hold_verify" -eq 1 ]; then
		cat > "$release_dir/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${FAKE_VERIFY_MARKER:-}" ]; then
	: > "$FAKE_VERIFY_MARKER"
fi
if [ -n "${FAKE_VERIFY_STARTED:-}" ]; then
	: > "$FAKE_VERIFY_STARTED"
fi
if [ -n "${FAKE_VERIFY_RELEASE:-}" ]; then
	while [ ! -f "$FAKE_VERIFY_RELEASE" ]; do
		sleep 0.01
	done
fi
EOF
	else
		cat > "$release_dir/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${FAKE_VERIFY_MARKER:-}" ]; then
	: > "$FAKE_VERIFY_MARKER"
fi
EOF
	fi
	cat > "$release_dir/ops/check-readiness.sh" <<'EOF'
#!/bin/sh
set -eu
exit 0
EOF
	cat > "$release_dir/ops/backup.sh" <<'EOF'
#!/bin/sh
set -eu
exit 0
EOF
	cat > "$release_dir/ops/verify-restore.sh" <<'EOF'
#!/bin/sh
set -eu
exit 0
EOF
}

cat > "$fake_bin/systemctl" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FAKE_SYSTEMCTL_LOG"
exit 0
EOF
chmod 0555 "$fake_bin/systemctl"
: > "$systemctl_log"

cat > "$mv_bin/mv" <<'EOF'
#!/bin/sh
set -eu
hold=0
for arg; do
	case "$arg" in
	*/.current.next)
		hold=1
		;;
	esac
done
if [ "$hold" -eq 1 ] && [ -n "${FAKE_MV_HOLD:-}" ]; then
	while [ -f "$FAKE_MV_HOLD" ]; do
		sleep 0.01
	done
	exit 1
fi
exec /bin/mv "$@"
EOF
chmod 0555 "$mv_bin/mv"

make_release previous
make_release candidate-one 1
make_release candidate-two
make_release blocked-candidate
ln -s previous "$current"

wait_for_file() {
	path=$1
	attempt=0
	while [ ! -e "$path" ]; do
		attempt=$((attempt + 1))
		test "$attempt" -lt 500
		sleep 0.01
	done
}

pointer_target() {
	readlink "$current"
}

root_snapshot() {
	{
		printf 'current='
		if [ -L "$current" ]; then
			readlink "$current"
		else
			printf 'missing\n'
		fi
		find "$release_root" -maxdepth 1 -printf '%f %y %s\n' | LC_ALL=C sort
		sha256sum "$systemctl_log"
	}
}

run_deploy() {
	release_root_arg=$1
	candidate=$2
	COMMONS_RELEASE_ROOT=$release_root_arg \
	COMMONS_DB=$db \
	COMMONS_SYSTEMCTL=$fake_bin/systemctl \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	FAKE_VERIFY_STARTED=$verify_started \
	FAKE_VERIFY_RELEASE=$verify_release \
	/bin/sh "$deploy_script" "$candidate"
}

# Two concurrent deploys: the holder locks before candidate verification. The
# loser, including through a symlink alias of the root, must exit 75 and leave
# the disposable tree unchanged.
rm -f -- "$verify_started" "$verify_release" "$verify_marker"
run_deploy "$release_root" "$release_root/candidate-one" &
first_pid=$!
wait_for_file "$verify_started"
test "$(pointer_target)" = previous
test ! -e "$release_root/.current.next"
test ! -L "$release_root/.current.next"
before_contention=$(root_snapshot)
alias_root=$root/release-root-alias
ln -s "$release_root" "$alias_root"
rm -f -- "$verify_marker"
if COMMONS_RELEASE_ROOT="$alias_root" \
	COMMONS_DB=$db \
	COMMONS_SYSTEMCTL=$fake_bin/systemctl \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	/bin/sh "$deploy_script" "$release_root/blocked-candidate" \
	>"$root/contention.out" 2>"$root/contention.err"; then
	printf 'contention unexpectedly succeeded\n' >&2
	exit 1
else
	contention_status=$?
fi
test "$contention_status" -eq 75
test ! -e "$verify_marker"
test "$(root_snapshot)" = "$before_contention"
grep -Fq 'another deploy already holds' "$root/contention.err"
: > "$verify_release"
if ! wait "$first_pid"; then
	printf 'holder deploy failed\n' >&2
	exit 1
fi
first_pid=
test "$(pointer_target)" = candidate-one
test ! -e "$release_root/.current.next"
test ! -L "$release_root/.current.next"
printf 'DEPLOY_LOCK_CONTENTION=pass\n'
printf 'DEPLOY_LOCK_LOSER_ZERO_MUTATIONS=pass\n'

rm -f -- "$current"
ln -s previous "$current"
: > "$systemctl_log"

# A leftover relative-basename symlink is cleaned without following it.
leftover_target=$release_root/previous
test -d "$leftover_target"
ln -s previous "$release_root/.current.next"
rm -f -- "$verify_marker"
run_deploy "$release_root" "$release_root/candidate-two"
test "$(pointer_target)" = candidate-two
test ! -e "$release_root/.current.next"
test ! -L "$release_root/.current.next"
test -d "$leftover_target"
test -f "$leftover_target/VERSION"
test "$(sed -n '1p' "$leftover_target/VERSION")" = previous
printf 'DEPLOY_LEFTOVER_NEXT_CLEANED=pass\n'

rm -f -- "$current"
ln -s previous "$current"
: > "$systemctl_log"

expect_suspicious_next() {
	case_name=$1
	before=$(root_snapshot)
	outside_before=
	if [ -f "$root/outside-target" ]; then
		outside_before=$(sha256sum "$root/outside-target")
	fi
	rm -f -- "$verify_marker"
	if COMMONS_RELEASE_ROOT="$release_root" \
		COMMONS_DB=$db \
		COMMONS_SYSTEMCTL=$fake_bin/systemctl \
		FAKE_SYSTEMCTL_LOG=$systemctl_log \
		FAKE_VERIFY_MARKER=$verify_marker \
		/bin/sh "$deploy_script" "$release_root/candidate-two" \
		>"$root/suspicious-$case_name.out" 2>"$root/suspicious-$case_name.err"; then
		printf 'suspicious .current.next %s unexpectedly succeeded\n' "$case_name" >&2
		exit 1
	else
		status=$?
	fi
	test "$status" -eq 66
	test ! -e "$verify_marker"
	test "$(pointer_target)" = previous
	test "$(root_snapshot)" = "$before"
	if [ -n "$outside_before" ]; then
		test "$(sha256sum "$root/outside-target")" = "$outside_before"
	fi
	grep -Fq 'refusing to follow or overwrite suspicious' "$root/suspicious-$case_name.err"
}

printf 'stale-next\n' > "$release_root/.current.next"
expect_suspicious_next regular-file
rm -f -- "$release_root/.current.next"

mkdir "$release_root/.current.next"
printf inner > "$release_root/.current.next/payload"
expect_suspicious_next directory
rm -rf -- "$release_root/.current.next"

printf outside > "$root/outside-target"
ln -s "$root/outside-target" "$release_root/.current.next"
expect_suspicious_next absolute-symlink
rm -f -- "$release_root/.current.next"

printf 'DEPLOY_SUSPICIOUS_NEXT=pass\n'

# Signal interruption after this invocation creates .current.next must remove
# that temporary pointer and leave current pointing at the previous release.
# Deliver TERM to the held mv child first so a POSIX shell blocked in wait
# can run its EXIT cleanup.
: > "$mv_hold"
rm -f -- "$verify_marker"
COMMONS_RELEASE_ROOT=$release_root \
COMMONS_DB=$db \
COMMONS_SYSTEMCTL=$fake_bin/systemctl \
FAKE_SYSTEMCTL_LOG=$systemctl_log \
FAKE_MV_HOLD=$mv_hold \
PATH="$mv_bin:$PATH" \
/bin/sh "$deploy_script" "$release_root/candidate-two" \
	>"$root/interrupt.out" 2>"$root/interrupt.err" &
first_pid=$!
attempt=0
while [ ! -L "$release_root/.current.next" ]; do
	attempt=$((attempt + 1))
	test "$attempt" -lt 500
	sleep 0.01
done
test "$(pointer_target)" = previous
test "$(readlink "$release_root/.current.next")" = candidate-two
for child in $(ps -o pid= --ppid "$first_pid" 2>/dev/null); do
	kill -TERM "$child" 2>/dev/null || true
done
kill -TERM "$first_pid" 2>/dev/null || true
rm -f -- "$mv_hold"
if wait "$first_pid"; then
	printf 'interrupted deploy exited 0\n' >&2
	exit 1
else
	interrupt_status=$?
fi
first_pid=
test "$interrupt_status" -ne 0
test "$(pointer_target)" = previous
test ! -e "$release_root/.current.next"
test ! -L "$release_root/.current.next"
test -d "$release_root/previous"
test -d "$release_root/candidate-two"
printf 'DEPLOY_INTERRUPT_CLEANUP=pass\n'

printf 'PHASE4_PR1_DEPLOY_LOCK=pass\n'
