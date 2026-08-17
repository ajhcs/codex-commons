#!/bin/sh
set -eu

# Phase 1.3 is deliberately self-contained. The exact committed Phase 1
# boundary does not contain the older untracked release builder/runtime tree,
# so all build and staging inputs below live under one disposable root.
repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
stage_script=$repo_root/ops/stage-release.sh
verify_script=$repo_root/ops/verify-release.sh
readiness_script=$repo_root/ops/check-readiness.sh
deploy_test=$repo_root/ops/test-deploy-release.sh
launcher_test=$repo_root/ops/test-launcher.sh
test -f "$stage_script"
test -f "$verify_script"
test -f "$readiness_script"
test -f "$deploy_test"
test -f "$launcher_test"

# Phase 4 deploy locking and current-pointer transactions are exercised only
# against disposable release roots and fake systemctl/readiness scripts. This
# never activates a candidate or touches a live database/service.
sh "$deploy_test"
sh "$launcher_test"

# Keep the Phase 2 deployment contract reviewable without turning this suite
# into a live systemd or runtime test. These assertions cover only source
# artifacts; they never start a service, read an environment file, or switch a
# release pointer.
service_unit=$repo_root/deploy/systemd/codex-commons.service
env_example=$repo_root/deploy/systemd/dogfood.env.example
runbook=$repo_root/deploy/CONTINUOUS_DOGFOOD_RUNBOOK.md
launcher=$repo_root/deploy/bin/codex-commons-launch
launcher_installer=$repo_root/ops/install-launcher.sh
phase2_ledger=$repo_root/docs/phase-2-completion-ledger.md
for source_artifact in "$service_unit" "$env_example" "$runbook" "$launcher" "$launcher_installer" "$phase2_ledger"; do
	test -f "$source_artifact"
done
grep -Fq 'Type=notify' "$service_unit"
grep -Fq 'NotifyAccess=main' "$service_unit"
grep -Eq '^TimeoutStartSec=180$' "$service_unit"
grep -Fq 'WatchdogSec=60' "$service_unit"
grep -Fq 'ExecStart=%h/.local/libexec/codex-commons-launch' "$service_unit"
test "$(grep -c '^ExecStart=' "$service_unit")" -eq 1
! grep -Eq '^(WorkingDirectory|ExecStartPre)=' "$service_unit"
! grep -Fq '/current/' "$service_unit"
grep -Fq 'NoNewPrivileges=true' "$service_unit"
grep -Fq 'PrivateTmp=true' "$service_unit"
grep -Fq 'ProtectSystem=full' "$service_unit"
grep -Fq 'ProtectHome=read-only' "$service_unit"
grep -Fq 'ReadWritePaths=%h/.local/state/codex-commons %h/.codex' "$service_unit"
grep -Fq 'release_dir=$(readlink -f -- "$current")' "$launcher"
grep -Fq 'test "$(dirname -- "$release_dir")" = "$release_root"' "$launcher"
grep -Fq '/bin/sh "$release_dir/ops/verify-release.sh"' "$launcher"
grep -Fq 'exec "$release_dir/commons-server" "$@"' "$launcher"
grep -Fq 'PATH=/usr/bin:/bin' "$launcher"
grep -Fq 'install_temp=$(mktemp "$install_dir/.codex-commons-launch.XXXXXX")' "$launcher_installer"
grep -Fq 'mv -Tf -- "$install_temp" "$target"' "$launcher_installer"
grep -Fq 'COMMONS_RELEASE_ROOT=/home/USER/.local/lib/codex-commons' "$env_example"
! grep -Eq '^COMMONS_(RELEASE_IDENTITY_FILE|RELEASE_DIR|WEB_DIR|CODEX_BIN)=' "$env_example"
grep -Fq 'COMMONS_REQUIRE_CODEX_READY=false' "$env_example"
grep -Fqi 'optional-safe default' "$env_example"
grep -Fq 'runtime_policy_ready' "$readiness_script"
grep -Fq 'optional:false:true|required:true:true' "$readiness_script"
grep -Fq 'if [ "$runtime_required" = false ]' "$readiness_script"
grep -Fq '"service":{"version":"' "$readiness_script"
grep -Fq 'runtime_component_ready' "$readiness_script"
grep -Fq 'healthy:true:true' "$readiness_script"
for phase2_phrase in \
	'COMMONS_REQUIRE_CODEX_READY=true' \
	'READY=1' \
	'WatchdogSec=60' \
	'persistence latch' \
	'NO-GO'; do
	grep -Fq "$phase2_phrase" "$runbook"
done
grep -Fq 'Increment 01' "$phase2_ledger"
grep -Fq 'Increment 13' "$phase2_ledger"
grep -Fq 'Deployment approval' "$phase2_ledger"

root=$(mktemp -d)
cleanup() {
	chmod -R u+w "$root" 2>/dev/null || true
	rm -rf -- "$root"
}
trap cleanup 0 1 2 15

negative_cases=0
restored_cases=0
positive_cases=0

record_positive() {
	label=$1
	positive_cases=$((positive_cases + 1))
	printf 'VALID %s\n' "$label"
}

