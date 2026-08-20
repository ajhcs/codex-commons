#!/bin/sh
set -eu

# Phase 4 PR 2: offline exact-launcher fixtures. Disposable directories and
# fake verify/server commands only. This suite never starts a service, binds a
# listener, or touches a live release pointer or database.
repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
launcher=$repo_root/ops/commons-launch.sh
service_unit=$repo_root/deploy/systemd/codex-commons.service
env_example=$repo_root/deploy/systemd/dogfood.env.example
runbook=$repo_root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md
stage_script=$repo_root/ops/stage-release.sh
test -f "$launcher"
test -f "$service_unit"
test -f "$env_example"
test -f "$runbook"
test -f "$stage_script"

grep -Fq 'set -eu' "$launcher"
if grep -Eq '(^|[[:space:]])eval[[:space:]]' "$launcher"; then
	printf 'commons-launch.sh must not use eval\n' >&2
	exit 1
fi
test "$(grep -c 'readlink -- "$current"' "$launcher")" -eq 1
if grep -Fq 'readlink -f "$current"' "$launcher"; then
	printf 'commons-launch.sh must not re-resolve current\n' >&2
	exit 1
fi
grep -Fq '/bin/sh "$COMMONS_RELEASE_DIR/ops/verify-release.sh"' "$launcher"
grep -Fq 'cd -- "$COMMONS_RELEASE_DIR"' "$launcher"
grep -Fq 'exec "$COMMONS_RELEASE_DIR/commons-server" "$@"' "$launcher"
grep -Fq 'COMMONS_WEB_DIR="$COMMONS_RELEASE_DIR/web"' "$launcher"
grep -Fq 'COMMONS_CODEX_BIN="$COMMONS_RELEASE_DIR/bin/codex"' "$launcher"
grep -Fq 'COMMONS_RELEASE_IDENTITY_FILE="$COMMONS_RELEASE_DIR/VERSION"' "$launcher"

grep -Fq 'ExecStart=/bin/sh %h/.local/libexec/codex-commons/commons-launch.sh' "$service_unit"
if grep -Eq '^(WorkingDirectory|ExecStartPre|ExecStart)=.*current' "$service_unit"; then
	printf 'systemd unit still starts through mutable current\n' >&2
	exit 1
fi
if grep -Eq '^ExecStartPre=' "$service_unit"; then
	printf 'systemd unit must not use ExecStartPre\n' >&2
	exit 1
fi
if grep -Eq '^WorkingDirectory=' "$service_unit"; then
	printf 'systemd unit must not set WorkingDirectory\n' >&2
	exit 1
fi
grep -Fq 'ops/test-launch.sh' "$runbook"
grep -Fq 'ops/commons-launch.sh' "$runbook"
grep -Fq '~/.local/libexec/codex-commons/commons-launch.sh' "$runbook"
grep -Fq 'resolves `$COMMONS_RELEASE_ROOT/current` exactly once' "$runbook"
if grep -Fq 'commons-launch.sh' "$stage_script"; then
	printf 'stable launcher must not be packaged into release directories\n' >&2
	exit 1
fi
if grep -Fq systemctl "$launcher"; then
	printf 'commons-launch.sh must not call systemctl\n' >&2
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

release_root=$root/releases
mkdir -p "$release_root"
current=$release_root/current
verify_log=$root/verify.log
server_log=$root/server.log
verify_started=$root/verify.started
verify_release=$root/verify.release

make_release() {
	release_id=$1
	release_dir=$release_root/$release_id
	mkdir -p "$release_dir/ops" "$release_dir/web" "$release_dir/bin"
	printf '%s\n' "$release_id" > "$release_dir/VERSION"
	printf 'web\n' > "$release_dir/web/index.html"
	printf 'codex\n' > "$release_dir/bin/codex"
	cat > "$release_dir/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${FAKE_VERIFY_LOG:-}" ]; then
	{
		printf 'verify_release_dir=%s\n' "$COMMONS_RELEASE_DIR"
		printf 'verify_web_dir=%s\n' "$COMMONS_WEB_DIR"
		printf 'verify_codex_bin=%s\n' "$COMMONS_CODEX_BIN"
		printf 'verify_identity=%s\n' "$COMMONS_RELEASE_IDENTITY_FILE"
		printf 'verify_root=%s\n' "$COMMONS_RELEASE_ROOT"
		printf 'verify_pwd=%s\n' "$(pwd)"
	} > "$FAKE_VERIFY_LOG"
fi
if [ -n "${FAKE_VERIFY_STARTED:-}" ]; then
	: > "$FAKE_VERIFY_STARTED"
