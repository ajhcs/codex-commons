#!/bin/sh
set -eu

# Phase 4 PR 1 + PR 3: offline deploy-lock, previous-target, and receipt
# fixtures. Disposable directories and fake commands only. This suite never
# starts a service, binds a listener, or touches a live release pointer or
# database.
repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
deploy_script=$repo_root/ops/deploy-release.sh
launcher=$repo_root/ops/commons-launch.sh
runbook=$repo_root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md
env_example=$repo_root/deploy/systemd/dogfood.env.example
test -f "$deploy_script"
test -f "$launcher"
test -f "$runbook"
test -f "$env_example"

grep -Fq 'exec 9<"$COMMONS_RELEASE_ROOT"' "$deploy_script"
grep -Fq 'flock -n 9' "$deploy_script"
grep -Fq 'exit 75' "$deploy_script"
grep -Fq '.current.next' "$deploy_script"
grep -Fq 'without_lock_fd() {' "$deploy_script"
grep -Fq '"$@" 9<&-' "$deploy_script"
grep -Fq 'without_lock_fd /bin/sh "$staged/ops/verify-release.sh"' "$deploy_script"
grep -Fq 'without_lock_fd /bin/sh "$staged/ops/backup.sh"' "$deploy_script"
grep -Fq 'without_lock_fd /bin/sh "$staged/ops/check-readiness.sh"' "$deploy_script"
grep -Fq 'without_lock_fd /bin/sh "$staged/ops/verify-restore.sh"' "$deploy_script"
grep -Fq 'without_lock_fd "$systemctl_cmd"' "$deploy_script"
grep -Fq 'ops/test-deploy.sh' "$runbook"
grep -Fq 'canonical release-root directory' "$runbook"
grep -Fq 'do not receive a usable copy of that lock descriptor' "$runbook"
if grep -Fq 'exec 9>' "$deploy_script"; then
	printf 'deploy-release.sh must open the release-root directory, not a lock file\n' >&2
	exit 1
fi

# Shared current-target grammar with the stable launcher. Capture current
# losslessly exactly once after candidate verification; never re-read it for
# rollback. Receipt helpers must close the lock fd.
test "$(grep -c 'readlink -- "$current"' "$deploy_script")" -eq 1
if grep -Fq 'readlink -f "$current"' "$deploy_script"; then
	printf 'deploy-release.sh must not re-resolve current with readlink -f\n' >&2
	exit 1
fi
grep -Fq '.|..|*/*|*[!A-Za-z0-9._-]*|' "$launcher"
grep -Fq '.|..|*/*|*[!A-Za-z0-9._-]*|' "$deploy_script"
grep -Fq 'COMMONS_RELEASE_IDENTITY_FILE=$previous/VERSION' "$deploy_script"
grep -Fq 'COMMONS_CODEX_BIN=$previous/bin/codex' "$deploy_script"
grep -Fq 'COMMONS_WEB_DIR=$previous/web' "$deploy_script"
grep -Fq 'ln -sfn -- "$previous_id" "$current"' "$deploy_script"
grep -Fq 'write_deployment_attempt_receipt' "$deploy_script"
grep -Fq 'kind=deployment-attempt' "$deploy_script"
grep -Fq 'status=recorded' "$deploy_script"
grep -Fq 'without_lock_fd sh -c' "$deploy_script"
grep -Fq 'capture /usr/bin/sha256sum --' "$deploy_script"
grep -Fq '/usr/bin/mktemp --' "$deploy_script"
grep -Fq 'mv -Tf -- "$receipt_tmp" "$receipt"' "$deploy_script"
grep -Fq 'owned_receipt_tmp' "$deploy_script"
grep -Fq 'COMMONS_DEPLOY_STATE_DIR' "$deploy_script"
grep -Fq 'COMMONS_DEPLOY_STATE_DIR' "$env_example"
grep -Fq 'inspects `current` exactly once' "$runbook"
grep -Fq 'deployment-attempt receipt' "$runbook"
grep -Fq 'captured exact previous' "$runbook"
grep -Fq '/usr/bin/sha256sum' "$runbook"
if grep -Eq 'capture sha256sum ' "$deploy_script"; then
	printf 'deploy-release.sh must hash identity evidence with /usr/bin/sha256sum\n' >&2
	exit 1
fi
if grep -Fq 'deployment-attempt.sha256' "$deploy_script" "$runbook" "$env_example"; then
	printf 'deployment-attempt sidecar digest must not remain in the receipt contract\n' >&2
	exit 1
fi
if grep -Fq 'deployment-attempt.tmp' "$deploy_script"; then
	printf 'deploy-release.sh must not use a predictable receipt temp name\n' >&2
	exit 1