verify_test_release() {
	COMMONS_RELEASE_ROOT="$release_root" \
	COMMONS_RELEASE_DIR="$release_dir" \
	COMMONS_CODEX_BIN="$release_dir/bin/codex" \
	COMMONS_WEB_DIR="$release_dir/web" \
	COMMONS_CODEX_SHA256="$codex_sha" \
	COMMONS_CODE_MODE_HOST_SHA256="$host_sha" \
	COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" \
	COMMONS_CODEX_ZSH_SHA256="$zsh_sha" \
	COMMONS_CODEX_RG_SHA256="$rg_sha" \
	COMMONS_CODEX_PACKAGE_SHA256="$package_sha" \
	/bin/sh "$release_dir/ops/verify-release.sh"
}

verify_staged_release() {
	staged_root=$1
	staged_dir=$2
	COMMONS_RELEASE_ROOT="$staged_root" \
	COMMONS_RELEASE_DIR="$staged_dir" \
	COMMONS_CODEX_BIN="$staged_dir/bin/codex" \
	COMMONS_WEB_DIR="$staged_dir/web" \
	COMMONS_CODEX_SHA256="$codex_sha" \
	COMMONS_CODE_MODE_HOST_SHA256="$host_sha" \
	COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" \
	COMMONS_CODEX_ZSH_SHA256="$zsh_sha" \
	COMMONS_CODEX_RG_SHA256="$rg_sha" \
	COMMONS_CODEX_PACKAGE_SHA256="$package_sha" \
	/bin/sh "$staged_dir/ops/verify-release.sh"
}

expect_verify_failure() {
	label=$1
	negative_cases=$((negative_cases + 1))
	if verify_test_release >/dev/null 2>&1; then
		printf 'unexpected verifier acceptance: %s\n' "$label" >&2
		exit 1
	fi
	printf 'REJECTED %s\n' "$label"
}

expect_stage_failure() {
	label=$1
	id=$2
	server=$3
	target_root=$4
	web_source=$5
	bundle_source=$6
	negative_cases=$((negative_cases + 1))
	if stage_release "$id" "$server" "$target_root" "$web_source" "$bundle_source" >/dev/null 2>&1; then
		printf 'unexpected staging acceptance: %s\n' "$label" >&2
		exit 1
	fi
	test ! -e "$target_root/$id"
	verify_test_release >/dev/null
	restored_cases=$((restored_cases + 1))
	printf 'REJECTED %s\n' "$label"
	printf 'RESTORED %s\n' "$label"
}

expect_limited_stage_failure() {
	label=$1
	id=$2
	server=$3
	target_root=$4
	web_source=$5
	bundle_source=$6
	negative_cases=$((negative_cases + 1))
	if (
		# VERSION fits, but the server copy exceeds this limit. This forces a
		# deterministic failure after the target directory has been created.
		ulimit -f 1
		stage_release "$id" "$server" "$target_root" "$web_source" "$bundle_source"
	) >/dev/null 2>&1; then
		printf 'unexpected staging acceptance: %s\n' "$label" >&2
		exit 1
	fi
	test ! -e "$target_root/$id"
	verify_test_release >/dev/null
	restored_cases=$((restored_cases + 1))
	printf 'REJECTED %s\n' "$label"
	printf 'RESTORED %s\n' "$label"
}

restore_valid() {
	label=$1
	verify_test_release >/dev/null
	restored_cases=$((restored_cases + 1))
	printf 'RESTORED %s\n' "$label"
}

expect_identity_failure() {
	label=$1
	negative_cases=$((negative_cases + 1))
	if PATH="$fake_id_dir:$PATH" verify_test_release >/dev/null 2>&1; then
		printf 'unexpected verifier acceptance: %s\n' "$label" >&2
		exit 1
	fi
	printf 'REJECTED %s\n' "$label"
	restore_valid "$label"
}

stage_release() {
	id=$1
	server=$2
	target_root=$3
	web_source=$4
	bundle_source=$5
	(
		CDPATH= cd -- "$stage_cwd"
		COMMONS_RELEASE_ROOT="$target_root" \
		COMMONS_SERVER_SOURCE="$server" \
		COMMONS_WEB_SOURCE="$web_source" \
		COMMONS_CODEX_BUNDLE_SOURCE="$bundle_source" \
		/bin/sh "$stage_script" "$id"
	)
}

build_server() {
	id=$1
	out=$2
	mkdir -p "$(dirname "$out")"
	(
		CDPATH= cd -- "$root/server-src"
		go build -trimpath -buildvcs=false -ldflags "-X main.buildID=$id" -o "$out" .
	)
}

