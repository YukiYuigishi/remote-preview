#!/bin/sh

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	printf 'usage: %s VERSION [OUTPUT_DIR]\n' "$0" >&2
	exit 2
fi

version=$1
output_dir=${2:-dist}
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
staging_dir=$(mktemp -d "${TMPDIR:-/tmp}/remote-preview-release.XXXXXX")
trap 'rm -rf "$staging_dir"' EXIT HUP INT TERM

mkdir -p "$output_dir"
for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
	goos=${target%-*}
	goarch=${target#*-}
	archive_base="remote-preview-${version}-${target}"
	package_dir="$staging_dir/$archive_base"
	mkdir -p "$package_dir"

	GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build \
		-trimpath -buildvcs=false -ldflags='-s -w -buildid=' \
		-o "$package_dir/remote-preview" \
		"$repo_root/cmd/remote-preview"
	cp "$repo_root/README.md" "$repo_root/LICENSE" "$repo_root/THIRD_PARTY_NOTICES.md" "$package_dir/"
	tar -czf "$output_dir/$archive_base.tar.gz" -C "$staging_dir" "$archive_base"
done

if command -v sha256sum >/dev/null 2>&1; then
	(
		cd "$output_dir"
		sha256sum -- *.tar.gz > SHA256SUMS
	)
else
	(
		cd "$output_dir"
		shasum -a 256 *.tar.gz > SHA256SUMS
	)
fi