fi
current_line=$(grep -n 'readlink -- "$current"' "$deploy_script" | head -n1 | cut -d: -f1)
backup_line=$(grep -n 'without_lock_fd /bin/sh "$staged/ops/backup.sh"' "$deploy_script" | head -n1 | cut -d: -f1)
receipt_line=$(grep -n '^write_deployment_attempt_receipt$' "$deploy_script" | head -n1 | cut -d: -f1)
test -n "$current_line" && test -n "$backup_line" && test -n "$receipt_line"
test "$current_line" -lt "$receipt_line"
test "$receipt_line" -lt "$backup_line"
if grep -Eq 'readlink -f "\$current" 2>/dev/null \|\| true' "$deploy_script"; then
	printf 'deploy-release.sh still treats an invalid current pointer as absent\n' >&2
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
present_db=$root/present.sqlite3
printf 'not-a-live-database\n' > "$present_db"
backup_dir=$root/backups
mkdir -p "$backup_dir/daily"
deploy_state=$root/deploy-state
mkdir -p "$deploy_state"
chmod 0700 "$deploy_state"
backup_marker=$root/backup.marker
previous_verify_log=$root/previous.verify
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
	printf 'manifest-for-%s\n' "$release_id" > "$release_dir/SHA256SUMS"
	if [ "$hold_verify" -eq 1 ]; then
		cat > "$release_dir/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${FAKE_PREVIOUS_VERIFY_LOG:-}" ]; then
	{
		printf 'verify_release_dir=%s\n' "$COMMONS_RELEASE_DIR"
		printf 'verify_web_dir=%s\n' "$COMMONS_WEB_DIR"
		printf 'verify_codex_bin=%s\n' "$COMMONS_CODEX_BIN"
		printf 'verify_identity=%s\n' "$COMMONS_RELEASE_IDENTITY_FILE"
		printf 'verify_root=%s\n' "$COMMONS_RELEASE_ROOT"
	} >> "$FAKE_PREVIOUS_VERIFY_LOG"
fi
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
if [ -n "${FAKE_PREVIOUS_VERIFY_LOG:-}" ]; then
	{
		printf 'verify_release_dir=%s\n' "$COMMONS_RELEASE_DIR"
		printf 'verify_web_dir=%s\n' "$COMMONS_WEB_DIR"
		printf 'verify_codex_bin=%s\n' "$COMMONS_CODEX_BIN"
		printf 'verify_identity=%s\n' "$COMMONS_RELEASE_IDENTITY_FILE"
		printf 'verify_root=%s\n' "$COMMONS_RELEASE_ROOT"
	} >> "$FAKE_PREVIOUS_VERIFY_LOG"
fi
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
if [ -n "${FAKE_BACKUP_MARKER:-}" ]; then
	: > "$FAKE_BACKUP_MARKER"
fi
if [ -n "${FAKE_BACKUP_SWAP_TO:-}" ] && [ -n "${FAKE_CURRENT:-}" ]; then
	rm -f -- "$FAKE_CURRENT"
	ln -s -- "$FAKE_BACKUP_SWAP_TO" "$FAKE_CURRENT"
fi
if [ -n "${COMMONS_BACKUP_DIR:-}" ]; then
	mkdir -p "$COMMONS_BACKUP_DIR/daily"
	: > "$COMMONS_BACKUP_DIR/daily/commons-fake.sqlite3"
fi
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
		elif [ -e "$current" ]; then
			printf 'not-symlink\n'
		else
			printf 'missing\n'
		fi
		find "$release_root" -maxdepth 1 -printf '%f %y %s\n' | LC_ALL=C sort
		sha256sum "$systemctl_log"
		if [ -e "$backup_marker" ]; then
			printf 'backup=present\n'
		else
			printf 'backup=absent\n'
		fi
	}
}