check_modes() {
	dir=$1
	test "$(stat -c %a "$dir")" = 555
	test -z "$(find "$dir" -type d \! -perm 0555 -print -quit)"
	test -z "$(find "$dir" -type f \! -perm 0444 \! -perm 0555 -print -quit)"
	test "$(stat -c %a "$dir/VERSION")" = 444
	test "$(stat -c %a "$dir/SHA256SUMS")" = 444
	test "$(stat -c %a "$dir/codex-package.json")" = 444
	test -z "$(find "$dir/web" "$dir/ops" -type f \! -perm 0444 -print -quit)"
	mode_list=$(mktemp "$root/mode-list.XXXXXX")
	actual_list=$(mktemp "$root/actual-list.XXXXXX")
	find "$dir" -type f -perm 0555 -printf '%P\n' | LC_ALL=C sort > "$actual_list"
	printf '%s\n' \
		bin/codex \
		bin/codex-code-mode-host \
		codex-resources/bwrap \
		codex-resources/zsh/bin/zsh \
		codex-path/rg \
		commons-server | LC_ALL=C sort > "$mode_list"
	cmp -s "$mode_list" "$actual_list"
	rm -f -- "$mode_list" "$actual_list"
}

set_manifest_from() {
	source_manifest=$1
	chmod u+w "$manifest"
	cp "$source_manifest" "$manifest"
	chmod 0444 "$manifest"
}

restore_manifest() {
	set_manifest_from "$valid_manifest"
}

mkdir -p "$root/server-src"
printf '%s\n' \
	'module codex-commons/cmd/commons-server' \
	'' \
	'go 1.25.0' > "$root/server-src/go.mod"
printf '%s\n' \
	'package main' \
	'' \
	'import (' \
	'    "fmt"' \
	'    "os"' \
	'    "strings"' \
	')' \
	'' \
	'var buildID = ""' \
	'' \
	'func main() {' \
	'    if len(os.Args) < 2 || os.Args[1] != "--build-id" {' \
	'        return' \
	'    }' \
	'    if path := os.Getenv("COMMONS_RELEASE_IDENTITY_FILE"); path != "" {' \
	'        if data, err := os.ReadFile(path); err == nil {' \
	'            fmt.Print(strings.TrimSpace(string(data)))' \
	'            return' \
	'        }' \
	'    }' \
	'    fmt.Print(buildID)' \
	'}' > "$root/server-src/main.go"

mkdir -p "$root/source-web"
printf '<!doctype html>\n' > "$root/source-web/index.html"

mkdir -p "$root/codex-bundle/bin" "$root/codex-bundle/codex-resources/zsh/bin" "$root/codex-bundle/codex-path"
printf '#!/bin/sh\nprintf "codex-cli 0.147.0\\n"\n' > "$root/codex-bundle/bin/codex"
printf '#!/bin/sh\nexit 0\n' > "$root/codex-bundle/bin/codex-code-mode-host"
printf '#!/bin/sh\nexit 0\n' > "$root/codex-bundle/codex-resources/bwrap"
printf '#!/bin/sh\nexec /bin/sh "$@"\n' > "$root/codex-bundle/codex-resources/zsh/bin/zsh"
printf '#!/bin/sh\nprintf "rg fixture\\n"\n' > "$root/codex-bundle/codex-path/rg"
printf '{}\n' > "$root/codex-bundle/codex-package.json"
chmod 0755 \
	"$root/codex-bundle/bin/codex" \
	"$root/codex-bundle/bin/codex-code-mode-host" \
	"$root/codex-bundle/codex-resources/bwrap" \
	"$root/codex-bundle/codex-resources/zsh/bin/zsh" \
	"$root/codex-bundle/codex-path/rg"

# stage-release.sh copies only these runtime operations files. They are
# harmless disposable fixtures; the test does not package the test harness.
stage_cwd=$root/stage-cwd
mkdir -p "$stage_cwd/ops"
cp "$stage_script" "$stage_cwd/ops/stage-release.sh"
cp "$verify_script" "$stage_cwd/ops/verify-release.sh"
for runtime_op in backup.sh check-readiness.sh deploy-release.sh record-evidence.sh seal-archive.sh verify-restore.sh; do
	printf '#!/bin/sh\nexit 0\n' > "$stage_cwd/ops/$runtime_op"
done

release_root=$root/releases
mkdir -p "$release_root"
build_server release-test "$root/commons-server"
codex_sha=$(sha256sum "$root/codex-bundle/bin/codex" | awk '{print $1}')
host_sha=$(sha256sum "$root/codex-bundle/bin/codex-code-mode-host" | awk '{print $1}')
bwrap_sha=$(sha256sum "$root/codex-bundle/codex-resources/bwrap" | awk '{print $1}')
zsh_sha=$(sha256sum "$root/codex-bundle/codex-resources/zsh/bin/zsh" | awk '{print $1}')
rg_sha=$(sha256sum "$root/codex-bundle/codex-path/rg" | awk '{print $1}')
package_sha=$(sha256sum "$root/codex-bundle/codex-package.json" | awk '{print $1}')
stage_release release-test "$root/commons-server" "$release_root" "$root/source-web" "$root/codex-bundle"
release_dir=$release_root/release-test
# The stable launcher is a separately governed host input, not mutable release
# content. Its installer is likewise unavailable from an activated candidate.
test ! -e "$release_dir/bin/codex-commons-launch"
test ! -e "$release_dir/ops/install-launcher.sh"
manifest=$release_dir/SHA256SUMS
valid_manifest=$root/valid-SHA256SUMS
cp "$manifest" "$valid_manifest"
original_index=$root/original-index.html
cp "$release_dir/web/index.html" "$original_index"

