#!/bin/sh
set -eu
! grep -Eq '^ProtectKernelModules=' deploy/systemd/codex-commons.service
grep -Fxq 'ProtectKernelTunables=true' deploy/systemd/codex-commons.service
grep -Fxq 'ProtectControlGroups=true' deploy/systemd/codex-commons.service
root=$(mktemp -d)
trap 'chmod -R u+w "$root" 2>/dev/null || true; rm -rf -- "$root"' EXIT HUP INT TERM
mkdir -p "$root/source-web" "$root/releases"
printf '<!doctype html>' > "$root/source-web/index.html"
mkdir -p "$root/codex-bundle/bin" "$root/codex-bundle/codex-resources/zsh/bin" "$root/codex-bundle/codex-path"
printf '#!/bin/sh\necho codex-cli 0.147.0\n' > "$root/codex-bundle/bin/codex"
printf '#!/bin/sh\nexit 0\n' > "$root/codex-bundle/bin/codex-code-mode-host"
printf '#!/bin/sh\nexit 0\n' > "$root/codex-bundle/codex-resources/bwrap"
printf '#!/bin/sh\nexec /bin/sh "$@"\n' > "$root/codex-bundle/codex-resources/zsh/bin/zsh"
printf '#!/bin/sh\necho rg fixture\n' > "$root/codex-bundle/codex-path/rg"
printf '{}\n' > "$root/codex-bundle/codex-package.json"
chmod 0755 "$root/codex-bundle/bin/codex" "$root/codex-bundle/bin/codex-code-mode-host" "$root/codex-bundle/codex-resources/bwrap" "$root/codex-bundle/codex-resources/zsh/bin/zsh" "$root/codex-bundle/codex-path/rg"
/bin/sh ops/build-release.sh release-test "$root/commons-server"
sha=$(sha256sum "$root/codex-bundle/bin/codex" | awk '{print $1}')
host_sha=$(sha256sum "$root/codex-bundle/bin/codex-code-mode-host" | awk '{print $1}')
bwrap_sha=$(sha256sum "$root/codex-bundle/codex-resources/bwrap" | awk '{print $1}')
zsh_sha=$(sha256sum "$root/codex-bundle/codex-resources/zsh/bin/zsh" | awk '{print $1}')
rg_sha=$(sha256sum "$root/codex-bundle/codex-path/rg" | awk '{print $1}')
package_sha=$(sha256sum "$root/codex-bundle/codex-package.json" | awk '{print $1}')
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh release-test
test ! -e "$root/releases/release-test/ops/build-release.sh"; test ! -e "$root/releases/release-test/ops/stage-release.sh"; test ! -e "$root/releases/release-test/ops/test-ops.sh"
release_dir="$root/releases/release-test"
manifest="$release_dir/SHA256SUMS"
COMMONS_CODEX_BIN="$release_dir/bin/codex"
export COMMONS_CODEX_BIN
cp "$manifest" "$root/valid-SHA256SUMS"
cp "$release_dir/ops/backup.sh" "$root/backup.sh.original"
verify_test_release() {
	COMMONS_RELEASE_ROOT="$root/releases" COMMONS_RELEASE_DIR="$root/releases/release-test" COMMONS_CODEX_BIN="$root/releases/release-test/bin/codex" COMMONS_WEB_DIR="$root/releases/release-test/web" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="${VERIFY_HOST_SHA:-$host_sha}" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" /bin/sh "$root/releases/release-test/ops/verify-release.sh"
}

expect_verify_failure() {
	if verify_test_release >/dev/null 2>&1; then exit 1; fi
}

restore_manifest() {
	chmod u+w "$manifest"
	cp "$root/valid-SHA256SUMS" "$manifest"
	chmod a-w "$manifest"
}

# A staged tree with its complete manifest is accepted.
verify_test_release

# Complete-tree file and directory inventory rejects root and nested extras.
chmod u+w "$release_dir"
printf extra > "$release_dir/unmanifested"
expect_verify_failure
rm "$release_dir/unmanifested"
chmod u+w "$release_dir/web"
printf extra > "$release_dir/web/unmanifested"
expect_verify_failure
rm "$release_dir/web/unmanifested"
mkdir "$release_dir/unmanifested-empty"
expect_verify_failure
rmdir "$release_dir/unmanifested-empty"

# Symlinks and special files are rejected at both root and nested levels.
ln -s VERSION "$release_dir/unmanifested-link"
expect_verify_failure
rm "$release_dir/unmanifested-link"
mkfifo "$release_dir/unmanifested.fifo"
expect_verify_failure
rm "$release_dir/unmanifested.fifo"
mkfifo "$release_dir/web/unmanifested.fifo"
expect_verify_failure
rm "$release_dir/web/unmanifested.fifo"