run_deploy() {
	release_root_arg=$1
	candidate=$2
	COMMONS_RELEASE_ROOT=$release_root_arg \
	COMMONS_DB=$db \
	COMMONS_SYSTEMCTL=$fake_bin/systemctl \
	COMMONS_DEPLOY_STATE_DIR=$deploy_state \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	FAKE_VERIFY_STARTED=$verify_started \
	FAKE_VERIFY_RELEASE=$verify_release \
	FAKE_BACKUP_MARKER=$backup_marker \
	FAKE_CURRENT=$current \
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
	COMMONS_DEPLOY_STATE_DIR=$deploy_state \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	FAKE_BACKUP_MARKER=$backup_marker \
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
		COMMONS_DEPLOY_STATE_DIR=$deploy_state \
		FAKE_SYSTEMCTL_LOG=$systemctl_log \
		FAKE_VERIFY_MARKER=$verify_marker \
		FAKE_BACKUP_MARKER=$backup_marker \
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
COMMONS_DEPLOY_STATE_DIR=$deploy_state \
FAKE_SYSTEMCTL_LOG=$systemctl_log \
FAKE_MV_HOLD=$mv_hold \
FAKE_BACKUP_MARKER=$backup_marker \
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

# A staged child that attempts flock -u 9 must not release the parent lock.
# The contender still exits 75 with zero mutations until the true holder exits.
rm -f -- "$current"
ln -s previous "$current"
: > "$systemctl_log"
make_release unlock-holder 1
cat > "$release_root/unlock-holder/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
flock -u 9 2>/dev/null || true
if [ -n "${FAKE_VERIFY_STARTED:-}" ]; then
	: > "$FAKE_VERIFY_STARTED"
fi
if [ -n "${FAKE_VERIFY_RELEASE:-}" ]; then
	while [ ! -f "$FAKE_VERIFY_RELEASE" ]; do
		sleep 0.01
	done
fi
EOF
rm -f -- "$verify_started" "$verify_release" "$verify_marker"
run_deploy "$release_root" "$release_root/unlock-holder" &
first_pid=$!
wait_for_file "$verify_started"
test "$(pointer_target)" = previous
test ! -e "$release_root/.current.next"
test ! -L "$release_root/.current.next"
before_unlock=$(root_snapshot)
rm -f -- "$verify_marker"
if COMMONS_RELEASE_ROOT="$release_root" \
	COMMONS_DB=$db \
	COMMONS_SYSTEMCTL=$fake_bin/systemctl \
	COMMONS_DEPLOY_STATE_DIR=$deploy_state \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	FAKE_BACKUP_MARKER=$backup_marker \
	/bin/sh "$deploy_script" "$release_root/blocked-candidate" \
	>"$root/unlock.out" 2>"$root/unlock.err"; then
	printf 'contender succeeded after child flock -u 9\n' >&2
	exit 1
else
	unlock_status=$?
fi
test "$unlock_status" -eq 75
test ! -e "$verify_marker"
test "$(pointer_target)" = previous
test "$(root_snapshot)" = "$before_unlock"
grep -Fq 'another deploy already holds' "$root/unlock.err"
: > "$verify_release"
if ! wait "$first_pid"; then
	printf 'unlock-holder deploy failed\n' >&2
	exit 1
fi
first_pid=
test "$(pointer_target)" = unlock-holder
test ! -e "$release_root/.current.next"
test ! -L "$release_root/.current.next"
printf 'DEPLOY_LOCK_FD_NOT_INHERITED=pass\n'

printf 'PHASE4_PR1_DEPLOY_LOCK=pass\n'

restore_previous() {
	make_release previous
	rm -f -- "$current"
	ln -s previous "$current"
	: > "$systemctl_log"
	rm -f -- "$backup_marker" "$verify_marker" "$previous_verify_log"
}

mutation_snapshot() {
	{
		root_snapshot
		printf 'current_raw='
		if [ -L "$current" ] || [ -e "$current" ]; then
			readlink -n -- "$current" 2>/dev/null || printf 'unreadable'
			printf 'x\n'
		else
			printf 'missingx\n'
		fi
		if [ -f "$deploy_state/deployment-attempt" ]; then
			sha256sum "$deploy_state/deployment-attempt"
		else
			printf 'receipt=absent\n'
		fi
		find "$backup_dir" -maxdepth 2 -printf '%P %y %s\n' 2>/dev/null | LC_ALL=C sort
	}
}

assert_receipt() {
	expected_prev_state=$1
	expected_candidate=$2
	expected_previous=${3:-}
	receipt=$deploy_state/deployment-attempt
	test -f "$receipt"
	test ! -L "$receipt"
	test "$(stat -c %a "$receipt")" = 600
	test "$(stat -c %u "$receipt")" = "$(id -u)"
	test "$(stat -c %g "$receipt")" = "$(id -g)"
	test "$(stat -c %a "$deploy_state")" = 700
	test "$(stat -c %u "$deploy_state")" = "$(id -u)"
	test "$(stat -c %g "$deploy_state")" = "$(id -g)"
	test ! -e "$deploy_state/deployment-attempt.sha256"
	for leftover in "$deploy_state"/deployment-attempt.*; do
		if [ -e "$leftover" ]; then
			printf 'owned receipt temp leaked: %s\n' "$leftover" >&2
			exit 1
		fi
	done
	test "$(wc -l < "$receipt")" -eq 7
	candidate_digest=$(/usr/bin/sha256sum "$release_root/$expected_candidate/SHA256SUMS" | awk '{print $1}')
	if [ "$expected_prev_state" = validated ]; then
		previous_digest=$(/usr/bin/sha256sum "$release_root/$expected_previous/SHA256SUMS" | awk '{print $1}')
		expected_body=$(printf '%s\n' \
			'kind=deployment-attempt' \
			'status=recorded' \
			"candidate_id=$expected_candidate" \
			"candidate_digest=$candidate_digest" \
			'previous_state=validated' \
			"previous_id=$expected_previous" \
			"previous_digest=$previous_digest")
	else
		expected_body=$(printf '%s\n' \
			'kind=deployment-attempt' \
			'status=recorded' \
			"candidate_id=$expected_candidate" \
			"candidate_digest=$candidate_digest" \
			'previous_state=absent' \
			'previous_id=' \
			'previous_digest=')
	fi
	test "$(cat "$receipt")" = "$expected_body"
	expected_sha=$(/usr/bin/sha256sum "$receipt" | awk '{print $1}')
	test "${#expected_sha}" -eq 64
	test "$(/usr/bin/sha256sum "$receipt" | awk '{print $1}')" = "$expected_sha"
	if grep -Eq 'present.sqlite3|missing.sqlite3|COMMONS_DB=|/home/USER|prompt|secret|binding.key|COMMONS_CODEX_BINDING' "$receipt"; then
		printf 'deployment-attempt receipt contains forbidden payload\n' >&2
		exit 1
	fi
}

expect_invalid_previous() {
	case_name=$1
	expected_status=${2:-64}
	state_dir_arg=${3:-$deploy_state}
	rm -f -- "$backup_marker" "$verify_marker"
	: > "$systemctl_log"
	before=$(mutation_snapshot)
	if COMMONS_RELEASE_ROOT=$release_root \
		COMMONS_DB=$present_db \
		COMMONS_BACKUP_DIR=$backup_dir \
		COMMONS_SYSTEMCTL=$fake_bin/systemctl \
		COMMONS_DEPLOY_STATE_DIR=$state_dir_arg \
		FAKE_SYSTEMCTL_LOG=$systemctl_log \
		FAKE_VERIFY_MARKER=$verify_marker \
		FAKE_BACKUP_MARKER=$backup_marker \
		FAKE_CURRENT=$current \
		/bin/sh "$deploy_script" "$release_root/candidate-two" \
		>"$root/invalid-$case_name.out" 2>"$root/invalid-$case_name.err"; then
		printf 'invalid previous %s unexpectedly succeeded\n' "$case_name" >&2
		exit 1
	else
		status=$?
	fi
	test "$status" -eq "$expected_status"
	test "$(mutation_snapshot)" = "$before"
	test ! -e "$backup_marker"
	test ! -e "$release_root/.current.next"
	test ! -L "$release_root/.current.next"
	printf 'REJECTED %s\n' "$case_name"
}

# Valid previous capture uses the exact previous verifier environment and
# records candidate/previous identity in the sanitized receipt.
restore_previous
cat > "$release_root/previous/ops/verify-release.sh" <<EOF
#!/bin/sh
set -eu
{
	printf 'verify_release_dir=%s\\n' "\$COMMONS_RELEASE_DIR"
	printf 'verify_web_dir=%s\\n' "\$COMMONS_WEB_DIR"
	printf 'verify_codex_bin=%s\\n' "\$COMMONS_CODEX_BIN"
	printf 'verify_identity=%s\\n' "\$COMMONS_RELEASE_IDENTITY_FILE"
	printf 'verify_root=%s\\n' "\$COMMONS_RELEASE_ROOT"
} > "$previous_verify_log"
EOF
rm -f -- "$previous_verify_log"
run_deploy "$release_root" "$release_root/candidate-two"
test "$(pointer_target)" = candidate-two
test -f "$previous_verify_log"
grep -Fxq "verify_release_dir=$release_root/previous" "$previous_verify_log"
grep -Fxq "verify_web_dir=$release_root/previous/web" "$previous_verify_log"
grep -Fxq "verify_codex_bin=$release_root/previous/bin/codex" "$previous_verify_log"
grep -Fxq "verify_identity=$release_root/previous/VERSION" "$previous_verify_log"
grep -Fxq "verify_root=$release_root" "$previous_verify_log"
assert_receipt validated candidate-two previous
printf 'DEPLOY_PREVIOUS_CAPTURE_EXACT_ENV=pass\n'

# First deployment with current absent is allowed and records previous_state=absent.
rm -f -- "$current"
: > "$systemctl_log"
rm -f -- "$backup_marker"
run_deploy "$release_root" "$release_root/blocked-candidate"
test "$(pointer_target)" = blocked-candidate
assert_receipt absent blocked-candidate
printf 'DEPLOY_FIRST_ABSENT=pass\n'

restore_previous

# Invalid current/previous aborts before backup, pointer mutation, or systemctl.
rm -f -- "$current"
mkdir "$current"
expect_invalid_previous current-is-directory
rmdir "$current"
restore_previous

rm -f -- "$current"
printf not-a-release > "$current"
expect_invalid_previous current-is-regular-file
rm -f -- "$current"
restore_previous

mkdir -p "$release_root/nested/child/ops" "$release_root/nested/child/web" "$release_root/nested/child/bin"
printf nested > "$release_root/nested/child/VERSION"
printf 'manifest-for-nested\n' > "$release_root/nested/child/SHA256SUMS"
printf '#!/bin/sh\nexit 0\n' > "$release_root/nested/child/ops/verify-release.sh"
rm -f -- "$current"
ln -s nested/child "$current"
expect_invalid_previous nested-target
rm -rf -- "$release_root/nested"
restore_previous

rm -f -- "$current"
ln -s ../outside "$current"
expect_invalid_previous outside-relative
restore_previous

outside=$root/outside-release
mkdir -p "$outside"
rm -f -- "$current"
ln -s "$outside" "$current"
expect_invalid_previous outside-absolute
restore_previous

rm -f -- "$current"
ln -s ../releases/previous "$current"
expect_invalid_previous traversal-target
restore_previous

rm -f -- "$current"
ln -s ./previous "$current"
expect_invalid_previous dot-slash-target
restore_previous

rm -f -- "$current"
ln -s . "$current"
expect_invalid_previous dot-target
restore_previous

rm -f -- "$current"
ln -s .. "$current"
expect_invalid_previous dot-dot-target
restore_previous

rm -f -- "$current"
ln -s missing-release "$current"
expect_invalid_previous missing-target
restore_previous

printf not-a-dir > "$release_root/not-a-dir"
rm -f -- "$current"
ln -s not-a-dir "$current"
expect_invalid_previous non-directory-target
rm -f -- "$release_root/not-a-dir"
restore_previous

ln -s previous "$release_root/alias"
rm -f -- "$current"
ln -s alias "$current"
expect_invalid_previous symlink-shaped-previous
rm -f -- "$release_root/alias"
restore_previous

rm -f -- "$current"
ln -s 'release a' "$current"
expect_invalid_previous whitespace-target
restore_previous

rm -f -- "$current"
nl_target=$(printf 'previous\n.')
nl_target=${nl_target%.}
ln -s -- "$nl_target" "$current"
nl_raw=$(readlink -n -- "$current"; printf x)
test "$nl_raw" = "$(printf 'previous\nx')"
expect_invalid_previous newline-target
restore_previous

rm -f -- "$current"
ctrl_target=$(printf 'previous\001.')
ctrl_target=${ctrl_target%.}
ln -s -- "$ctrl_target" "$current"
ctrl_raw=$(readlink -n -- "$current"; printf x)
test "$ctrl_raw" = "$(printf 'previous\001x')"
expect_invalid_previous control-target
restore_previous

printf 'not-the-basename\n' > "$release_root/previous/VERSION"
expect_invalid_previous version-mismatch
restore_previous

cat > "$release_root/previous/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
exit 17
EOF
expect_invalid_previous previous-verifier-failure 17
restore_previous

rm -f -- "$release_root/previous/SHA256SUMS"
expect_invalid_previous manifest-missing
restore_previous

rm -f -- "$release_root/previous/SHA256SUMS"
ln -s "$release_root/candidate-two/SHA256SUMS" "$release_root/previous/SHA256SUMS"
expect_invalid_previous manifest-symlink
restore_previous

rm -f -- "$release_root/previous/SHA256SUMS"
mkdir "$release_root/previous/SHA256SUMS"
expect_invalid_previous manifest-directory
rmdir "$release_root/previous/SHA256SUMS"
restore_previous

# A PATH/EnvironmentFile sha256sum substitute must not become identity
# evidence. Injection stays limited to explicit fake commands already used
# for other deploy behavior.
sha_bin=$root/sha-bin
mkdir -p "$sha_bin"
cat > "$sha_bin/sha256sum" <<'EOF'
#!/bin/sh
echo "PATH sha256sum must not be used for identity evidence" >&2
exit 1
EOF
chmod 0755 "$sha_bin/sha256sum"
restore_previous
rm -f -- "$backup_marker"
: > "$systemctl_log"
PATH="$sha_bin:$PATH" \
COMMONS_RELEASE_ROOT=$release_root \
COMMONS_DB=$db \
COMMONS_SYSTEMCTL=$fake_bin/systemctl \
COMMONS_DEPLOY_STATE_DIR=$deploy_state \
FAKE_SYSTEMCTL_LOG=$systemctl_log \
FAKE_BACKUP_MARKER=$backup_marker \
FAKE_CURRENT=$current \
/bin/sh "$deploy_script" "$release_root/candidate-two" \
	>"$root/trusted-sha.out" 2>"$root/trusted-sha.err"
test "$(pointer_target)" = candidate-two
assert_receipt validated candidate-two previous
printf 'DEPLOY_TRUSTED_SHA256SUM=pass\n'
printf 'DEPLOY_INVALID_PREVIOUS_ZERO_MUTATION=pass\n'

# Receipt path must be a regular file in a real parent; symlink-shaped state is rejected.
restore_previous
rm -f -- "$deploy_state/deployment-attempt"
printf outside > "$root/outside-receipt"
ln -s "$root/outside-receipt" "$deploy_state/deployment-attempt"
expect_invalid_previous receipt-symlink
rm -f -- "$deploy_state/deployment-attempt"
restore_previous

rm -f -- "$deploy_state/deployment-attempt"
mkdir "$deploy_state/deployment-attempt"
expect_invalid_previous receipt-directory
rmdir "$deploy_state/deployment-attempt"
restore_previous

mkdir -p "$root/real-state-parent"
chmod 0755 "$root/real-state-parent"
ln -s "$root/real-state-parent" "$root/sym-state-parent"
expect_invalid_previous unsafe-state-parent 64 "$root/sym-state-parent/deploy"
rm -f -- "$root/sym-state-parent"
restore_previous

ln -s "$deploy_state" "$root/state-dir-alias"
expect_invalid_previous state-dir-symlink 64 "$root/state-dir-alias"
rm -f -- "$root/state-dir-alias"
restore_previous

writable_parent=$root/writable-parent
mkdir -p "$writable_parent/deploy"
chmod 0700 "$writable_parent/deploy"
chmod 0777 "$writable_parent"
expect_invalid_previous world-writable-state-parent 64 "$writable_parent/deploy"
test ! -e "$writable_parent/deploy/deployment-attempt"
chmod 0775 "$writable_parent"
expect_invalid_previous group-writable-state-parent 64 "$writable_parent/deploy"
test ! -e "$writable_parent/deploy/deployment-attempt"
chmod 0700 "$writable_parent"
restore_previous

mode_parent=$root/mode-parent
mkdir -p "$mode_parent/deploy"
chmod 0755 "$mode_parent"
chmod 0755 "$mode_parent/deploy"
expect_invalid_previous wrong-mode-state-dir 64 "$mode_parent/deploy"
test ! -e "$mode_parent/deploy/deployment-attempt"
chmod 0700 "$mode_parent/deploy"
restore_previous

if [ "$(stat -c %u /usr)" != "$(id -u)" ]; then
	expect_invalid_previous unowned-state-parent 64 /usr/codex-commons-deploy-should-not-exist
	test ! -e /usr/codex-commons-deploy-should-not-exist
fi

# A leftover predictable temp name must not be pre-deleted or followed.
printf outside-tmp > "$root/outside-tmp"
outside_tmp_before=$(/usr/bin/sha256sum "$root/outside-tmp")
ln -s "$root/outside-tmp" "$deploy_state/deployment-attempt.tmp"
run_deploy "$release_root" "$release_root/candidate-two"
test "$(pointer_target)" = candidate-two
test -L "$deploy_state/deployment-attempt.tmp"
test "$(/usr/bin/sha256sum "$root/outside-tmp")" = "$outside_tmp_before"
rm -f -- "$deploy_state/deployment-attempt.tmp"
assert_receipt validated candidate-two previous
printf 'DEPLOY_RECEIPT_PATH_REJECT=pass\n'

# Receipt publish must fail closed on sync/mv errors and leave only the owned temp gone.
restore_previous
rm -f -- "$deploy_state/deployment-attempt"
receipt_fail_bin=$root/receipt-fail-bin
mkdir -p "$receipt_fail_bin"
cat > "$receipt_fail_bin/mv" <<'EOF'
#!/bin/sh
set -eu
for arg; do
	case "$arg" in
	*/deployment-attempt)
		echo "forced receipt mv failure" >&2
		exit 1
		;;
	esac
