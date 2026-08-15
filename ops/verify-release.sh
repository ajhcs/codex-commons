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
test -d "$COMMONS_RELEASE_DIR"; test ! -L "$COMMONS_RELEASE_DIR"
COMMONS_RELEASE_DIR=$(readlink -f "$COMMONS_RELEASE_DIR"); COMMONS_RELEASE_ROOT=$(readlink -f "$COMMONS_RELEASE_ROOT")
case "$COMMONS_RELEASE_DIR" in "$COMMONS_RELEASE_ROOT"/*) ;; *) exit 64;; esac
test "$(dirname "$COMMONS_RELEASE_DIR")" = "$COMMONS_RELEASE_ROOT"
verify_uid=$(id -u) || exit 64
verify_gid=$(id -g) || exit 64
case "$verify_uid" in *[!0-9]*|'') exit 64;; esac
case "$verify_gid" in *[!0-9]*|'') exit 64;; esac
test "$(stat -c %u "$COMMONS_RELEASE_DIR")" = "$verify_uid" || exit 64
test "$(stat -c %g "$COMMONS_RELEASE_DIR")" = "$verify_gid" || exit 64
test "$(stat -c %a "$COMMONS_RELEASE_DIR")" = 555 || exit 64
test -z "$(find "$COMMONS_RELEASE_DIR" -mindepth 1 \! -uid "$verify_uid" -print -quit)" || exit 64
test -z "$(find "$COMMONS_RELEASE_DIR" -mindepth 1 \! -gid "$verify_gid" -print -quit)" || exit 64
test -z "$(find "$COMMONS_RELEASE_DIR" -type d \! -perm 0555 -print -quit)" || exit 64
test -z "$(find "$COMMONS_RELEASE_DIR" -type f \! -perm 0444 \! -perm 0555 -print -quit)" || exit 64
for file in commons-server bin/codex bin/codex-code-mode-host codex-resources/bwrap codex-resources/zsh/bin/zsh codex-path/rg; do test -x "$COMMONS_RELEASE_DIR/$file"; test -f "$COMMONS_RELEASE_DIR/$file"; test ! -L "$COMMONS_RELEASE_DIR/$file"; test "$(stat -c %a "$COMMONS_RELEASE_DIR/$file")" = 555; test "$(stat -c %u "$COMMONS_RELEASE_DIR/$file")" = "$verify_uid"; test "$(stat -c %g "$COMMONS_RELEASE_DIR/$file")" = "$verify_gid"; done
test -f "$COMMONS_RELEASE_DIR/codex-package.json"; test ! -L "$COMMONS_RELEASE_DIR/codex-package.json"; test "$(stat -c %a "$COMMONS_RELEASE_DIR/codex-package.json")" = 444; test "$(stat -c %u "$COMMONS_RELEASE_DIR/codex-package.json")" = "$verify_uid"; test "$(stat -c %g "$COMMONS_RELEASE_DIR/codex-package.json")" = "$verify_gid"
test -r "$COMMONS_RELEASE_DIR/web/index.html"; test -f "$COMMONS_RELEASE_DIR/VERSION"; test ! -L "$COMMONS_RELEASE_DIR/VERSION"
test "$(stat -c %a "$COMMONS_RELEASE_DIR/VERSION")" = 444 || exit 64
test "$(stat -c %u "$COMMONS_RELEASE_DIR/VERSION")" = "$verify_uid" || exit 64
test "$(stat -c %g "$COMMONS_RELEASE_DIR/VERSION")" = "$verify_gid" || exit 64
test -f "$COMMONS_RELEASE_DIR/SHA256SUMS"; test ! -L "$COMMONS_RELEASE_DIR/SHA256SUMS"; test "$(stat -c %a "$COMMONS_RELEASE_DIR/SHA256SUMS")" = 444 || exit 64
test "$(stat -c %u "$COMMONS_RELEASE_DIR/SHA256SUMS")" = "$verify_uid" || exit 64
test "$(stat -c %g "$COMMONS_RELEASE_DIR/SHA256SUMS")" = "$verify_gid" || exit 64
test -z "$(find "$COMMONS_RELEASE_DIR/web" "$COMMONS_RELEASE_DIR/ops" -type f \! -perm 0444 -print -quit)" || exit 64
verify_tmp=
cleanup_verify_tmp() {
	test -z "$verify_tmp" || rm -f -- "$verify_tmp"
}
trap cleanup_verify_tmp EXIT HUP INT TERM
verify_tmp=$(mktemp)
find "$COMMONS_RELEASE_DIR" -type f -perm 0555 -printf '%P\n' | LC_ALL=C sort > "$verify_tmp"
printf '%s\n' "bin/codex" "bin/codex-code-mode-host" "codex-resources/bwrap" "codex-resources/zsh/bin/zsh" "codex-path/rg" "commons-server" | LC_ALL=C sort | cmp -s - "$verify_tmp" || { rm -f -- "$verify_tmp"; exit 64; }
rm -f -- "$verify_tmp"; verify_tmp=
release_id=$(sed -n '1p' "$COMMONS_RELEASE_DIR/VERSION"); test -n "$release_id"; test "$(wc -l < "$COMMONS_RELEASE_DIR/VERSION")" -eq 1; test "$release_id" = "$(basename "$COMMONS_RELEASE_DIR")"
test "$(COMMONS_RELEASE_IDENTITY_FILE="$COMMONS_RELEASE_DIR/VERSION" "$COMMONS_RELEASE_DIR/commons-server" --build-id)" = "$release_id"
go version -m "$COMMONS_RELEASE_DIR/commons-server" | grep -Fq 'path'"$(printf '\t')"'codex-commons/cmd/commons-server'; go version -m "$COMMONS_RELEASE_DIR/commons-server" | grep -Fq 'build'"$(printf '\t')"'-trimpath=true'
test "$(readlink -f "${COMMONS_CODEX_BIN:-/missing}")" = "$COMMONS_RELEASE_DIR/bin/codex"; test "$(readlink -f "${COMMONS_WEB_DIR:-/missing}")" = "$COMMONS_RELEASE_DIR/web"
for spec in "bin/codex:$COMMONS_CODEX_SHA256" "bin/codex-code-mode-host:$COMMONS_CODE_MODE_HOST_SHA256" "codex-resources/bwrap:$COMMONS_CODEX_BWRAP_SHA256" "codex-resources/zsh/bin/zsh:$COMMONS_CODEX_ZSH_SHA256" "codex-path/rg:$COMMONS_CODEX_RG_SHA256" "codex-package.json:$COMMONS_CODEX_PACKAGE_SHA256"; do file=${spec%%:*}; sha=${spec#*:}; test "$(sha256sum "$COMMONS_RELEASE_DIR/$file" | awk '{print $1}')" = "$sha"; done
test "$($COMMONS_RELEASE_DIR/bin/codex --version | awk '{print $2}')" = 0.147.0
test -f "$COMMONS_RELEASE_DIR/SHA256SUMS"; test ! -L "$COMMONS_RELEASE_DIR/SHA256SUMS"
(cd "$COMMONS_RELEASE_DIR" && test -z "$(find . -type l -print -quit)" && test -z "$(find . \( -name '*_test*' -o -name 'test-*' -o -name '*.map' -o -name '*.tmp' \) -print -quit)" && test ! -e ops/build-release.sh && test ! -e ops/stage-release.sh && test ! -e ops/test-ops.sh)
test -z "$(find "$COMMONS_RELEASE_DIR" -mindepth 1 -type l -print -quit)"
test -z "$(find "$COMMONS_RELEASE_DIR" -mindepth 1 \! -type f \! -type d -print -quit)"
# The manifest is line-oriented, so the complete tree uses the same portable
# filename grammar. This also makes actual-tree inventory unambiguous.
test -z "$(find "$COMMONS_RELEASE_DIR" -mindepth 1 -name '*[!A-Za-z0-9._-]*' -print -quit)"

manifest_paths=
actual_files=
actual_dirs=
allowed_dirs=
cleanup() {
	test -z "$manifest_paths" || rm -f -- "$manifest_paths"
	test -z "$actual_files" || rm -f -- "$actual_files"
	test -z "$actual_dirs" || rm -f -- "$actual_dirs"
	test -z "$allowed_dirs" || rm -f -- "$allowed_dirs"
}
trap cleanup EXIT HUP INT TERM
manifest_paths=$(mktemp)
actual_files=$(mktemp)
actual_dirs=$(mktemp)
allowed_dirs=$(mktemp)

# Accept only the exact sha256sum text form emitted by stage-release.sh.
# Paths are deliberately restricted to relative, slash-separated components;
# this rejects absolute, traversal, control, newline, and backslash-escaped
# names before checksum verification can interpret them.
LC_ALL=C awk '
{
	line = $0
	digest = substr(line, 1, 64)
	path = substr(line, 67)
	if (length(line) < 67 || length(digest) != 64 || digest !~ /^[0-9a-f]+$/ || substr(line, 65, 2) != "  ") {
		invalid = 1
		next
	}
	if (path == "" || path == "SHA256SUMS" || path !~ /^[A-Za-z0-9._\/-]+$/ || path ~ /^\// || path ~ /\/$/ || path ~ /\/\// || path ~ /(^|\/)\.($|\/)/ || path ~ /(^|\/)\.\.($|\/)/) {
		invalid = 1
		next
	}
	if (seen[path]++) {
		invalid = 1
		next
	}
	print path
}
END { exit invalid }
' "$COMMONS_RELEASE_DIR/SHA256SUMS" > "$manifest_paths"
LC_ALL=C sort -o "$manifest_paths" "$manifest_paths"

LC_ALL=C find "$COMMONS_RELEASE_DIR" -mindepth 1 -type f -printf '%P\n' |
	LC_ALL=C sed '/^SHA256SUMS$/d' |
	LC_ALL=C sort > "$actual_files"
LC_ALL=C find "$COMMONS_RELEASE_DIR" -mindepth 1 -type d -printf '%P\n' |
	LC_ALL=C sort > "$actual_dirs"
LC_ALL=C awk -F/ '
{
	parent = ""
	for (i = 1; i < NF; i++) {
		parent = (parent == "" ? $i : parent "/" $i)
		print parent
	}
}
' "$manifest_paths" | LC_ALL=C sort -u > "$allowed_dirs"

cmp -s "$manifest_paths" "$actual_files"
cmp -s "$allowed_dirs" "$actual_dirs"
(cd "$COMMONS_RELEASE_DIR" && sha256sum -c SHA256SUMS)