# A correctly staged complete tree is accepted before any negative fixture.
verify_test_release >/dev/null
record_positive known-good-complete-tree

# Exercise the readiness policy against bounded local fixtures. These calls
# use only a disposable SQLite file and fake status commands; no service or
# listener is started. Optional Codex degradation must pass, while required
# mode must prove each Codex runtime component is healthy, ready, and required.
readiness_db=$root/readiness.sqlite3
sqlite3 "$readiness_db" 'CREATE TABLE schema_migrations(version INTEGER); INSERT INTO schema_migrations(version) VALUES (15);'
readiness_systemctl=$root/readiness-systemctl
printf '%s\n' '#!/bin/sh' 'exit 0' > "$readiness_systemctl"
chmod 0555 "$readiness_systemctl"
readiness_curl=$root/readiness-curl
printf '%s\n' \
	'#!/bin/sh' \
	'case "$*" in' \
	'    *"/v1/health") cat "$READINESS_HEALTH_FILE" ;;' \
	'    *"/v1/internal/readiness") cat "$READINESS_STATUS_FILE" ;;' \
	'    *) exit 1 ;;' \
	'esac' > "$readiness_curl"
chmod 0555 "$readiness_curl"
readiness_health=$root/readiness-health.json
printf '%s\n' '{"status":"ok","version":"release-test"}' > "$readiness_health"
readiness_optional=$root/readiness-optional.json
printf '%s\n' \
	'{"service":{"version":"release-test"},"runtime":{"mode":"optional","required":false,"state":"degraded","ready":true,"components":{"codex":{"state":"degraded","ready":false,"required":false}}}}' > "$readiness_optional"
readiness_required=$root/readiness-required.json
printf '%s\n' \
	'{"service":{"version":"release-test"},"runtime":{"mode":"required","required":true,"state":"ready","ready":true,"components":{"codex":{"state":"healthy","ready":true,"required":true,"status":"healthy"},"supervisor":{"state":"healthy","ready":true,"required":true,"status":"healthy"},"account":{"state":"healthy","ready":true,"required":true,"status":"healthy"},"model":{"state":"healthy","ready":true,"required":true,"status":"healthy"}}}}' > "$readiness_required"
readiness_required_degraded=$root/readiness-required-degraded.json
printf '%s\n' \
	'{"service":{"version":"release-test"},"runtime":{"mode":"required","required":true,"state":"degraded","ready":true,"components":{"codex":{"state":"degraded","ready":false,"required":true,"status":"degraded"},"supervisor":{"state":"healthy","ready":true,"required":true,"status":"healthy"},"account":{"state":"healthy","ready":true,"required":true,"status":"healthy"},"model":{"state":"healthy","ready":true,"required":true,"status":"healthy"}}}}' > "$readiness_required_degraded"
run_readiness_fixture() {
	COMMONS_DB="$readiness_db" \
	COMMONS_RELEASE_DIR="$release_dir" \
	COMMONS_CODEX_BIN="$release_dir/bin/codex" \
	COMMONS_CODEX_SHA256="$codex_sha" \
	COMMONS_CODE_MODE_HOST_SHA256="$host_sha" \
	COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" \
	COMMONS_CODEX_ZSH_SHA256="$zsh_sha" \
	COMMONS_CODEX_RG_SHA256="$rg_sha" \
	COMMONS_CODEX_PACKAGE_SHA256="$package_sha" \
	COMMONS_SYSTEMCTL="$readiness_systemctl" \
	COMMONS_CURL="$readiness_curl" \
	COMMONS_READINESS_ATTEMPTS=1 \
	/bin/sh "$readiness_script"
}
READINESS_HEALTH_FILE="$readiness_health" READINESS_STATUS_FILE="$readiness_optional" run_readiness_fixture
READINESS_HEALTH_FILE="$readiness_health" READINESS_STATUS_FILE="$readiness_required" run_readiness_fixture
if READINESS_HEALTH_FILE="$readiness_health" READINESS_STATUS_FILE="$readiness_required_degraded" run_readiness_fixture; then
	printf 'unexpected readiness acceptance: required degraded Codex fixture\n' >&2
	exit 1
fi
printf 'READINESS_FIXTURES=optional-pass|required-pass|required-degraded-rejected\n'

# Complete-tree inventory: every destructive case is followed immediately by
# a valid verification, so a stale fixture cannot make a later case green.
chmod u+w "$release_dir"
printf extra > "$release_dir/unmanifested"
chmod 0444 "$release_dir/unmanifested"
chmod 0555 "$release_dir"
expect_verify_failure extra-root-regular-file
chmod u+w "$release_dir"
rm -f -- "$release_dir/unmanifested"
chmod 0555 "$release_dir"
restore_valid extra-root-regular-file

chmod u+w "$release_dir/web"
printf extra > "$release_dir/web/unmanifested"
chmod 0444 "$release_dir/web/unmanifested"
chmod 0555 "$release_dir/web"
expect_verify_failure extra-nested-regular-file
chmod u+w "$release_dir/web"
rm -f -- "$release_dir/web/unmanifested"
chmod 0555 "$release_dir/web"
restore_valid extra-nested-regular-file