done
exec /bin/mv "$@"
EOF
chmod 0555 "$receipt_fail_bin/mv"
before=$(mutation_snapshot)
if COMMONS_RELEASE_ROOT=$release_root \
	COMMONS_DB=$present_db \
	COMMONS_BACKUP_DIR=$backup_dir \
	COMMONS_SYSTEMCTL=$fake_bin/systemctl \
	COMMONS_DEPLOY_STATE_DIR=$deploy_state \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	FAKE_BACKUP_MARKER=$backup_marker \
	FAKE_CURRENT=$current \
	PATH="$receipt_fail_bin:$PATH" \
	/bin/sh "$deploy_script" "$release_root/candidate-two" \
	>"$root/receipt-mv.out" 2>"$root/receipt-mv.err"; then
	printf 'receipt mv failure unexpectedly succeeded\n' >&2
	exit 1
else
	receipt_mv_status=$?
fi
test "$receipt_mv_status" -eq 64
test "$(mutation_snapshot)" = "$before"
test ! -e "$backup_marker"
test "$(pointer_target)" = previous
grep -Fq 'failed to publish deployment-attempt receipt' "$root/receipt-mv.err"
for leftover in "$deploy_state"/deployment-attempt.*; do
	if [ -e "$leftover" ]; then
		printf 'owned receipt temp leaked after mv failure: %s\n' "$leftover" >&2
		exit 1
	fi