# Manifest records are strict, unique, relative, and grammar-safe.
{
	sed -n '1p' "$root/valid-SHA256SUMS"
	cat "$root/valid-SHA256SUMS"
} > "$root/duplicate-manifest"
chmod u+w "$manifest"; cp "$root/duplicate-manifest" "$manifest"; chmod a-w "$manifest"
expect_verify_failure
restore_manifest
printf '%064d  /VERSION\n' 0 > "$root/absolute-manifest"
chmod u+w "$manifest"; cp "$root/absolute-manifest" "$manifest"; chmod a-w "$manifest"
expect_verify_failure
restore_manifest
printf '%064d  ../VERSION\n' 0 > "$root/traversal-manifest"
chmod u+w "$manifest"; cp "$root/traversal-manifest" "$manifest"; chmod a-w "$manifest"
expect_verify_failure
restore_manifest
printf '%064d bad/path\n' 0 > "$root/malformed-manifest"
chmod u+w "$manifest"; cp "$root/malformed-manifest" "$manifest"; chmod a-w "$manifest"
expect_verify_failure
restore_manifest
printf '%064d  bad\\path\n' 0 > "$root/backslash-manifest"
chmod u+w "$manifest"; cp "$root/backslash-manifest" "$manifest"; chmod a-w "$manifest"
expect_verify_failure
restore_manifest
printf '%064d  bad\001path\n' 0 > "$root/control-manifest"
chmod u+w "$manifest"; cp "$root/control-manifest" "$manifest"; chmod a-w "$manifest"
expect_verify_failure
restore_manifest

# A missing manifest-listed file is rejected before checksum verification.
chmod u+w "$release_dir/ops"
mv "$release_dir/ops/backup.sh" "$root/backup.sh.missing"
expect_verify_failure
cp "$root/backup.sh.original" "$release_dir/ops/backup.sh"
chmod a-w "$release_dir/ops/backup.sh"

# The final structural pass accepts the restored staged tree.
chmod -R a-w "$release_dir"
verify_test_release

VERIFY_HOST_SHA=0000000000000000000000000000000000000000000000000000000000000000
if verify_test_release >/dev/null 2>&1; then exit 1; fi
unset VERIFY_HOST_SHA
chmod u+w "$root/releases/release-test/codex-resources/bwrap"
printf tamper >> "$root/releases/release-test/codex-resources/bwrap"
if verify_test_release >/dev/null 2>&1; then exit 1; fi
cp "$root/codex-bundle/codex-resources/bwrap" "$root/releases/release-test/codex-resources/bwrap"; chmod 0555 "$root/releases/release-test/codex-resources/bwrap"
chmod u+w "$root/releases/release-test"
mv "$root/releases/release-test/codex-package.json" "$root/releases/release-test/codex-package.json.missing"
if verify_test_release >/dev/null 2>&1; then exit 1; fi
mv "$root/releases/release-test/codex-package.json.missing" "$root/releases/release-test/codex-package.json"
chmod u+w "$root/releases/release-test/web/index.html"
printf tamper >> "$root/releases/release-test/web/index.html"
if verify_test_release >/dev/null 2>&1; then exit 1; fi
printf '#!/bin/sh\nprintf "release-test\\n"\n' > "$root/not-commons"; chmod 700 "$root/not-commons"
if COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/not-commons" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh arbitrary-shell >/dev/null 2>&1; then exit 1; fi
if COMMONS_RELEASE_ROOT=relative COMMONS_SERVER_SOURCE="$root/commons-server" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh relative-root >/dev/null 2>&1; then exit 1; fi
if COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server" COMMONS_WEB_SOURCE=relative-web COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh relative-web >/dev/null 2>&1; then exit 1; fi
cp -R "$root/codex-bundle" "$root/codex-bundle-missing"; rm "$root/codex-bundle-missing/codex-resources/bwrap"
if COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle-missing" /bin/sh ops/stage-release.sh missing-bwrap >/dev/null 2>&1; then exit 1; fi
cp -R "$root/codex-bundle" "$root/codex-bundle-link"; rm "$root/codex-bundle-link/codex-path/rg"; ln -s "$root/codex-bundle/codex-path/rg" "$root/codex-bundle-link/codex-path/rg"
if COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle-link" /bin/sh ops/stage-release.sh linked-rg >/dev/null 2>&1; then exit 1; fi
cp -R "$root/codex-bundle" "$root/codex-bundle-extra"; printf extra > "$root/codex-bundle-extra/unmanifested"
if COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle-extra" /bin/sh ops/stage-release.sh extra-runtime >/dev/null 2>&1; then exit 1; fi
mkdir "$root/source-web-bad"; ln -s /etc/passwd "$root/source-web-bad/index.html"
if COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server" COMMONS_WEB_SOURCE="$root/source-web-bad" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh bad >/dev/null 2>&1; then exit 1; fi

