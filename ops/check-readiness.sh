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
runtime_component_ready() {
	component_name=$1
	component_policy=$(printf %s "$status_compact" | sed -n 's/.*"'"$component_name"'":{"state":"\([^"]*\)","ready":\([^,]*\),"required":\([^,]*\),.*/\1:\2:\3/p')
	[ "$component_policy" = healthy:true:true ]
}
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
		health_compact=$(printf %s "$health" | tr -d '[:space:]')
		status_compact=$(printf %s "$status" | tr -d '[:space:]')
		runtime_policy=$(printf %s "$status_compact" | sed -n 's/.*"runtime":{"mode":"\([^"]*\)","required":\([^,]*\),"state":"[^"]*","ready":\([^,]*\),.*/\1:\2:\3/p')
		runtime_mode=
		runtime_required=
		runtime_ready=
		if [ -n "$runtime_policy" ]; then
			runtime_mode=${runtime_policy%%:*}
			runtime_ready=${runtime_policy##*:}
			runtime_required=${runtime_policy#*:}
			runtime_required=${runtime_required%%:*}
		fi
		runtime_policy_ready=false
		case "$runtime_mode:$runtime_required:$runtime_ready" in
		optional:false:true|required:true:true) runtime_policy_ready=true ;;
		esac
		if [ "$runtime_policy_ready" = true ] &&
			printf %s "$health_compact" | grep -Fq '"status":"ok"' &&
			printf %s "$health_compact" | grep -Fq '"version":"'"$release_id"'"' &&
			printf %s "$status_compact" | grep -Fq '"service":{"version":"'"$release_id"'"' &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/bin/codex" | awk '{print $1}')" = "$COMMONS_CODEX_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/bin/codex-code-mode-host" | awk '{print $1}')" = "$COMMONS_CODE_MODE_HOST_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/codex-resources/bwrap" | awk '{print $1}')" = "$COMMONS_CODEX_BWRAP_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/codex-resources/zsh/bin/zsh" | awk '{print $1}')" = "$COMMONS_CODEX_ZSH_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/codex-path/rg" | awk '{print $1}')" = "$COMMONS_CODEX_RG_SHA256" &&
			test "$(sha256sum "$COMMONS_RELEASE_DIR/codex-package.json" | awk '{print $1}')" = "$COMMONS_CODEX_PACKAGE_SHA256" &&
			test "$(sqlite3 "$COMMONS_DB" 'SELECT COALESCE(max(version),0) FROM schema_migrations')" -eq 15; then
			codex_policy_ready=false
			if [ "$runtime_required" = false ]; then
				# Optional Codex degradation may pass the service gate. The
				# runtime snapshot remains the source of Codex health details;
				# do not mistake core readiness for a healthy Codex capability.
				codex_policy_ready=true
			elif runtime_component_ready codex &&
				runtime_component_ready supervisor &&
				runtime_component_ready account &&
				runtime_component_ready model; then
				codex_policy_ready=true
			fi
			if [ "$codex_policy_ready" = true ]; then exit 0; fi
		fi
	fi
	attempts=$((attempts-1)); [ "$attempts" -eq 0 ] || sleep 1
done
exit 1