done
printf 'DEPLOY_RECEIPT_MV_FAIL_CLOSED=pass\n'

cat > "$receipt_fail_bin/sync" <<'EOF'
#!/bin/sh
set -eu
for arg; do
	case "$arg" in
	--|-d|-f) ;;
	*/deployment-attempt.*)
		echo "forced receipt sync failure" >&2
		exit 1
		;;
	esac
done
exec /bin/sync "$@"
EOF
chmod 0555 "$receipt_fail_bin/sync"
rm -f -- "$receipt_fail_bin/mv"
before=$(mutation_snapshot)
if COMMONS_RELEASE_ROOT=$release_root \
	COMMONS_DB=$present_db \
	COMMONS_BACKUP_DIR=$backup_dir \
	COMMONS_SYSTEMCTL=$fake_bin/systemctl \
	COMMONS_DEPLOY_STATE_DIR=$deploy_state \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	FAKE_BACKUP_MARKER=$backup_marker \
	FAKE_CURRENT=$current \
	PATH="$receipt_fail_bin:$PATH" \
	/bin/sh "$deploy_script" "$release_root/candidate-two" \
	>"$root/receipt-sync.out" 2>"$root/receipt-sync.err"; then
	printf 'receipt sync failure unexpectedly succeeded\n' >&2
	exit 1