for schema in 13 14 15; do
	db="$root/schema-$schema.sqlite3"; backups="$root/backups-$schema"
	sqlite3 "$db" "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT,applied_at TEXT);INSERT INTO schema_migrations VALUES($schema,'fixture','x');CREATE TABLE projects(id TEXT);CREATE TABLE tasks(id TEXT);CREATE TABLE archaeology_native_batches(id TEXT);"
	if [ "$schema" -eq 15 ]; then
		sqlite3 "$db" "CREATE TABLE archaeology_selected_imports(id TEXT,batch_id TEXT,principal TEXT,request_key TEXT,selection_digest TEXT,manifest_digest TEXT,outcome_ids_json TEXT,result_json TEXT,created_at TEXT);CREATE TABLE installation_status(id INTEGER PRIMARY KEY,backup_status TEXT,backup_verified_at TEXT,restore_status TEXT,restore_verified_at TEXT,report_recovery_status TEXT,report_recovery_violations INTEGER,report_recovery_checked_at TEXT,duplicate_launch_status TEXT,duplicate_launch_violations INTEGER,duplicate_launch_checked_at TEXT,repository_immutability_status TEXT,repository_immutability_violations INTEGER,repository_immutability_checked_at TEXT,canonical_immutability_status TEXT,canonical_immutability_violations INTEGER,canonical_immutability_checked_at TEXT,report_recovery_receipt_digest TEXT,duplicate_launch_receipt_digest TEXT,repository_immutability_receipt_digest TEXT,canonical_immutability_receipt_digest TEXT,updated_at TEXT);CREATE TABLE installation_evidence_receipts(id INTEGER PRIMARY KEY,kind TEXT,status TEXT,violations INTEGER,checked_at TEXT,scope_digest TEXT,receipt_digest TEXT UNIQUE,recorded_at TEXT);INSERT INTO installation_status VALUES(1,'unknown',NULL,'unknown',NULL,'unknown',0,NULL,'unknown',0,NULL,'unknown',0,NULL,'unknown',0,NULL,'','','','','x');"
	fi
	COMMONS_DB="$db" COMMONS_BACKUP_DIR="$backups" /bin/sh ops/backup.sh
	backup=$(find "$backups/daily" -name 'commons-*.sqlite3' -type f | head -1)
	test -n "$backup"; /bin/sh ops/verify-restore.sh "$backup"
	copy="$root/copy-$schema.sqlite3"; cp "$backup" "$copy"; cp "$backup.sha256" "$copy.sha256"; cp "$backup.receipt.json" "$copy.receipt.json"
	/bin/sh ops/verify-restore.sh "$copy"
	printf tamper >> "$copy"
	if /bin/sh ops/verify-restore.sh "$copy" >/dev/null 2>&1; then exit 1; fi
	test -f "$backups/monthly/commons-$(date -u +%Y-%m).sqlite3.receipt.json"
	archive="$root/archive-$schema.sqlite3"; /bin/sh ops/seal-archive.sh "$db" "$archive"
	test "$(stat -c %a "$archive")" = 600; test "$(sqlite3 "$archive" 'PRAGMA integrity_check')" = ok
	if [ "$schema" -eq 15 ]; then
		COMMONS_RESTORE_STATUS_DB="$db" /bin/sh ops/verify-restore.sh "$backup" --record-drill
		test "$(sqlite3 "$db" 'SELECT restore_status FROM installation_status WHERE id=1')" = verified
		receipt="$root/evidence.json"; printf '{"kind":"report_recovery","status":"verified","violations":0,"checked_at":"2026-08-13T12:00:00Z","scope_digest":"%064d"}\n' 0 > "$receipt"; chmod 600 "$receipt"; sha256sum "$receipt" > "$receipt.sha256"
		COMMONS_DB="$db" /bin/sh ops/record-evidence.sh "$receipt"
		printf '{"kind":"duplicate_launch","status":"attention","violations":2,"checked_at":"2026-08-13T12:01:00Z","scope_digest":"%064d"}\n' 1 > "$receipt"; sha256sum "$receipt" > "$receipt.sha256"
		COMMONS_DB="$db" /bin/sh ops/record-evidence.sh "$receipt"
		test "$(sqlite3 "$db" "SELECT report_recovery_status||':'||duplicate_launch_violations FROM installation_status")" = verified:2
		printf tamper >> "$receipt"; if COMMONS_DB="$db" /bin/sh ops/record-evidence.sh "$receipt" >/dev/null 2>&1; then exit 1; fi
		before=$(sqlite3 "$db" 'SELECT count(*) FROM installation_evidence_receipts')
		printf '{"kind":"report_recovery","status":"verified","violations":0,"checked_at":"2026-08-13T12:00:00Z'';DROP TABLE installation_status;--","scope_digest":"%064d"}\n' 0 > "$receipt"; sha256sum "$receipt" > "$receipt.sha256"
		if COMMONS_DB="$db" /bin/sh ops/record-evidence.sh "$receipt" >/dev/null 2>&1; then exit 1; fi
		test "$(sqlite3 "$db" 'SELECT count(*) FROM installation_evidence_receipts')" = "$before"
		test "$(sqlite3 "$db" "SELECT count(*) FROM sqlite_schema WHERE name='installation_status'")" = 1
	fi
done