chmod u+w "$release_dir"
mkdir "$release_dir/unmanifested-empty"
chmod 0555 "$release_dir/unmanifested-empty"
chmod 0555 "$release_dir"
expect_verify_failure unmanifested-empty-root-directory
chmod u+w "$release_dir"
rmdir "$release_dir/unmanifested-empty"
chmod 0555 "$release_dir"
restore_valid unmanifested-empty-root-directory

chmod u+w "$release_dir/web"
mkdir "$release_dir/web/unmanifested-empty"
chmod 0555 "$release_dir/web/unmanifested-empty"
chmod 0555 "$release_dir/web"
expect_verify_failure unmanifested-empty-nested-directory
chmod u+w "$release_dir/web"
rmdir "$release_dir/web/unmanifested-empty"
chmod 0555 "$release_dir/web"
restore_valid unmanifested-empty-nested-directory

chmod u+w "$release_dir"
mkfifo "$release_dir/unmanifested.fifo"
chmod 0555 "$release_dir"
expect_verify_failure root-fifo
chmod u+w "$release_dir"
rm -f -- "$release_dir/unmanifested.fifo"
chmod 0555 "$release_dir"
restore_valid root-fifo

chmod u+w "$release_dir/web"
mkfifo "$release_dir/web/unmanifested.fifo"
chmod 0555 "$release_dir/web"
expect_verify_failure nested-fifo
chmod u+w "$release_dir/web"
rm -f -- "$release_dir/web/unmanifested.fifo"
chmod 0555 "$release_dir/web"
restore_valid nested-fifo

chmod u+w "$release_dir"
ln -s VERSION "$release_dir/unmanifested-link"
chmod 0555 "$release_dir"
expect_verify_failure root-symlink
chmod u+w "$release_dir"
rm -f -- "$release_dir/unmanifested-link"
chmod 0555 "$release_dir"
restore_valid root-symlink

chmod u+w "$release_dir/web"
ln -s ../VERSION "$release_dir/web/unmanifested-link"
chmod 0555 "$release_dir/web"
expect_verify_failure nested-symlink
chmod u+w "$release_dir/web"
rm -f -- "$release_dir/web/unmanifested-link"
chmod 0555 "$release_dir/web"
restore_valid nested-symlink

chmod u+w "$release_dir"
ln -s no-such-target "$release_dir/dangling-link"
chmod 0555 "$release_dir"
expect_verify_failure root-dangling-symlink
chmod u+w "$release_dir"
rm -f -- "$release_dir/dangling-link"
chmod 0555 "$release_dir"
restore_valid root-dangling-symlink

chmod u+w "$release_dir/web"
ln -s no-such-target "$release_dir/web/dangling-link"
chmod 0555 "$release_dir/web"
expect_verify_failure nested-dangling-symlink
chmod u+w "$release_dir/web"
rm -f -- "$release_dir/web/dangling-link"
chmod 0555 "$release_dir/web"
restore_valid nested-dangling-symlink

chmod u+w "$release_dir/web"
mv "$release_dir/web/index.html" "$root/missing-index.html"
chmod 0555 "$release_dir/web"
expect_verify_failure missing-manifested-file
chmod u+w "$release_dir/web"
cp "$original_index" "$release_dir/web/index.html"
chmod 0444 "$release_dir/web/index.html"
chmod 0555 "$release_dir/web"
restore_valid missing-manifested-file

zeros=$(printf '%064d' 0)
bad_digest=$(printf '%064d' 0 | tr 0 g)
cp "$valid_manifest" "$root/additional-manifest"
chmod u+w "$root/additional-manifest"
printf '%s  web/manifest-extra\n' "$zeros" >> "$root/additional-manifest"
set_manifest_from "$root/additional-manifest"
expect_verify_failure unexpected-additional-manifest-entry
restore_manifest
restore_valid unexpected-additional-manifest-entry

{
	sed -n '1p' "$valid_manifest"
	cat "$valid_manifest"
} > "$root/duplicate-manifest"
set_manifest_from "$root/duplicate-manifest"
expect_verify_failure duplicate-manifest-record
restore_manifest
restore_valid duplicate-manifest-record

LC_ALL=C awk '{ record[NR] = $0 } END { for (i = NR; i > 0; i--) print record[i] }' "$valid_manifest" > "$root/reordered-manifest"
set_manifest_from "$root/reordered-manifest"
expect_verify_failure noncanonical-manifest-order
restore_manifest
restore_valid noncanonical-manifest-order

printf '%s  VERSION\n' "$bad_digest" > "$root/malformed-digest"
set_manifest_from "$root/malformed-digest"
expect_verify_failure malformed-digest
restore_manifest
restore_valid malformed-digest

printf '%s VERSION\n' "$zeros" > "$root/malformed-record"
set_manifest_from "$root/malformed-record"
expect_verify_failure malformed-record-spacing
restore_manifest
restore_valid malformed-record-spacing

printf '%s  /VERSION\n' "$zeros" > "$root/absolute-manifest"
set_manifest_from "$root/absolute-manifest"
expect_verify_failure absolute-manifest-path
restore_manifest
restore_valid absolute-manifest-path

