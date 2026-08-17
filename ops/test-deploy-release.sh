#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
deploy_script=$repo_root/ops/deploy-release.sh
test -f "$deploy_script"

root=$(mktemp -d)
first_pid=
cleanup() {
	if [ -n "${first_pid:-}" ]; then
		if kill -0 "$first_pid" 2>/dev/null; then
			if [ -n "${fake_systemctl_release:-}" ]; then
				: > "$fake_systemctl_release"
			fi
			kill "$first_pid" 2>/dev/null || true
		fi
		wait "$first_pid" 2>/dev/null || true
	fi
	chmod -R u+w "$root" 2>/dev/null || true
	rm -rf -- "$root"
}
trap cleanup 0 1 2 15

release_root=$root/releases
mkdir -p "$release_root"

make_release() {
	release_id=$1
	release_dir=$release_root/$release_id
	mkdir -p "$release_dir/ops"
	printf '%s\n' "$release_id" > "$release_dir/VERSION"
	cat > "$release_dir/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${FAKE_VERIFY_MARKER:-}" ]; then
	: > "$FAKE_VERIFY_MARKER"
fi
if [ "${FAKE_VERIFY_RESULT:-0}" -ne 0 ]; then
	exit "$FAKE_VERIFY_RESULT"
fi
EOF
	cat > "$release_dir/ops/check-readiness.sh" <<'EOF'
#!/bin/sh
set -eu
test "$COMMONS_RELEASE_IDENTITY_FILE" = "$COMMONS_RELEASE_DIR/VERSION"
test "$COMMONS_CODEX_BIN" = "$COMMONS_RELEASE_DIR/bin/codex"
test "$COMMONS_WEB_DIR" = "$COMMONS_RELEASE_DIR/web"
if [ "${FAKE_READINESS_RESULT:-0}" -ne 0 ]; then
	exit "$FAKE_READINESS_RESULT"
fi
EOF
	cat > "$release_dir/ops/backup.sh" <<'EOF'
#!/bin/sh
set -eu
mkdir -p "$COMMONS_BACKUP_DIR/daily"
backup=$COMMONS_BACKUP_DIR/daily/commons-test.sqlite3
cp -- "$COMMONS_DB" "$backup"
EOF
	cat > "$release_dir/ops/verify-restore.sh" <<'EOF'