fi
if [ -n "${FAKE_VERIFY_RELEASE:-}" ]; then
	while [ ! -f "$FAKE_VERIFY_RELEASE" ]; do
		sleep 0.01
	done
fi
if [ -n "${FAKE_VERIFY_SWAP_TO:-}" ]; then
	rm -f -- "$COMMONS_RELEASE_ROOT/current"
	ln -s "$FAKE_VERIFY_SWAP_TO" "$COMMONS_RELEASE_ROOT/current"
fi
if [ -n "${FAKE_VERIFY_STATUS:-}" ]; then
	exit "$FAKE_VERIFY_STATUS"
fi
EOF
	cat > "$release_dir/commons-server" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${FAKE_SERVER_LOG:-}" ]; then
	{
		printf 'server_path=%s\n' "$0"
		printf 'server_pwd=%s\n' "$(pwd)"
		printf 'server_pwd_physical=%s\n' "$(pwd -P)"
		printf 'server_release_dir=%s\n' "$COMMONS_RELEASE_DIR"
		printf 'server_web_dir=%s\n' "$COMMONS_WEB_DIR"
		printf 'server_codex_bin=%s\n' "$COMMONS_CODEX_BIN"
		printf 'server_identity=%s\n' "$COMMONS_RELEASE_IDENTITY_FILE"
		printf 'server_version=%s\n' "$(sed -n '1p' "$COMMONS_RELEASE_DIR/VERSION")"
	} > "$FAKE_SERVER_LOG"
fi
if [ -n "${FAKE_SERVER_STATUS:-}" ]; then
	exit "$FAKE_SERVER_STATUS"
fi
exit 0
EOF
	chmod 0755 "$release_dir/commons-server"
}

wait_for_file() {
	path=$1
	attempt=0
	while [ ! -e "$path" ]; do
		attempt=$((attempt + 1))
		test "$attempt" -lt 500
		sleep 0.01
	done
}

reset_logs() {
	rm -f -- "$verify_log" "$server_log" "$verify_started" "$verify_release"
}

assert_pinned() {
	pinned_id=$1
	pinned_dir=$release_root/$pinned_id
	test -f "$verify_log"
	test -f "$server_log"
	grep -Fxq "verify_release_dir=$pinned_dir" "$verify_log"
	grep -Fxq "verify_web_dir=$pinned_dir/web" "$verify_log"
	grep -Fxq "verify_codex_bin=$pinned_dir/bin/codex" "$verify_log"
	grep -Fxq "verify_identity=$pinned_dir/VERSION" "$verify_log"
	grep -Fxq "verify_root=$release_root" "$verify_log"
	grep -Fxq "server_path=$pinned_dir/commons-server" "$server_log"
	grep -Fxq "server_pwd=$pinned_dir" "$server_log"
	grep -Fxq "server_pwd_physical=$pinned_dir" "$server_log"
	grep -Fxq "server_release_dir=$pinned_dir" "$server_log"
	grep -Fxq "server_web_dir=$pinned_dir/web" "$server_log"
	grep -Fxq "server_codex_bin=$pinned_dir/bin/codex" "$server_log"
	grep -Fxq "server_identity=$pinned_dir/VERSION" "$server_log"
	grep -Fxq "server_version=$pinned_id" "$server_log"
}

run_launch() {
	COMMONS_RELEASE_ROOT=$release_root \
	FAKE_VERIFY_LOG=$verify_log \
	FAKE_SERVER_LOG=$server_log \
	/bin/sh "$launcher"
}

expect_launch_failure() {
	case_name=$1
	reset_logs
	if COMMONS_RELEASE_ROOT=$release_root \
		FAKE_VERIFY_LOG=$verify_log \
		FAKE_SERVER_LOG=$server_log \
		/bin/sh "$launcher" \
		>"$root/fail-$case_name.out" 2>"$root/fail-$case_name.err"; then
		printf 'launch unexpectedly succeeded: %s\n' "$case_name" >&2
		exit 1
	else
		status=$?
	fi
	test "$status" -eq 64
	test ! -e "$server_log"
	printf 'REJECTED %s\n' "$case_name"
}

make_release release-a
make_release release-b
ln -s release-a "$current"

