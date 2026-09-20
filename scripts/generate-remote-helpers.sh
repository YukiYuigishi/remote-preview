#!/bin/sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
output_dir="$repo_root/internal/preview/remote_helpers"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/ykview-helper-build.XXXXXX")
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM
go_command=${GO:-go}

mkdir -p "$output_dir"
for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
	goos=${target%-*}
	goarch=${target#*-}
	GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 "$go_command" build \
		-trimpath -buildvcs=false -ldflags='-s -w -buildid=' \
		-o "$temp_dir/ykview-helper-$target" \
		"$repo_root/cmd/ykview-helper"
	gzip -n -c "$temp_dir/ykview-helper-$target" > "$output_dir/$target.gz"
done
