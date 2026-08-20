#!/bin/sh
set -eu
: "${1:?release id required}"
: "${2:?output path required}"
id=$1; output=$2
case "$id" in *[!A-Za-z0-9._-]*|'') exit 64;; esac
case "$output" in /*) ;; *) exit 64;; esac
build_dir=$(dirname "$output")
ops_output=${3:-$build_dir/commons-ops}
case "$ops_output" in /*) ;; *) exit 64;; esac
test "$ops_output" != "$output"
CGO_ENABLED=0 go build -trimpath -buildvcs=true -ldflags "-buildid= -X main.releaseID=$id" -o "$output" ./cmd/commons-server
test "$($output --build-id)" = "$id"
go version -m "$output" | grep -Fq 'path'"$(printf '\t')"'codex-commons/cmd/commons-server'
go version -m "$output" | grep -Fq 'build'"$(printf '\t')"'-trimpath=true'
CGO_ENABLED=0 go build -trimpath -buildvcs=true -ldflags "-buildid= -X main.releaseID=$id" -o "$ops_output" ./cmd/commons-ops
test "$($ops_output --build-id)" = "$id"
go version -m "$ops_output" | grep -Fq 'path'"$(printf '\t')"'codex-commons/cmd/commons-ops'
go version -m "$ops_output" | grep -Fq 'build'"$(printf '\t')"'-trimpath=true'
