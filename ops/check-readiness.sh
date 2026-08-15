#!/bin/sh
set -eu
: "${COMMONS_DB:?COMMONS_DB is required}"
: "${COMMONS_RELEASE_DIR:?COMMONS_RELEASE_DIR is required}"
: "${COMMONS_CODEX_SHA256:?COMMONS_CODEX_SHA256 is required}"
: "${COMMONS_CODE_MODE_HOST_SHA256:?required}"
: "${COMMONS_CODEX_BWRAP_SHA256:?required}"
: "${COMMONS_CODEX_ZSH_SHA256:?required}"
: "${COMMONS_CODEX_RG_SHA256:?required}"
: "${COMMONS_CODEX_PACKAGE_SHA256:?required}"
test "$(readlink -f "${COMMONS_CODEX_BIN:-/missing}")" = "$COMMONS_RELEASE_DIR/bin/codex"
for file in bin/codex bin/codex-code-mode-host codex-resources/bwrap codex-resources/zsh/bin/zsh codex-path/rg; do
	test -f "$COMMONS_RELEASE_DIR/$file"; test ! -L "$COMMONS_RELEASE_DIR/$file"; test -x "$COMMONS_RELEASE_DIR/$file"; test "$(stat -c %a "$COMMONS_RELEASE_DIR/$file")" = 555
done
test -f "$COMMONS_RELEASE_DIR/codex-package.json"; test ! -L "$COMMONS_RELEASE_DIR/codex-package.json"; test "$(stat -c %a "$COMMONS_RELEASE_DIR/codex-package.json")" = 444
release_id=$(sed -n '1p' "$COMMONS_RELEASE_DIR/VERSION")
test -n "$release_id"
test "$release_id" = "$(basename "$(readlink -f "$COMMONS_RELEASE_DIR")")"
systemctl_cmd=${COMMONS_SYSTEMCTL:-systemctl}; curl_cmd=${COMMONS_CURL:-curl}; attempts=${COMMONS_READINESS_ATTEMPTS:-30}
health_host=127.0.0.1:8088
if [ -n "${COMMONS_PUBLIC_ORIGIN:-}" ]; then
	case "$COMMONS_PUBLIC_ORIGIN" in
	https://*) health_host=${COMMONS_PUBLIC_ORIGIN#https://} ;;
	*) exit 64 ;;
	esac
	case "$health_host" in ""|*[!A-Za-z0-9.:_-]*) exit 64 ;; esac
fi
while [ "$attempts" -gt 0 ]; do
	if "$systemctl_cmd" --user is-active --quiet codex-commons.service; then
		health=$($curl_cmd -fsS -H "Host: $health_host" http://127.0.0.1:8088/v1/health 2>/dev/null || true)
		status=$($curl_cmd -fsS -H "Host: 127.0.0.1:8088" http://127.0.0.1:8088/v1/internal/readiness 2>/dev/null || true)
		if printf %s "$health" | grep -Fq '"status":"ok"' &&
			printf %s "$health" | grep -Fq '"version":"'"$release_id"'"' &&
			printf %s "$status" | grep -Fq '"version":"'"$release_id"'"' &&
			printf %s "$status" | grep -Fq '"schema_version":15' &&
			printf %s "$status" | grep -Fq '"configured":true' &&
			printf %s "$status" | grep -Fq '"available":true' &&
			printf %s "$status" | grep -Fq '"version":"0.147.0"' &&
			printf %s "$status" | grep -Fq '"account_state":"signed_in"' &&
			printf %s "$status" | grep -Fq '"compatibility_status":"compatible"' &&
			printf %s "$status" | grep -Fq '"status":"healthy"' &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/bin/codex" | awk '{print $1}')" = "$COMMONS_CODEX_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/bin/codex-code-mode-host" | awk '{print $1}')" = "$COMMONS_CODE_MODE_HOST_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/codex-resources/bwrap" | awk '{print $1}')" = "$COMMONS_CODEX_BWRAP_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/codex-resources/zsh/bin/zsh" | awk '{print $1}')" = "$COMMONS_CODEX_ZSH_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/codex-path/rg" | awk '{print $1}')" = "$COMMONS_CODEX_RG_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/codex-package.json" | awk '{print $1}')" = "$COMMONS_CODEX_PACKAGE_SHA256" &&
			test "$(sqlite3 "$COMMONS_DB" 'SELECT COALESCE(max(version),0) FROM schema_migrations')" -eq 15; then exit 0; fi
	fi
	attempts=$((attempts-1)); [ "$attempts" -eq 0 ] || sleep 1
done
exit 1
