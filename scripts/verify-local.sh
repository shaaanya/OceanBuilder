#!/usr/bin/env bash
# Local verification procedure, for machines without hosted CI. usage: scripts/verify-local.sh <tier>...
#   fast       go vet + go test -short                 (about 1.5 min; no network; needs GDAL on PATH)
#   full       go vet + go test (adds the slow offline tests: resume at every stage, golden tiles, GeoTIFF agreement)
#   cross      go vet and test-binary compile for linux/windows/darwin on amd64 and arm64 (no GDAL needed; does NOT run tests)
#   scripts    real-process checks: interrupt/resume (SIGINT, SIGKILL), 3 concurrent builds (POSIX only), python reference reader
#   lod        dotnet property tests of the Unity LOD code (needs the .NET SDK)
#   smoke      real services (GMRT/ETOPO/GEBCO/CUDEM/Overpass); needs network, may flake
# The Unity PlayMode tests and the player run are in docs/testing.md (they need a licensed Unity Editor).
set -euo pipefail
cd "$(dirname "$0")/.."
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
for tier in "${@:-fast}"; do
  echo "== $tier"
  case $tier in
    fast)  go vet ./... && go test -short ./... -count=1 -timeout 15m ;;
    full)  go vet ./... && go test ./... -count=1 -timeout 30m ;;
    cross)
      for t in linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/arm64 darwin/amd64; do
        export GOOS=${t%/*} GOARCH=${t#*/}
        go vet ./... && go build -o /dev/null ./cmd/ocean
        for p in $(go list ./...); do go test -c -o /dev/null "$p" 2>&1 | grep -v "no test files" || true; done
        echo "$t ok"
      done; unset GOOS GOARCH ;;
    scripts)
      go build -o "$tmp/ocean" ./cmd/ocean
      cfg="$tmp/w.world.json"
      printf '{"name":"verify","bbox":[-5.1370,50.1445,-5.0540,50.1970],"cell_m":24,"tile_cells":64,"coastline":"none","lod_levels":3,"local":{"topo":"%s/testdata/falmouth/topo.tif","mask":"%s/testdata/falmouth/mask.tif","license":"CC BY 4.0 (GMRT)","attribution":"GMRT, Ryan et al. 2009"}}' "$PWD" "$PWD" > "$cfg"
      python3 scripts/interrupt_resume.py "$tmp/ocean" "$cfg" "$tmp/ir"
      python3 scripts/concurrent_builds.py "$tmp/ocean" "$cfg" "$tmp/cb"
      "$tmp/ocean" build "$cfg" -out "$tmp/world" >/dev/null && "$tmp/ocean" validate "$tmp/world"
      python3 -I examples/python-reader/ocean_reader.py "$tmp/world" --check
      python3 -I scripts/reader_crosscheck.py "$tmp/ocean" "$tmp/world" ;;
    lod)   dotnet run --project tools/lodtests -c Release ;;
    smoke) OCEAN_SMOKE=1 go test ./internal/etopo ./internal/gebco ./internal/cudem -run Smoke -v -count=1 ;;
    *) echo "unknown tier $tier"; exit 2 ;;
  esac
done