printf '%s  ./VERSION\n' "$zeros" > "$root/dot-slash-manifest"
set_manifest_from "$root/dot-slash-manifest"
expect_verify_failure noncanonical-dot-slash-manifest-path
restore_manifest
restore_valid noncanonical-dot-slash-manifest-path

printf '%s  ../VERSION\n' "$zeros" > "$root/traversal-manifest"
set_manifest_from "$root/traversal-manifest"
expect_verify_failure traversal-dot-dot-manifest-path
restore_manifest
restore_valid traversal-dot-dot-manifest-path

backslash_path='web/bad\path'
printf '%s  %s\n' "$zeros" "$backslash_path" > "$root/backslash-manifest"
set_manifest_from "$root/backslash-manifest"
expect_verify_failure backslash-manifest-path
restore_manifest
restore_valid backslash-manifest-path

printf '%s  web/bad path\n' "$zeros" > "$root/whitespace-manifest"
set_manifest_from "$root/whitespace-manifest"
expect_verify_failure whitespace-manifest-path
restore_manifest
restore_valid whitespace-manifest-path

control_path=$(printf 'web/bad\001path')
printf '%s  %s\n' "$zeros" "$control_path" > "$root/control-manifest"
set_manifest_from "$root/control-manifest"
expect_verify_failure control-manifest-path
restore_manifest
restore_valid control-manifest-path

printf '%s  web/bad\npath\n' "$zeros" > "$root/newline-manifest"
set_manifest_from "$root/newline-manifest"
expect_verify_failure newline-manifest-path
restore_manifest
restore_valid newline-manifest-path

chmod u+w "$release_dir/web/index.html"
printf tamper >> "$release_dir/web/index.html"
chmod 0444 "$release_dir/web/index.html"
expect_verify_failure checksum-mismatch
chmod u+w "$release_dir/web/index.html"
cp "$original_index" "$release_dir/web/index.html"
chmod 0444 "$release_dir/web/index.html"
restore_valid checksum-mismatch

for artifact_case in root-map nested-map root-tmp nested-test-prefix root-underscore-test; do
	case "$artifact_case" in
	root-map) artifact_path=$release_dir/artifact.map; artifact_rel=artifact.map ;;
	nested-map) artifact_path=$release_dir/web/artifact.map; artifact_rel=web/artifact.map ;;
	root-tmp) artifact_path=$release_dir/artifact.tmp; artifact_rel=artifact.tmp ;;
	nested-test-prefix) artifact_path=$release_dir/web/test-artifact; artifact_rel=web/test-artifact ;;
	root-underscore-test) artifact_path=$release_dir/fixture_test_output; artifact_rel=fixture_test_output ;;
	esac
	artifact_parent=$(dirname "$artifact_path")
	chmod u+w "$artifact_parent"
	printf artifact > "$artifact_path"
	chmod 0444 "$artifact_path"
	chmod 0555 "$artifact_parent"
	artifact_sha=$(sha256sum "$artifact_path" | awk '{print $1}')
	printf '%s  %s\n' "$artifact_sha" "$artifact_rel" > "$root/artifact-manifest"
	cat "$valid_manifest" >> "$root/artifact-manifest"
	set_manifest_from "$root/artifact-manifest"
	expect_verify_failure "forbidden-artifact-$artifact_case"
	restore_manifest
	chmod u+w "$artifact_parent"
	rm -f -- "$artifact_path"
	chmod 0555 "$artifact_parent"
	restore_valid "forbidden-artifact-$artifact_case"
done

# Source/package path grammar and required runtime inputs are checked through
# fresh disposable staging attempts. Each rejection also verifies the pristine
# main release before the next attempt.
mkdir -p "$root/stage-rejects"
for path_case in whitespace control newline; do
	bad_web=$root/source-web-$path_case
	cp -R "$root/source-web" "$bad_web"
	case "$path_case" in
	whitespace) bad_name='bad name' ;;
	control) bad_name=$(printf 'bad\001name') ;;
	newline) bad_name=$(printf 'bad\nname') ;;
	esac
	printf bad > "$bad_web/$bad_name"
	bad_id="reject-$path_case-path"
	bad_server=$root/servers/$bad_id
	build_server "$bad_id" "$bad_server"
	expect_stage_failure "packaged-$path_case-path" "$bad_id" "$bad_server" "$root/stage-rejects" "$bad_web" "$root/codex-bundle"
done

missing_exec_bundle=$root/codex-bundle-missing-exec
cp -R "$root/codex-bundle" "$missing_exec_bundle"
chmod 0644 "$missing_exec_bundle/codex-path/rg"
build_server reject-missing-executable "$root/servers/reject-missing-executable"
expect_stage_failure missing-required-executable-bit reject-missing-executable "$root/servers/reject-missing-executable" "$root/stage-rejects" "$root/source-web" "$missing_exec_bundle"

extra_bundle=$root/codex-bundle-extra
cp -R "$root/codex-bundle" "$extra_bundle"
printf extra > "$extra_bundle/unmanifested"
build_server reject-extra-bundle-file "$root/servers/reject-extra-bundle-file"
expect_stage_failure extra-runtime-bundle-file reject-extra-bundle-file "$root/servers/reject-extra-bundle-file" "$root/stage-rejects" "$root/source-web" "$extra_bundle"