# Exact readiness is independently injectable: active+matching health+schema15
# succeeds, while wrong build or schema fails without touching a service.
mkdir "$root/fake-bin"
printf '#!/bin/sh\nexit 0\n' > "$root/fake-bin/systemctl"
printf '#!/bin/sh\ntest -z "${COMMONS_CURL_LOG:-}" || printf "%%s\\n" "$*" >> "$COMMONS_CURL_LOG"\nbuild=${COMMONS_FAKE_BUILD:-release-test}; schema=${COMMONS_FAKE_SCHEMA:-15}; configured=${COMMONS_FAKE_CONFIGURED:-true}; available=${COMMONS_FAKE_AVAILABLE:-true}; codex=${COMMONS_FAKE_CODEX_VERSION:-0.147.0}; account=${COMMONS_FAKE_ACCOUNT:-signed_in}; compatibility=${COMMONS_FAKE_COMPATIBILITY:-compatible}; reconciliation=${COMMONS_FAKE_RECONCILIATION:-healthy}; case "$*" in *health*) printf '\''{"ok":true,"data":{"status":"ok","version":"%%s"}}'\'' "$build" ;; *) printf '\''{"ok":true,"data":{"service":{"version":"%%s"},"database":{"schema_version":%%s},"codex":{"configured":%%s,"available":%%s,"version":"%%s","account_state":"%%s","compatibility_status":"%%s"},"reconciliation":{"status":"%%s"}}}'\'' "$build" "$schema" "$configured" "$available" "$codex" "$account" "$compatibility" "$reconciliation" ;; esac\n' > "$root/fake-bin/curl"
chmod 700 "$root/fake-bin/systemctl" "$root/fake-bin/curl"
readydb="$root/ready.sqlite3"; sqlite3 "$readydb" "CREATE TABLE schema_migrations(version INTEGER);INSERT INTO schema_migrations VALUES(15)"
COMMONS_DB="$readydb" COMMONS_RELEASE_DIR="$root/releases/release-test" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_SYSTEMCTL="$root/fake-bin/systemctl" COMMONS_CURL="$root/fake-bin/curl" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/check-readiness.sh
readiness_curl_log="$root/readiness-curl.log"
COMMONS_PUBLIC_ORIGIN=https://commons.test COMMONS_CURL_LOG="$readiness_curl_log" COMMONS_DB="$readydb" COMMONS_RELEASE_DIR="$root/releases/release-test" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_SYSTEMCTL="$root/fake-bin/systemctl" COMMONS_CURL="$root/fake-bin/curl" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/check-readiness.sh
test "$(sed -n '1p' "$readiness_curl_log")" = '-fsS -H Host: commons.test http://127.0.0.1:8088/v1/health'
test "$(sed -n '2p' "$readiness_curl_log")" = '-fsS -H Host: 127.0.0.1:8088 http://127.0.0.1:8088/v1/internal/readiness'
if COMMONS_PUBLIC_ORIGIN=https://commons.test/path COMMONS_DB="$readydb" COMMONS_RELEASE_DIR="$root/releases/release-test" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_SYSTEMCTL="$root/fake-bin/systemctl" COMMONS_CURL="$root/fake-bin/curl" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/check-readiness.sh; then exit 1; fi
for setting in 'COMMONS_FAKE_BUILD=wrong' 'COMMONS_FAKE_CONFIGURED=false' 'COMMONS_FAKE_AVAILABLE=false' 'COMMONS_FAKE_CODEX_VERSION=0.148.0' 'COMMONS_FAKE_ACCOUNT=signed_out' 'COMMONS_FAKE_ACCOUNT=unknown' 'COMMONS_FAKE_COMPATIBILITY=incompatible' 'COMMONS_FAKE_COMPATIBILITY=unknown' 'COMMONS_FAKE_RECONCILIATION=attention'; do
	if env $setting COMMONS_DB="$readydb" COMMONS_RELEASE_DIR="$root/releases/release-test" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_SYSTEMCTL="$root/fake-bin/systemctl" COMMONS_CURL="$root/fake-bin/curl" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/check-readiness.sh; then exit 1; fi
done
chmod u+w "$root/releases/release-test/VERSION"; printf wrong > "$root/releases/release-test/VERSION"
if COMMONS_DB="$readydb" COMMONS_RELEASE_DIR="$root/releases/release-test" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_SYSTEMCTL="$root/fake-bin/systemctl" COMMONS_CURL="$root/fake-bin/curl" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/check-readiness.sh; then exit 1; fi
printf 'release-test\n' > "$root/releases/release-test/VERSION"; chmod a-w "$root/releases/release-test/VERSION"
sqlite3 "$readydb" 'UPDATE schema_migrations SET version=14'
if COMMONS_DB="$readydb" COMMONS_RELEASE_DIR="$root/releases/release-test" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_SYSTEMCTL="$root/fake-bin/systemctl" COMMONS_CURL="$root/fake-bin/curl" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/check-readiness.sh; then exit 1; fi

