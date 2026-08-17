#!/bin/sh
set -eu
PATH=/usr/bin:/bin
export PATH

: "${1:?absolute launcher target required}"
target=$1
case "$target" in
/*/codex-commons-launch) ;;
*) exit 64 ;;
esac

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
source_launcher=$repo_root/deploy/bin/codex-commons-launch
test -f "$source_launcher"
test ! -L "$source_launcher"

install_dir=$(dirname -- "$target")
if [ ! -e "$install_dir" ]; then
	install -d -m 0755 -- "$install_dir"
fi
test -d "$install_dir"
test ! -L "$install_dir"
install_dir=$(readlink -f -- "$install_dir")
test -n "$install_dir"
target=$install_dir/codex-commons-launch

install_temp=
cleanup_install_temp() {
	if [ -n "${install_temp:-}" ] && [ -e "$install_temp" ]; then
		rm -f -- "$install_temp"
	fi
}
trap cleanup_install_temp 0 1 2 15
install_temp=$(mktemp "$install_dir/.codex-commons-launch.XXXXXX")
install -m 0555 -- "$source_launcher" "$install_temp"
mv -Tf -- "$install_temp" "$target"
install_temp=
trap - 0 1 2 15