linked_bundle=$root/codex-bundle-linked
cp -R "$root/codex-bundle" "$linked_bundle"
rm -f -- "$linked_bundle/codex-path/rg"
ln -s "$root/codex-bundle/codex-path/rg" "$linked_bundle/codex-path/rg"
build_server reject-linked-bundle-file "$root/servers/reject-linked-bundle-file"
expect_stage_failure linked-runtime-bundle-file reject-linked-bundle-file "$root/servers/reject-linked-bundle-file" "$root/stage-rejects" "$root/source-web" "$linked_bundle"

bad_web_link=$root/source-web-linked
mkdir "$bad_web_link"
ln -s /etc/passwd "$bad_web_link/index.html"
build_server reject-linked-web-file "$root/servers/reject-linked-web-file"
expect_stage_failure linked-web-source-file reject-linked-web-file "$root/servers/reject-linked-web-file" "$root/stage-rejects" "$bad_web_link" "$root/codex-bundle"

build_server reject-partial-target "$root/servers/reject-partial-target"
expect_limited_stage_failure partial-target-cleanup reject-partial-target "$root/servers/reject-partial-target" "$root/stage-rejects" "$root/source-web" "$root/codex-bundle"

# Stage normalization remains deterministic for both restrictive and
# permissive umasks, and source execute bits are not inherited by ordinary
# packaged assets.
for umask_case in 077 000; do
	umask_root=$root/umask-$umask_case
	mkdir -p "$umask_root"
	umask_id="umask-$umask_case"
	build_server "$umask_id" "$root/servers/$umask_id"
	(
		umask "$umask_case"
		stage_release "$umask_id" "$root/servers/$umask_id" "$umask_root" "$root/source-web" "$root/codex-bundle"
	)
	umask_dir=$umask_root/$umask_id
	check_modes "$umask_dir"
	verify_staged_release "$umask_root" "$umask_dir" >/dev/null
	record_positive "deterministic-modes-umask-$umask_case"
done

chmod 0755 "$root/source-web/index.html"
execbit_id=source-execute-bit
build_server "$execbit_id" "$root/servers/$execbit_id"
execbit_root=$root/source-exec-release
mkdir -p "$execbit_root"
stage_release "$execbit_id" "$root/servers/$execbit_id" "$execbit_root" "$root/source-web" "$root/codex-bundle"
execbit_dir=$execbit_root/$execbit_id
test "$(stat -c %a "$execbit_dir/web/index.html")" = 444
check_modes "$execbit_dir"
verify_staged_release "$execbit_root" "$execbit_dir" >/dev/null
chmod 0644 "$root/source-web/index.html"
record_positive source-execute-bit-removed

# Restore the main disposable release after the stage-only cases.
chmod -R u+w "$release_dir"
chmod 0555 "$release_dir"
find "$release_dir" -type d -exec chmod 0555 {} +
find "$release_dir" -type f -exec chmod 0444 {} +
chmod 0555 "$release_dir/commons-server" "$release_dir/bin/codex" "$release_dir/bin/codex-code-mode-host" "$release_dir/codex-resources/bwrap" "$release_dir/codex-resources/zsh/bin/zsh" "$release_dir/codex-path/rg"
verify_test_release >/dev/null

# Mode drift: root, nested directories, all six runtime executables, every
# ordinary-file class, and an unexpected executable bit are independently
# rejected and restored.
chmod u+w "$release_dir"
chmod 0777 "$release_dir"
expect_verify_failure root-mode-drift
chmod 0555 "$release_dir"
restore_valid root-mode-drift

for dir_case in web codex-resources codex-resources/zsh/bin; do
	chmod u+w "$release_dir/$dir_case"
	chmod 0777 "$release_dir/$dir_case"
	expect_verify_failure "nested-directory-mode-drift-$dir_case"
	chmod 0555 "$release_dir/$dir_case"
	restore_valid "nested-directory-mode-drift-$dir_case"
done

for exec_file in commons-server bin/codex bin/codex-code-mode-host codex-resources/bwrap codex-resources/zsh/bin/zsh codex-path/rg; do
	chmod u+w "$release_dir/$exec_file"
	chmod 0444 "$release_dir/$exec_file"
	expect_verify_failure "required-executable-mode-drift-$exec_file"
	chmod 0555 "$release_dir/$exec_file"
	restore_valid "required-executable-mode-drift-$exec_file"
done

for ordinary_file in VERSION codex-package.json web/index.html ops/backup.sh SHA256SUMS; do
	chmod u+w "$release_dir/$ordinary_file"
	chmod 0644 "$release_dir/$ordinary_file"
	expect_verify_failure "ordinary-file-mode-drift-$ordinary_file"
	chmod 0444 "$release_dir/$ordinary_file"
	restore_valid "ordinary-file-mode-drift-$ordinary_file"
done

chmod u+w "$release_dir/web/index.html"
chmod 0555 "$release_dir/web/index.html"
expect_verify_failure unexpected-executable-bit-on-ordinary-file
chmod 0444 "$release_dir/web/index.html"
restore_valid unexpected-executable-bit-on-ordinary-file