else
	receipt_sync_status=$?
fi
test "$receipt_sync_status" -eq 64
test "$(mutation_snapshot)" = "$before"
test ! -e "$backup_marker"
test "$(pointer_target)" = previous
grep -Fq 'failed to sync deployment-attempt receipt' "$root/receipt-sync.err"
for leftover in "$deploy_state"/deployment-attempt.*; do
	if [ -e "$leftover" ]; then
		printf 'owned receipt temp leaked after sync failure: %s\n' "$leftover" >&2
		exit 1
	fi
done
printf 'DEPLOY_RECEIPT_SYNC_FAIL_CLOSED=pass\n'

# Signal interruption while the owned temp exists must clean only that temp.
sync_hold=$root/sync.hold
: > "$sync_hold"
rm -f -- "$receipt_fail_bin/sync"
cat > "$receipt_fail_bin/sync" <<EOF
#!/bin/sh
set -eu
hold=0
for arg; do
	case "\$arg" in
	--|-d|-f) ;;
	*/deployment-attempt.*)
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
chmod 0555 "$receipt_fail_bin/sync"
rm -f -- "$deploy_state/deployment-attempt"
rm -f -- "$verify_marker"
COMMONS_RELEASE_ROOT=$release_root \
COMMONS_DB=$db \
COMMONS_SYSTEMCTL=$fake_bin/systemctl \
COMMONS_DEPLOY_STATE_DIR=$deploy_state \
FAKE_SYSTEMCTL_LOG=$systemctl_log \
FAKE_SYNC_HOLD=$sync_hold \
FAKE_BACKUP_MARKER=$backup_marker \
PATH="$receipt_fail_bin:$PATH" \
/bin/sh "$deploy_script" "$release_root/candidate-two" \
	>"$root/receipt-interrupt.out" 2>"$root/receipt-interrupt.err" &
