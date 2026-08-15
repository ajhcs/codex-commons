#!/bin/sh
set -eu
: "${1:?release id required}"
: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT required}"
: "${COMMONS_SERVER_SOURCE:?COMMONS_SERVER_SOURCE required}"
: "${COMMONS_WEB_SOURCE:?COMMONS_WEB_SOURCE required}"
: "${COMMONS_CODEX_BUNDLE_SOURCE:?COMMONS_CODEX_BUNDLE_SOURCE required}"
id=$1
case "$id" in *[!A-Za-z0-9._-]*|'') exit 64;; esac
case "$COMMONS_RELEASE_ROOT:$COMMONS_WEB_SOURCE" in /*:/*) ;; *) exit 64;; esac
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
test -d "$COMMONS_WEB_SOURCE"; test ! -L "$COMMONS_WEB_SOURCE"
test -d ops; test ! -L ops
# SHA256SUMS is deliberately line-oriented. Keep packaged path components to
# a grammar that cannot be confused with shell, awk, or sha256sum syntax.
test -z "$(find "$COMMONS_WEB_SOURCE" ops -mindepth 1 \( -type l -o \! -type f \! -type d -o -name '*[!A-Za-z0-9._-]*' \) -print -quit)"
stage_uid=$(id -u) || exit 64
stage_gid=$(id -g) || exit 64
case "$stage_uid" in *[!0-9]*|'') exit 64;; esac
case "$stage_gid" in *[!0-9]*|'') exit 64;; esac
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
chmod 0644 "$target/VERSION"
chmod 0755 "$target"
(cd "$target" && find . -type f \! -path './SHA256SUMS' -printf '%P\0' | LC_ALL=C sort -z | xargs -0 sha256sum -- > SHA256SUMS)
chown -R "$stage_uid:$stage_gid" "$target" || exit 64
test -z "$(find "$target" \! -uid "$stage_uid" -print -quit)" || exit 64
test -z "$(find "$target" \! -gid "$stage_gid" -print -quit)" || exit 64
chmod 0555 "$target" || exit 64
find "$target" -type d -exec chmod 0555 {} + || exit 64
find "$target" -type f -exec chmod 0444 {} + || exit 64
chmod 0555 "$target/commons-server" "$target/bin/codex" "$target/bin/codex-code-mode-host" "$target/codex-resources/bwrap" "$target/codex-resources/zsh/bin/zsh" "$target/codex-path/rg" || exit 64
test "$(stat -c %a "$target")" = 555 || exit 64
test "$(stat -c %a "$target/SHA256SUMS")" = 444 || exit 64