# One exact numeric UID/GID is required over the entire tree. The managed
# account has a disposable supplementary group; use it for every negative
# ownership fixture and fail clearly if none is available.
phase12_uid=$(id -u)
phase12_gid=$(id -g)
phase12_alt_gid=
for candidate_gid in $(id -G); do
	if test "$candidate_gid" != "$phase12_gid"; then
		phase12_alt_gid=$candidate_gid
		break
	fi
done
test -n "$phase12_alt_gid" || { printf 'no alternate supplementary group available for ownership matrix\n' >&2; exit 1; }

fake_id_dir=$root/fake-id
mkdir -p "$fake_id_dir"
printf '%s\n' \
	'#!/bin/sh' \
	'case "$1" in' \
	'    -u) printf "65534\\n" ;;' \
	'    -g) printf "65533\\n" ;;' \
	'    *) exec /usr/bin/id "$@" ;;' \
	'esac' > "$fake_id_dir/id"
chmod 0555 "$fake_id_dir/id"

# The managed sandbox exposes the supplementary group in id(1) but may not
# map it for chown(2). Probe the disposable fixture explicitly. If the
# isolated host permission is unavailable, exercise the same exact numeric
# ownership branch with a disposable alternate identity rather than silently
# skipping ownership coverage.
ownership_probe=$root/ownership-probe
printf probe > "$ownership_probe"
if chown "$phase12_uid:$phase12_alt_gid" "$ownership_probe" 2>/dev/null; then
	ownership_fixture_mode=group
	chown "$phase12_uid:$phase12_gid" "$ownership_probe"
else
	ownership_fixture_mode=identity-mismatch
fi
printf 'OWNERSHIP_FIXTURE_MODE=%s\n' "$ownership_fixture_mode"
ownership_path_cases=0

test "$(stat -c %u "$release_dir")" = "$phase12_uid"
test "$(stat -c %g "$release_dir")" = "$phase12_gid"
test -z "$(find "$release_dir" \! -uid "$phase12_uid" -print -quit)"
test -z "$(find "$release_dir" \! -gid "$phase12_gid" -print -quit)"
for owned_path in "$release_dir" "$release_dir/web" "$release_dir/bin/codex" "$release_dir/web/index.html" "$release_dir/SHA256SUMS"; do
	test "$(stat -c %u "$owned_path")" = "$phase12_uid"
	test "$(stat -c %g "$owned_path")" = "$phase12_gid"
done

change_group() {
	owned_path=$1
	if ! chown "$phase12_uid:$phase12_alt_gid" "$owned_path" 2>/dev/null; then
		chgrp "$phase12_alt_gid" "$owned_path"
	fi
	test "$(stat -c %g "$owned_path")" = "$phase12_alt_gid"
}

restore_group() {
	owned_path=$1
	if ! chown "$phase12_uid:$phase12_gid" "$owned_path" 2>/dev/null; then
		chgrp "$phase12_gid" "$owned_path"
	fi
	test "$(stat -c %u "$owned_path")" = "$phase12_uid"
	test "$(stat -c %g "$owned_path")" = "$phase12_gid"
}

expect_ownership_failure() {
	label=$1
	owned_path=$2
	change_group "$owned_path"
	expect_verify_failure "$label"
	restore_group "$owned_path"
	restore_valid "$label"
	ownership_path_cases=$((ownership_path_cases + 1))
}

expect_identity_failure effective-identity-mismatch

if test "$ownership_fixture_mode" = group; then
	for ownership_case in root-group release-directory-group required-executable-group ordinary-file-group manifest-group; do
		case "$ownership_case" in
		root-group) ownership_path=$release_dir ;;
		release-directory-group) ownership_path=$release_dir/web ;;
		required-executable-group) ownership_path=$release_dir/bin/codex ;;
		ordinary-file-group) ownership_path=$release_dir/web/index.html ;;
		manifest-group) ownership_path=$release_dir/SHA256SUMS ;;
		esac
		expect_ownership_failure "$ownership_case-drift" "$ownership_path"
	done
else
	printf 'OWNERSHIP_PATH_CASES_BLOCKED=managed-group-mutation-unavailable\n'
fi

# This count is intentionally explicit: a missing block or an early green exit
# cannot satisfy Phase 1.3 merely by returning status zero.
expected_negative_cases=$((54 + ownership_path_cases))
expected_restored_cases=$((54 + ownership_path_cases))
expected_positive_cases=4
test "$negative_cases" -eq "$expected_negative_cases"
test "$restored_cases" -eq "$expected_restored_cases"
test "$positive_cases" -eq "$expected_positive_cases"
verify_test_release >/dev/null
printf 'PHASE13_NEGATIVE_CASES=%s\n' "$negative_cases"
printf 'PHASE13_RESTORATIONS=%s\n' "$restored_cases"
printf 'PHASE13_POSITIVE_CASES=%s\n' "$positive_cases"
printf 'PHASE13_OWNERSHIP_PATH_CASES=%s\n' "$ownership_path_cases"
printf 'PHASE13_FINAL_PRISTINE=1\n'