#!/bin/sh
set -eu
test -f "$1"
EOF
	chmod 0555 "$release_dir" "$release_dir/ops"
	chmod 0444 "$release_dir/VERSION" "$release_dir/ops"/*.sh
}

make_release previous
make_release candidate-one
make_release candidate-two
make_release blocked-candidate

current=$release_root/current
ln -s previous "$current"
# This stale path belongs to an earlier transaction and must survive every
# later forward/rollback switch.
stale_pointer=$release_root/.current.next
printf 'stale-pointer\n' > "$stale_pointer"
stale_digest=$(sha256sum "$stale_pointer" | awk '{print $1}')
stale_unique_pointer=$release_root/.current.next.ABCDEF
printf 'stale-unique-pointer\n' > "$stale_unique_pointer"
stale_unique_digest=$(sha256sum "$stale_unique_pointer" | awk '{print $1}')

fake_systemctl=$root/fake-systemctl
fake_systemctl_log=$root/systemctl.log
fake_systemctl_started=$root/systemctl.started
fake_systemctl_release=$root/systemctl.release
cat > "$fake_systemctl" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FAKE_SYSTEMCTL_LOG"
if [ "${2:-}" = restart ]; then
	case "${FAKE_SYSTEMCTL_RESTART:-pass}" in
	block)
		: > "$FAKE_SYSTEMCTL_STARTED"
		while [ ! -f "$FAKE_SYSTEMCTL_RELEASE" ]; do
			sleep 0.01
		done
		;;
	fail)
		exit 1
		;;
	pass)
		;;
	*)
		exit 2
		;;
	esac
fi
EOF
chmod 0555 "$fake_systemctl"
: > "$fake_systemctl_log"

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

temp_paths() {
	find "$release_root" -maxdepth 1 \( -type l -o -type f \) -name '.current.next.*' -printf '%f\n' | LC_ALL=C sort
}

state_snapshot() {
	printf 'current=%s\n' "$(pointer_target)"
	printf 'stale=%s\n' "$(sha256sum "$stale_pointer" | awk '{print $1}')"
	printf 'stale-unique=%s\n' "$(sha256sum "$stale_unique_pointer" | awk '{print $1}')"
	printf 'systemctl=%s\n' "$(sha256sum "$fake_systemctl_log" | awk '{print $1}')"
	printf 'transaction-temp-paths=%s\n' "$(temp_paths)"
}

deploy_env() {
	release_root_arg=$1
	candidate=$2
	restart_mode=${3:-pass}
	readiness_result=${4:-0}
	COMMONS_RELEASE_ROOT=$release_root_arg \
	COMMONS_DB=$root/missing.sqlite3 \
	COMMONS_SYSTEMCTL=$fake_systemctl \
	FAKE_SYSTEMCTL_LOG=$fake_systemctl_log \
	FAKE_SYSTEMCTL_STARTED=$fake_systemctl_started \
	FAKE_SYSTEMCTL_RELEASE=$fake_systemctl_release \
	FAKE_SYSTEMCTL_RESTART=$restart_mode \
	FAKE_READINESS_RESULT=$readiness_result \
	/bin/sh "$deploy_script" "$candidate"
}

# A blocked transaction switches current and then holds the canonical root
# lock in the fake restart. An alias of that root must contend on the same FD
# lock, before the blocked candidate's verifier is invoked.
rm -f -- "$fake_systemctl_started" "$fake_systemctl_release"
deploy_env "$release_root" "$release_root/candidate-one" block 0 &
first_pid=$!
wait_for_file "$fake_systemctl_started"
test "$(pointer_target)" = candidate-one
before_contention=$(state_snapshot)
blocked_verify_marker=$root/blocked-verifier-ran
alias_root=$root/release-root-alias
ln -s "$release_root" "$alias_root"
if COMMONS_RELEASE_ROOT="$alias_root" \
	COMMONS_DB=$root/missing.sqlite3 \
	COMMONS_SYSTEMCTL=$fake_systemctl \
	FAKE_SYSTEMCTL_LOG=$fake_systemctl_log \
	FAKE_SYSTEMCTL_STARTED=$fake_systemctl_started \
	FAKE_SYSTEMCTL_RELEASE=$fake_systemctl_release \
	FAKE_VERIFY_MARKER=$blocked_verify_marker \
	/bin/sh "$deploy_script" "$release_root/blocked-candidate" \
	>"$root/contention.out" 2>&1; then
	printf 'contention unexpectedly succeeded\n' >&2
	exit 1
else
	contention_status=$?
fi
test "$contention_status" -eq 75
test ! -e "$blocked_verify_marker"
test "$(state_snapshot)" = "$before_contention"

touch "$fake_systemctl_release"
if ! wait "$first_pid"; then
	printf 'blocked success transaction failed\n' >&2
	exit 1
fi
first_pid=
test "$(pointer_target)" = candidate-one
test -f "$stale_pointer"
test "$(sha256sum "$stale_pointer" | awk '{print $1}')" = "$stale_digest"
test -f "$stale_unique_pointer"
test "$(sha256sum "$stale_unique_pointer" | awk '{print $1}')" = "$stale_unique_digest"
test "$(temp_paths)" = .current.next.ABCDEF

# A failed readiness check must atomically restore the previous release. The
# forward and rollback transactions each get their own temporary pathname;
# neither may remove the stale unqualified path above.
if deploy_env "$release_root" "$release_root/candidate-two" pass 1; then
	printf 'rollback transaction unexpectedly succeeded\n' >&2
	exit 1
fi
test "$(pointer_target)" = candidate-one
test -f "$stale_pointer"
test "$(sha256sum "$stale_pointer" | awk '{print $1}')" = "$stale_digest"
test -f "$stale_unique_pointer"
test "$(sha256sum "$stale_unique_pointer" | awk '{print $1}')" = "$stale_unique_digest"
test "$(temp_paths)" = .current.next.ABCDEF
grep -Fq -- '--user restart codex-commons.service' "$fake_systemctl_log"
grep -Fq -- '--user stop codex-commons.service' "$fake_systemctl_log"

# Any existing current entry must be one symlink that resolves to a direct
# child release directory. Malformed and dangling state fails before candidate
# verification or service control instead of being treated as a first deploy.
assert_invalid_current_rejected() {
	case_name=$1
	before_log_digest=$(sha256sum "$fake_systemctl_log" | awk '{print $1}')
	invalid_verify_marker=$root/invalid-current-$case_name-verifier-ran
	if COMMONS_RELEASE_ROOT="$release_root" \
		COMMONS_DB=$root/missing.sqlite3 \
		COMMONS_SYSTEMCTL=$fake_systemctl \
		FAKE_SYSTEMCTL_LOG=$fake_systemctl_log \
		FAKE_VERIFY_MARKER=$invalid_verify_marker \
		/bin/sh "$deploy_script" "$release_root/candidate-one" \
		>"$root/invalid-current-$case_name.out" 2>&1; then
		printf 'invalid current case %s unexpectedly succeeded\n' "$case_name" >&2
		exit 1
	else
		invalid_status=$?
	fi
	test "$invalid_status" -eq 78
	test ! -e "$invalid_verify_marker"
	test "$(sha256sum "$fake_systemctl_log" | awk '{print $1}')" = "$before_log_digest"
}

rm -f -- "$current"
ln -s missing-release "$current"
assert_invalid_current_rejected dangling

rm -f -- "$current"
printf 'not-a-pointer\n' > "$current"
assert_invalid_current_rejected regular-file

rm -f -- "$current"
mkdir "$current"
assert_invalid_current_rejected directory
rmdir "$current"

outside_release=$root/outside-release
mkdir "$outside_release"
ln -s "$outside_release" "$current"
assert_invalid_current_rejected outside-root
rm -f -- "$current"
ln -s candidate-one "$current"

# A failed first deployment has no rollback release. It must remove the
# candidate pointer and every transaction-owned temporary path while leaving
# unrelated stale paths untouched.
rm -f -- "$current"
if deploy_env "$release_root" "$release_root/candidate-two" pass 1; then
	printf 'first-deploy rollback unexpectedly succeeded\n' >&2
	exit 1
fi
test ! -e "$current"
test ! -L "$current"
test -f "$stale_pointer"
test "$(sha256sum "$stale_pointer" | awk '{print $1}')" = "$stale_digest"
test -f "$stale_unique_pointer"
test "$(sha256sum "$stale_unique_pointer" | awk '{print $1}')" = "$stale_unique_digest"
test "$(temp_paths)" = .current.next.ABCDEF
ln -s candidate-one "$current"

# The release root must be absolute. This check is before any candidate
# verification and does not mutate the disposable root.
if COMMONS_RELEASE_ROOT=relative-root \
	COMMONS_DB=$root/missing.sqlite3 \
	COMMONS_SYSTEMCTL=$fake_systemctl \
	/bin/sh "$deploy_script" "$release_root/candidate-one" \
	>"$root/relative-root.out" 2>&1; then
	printf 'relative release root unexpectedly accepted\n' >&2
	exit 1
fi

printf 'DEPLOY_LOCK_CONTENTION=pass\n'
printf 'DEPLOY_POINTER_TRANSACTION=pass\n'
printf 'DEPLOY_ROLLBACK=pass\n'
printf 'DEPLOY_CANONICAL_ALIAS_LOCK=pass\n'
printf 'DEPLOY_INVALID_CURRENT_FAIL_CLOSED=pass\n'
printf 'DEPLOY_FIRST_RELEASE_ROLLBACK=pass\n'