# Env-file-shaped current paths must be overwritten with the exact pin.
reset_logs
COMMONS_RELEASE_ROOT=$release_root \
COMMONS_RELEASE_DIR=$current \
COMMONS_WEB_DIR=$current/web \
COMMONS_CODEX_BIN=$current/bin/codex \
COMMONS_RELEASE_IDENTITY_FILE=$current/VERSION \
FAKE_VERIFY_LOG=$verify_log \
FAKE_SERVER_LOG=$server_log \
/bin/sh "$launcher"
assert_pinned release-a
test "$(readlink "$current")" = release-a
printf 'LAUNCH_ENV_OVERWRITE=pass\n'

# Swap current during verification; verify/chdir/exec stay on the pin.
reset_logs
COMMONS_RELEASE_ROOT=$release_root \
FAKE_VERIFY_LOG=$verify_log \
FAKE_SERVER_LOG=$server_log \
FAKE_VERIFY_SWAP_TO=release-b \
/bin/sh "$launcher"
assert_pinned release-a
test "$(readlink "$current")" = release-b
printf 'LAUNCH_PIN_SWAP_DURING_VERIFY=pass\n'

rm -f -- "$current"
ln -s release-a "$current"

# Swap current after resolution while verify is held.
reset_logs
COMMONS_RELEASE_ROOT=$release_root \
FAKE_VERIFY_LOG=$verify_log \
FAKE_SERVER_LOG=$server_log \
FAKE_VERIFY_STARTED=$verify_started \
FAKE_VERIFY_RELEASE=$verify_release \
/bin/sh "$launcher" \
	>"$root/held.out" 2>"$root/held.err" &
first_pid=$!
wait_for_file "$verify_started"
test "$(readlink "$current")" = release-a
rm -f -- "$current"
ln -s release-b "$current"
: > "$verify_release"
if ! wait "$first_pid"; then
	printf 'held launch failed\n' >&2
	exit 1
fi
first_pid=
assert_pinned release-a
test "$(readlink "$current")" = release-b
printf 'LAUNCH_PIN_SWAP_WHILE_HELD=pass\n'

rm -f -- "$current"
ln -s release-a "$current"

# Whitespace in the release-root path is quoted through pin/verify/chdir/exec.
ws_root=$root/release\ root
mkdir -p "$ws_root"
ws_root=$(readlink -f "$ws_root")
ws_current=$ws_root/current
release_root=$ws_root
make_release ws-one
make_release ws-two
ln -s ws-one "$ws_current"
reset_logs
COMMONS_RELEASE_ROOT=$ws_root \
COMMONS_RELEASE_DIR=$ws_current \
COMMONS_WEB_DIR=$ws_current/web \
COMMONS_CODEX_BIN=$ws_current/bin/codex \
FAKE_VERIFY_LOG=$verify_log \
FAKE_SERVER_LOG=$server_log \
FAKE_VERIFY_SWAP_TO=ws-two \
/bin/sh "$launcher"
assert_pinned ws-one
test "$(readlink "$ws_current")" = ws-two
printf 'LAUNCH_WHITESPACE_ROOT_PIN=pass\n'

release_root=$root/releases
current=$release_root/current
rm -f -- "$current"
ln -s release-a "$current"

# Verifier failure is propagated and does not exec the server.
reset_logs
if COMMONS_RELEASE_ROOT=$release_root \
	FAKE_VERIFY_LOG=$verify_log \
	FAKE_SERVER_LOG=$server_log \
	FAKE_VERIFY_STATUS=17 \
	/bin/sh "$launcher" \
	>"$root/verify-fail.out" 2>"$root/verify-fail.err"; then
	printf 'launch ignored verifier failure\n' >&2
	exit 1
else
	verify_fail_status=$?
fi
test "$verify_fail_status" -eq 17
test -f "$verify_log"
test ! -e "$server_log"
printf 'LAUNCH_VERIFY_FAILURE=pass\n'

# Server failure is propagated through exec.
reset_logs
if COMMONS_RELEASE_ROOT=$release_root \
	FAKE_VERIFY_LOG=$verify_log \
	FAKE_SERVER_LOG=$server_log \
	FAKE_SERVER_STATUS=23 \
	/bin/sh "$launcher" \
	>"$root/server-fail.out" 2>"$root/server-fail.err"; then
	printf 'launch ignored server failure\n' >&2
	exit 1
else
	server_fail_status=$?
fi
test "$server_fail_status" -eq 23
assert_pinned release-a
printf 'LAUNCH_SERVER_FAILURE=pass\n'

# Missing, non-directory, nested, outside, traversal, symlink, and whitespace
# pointer targets fail closed before verify/exec.
rm -f -- "$current"
expect_launch_failure missing-current

mkdir "$current"
expect_launch_failure current-is-directory
rmdir "$current"

printf not-a-release > "$current"
expect_launch_failure current-is-regular-file
rm -f -- "$current"

