#!/usr/bin/env bash
# Build release archives and SHA256SUMS for six targets, then rebuild one target to check the build is reproducible.
# usage: scripts/release.sh [outdir]      (default dist/). Needs only Go; the archives contain the static Go binary,
# README, LICENSE, CHANGELOG, docs/formats. GDAL is a runtime dependency and is not bundled.
# The binary embeds the git commit (and "uncommitted changes" if the tree is dirty): release from a clean tree.
set -euo pipefail
cd "$(dirname "$0")/.."
out=${1:-dist}
ver=$(sed -n 's/.*ToolVersion *= *"\(.*\)"/\1/p' internal/world/world.go)
mkdir -p "$out"; out=$(cd "$out" && pwd); rm -f "$out"/ocean-"$ver"-* "$out"/SHA256SUMS
if [ -n "$(git status --porcelain)" ]; then echo "WARNING: working tree is not clean; the binaries will say 'uncommitted changes'" >&2; fi
build() { # os arch dest
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "-s -w" -o "$3" ./cmd/ocean
}
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os=${t%/*}; arch=${t#*/}; name=ocean-$ver-$os-$arch; d=$stage/$name; mkdir -p "$d/docs"
  exe=ocean; [ "$os" = windows ] && exe=ocean.exe
  build "$os" "$arch" "$d/$exe"
  cp README.md LICENSE CHANGELOG.md "$d/"; cp docs/formats/world-v2.md docs/integration.md "$d/docs/"
  if [ "$os" = windows ]; then (cd "$stage" && zip -q -X -r "$out/$name.zip" "$name"); else
    # GNU tar: fixed order, mtime and owner so the archive bytes depend only on content; bsdtar (macOS) falls back and is not byte-stable
    (cd "$stage" && tar --sort=name --mtime='2000-01-01 00:00Z' --owner=0 --group=0 --numeric-owner -cf - "$name" 2>/dev/null || tar -cf - "$name") | gzip -n > "$out/$name.tar.gz"
  fi
  echo "built $name"
done
(cd "$out" && { command -v sha256sum >/dev/null && sha256sum ocean-"$ver"-* || shasum -a 256 ocean-"$ver"-*; } > SHA256SUMS)
# reproducibility: the same commit built again must give the same binary
build linux amd64 "$stage/again"; build linux amd64 "$stage/first"
cmp "$stage/again" "$stage/first" && echo "reproducible: two builds of linux/amd64 are byte-identical ($(shasum -a 256 "$stage/first" | cut -c1-16)...)"
cat "$out/SHA256SUMS"
