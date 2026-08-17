#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
launcher=$repo_root/deploy/bin/codex-commons-launch
installer=$repo_root/ops/install-launcher.sh
test -f "$launcher"
test -f "$installer"

root=$(mktemp -d)
cleanup() {
	rm -rf -- "$root"
}
trap cleanup 0 1 2 15

release_root=$root/releases
release_one=$release_root/release-one
release_two=$release_root/release-two
mkdir -p "$release_one/ops" "$release_one/bin" "$release_one/web"
mkdir -p "$release_two/ops" "$release_two/bin" "$release_two/web"
printf '%s\n' release-one > "$release_one/VERSION"
printf '%s\n' release-two > "$release_two/VERSION"

cat > "$release_one/ops/verify-release.sh" <<'EOF'
#!/bin/sh
set -eu
test "$COMMONS_RELEASE_DIR" = "$LAUNCH_TEST_RELEASE_ONE"
test "$COMMONS_RELEASE_IDENTITY_FILE" = "$LAUNCH_TEST_RELEASE_ONE/VERSION"
test "$COMMONS_CODEX_BIN" = "$LAUNCH_TEST_RELEASE_ONE/bin/codex"
test "$COMMONS_WEB_DIR" = "$LAUNCH_TEST_RELEASE_ONE/web"
ln -s release-two "$COMMONS_RELEASE_ROOT/.current.swap"
mv -Tf -- "$COMMONS_RELEASE_ROOT/.current.swap" "$COMMONS_RELEASE_ROOT/current"
printf '%s\n' verified-release-one >> "$LAUNCH_TEST_LOG"
EOF
cat > "$release_one/commons-server" <<'EOF'
#!/bin/sh
set -eu
test "$COMMONS_RELEASE_DIR" = "$LAUNCH_TEST_RELEASE_ONE"
test "$COMMONS_RELEASE_IDENTITY_FILE" = "$LAUNCH_TEST_RELEASE_ONE/VERSION"
test "$COMMONS_CODEX_BIN" = "$LAUNCH_TEST_RELEASE_ONE/bin/codex"
test "$COMMONS_WEB_DIR" = "$LAUNCH_TEST_RELEASE_ONE/web"
test "$PWD" = "$LAUNCH_TEST_RELEASE_ONE"
test "$#" -eq 2
test "$1" = first
test "$2" = second
printf '%s\n' executed-release-one >> "$LAUNCH_TEST_LOG"
EOF
cat > "$release_two/ops/verify-release.sh" <<'EOF'
#!/bin/sh
printf '%s\n' verifier-failed-release-two >> "$LAUNCH_TEST_LOG"
exit 91
EOF
cat > "$release_two/commons-server" <<'EOF'
#!/bin/sh
printf '%s\n' server-ran-after-failed-verifier >> "$LAUNCH_TEST_LOG"
exit 92
EOF
chmod 0555 "$release_one/commons-server" "$release_two/commons-server"

log=$root/launch.log
ln -s release-one "$release_root/current"
COMMONS_RELEASE_ROOT=$release_root \
	COMMONS_RELEASE_DIR=/poison/release \
	COMMONS_RELEASE_IDENTITY_FILE=/poison/VERSION \
	COMMONS_CODEX_BIN=/poison/codex \
	COMMONS_WEB_DIR=/poison/web \
	PATH=/poison \
	LAUNCH_TEST_RELEASE_ONE=$release_one \
	LAUNCH_TEST_LOG=$log \
	/bin/sh "$launcher" first second

test "$(readlink "$release_root/current")" = release-two
test "$(sed -n '1p' "$log")" = verified-release-one
test "$(sed -n '2p' "$log")" = executed-release-one
test "$(wc -l < "$log")" -eq 2

# A verifier failure is terminal and must never reach the candidate server.
: > "$log"
if COMMONS_RELEASE_ROOT=$release_root \
	LAUNCH_TEST_RELEASE_ONE=$release_one \
	LAUNCH_TEST_LOG=$log \
	/bin/sh "$launcher"; then
	echo 'launcher ignored verifier failure' >&2
	exit 1
fi
test "$(cat "$log")" = verifier-failed-release-two

assert_rejected() {
	label=$1
	before_log=$(sha256sum "$log" | awk '{print $1}')
	if COMMONS_RELEASE_ROOT=$release_root \
		LAUNCH_TEST_RELEASE_ONE=$release_one \
		LAUNCH_TEST_LOG=$log \
		/bin/sh "$launcher" >"$root/$label.out" 2>&1; then
		printf 'launcher accepted invalid current: %s\n' "$label" >&2
		exit 1
	fi
	test "$(sha256sum "$log" | awk '{print $1}')" = "$before_log"
}

install_invalid_probe() {
	probe_dir=$1
	mkdir -p "$probe_dir/ops"
	cat > "$probe_dir/ops/verify-release.sh" <<'EOF'
#!/bin/sh
printf '%s\n' invalid-target-verifier-ran >> "$LAUNCH_TEST_LOG"
EOF
	cat > "$probe_dir/commons-server" <<'EOF'
#!/bin/sh
printf '%s\n' invalid-target-server-ran >> "$LAUNCH_TEST_LOG"
EOF
	chmod 0555 "$probe_dir/commons-server"
}

rm -f -- "$release_root/current"
assert_rejected missing
ln -s missing-release "$release_root/current"
assert_rejected dangling
rm -f -- "$release_root/current"
printf 'not a symlink\n' > "$release_root/current"
assert_rejected regular-file
rm -f -- "$release_root/current"
outside=$root/outside-release
install_invalid_probe "$outside"
ln -s "$outside" "$release_root/current"
assert_rejected outside-root
rm -f -- "$release_root/current"
install_invalid_probe "$release_root/nested/release"
ln -s nested/release "$release_root/current"
assert_rejected nested-release
rm -f -- "$release_root/current"
install_invalid_probe "$release_root"
ln -s . "$release_root/current"
assert_rejected release-root
rm -f -- "$release_root/current"
prefix_collision=$root/releases-elsewhere
install_invalid_probe "$prefix_collision"
ln -s "$prefix_collision" "$release_root/current"
assert_rejected prefix-collision

# The reviewed source is installed to a stable path through a same-directory
# temporary file and atomic rename. The helper is tested only in this fixture;
# it does not touch the host installation.
installed=$root/libexec/codex-commons-launch
/bin/sh "$installer" "$installed"
test -f "$installed"
test ! -L "$installed"
test "$(stat -c %a "$installed")" = 555
cmp -s "$launcher" "$installed"
test -z "$(find "$root/libexec" -maxdepth 1 -name '.codex-commons-launch.*' -print -quit)"

printf '%s\n' 'PINNED_LAUNCHER=pass'
