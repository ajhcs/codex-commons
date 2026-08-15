#!/bin/sh
set -eu
: "${1:?release id required}"
: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT required}"
: "${COMMONS_SERVER_SOURCE:?COMMONS_SERVER_SOURCE required}"
: "${COMMONS_WEB_SOURCE:?COMMONS_WEB_SOURCE required}"
: "${COMMONS_CODEX_BUNDLE_SOURCE:?COMMONS_CODEX_BUNDLE_SOURCE required}"
id=$1
case "$id" in *[!A-Za-z0-9._-]*|'') exit 64;; esac
bundle=$COMMONS_CODEX_BUNDLE_SOURCE
case "$bundle" in /*) ;; *) exit 64;; esac
test -d "$bundle"; test ! -L "$bundle"
test -z "$(find "$bundle" -type l -print -quit)"
test -z "$(find "$bundle" \! -type f \! -type d -print -quit)"
test "$(find "$bundle" -type f | wc -l)" -eq 6
for file in bin/codex bin/codex-code-mode-host codex-resources/bwrap codex-resources/zsh/bin/zsh codex-path/rg; do test -f "$bundle/$file"; test ! -L "$bundle/$file"; test -x "$bundle/$file"; done
test -f "$bundle/codex-package.json"; test ! -L "$bundle/codex-package.json"
target="$COMMONS_RELEASE_ROOT/$id"
test ! -e "$target"
test "$($COMMONS_SERVER_SOURCE --build-id)" = "$id"
go version -m "$COMMONS_SERVER_SOURCE" | grep -Fq 'path'"$(printf '\t')"'codex-commons/cmd/commons-server'
go version -m "$COMMONS_SERVER_SOURCE" | grep -Fq 'build'"$(printf '\t')"'-trimpath=true'
test -z "$(find "$COMMONS_WEB_SOURCE" ops \! -type f \! -type d -print -quit)"
test -z "$(find "$COMMONS_WEB_SOURCE" ops -name '*
*' -print -quit)"
mkdir -m 0755 "$target" "$target/web" "$target/ops" "$target/bin" "$target/codex-resources" "$target/codex-resources/zsh" "$target/codex-resources/zsh/bin" "$target/codex-path"
printf '%s\n' "$id" > "$target/VERSION"
cp "$COMMONS_SERVER_SOURCE" "$target/commons-server"
for file in bin/codex bin/codex-code-mode-host codex-resources/bwrap codex-resources/zsh/bin/zsh codex-path/rg codex-package.json; do cp "$bundle/$file" "$target/$file"; done
cp -R "$COMMONS_WEB_SOURCE"/. "$target/web/"
for runtime_op in backup.sh check-readiness.sh deploy-release.sh record-evidence.sh seal-archive.sh verify-release.sh verify-restore.sh; do cp "ops/$runtime_op" "$target/ops/$runtime_op"; done
chmod 0755 "$target/commons-server" "$target/bin/codex" "$target/bin/codex-code-mode-host" "$target/codex-resources/bwrap" "$target/codex-resources/zsh/bin/zsh" "$target/codex-path/rg"
chmod 0644 "$target/codex-package.json"
find "$target/web" "$target/ops" -type d -exec chmod 0755 {} +
find "$target/web" -type f -exec chmod 0644 {} +
find "$target/ops" -type f -exec chmod 0644 {} +
(cd "$target" && find VERSION commons-server bin codex-resources codex-path codex-package.json web ops -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
chmod -R a-w "$target"