first_pid=$!
attempt=0
while true; do
	found_tmp=
	for path in "$deploy_state"/deployment-attempt.*; do
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
test "$(pointer_target)" = previous
for child in $(ps -o pid= --ppid "$first_pid" 2>/dev/null); do
	kill -TERM "$child" 2>/dev/null || true
done
kill -TERM "$first_pid" 2>/dev/null || true
rm -f -- "$sync_hold"
if wait "$first_pid"; then
	printf 'receipt-interrupt deploy exited 0\n' >&2
	exit 1
else
	receipt_interrupt_status=$?
fi
first_pid=
test "$receipt_interrupt_status" -ne 0
test "$(pointer_target)" = previous
test ! -e "$deploy_state/deployment-attempt"
for leftover in "$deploy_state"/deployment-attempt.*; do
	if [ -e "$leftover" ]; then
		printf 'owned receipt temp leaked after signal: %s\n' "$leftover" >&2
		exit 1
	fi
done
printf 'DEPLOY_RECEIPT_INTERRUPT_CLEANUP=pass\n'

# Swapping current after the previous pin must not redirect rollback.
restore_previous
make_release decoy
make_release swap-candidate
cat > "$release_root/swap-candidate/ops/check-readiness.sh" <<'EOF'
#!/bin/sh
set -eu
rm -f -- "$FAKE_CURRENT"
ln -s -- decoy "$FAKE_CURRENT"
exit 1
EOF
rm -f -- "$backup_marker"
: > "$systemctl_log"
if COMMONS_RELEASE_ROOT=$release_root \
	COMMONS_DB=$present_db \
	COMMONS_BACKUP_DIR=$backup_dir \
	COMMONS_SYSTEMCTL=$fake_bin/systemctl \
	COMMONS_DEPLOY_STATE_DIR=$deploy_state \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	FAKE_BACKUP_MARKER=$backup_marker \
	FAKE_BACKUP_SWAP_TO=decoy \
	FAKE_CURRENT=$current \
	/bin/sh "$deploy_script" "$release_root/swap-candidate" \
	>"$root/swap.out" 2>"$root/swap.err"; then
	printf 'swap-candidate deploy unexpectedly succeeded\n' >&2
	exit 1