# The public-origin deployment gate requires exactly one durable binding in the
# real schema-009 table. This executes the staged deploy script with
# first-LAN-bind disabled, so a singular or otherwise stale table name fails
# before the disposable service is restarted.
mkdir "$root/public-releases"
/bin/sh ops/build-release.sh release-public "$root/commons-server-public"
COMMONS_RELEASE_ROOT="$root/public-releases" COMMONS_SERVER_SOURCE="$root/commons-server-public" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh release-public
COMMONS_CODEX_BIN="$root/public-releases/current/bin/codex"
export COMMONS_CODEX_BIN
bounddb="$root/bound.sqlite3"
sqlite3 "$bounddb" "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT,applied_at TEXT);INSERT INTO schema_migrations VALUES(15,'fixture','x');CREATE TABLE projects(id TEXT);CREATE TABLE tasks(id TEXT);CREATE TABLE archaeology_native_batches(id TEXT);CREATE TABLE human_account_bindings(principal TEXT PRIMARY KEY);INSERT INTO human_account_bindings VALUES('human:local-admin');"
boundlog="$root/bound-systemctl.log"
printf '#!/bin/sh\nprintf "%%s\\n" "$*" >> "$COMMONS_SYSTEMCTL_LOG"\nexit 0\n' > "$root/fake-bin/systemctl-bound"
printf '#!/bin/sh\ncase "$*" in *health*) printf %%s '\''{"ok":true,"data":{"status":"ok","version":"release-public"}}'\'' ;; *) printf %%s '\''{"ok":true,"data":{"service":{"version":"release-public"},"database":{"schema_version":15},"codex":{"configured":true,"available":true,"version":"0.147.0","account_state":"signed_in","compatibility_status":"compatible"},"reconciliation":{"status":"healthy"}}}'\'' ;; esac\n' > "$root/fake-bin/curl-bound"
chmod 700 "$root/fake-bin/systemctl-bound" "$root/fake-bin/curl-bound"
COMMONS_RELEASE_ROOT="$root/public-releases" COMMONS_DB="$bounddb" COMMONS_BACKUP_DIR="$root/bound-backups" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_PUBLIC_ORIGIN=https://commons.test COMMONS_ALLOW_FIRST_CODEX_BIND_LAN=false COMMONS_SYSTEMCTL="$root/fake-bin/systemctl-bound" COMMONS_SYSTEMCTL_LOG="$boundlog" COMMONS_CURL="$root/fake-bin/curl-bound" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/deploy-release.sh "$root/public-releases/release-public"
test "$(readlink "$root/public-releases/current")" = release-public
test "$(sqlite3 "$bounddb" 'SELECT count(*) FROM human_account_bindings')" -eq 1
test "$(sed -n '1p' "$boundlog")" = '--user restart codex-commons.service'
test "$(sed -n '2p' "$boundlog")" = '--user is-active --quiet codex-commons.service'
test "$(wc -l < "$boundlog")" -eq 2

# A failed upgraded release is stopped and atomically returns both current and
# the database to the matching pre-upgrade snapshot; WAL/SHM are cleared only
# after stop. This executes the actual deploy script with disposable commands.
/bin/sh ops/build-release.sh release-old "$root/commons-server-old"
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server-old" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh release-old
/bin/sh ops/build-release.sh release-new "$root/commons-server-new"
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server-new" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh release-new
COMMONS_CODEX_BIN="$root/releases/current/bin/codex"
export COMMONS_CODEX_BIN
ln -s release-old "$root/releases/current"
rollbackdb="$root/rollback.sqlite3"; sqlite3 "$rollbackdb" "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT,applied_at TEXT);INSERT INTO schema_migrations VALUES(15,'fixture','x');CREATE TABLE projects(id TEXT);CREATE TABLE tasks(id TEXT);CREATE TABLE archaeology_native_batches(id TEXT);CREATE TABLE installation_status(id INTEGER PRIMARY KEY,backup_status TEXT,backup_verified_at TEXT,updated_at TEXT);INSERT INTO installation_status VALUES(1,'unknown',NULL,'x');"
printf wal > "$rollbackdb-wal"; printf shm > "$rollbackdb-shm"
systemlog="$root/systemctl.log"; systemcount="$root/systemctl.count"
printf '#!/bin/sh\nprintf "%%s\\n" "$*" >> "$COMMONS_SYSTEMCTL_LOG"\nif test "$1 $2 $3" = "--user restart codex-commons.service"; then n=0; test ! -f "$COMMONS_SYSTEMCTL_COUNT" || n=$(cat "$COMMONS_SYSTEMCTL_COUNT"); n=$((n+1)); printf %%s "$n" > "$COMMONS_SYSTEMCTL_COUNT"; if test "$n" -eq 1; then sqlite3 "$COMMONS_DB" "INSERT INTO projects VALUES('"'"'release-new-marker'"'"')"; fi; fi\nexit 0\n' > "$root/fake-bin/systemctl"; chmod 700 "$root/fake-bin/systemctl"
curllog="$root/curl.log"
printf '#!/bin/sh\nprintf "%%s\\n" "$*" >> "$COMMONS_CURL_LOG"\nn=$(cat "$COMMONS_SYSTEMCTL_COUNT"); if test "$n" -ge 2; then build=release-old; else build=wrong; fi\ncase "$*" in *health*) printf '\''{"ok":true,"data":{"status":"ok","version":"%%s"}}'\'' "$build" ;; *) printf '\''{"ok":true,"data":{"service":{"version":"%%s"},"database":{"schema_version":15},"codex":{"configured":true,"available":true,"version":"0.147.0","account_state":"signed_in","compatibility_status":"compatible"},"reconciliation":{"status":"healthy"}}}'\'' "$build" ;; esac\n' > "$root/fake-bin/curl"; chmod 700 "$root/fake-bin/curl"
if COMMONS_RELEASE_ROOT="$root/releases" COMMONS_DB="$rollbackdb" COMMONS_BACKUP_DIR="$root/rollback-backups" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_SYSTEMCTL="$root/fake-bin/systemctl" COMMONS_SYSTEMCTL_LOG="$systemlog" COMMONS_SYSTEMCTL_COUNT="$systemcount" COMMONS_CURL="$root/fake-bin/curl" COMMONS_CURL_LOG="$curllog" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/deploy-release.sh "$root/releases/release-new"; then exit 1; fi
test "$(readlink "$root/releases/current")" = release-old
test ! -e "$rollbackdb-wal"; test ! -e "$rollbackdb-shm"
test "$(sqlite3 "$rollbackdb" 'SELECT max(version) FROM schema_migrations')" = 15
test "$(sqlite3 "$rollbackdb" "SELECT count(*) FROM projects WHERE id='release-new-marker'")" = 0
test "$(sed -n '3p' "$systemlog")" = '--user stop codex-commons.service'
test "$(sed -n '4p' "$systemlog")" = '--user restart codex-commons.service'
test "$(sed -n '5p' "$systemlog")" = '--user is-active --quiet codex-commons.service'
test "$(wc -l < "$curllog")" -eq 4

