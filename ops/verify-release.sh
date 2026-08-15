#!/bin/sh
set -eu
: "${COMMONS_RELEASE_DIR:?COMMONS_RELEASE_DIR is required}"
: "${COMMONS_RELEASE_ROOT:?COMMONS_RELEASE_ROOT is required}"
: "${COMMONS_CODEX_SHA256:?COMMONS_CODEX_SHA256 is required}"
: "${COMMONS_CODE_MODE_HOST_SHA256:?COMMONS_CODE_MODE_HOST_SHA256 is required}"
: "${COMMONS_CODEX_BWRAP_SHA256:?COMMONS_CODEX_BWRAP_SHA256 is required}"
: "${COMMONS_CODEX_ZSH_SHA256:?COMMONS_CODEX_ZSH_SHA256 is required}"
: "${COMMONS_CODEX_RG_SHA256:?COMMONS_CODEX_RG_SHA256 is required}"
: "${COMMONS_CODEX_PACKAGE_SHA256:?COMMONS_CODEX_PACKAGE_SHA256 is required}"
case "$COMMONS_RELEASE_DIR" in /*) ;; *) exit 64;; esac
COMMONS_RELEASE_DIR=$(readlink -f "$COMMONS_RELEASE_DIR"); COMMONS_RELEASE_ROOT=$(readlink -f "$COMMONS_RELEASE_ROOT")
case "$COMMONS_RELEASE_DIR" in "$COMMONS_RELEASE_ROOT"/*) ;; *) exit 64;; esac
test "$(dirname "$COMMONS_RELEASE_DIR")" = "$COMMONS_RELEASE_ROOT"
for file in commons-server bin/codex bin/codex-code-mode-host codex-resources/bwrap codex-resources/zsh/bin/zsh codex-path/rg; do test -x "$COMMONS_RELEASE_DIR/$file"; test -f "$COMMONS_RELEASE_DIR/$file"; test ! -L "$COMMONS_RELEASE_DIR/$file"; test "$(stat -c %a "$COMMONS_RELEASE_DIR/$file")" = 555; done
test -f "$COMMONS_RELEASE_DIR/codex-package.json"; test ! -L "$COMMONS_RELEASE_DIR/codex-package.json"; test "$(stat -c %a "$COMMONS_RELEASE_DIR/codex-package.json")" = 444
test -r "$COMMONS_RELEASE_DIR/web/index.html"; test -f "$COMMONS_RELEASE_DIR/VERSION"; test ! -L "$COMMONS_RELEASE_DIR/VERSION"
release_id=$(sed -n '1p' "$COMMONS_RELEASE_DIR/VERSION"); test -n "$release_id"; test "$(wc -l < "$COMMONS_RELEASE_DIR/VERSION")" -eq 1; test "$release_id" = "$(basename "$COMMONS_RELEASE_DIR")"
test "$(COMMONS_RELEASE_IDENTITY_FILE="$COMMONS_RELEASE_DIR/VERSION" "$COMMONS_RELEASE_DIR/commons-server" --build-id)" = "$release_id"
go version -m "$COMMONS_RELEASE_DIR/commons-server" | grep -Fq 'path'"$(printf '\t')"'codex-commons/cmd/commons-server'; go version -m "$COMMONS_RELEASE_DIR/commons-server" | grep -Fq 'build'"$(printf '\t')"'-trimpath=true'
test "$(readlink -f "${COMMONS_CODEX_BIN:-/missing}")" = "$COMMONS_RELEASE_DIR/bin/codex"; test "$(readlink -f "${COMMONS_WEB_DIR:-/missing}")" = "$COMMONS_RELEASE_DIR/web"
for spec in "bin/codex:$COMMONS_CODEX_SHA256" "bin/codex-code-mode-host:$COMMONS_CODE_MODE_HOST_SHA256" "codex-resources/bwrap:$COMMONS_CODEX_BWRAP_SHA256" "codex-resources/zsh/bin/zsh:$COMMONS_CODEX_ZSH_SHA256" "codex-path/rg:$COMMONS_CODEX_RG_SHA256" "codex-package.json:$COMMONS_CODEX_PACKAGE_SHA256"; do file=${spec%%:*}; sha=${spec#*:}; test "$(sha256sum "$COMMONS_RELEASE_DIR/$file" | awk '{print $1}')" = "$sha"; done
test "$($COMMONS_RELEASE_DIR/bin/codex --version | awk '{print $2}')" = 0.147.0
test -f "$COMMONS_RELEASE_DIR/SHA256SUMS"; test ! -L "$COMMONS_RELEASE_DIR/SHA256SUMS"
(cd "$COMMONS_RELEASE_DIR" && test -z "$(find . -type l -print -quit)" && test -z "$(find . \( -name '*_test*' -o -name 'test-*' -o -name '*.map' -o -name '*.tmp' \) -print -quit)" && test ! -e ops/build-release.sh && test ! -e ops/stage-release.sh && test ! -e ops/test-ops.sh)
(cd "$COMMONS_RELEASE_DIR" &&
	test -z "$(find VERSION commons-server bin codex-resources codex-path codex-package.json web ops \! -type f \! -type d -print -quit)" &&
	test -z "$(find VERSION commons-server bin codex-resources codex-path codex-package.json web ops -name '*\\*' -print -quit)" &&
	test -z "$(grep -Ev '^[0-9a-f]{64}  .+$' SHA256SUMS || true)" &&
	manifest_files=$(cut -c67- SHA256SUMS | sort) &&
	actual_files=$(find VERSION commons-server bin codex-resources codex-path codex-package.json web ops -type f | sort) &&
	test "$manifest_files" = "$actual_files" && sha256sum -c SHA256SUMS)