else
	swap_status=$?
fi
test "$swap_status" -eq 1
test "$(pointer_target)" = previous
test "$(pointer_target)" != decoy
test "$(pointer_target)" != swap-candidate
test -d "$release_root/decoy"
test -d "$release_root/previous"
assert_receipt validated swap-candidate previous
printf 'DEPLOY_PREVIOUS_PIN_SURVIVES_CURRENT_SWAP=pass\n'

# Previous verifier children cannot unlock the parent flock.
restore_previous
make_release lock-previous
cat > "$release_root/previous/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
flock -u 9 2>/dev/null || true
if [ -n "${FAKE_VERIFY_STARTED:-}" ]; then
	: > "$FAKE_VERIFY_STARTED"
fi
if [ -n "${FAKE_VERIFY_RELEASE:-}" ]; then
	while [ ! -f "$FAKE_VERIFY_RELEASE" ]; do
		sleep 0.01
	done
fi
EOF
rm -f -- "$verify_started" "$verify_release" "$verify_marker"
run_deploy "$release_root" "$release_root/lock-previous" &
first_pid=$!
wait_for_file "$verify_started"
test "$(pointer_target)" = previous
before_prev_unlock=$(mutation_snapshot)
rm -f -- "$verify_marker"
if COMMONS_RELEASE_ROOT="$release_root" \
	COMMONS_DB=$db \
	COMMONS_SYSTEMCTL=$fake_bin/systemctl \
	COMMONS_DEPLOY_STATE_DIR=$deploy_state \
	FAKE_SYSTEMCTL_LOG=$systemctl_log \
	FAKE_VERIFY_MARKER=$verify_marker \
	FAKE_BACKUP_MARKER=$backup_marker \
	/bin/sh "$deploy_script" "$release_root/blocked-candidate" \
	>"$root/prev-unlock.out" 2>"$root/prev-unlock.err"; then
	printf 'contender succeeded after previous verifier flock -u 9\n' >&2
	exit 1
else
	prev_unlock_status=$?
fi
test "$prev_unlock_status" -eq 75
test ! -e "$verify_marker"
test "$(mutation_snapshot)" = "$before_prev_unlock"
grep -Fq 'another deploy already holds' "$root/prev-unlock.err"
: > "$verify_release"
if ! wait "$first_pid"; then
	printf 'lock-previous deploy failed\n' >&2
	exit 1
fi
first_pid=
test "$(pointer_target)" = lock-previous
printf 'DEPLOY_PREVIOUS_VERIFY_LOCK_FD_NOT_INHERITED=pass\n'

# Default private state location is under the operator HOME when unset.
restore_previous
home_dir=$root/home
mkdir -p "$home_dir/.local/state/codex-commons"
chmod 0755 "$home_dir/.local/state/codex-commons"
rm -f -- "$backup_marker"
: > "$systemctl_log"
HOME=$home_dir \
COMMONS_RELEASE_ROOT=$release_root \
COMMONS_DB=$db \
COMMONS_SYSTEMCTL=$fake_bin/systemctl \
FAKE_SYSTEMCTL_LOG=$systemctl_log \
FAKE_BACKUP_MARKER=$backup_marker \
FAKE_CURRENT=$current \
/bin/sh "$deploy_script" "$release_root/candidate-two" \
	>"$root/default-state.out" 2>"$root/default-state.err"
test "$(pointer_target)" = candidate-two
default_dir=$home_dir/.local/state/codex-commons/deploy
default_receipt=$default_dir/deployment-attempt
test -d "$default_dir"
test ! -L "$default_dir"
test "$(stat -c %a "$default_dir")" = 700
test "$(stat -c %u "$default_dir")" = "$(id -u)"
test "$(stat -c %g "$default_dir")" = "$(id -g)"
test -f "$default_receipt"
test ! -L "$default_receipt"
test "$(stat -c %a "$default_receipt")" = 600
test "$(stat -c %u "$default_receipt")" = "$(id -u)"
test "$(stat -c %g "$default_receipt")" = "$(id -g)"
test ! -e "$default_dir/deployment-attempt.sha256"
grep -Fxq 'kind=deployment-attempt' "$default_receipt"
grep -Fxq 'previous_state=validated' "$default_receipt"
grep -Fxq 'previous_id=previous' "$default_receipt"
printf 'DEPLOY_DEFAULT_STATE_DIR=pass\n'

printf 'PHASE4_PR3_PREVIOUS_TARGET_RECEIPT=pass\n'