# A first deployment that creates state but never becomes ready is stopped,
# leaves no current pointer, and removes its unmatched candidate database.
mkdir "$root/first-releases"
/bin/sh ops/build-release.sh release-first "$root/commons-server-first"
COMMONS_RELEASE_ROOT="$root/first-releases" COMMONS_SERVER_SOURCE="$root/commons-server-first" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh release-first
COMMONS_CODEX_BIN="$root/first-releases/current/bin/codex"
export COMMONS_CODEX_BIN
firstdb="$root/first.sqlite3"; firstlog="$root/first-systemctl.log"
printf '#!/bin/sh\nprintf "%%s\\n" "$*" >> "$COMMONS_SYSTEMCTL_LOG"\nif test "$1 $2 $3" = "--user restart codex-commons.service"; then sqlite3 "$COMMONS_DB" "CREATE TABLE schema_migrations(version INTEGER);INSERT INTO schema_migrations VALUES(15);"; fi\nexit 0\n' > "$root/fake-bin/systemctl-first"; chmod 700 "$root/fake-bin/systemctl-first"
printf '#!/bin/sh\nprintf %%s '\''{"ok":true,"data":{"status":"ok","version":"wrong"}}'\''\n' > "$root/fake-bin/curl-first"; chmod 700 "$root/fake-bin/curl-first"
if COMMONS_RELEASE_ROOT="$root/first-releases" COMMONS_DB="$firstdb" COMMONS_BACKUP_DIR="$root/first-backups" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" COMMONS_SYSTEMCTL="$root/fake-bin/systemctl-first" COMMONS_SYSTEMCTL_LOG="$firstlog" COMMONS_CURL="$root/fake-bin/curl-first" COMMONS_READINESS_ATTEMPTS=1 /bin/sh ops/deploy-release.sh "$root/first-releases/release-first"; then exit 1; fi
test ! -e "$root/first-releases/current"; test ! -e "$firstdb"; test ! -e "$firstdb-wal"; test ! -e "$firstdb-shm"
test "$(sed -n '1p' "$firstlog")" = '--user restart codex-commons.service'
test "$(sed -n '2p' "$firstlog")" = '--user is-active --quiet codex-commons.service'
test "$(sed -n '3p' "$firstlog")" = '--user stop codex-commons.service'
test "$(wc -l < "$firstlog")" -eq 3

# Restore the main disposable release after the Phase 1.1 tamper cases before
# exercising Phase 1.2 mode and ownership checks.
cp "$root/source-web/index.html" "$release_dir/web/index.html"
chmod 0444 "$release_dir/web/index.html"
chmod 0555 "$release_dir"

# Phase 1.2: deterministic mode and ownership normalization.
phase12_uid=$(id -u) || exit 1
phase12_gid=$(id -g) || exit 1
phase12_primary_group=$(id -gn) || exit 1
phase12_alt_group=""
phase12_alt_gid=""
for gname in $(id -Gn); do
	if test "$gname" != "$phase12_primary_group"; then
		phase12_alt_group=$gname
		break
	fi
done
for gid in $(id -G); do
	if test "$gid" != "$phase12_gid"; then
		phase12_alt_gid=$gid
		break
	fi
done
if test -z "$phase12_alt_group"; then
	phase12_alt_group=$phase12_primary_group
fi
if test -z "$phase12_alt_gid"; then
	phase12_alt_gid=$phase12_gid