ln -s release-a "$current"
mkdir -p "$release_root/nested/child/ops" "$release_root/nested/child/web" "$release_root/nested/child/bin"
printf nested > "$release_root/nested/child/VERSION"
printf '#!/bin/sh\nexit 0\n' > "$release_root/nested/child/commons-server"
chmod 0755 "$release_root/nested/child/commons-server"
printf '#!/bin/sh\nexit 0\n' > "$release_root/nested/child/ops/verify-release.sh"
rm -f -- "$current"
ln -s nested/child "$current"
expect_launch_failure nested-target

rm -f -- "$current"
ln -s ../outside "$current"
expect_launch_failure outside-relative

outside=$root/outside-release
mkdir -p "$outside"
rm -f -- "$current"
ln -s "$outside" "$current"
expect_launch_failure outside-absolute

rm -f -- "$current"
ln -s ../releases/release-a "$current"
expect_launch_failure traversal-target

rm -f -- "$current"
ln -s ./release-a "$current"
expect_launch_failure dot-slash-target

rm -f -- "$current"
ln -s . "$current"
expect_launch_failure dot-target

rm -f -- "$current"
ln -s .. "$current"
expect_launch_failure dot-dot-target

rm -f -- "$current"
ln -s missing-release "$current"
expect_launch_failure missing-target

printf not-a-dir > "$release_root/not-a-dir"
rm -f -- "$current"
ln -s not-a-dir "$current"
expect_launch_failure non-directory-target
rm -f -- "$release_root/not-a-dir"

ln -s release-a "$release_root/alias"
rm -f -- "$current"
ln -s alias "$current"
expect_launch_failure symlink-shaped-target
rm -f -- "$release_root/alias"

rm -f -- "$current"
ln -s 'release a' "$current"
expect_launch_failure whitespace-target

# Restore a valid pointer and prove a missing verifier fails closed.
rm -f -- "$current"
ln -s release-a "$current"
chmod u+w "$release_root/release-a/ops"
rm -f -- "$release_root/release-a/ops/verify-release.sh"
expect_launch_failure missing-verify-script
cat > "$release_root/release-a/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${FAKE_VERIFY_LOG:-}" ]; then
	{
		printf 'verify_release_dir=%s\n' "$COMMONS_RELEASE_DIR"
		printf 'verify_web_dir=%s\n' "$COMMONS_WEB_DIR"
		printf 'verify_codex_bin=%s\n' "$COMMONS_CODEX_BIN"
		printf 'verify_identity=%s\n' "$COMMONS_RELEASE_IDENTITY_FILE"
		printf 'verify_root=%s\n' "$COMMONS_RELEASE_ROOT"
		printf 'verify_pwd=%s\n' "$(pwd)"
	} > "$FAKE_VERIFY_LOG"
fi
EOF

mv "$release_root/release-a/ops/verify-release.sh" "$root/verify-real.sh"
ln -s "$root/verify-real.sh" "$release_root/release-a/ops/verify-release.sh"
expect_launch_failure symlink-verify-script
rm -f -- "$release_root/release-a/ops/verify-release.sh"
mv "$root/verify-real.sh" "$release_root/release-a/ops/verify-release.sh"

chmod u+w "$release_root/release-a"
rm -f -- "$release_root/release-a/commons-server"
expect_launch_failure missing-server
cat > "$release_root/release-a/commons-server" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${FAKE_SERVER_LOG:-}" ]; then
	{
		printf 'server_path=%s\n' "$0"
		printf 'server_pwd=%s\n' "$(pwd)"
		printf 'server_pwd_physical=%s\n' "$(pwd -P)"
		printf 'server_release_dir=%s\n' "$COMMONS_RELEASE_DIR"
		printf 'server_web_dir=%s\n' "$COMMONS_WEB_DIR"
		printf 'server_codex_bin=%s\n' "$COMMONS_CODEX_BIN"
		printf 'server_identity=%s\n' "$COMMONS_RELEASE_IDENTITY_FILE"
		printf 'server_version=%s\n' "$(sed -n '1p' "$COMMONS_RELEASE_DIR/VERSION")"
	} > "$FAKE_SERVER_LOG"
fi
exit 0
EOF
chmod 0755 "$release_root/release-a/commons-server"

# Final known-good launch after the negative matrix.
reset_logs
run_launch
assert_pinned release-a
test "$(readlink "$current")" = release-a
printf 'LAUNCH_FINAL_PIN=pass\n'
printf 'PHASE4_PR2_EXACT_LAUNCHER=pass\n'