fi

# Ensure the current clean tree has the Phase 1.2 normalized modes/ownership.
test "$(stat -c %a "$release_dir")" = 555
test "$(stat -c %u "$release_dir")" = "$phase12_uid"
test "$(stat -c %g "$release_dir")" = "$phase12_gid"
test -z "$(find "$release_dir" -type d \! -perm 0555 -print -quit)"
test -z "$(find "$release_dir" -type f \! -perm 0444 \! -perm 0555 -print -quit)"
phase12_tmp=$(mktemp)
find "$release_dir" -type f -perm 0555 -printf "%P\n" | LC_ALL=C sort > "$phase12_tmp"
printf "%s\n" "bin/codex" "bin/codex-code-mode-host" "codex-resources/bwrap" "codex-resources/zsh/bin/zsh" "codex-path/rg" "commons-server" | LC_ALL=C sort | cmp -s - "$phase12_tmp" || exit 1
rm -f -- "$phase12_tmp"
test "$(stat -c %a "$release_dir/VERSION")" = 444
test "$(stat -c %a "$release_dir/SHA256SUMS")" = 444
test "$(stat -c %a "$release_dir/codex-package.json")" = 444
test -z "$(find "$release_dir/web" "$release_dir/ops" -type f \! -perm 0444 -print -quit)"

# Root mode drift is rejected before manifest verification.
chmod u+w "$release_dir"
chmod 0777 "$release_dir"
expect_verify_failure
chmod 0555 "$release_dir"
verify_test_release

# Nested directory mode drift is rejected.
chmod u+w "$release_dir/web"
chmod 0777 "$release_dir/web"
expect_verify_failure
chmod 0555 "$release_dir/web"
chmod u+w "$release_dir/ops"
chmod 0777 "$release_dir/ops"
expect_verify_failure
chmod 0555 "$release_dir/ops"
chmod 0555 "$release_dir"
verify_test_release

# Explicit executable mode drift is rejected (each of the six).
for exec_file in commons-server bin/codex bin/codex-code-mode-host codex-resources/bwrap codex-resources/zsh/bin/zsh codex-path/rg; do
	chmod u+w "$release_dir/$exec_file"
	chmod 0644 "$release_dir/$exec_file"
	expect_verify_failure
	chmod 0555 "$release_dir/$exec_file"
	verify_test_release
done

# Ordinary file mode drift is rejected (VERSION, codex-package.json, web asset, ops file).
for ordinary_file in VERSION codex-package.json web/index.html ops/backup.sh SHA256SUMS; do
	chmod u+w "$release_dir/$ordinary_file"
	chmod 0644 "$release_dir/$ordinary_file"
	expect_verify_failure
	chmod 0444 "$release_dir/$ordinary_file"
	verify_test_release
done
# Also reject unexpected executable bits on ordinary files.
chmod u+w "$release_dir/web/index.html"
chmod 0555 "$release_dir/web/index.html"
expect_verify_failure
chmod 0444 "$release_dir/web/index.html"
verify_test_release

# Manifest mode drift is rejected (SHA256SUMS must be 0444).
chmod u+w "$release_dir/SHA256SUMS"
chmod 0644 "$release_dir/SHA256SUMS"
expect_verify_failure
chmod 0444 "$release_dir/SHA256SUMS"
verify_test_release

# Source execute-bit normalization: staged tree must not inherit source +x.
chmod 0755 "$root/source-web/index.html"
/bin/sh ops/build-release.sh release-execbit "$root/commons-server-execbit"
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server-execbit" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh ops/stage-release.sh release-execbit
test "$(stat -c %a "$root/releases/release-execbit/web/index.html")" = 444
test "$(stat -c %a "$root/releases/release-execbit")" = 555
test -z "$(find "$root/releases/release-execbit" -type d \! -perm 0555 -print -quit)"
chmod 0644 "$root/source-web/index.html"
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_RELEASE_DIR="$root/releases/release-execbit" COMMONS_CODEX_BIN="$root/releases/release-execbit/bin/codex" COMMONS_WEB_DIR="$root/releases/release-execbit/web" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" /bin/sh "$root/releases/release-execbit/ops/verify-release.sh" >/dev/null 2>&1 || exit 1
chmod -R u+w "$root/releases/release-execbit" 2>/dev/null || true
rm -rf -- "$root/releases/release-execbit"

# Restrictive umask (077) must still produce deterministic 0555/0444.
(umask 077; /bin/sh ops/build-release.sh release-umask077 "$root/commons-server-umask077")
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server-umask077" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh -c "umask 077; exec /bin/sh ops/stage-release.sh release-umask077"
test "$(stat -c %a "$root/releases/release-umask077")" = 555
test -z "$(find "$root/releases/release-umask077" -type d \! -perm 0555 -print -quit)"
test "$(stat -c %a "$root/releases/release-umask077/VERSION")" = 444
test "$(stat -c %a "$root/releases/release-umask077/SHA256SUMS")" = 444
test "$(stat -c %a "$root/releases/release-umask077/commons-server")" = 555
test -z "$(find "$root/releases/release-umask077/web" "$root/releases/release-umask077/ops" -type f \! -perm 0444 -print -quit)"
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_RELEASE_DIR="$root/releases/release-umask077" COMMONS_CODEX_BIN="$root/releases/release-umask077/bin/codex" COMMONS_WEB_DIR="$root/releases/release-umask077/web" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" /bin/sh "$root/releases/release-umask077/ops/verify-release.sh" >/dev/null 2>&1 || exit 1
chmod -R u+w "$root/releases/release-umask077" 2>/dev/null || true
rm -rf -- "$root/releases/release-umask077"

# Permissive umask (000) must still produce deterministic 0555/0444.
(umask 000; /bin/sh ops/build-release.sh release-umask000 "$root/commons-server-umask000")
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_SERVER_SOURCE="$root/commons-server-umask000" COMMONS_WEB_SOURCE="$root/source-web" COMMONS_CODEX_BUNDLE_SOURCE="$root/codex-bundle" /bin/sh -c "umask 000; exec /bin/sh ops/stage-release.sh release-umask000"
test "$(stat -c %a "$root/releases/release-umask000")" = 555
test -z "$(find "$root/releases/release-umask000" -type d \! -perm 0555 -print -quit)"
test "$(stat -c %a "$root/releases/release-umask000/VERSION")" = 444
test "$(stat -c %a "$root/releases/release-umask000/SHA256SUMS")" = 444
test "$(stat -c %a "$root/releases/release-umask000/commons-server")" = 555
test -z "$(find "$root/releases/release-umask000/web" "$root/releases/release-umask000/ops" -type f \! -perm 0444 -print -quit)"
COMMONS_RELEASE_ROOT="$root/releases" COMMONS_RELEASE_DIR="$root/releases/release-umask000" COMMONS_CODEX_BIN="$root/releases/release-umask000/bin/codex" COMMONS_WEB_DIR="$root/releases/release-umask000/web" COMMONS_CODEX_SHA256="$sha" COMMONS_CODE_MODE_HOST_SHA256="$host_sha" COMMONS_CODEX_BWRAP_SHA256="$bwrap_sha" COMMONS_CODEX_ZSH_SHA256="$zsh_sha" COMMONS_CODEX_RG_SHA256="$rg_sha" COMMONS_CODEX_PACKAGE_SHA256="$package_sha" /bin/sh "$root/releases/release-umask000/ops/verify-release.sh" >/dev/null 2>&1 || exit 1
chmod -R u+w "$root/releases/release-umask000" 2>/dev/null || true
rm -rf -- "$root/releases/release-umask000"

# Ownership mismatch without sudo: use only an available supplementary group.
if test "$phase12_alt_group" != "$phase12_primary_group" || test "$phase12_alt_gid" != "$phase12_gid"; then
	chown -h "$phase12_uid:$phase12_alt_gid" "$release_dir" 2>/dev/null || chgrp "$phase12_alt_group" "$release_dir"
	expect_verify_failure
	chown -h "$phase12_uid:$phase12_gid" "$release_dir" || chgrp "$phase12_primary_group" "$release_dir" || exit 1
	verify_test_release
	chown "$phase12_uid:$phase12_alt_gid" "$release_dir/web/index.html" 2>/dev/null || chgrp "$phase12_alt_group" "$release_dir/web/index.html"
	expect_verify_failure
	chown "$phase12_uid:$phase12_gid" "$release_dir/web/index.html" || chgrp "$phase12_primary_group" "$release_dir/web/index.html" || exit 1
	verify_test_release
	chown "$phase12_uid:$phase12_alt_gid" "$release_dir/web" 2>/dev/null || chgrp "$phase12_alt_group" "$release_dir/web"
	expect_verify_failure
	chown "$phase12_uid:$phase12_gid" "$release_dir/web" || chgrp "$phase12_primary_group" "$release_dir/web" || exit 1
	test -z "$(find "$release_dir" -mindepth 1 \! -uid "$phase12_uid" -print -quit)"
	test -z "$(find "$release_dir" -mindepth 1 \! -gid "$phase12_gid" -print -quit)"
	verify_test_release
else
	echo "no alternate group available: ownership drift test skipped" >&2
	exit 1
fi

# Restored success after all Phase 1.2 drifts.
chmod -R u+w "$release_dir" 2>/dev/null || true
chmod 0555 "$release_dir" 2>/dev/null || true
find "$release_dir" -type d -exec chmod 0555 {} + 2>/dev/null || true
find "$release_dir" -type f -exec chmod 0444 {} + 2>/dev/null || true
chmod 0555 "$release_dir/commons-server" "$release_dir/bin/codex" "$release_dir/bin/codex-code-mode-host" "$release_dir/codex-resources/bwrap" "$release_dir/codex-resources/zsh/bin/zsh" "$release_dir/codex-path/rg" 2>/dev/null || true
test "$(stat -c %a "$release_dir")" = 555
test -z "$(find "$release_dir" -type d \! -perm 0555 -print -quit)"
verify_test_release
