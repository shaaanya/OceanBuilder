# Install, pin, reproduce

## Install

OceanBuilder is one static Go binary plus the GDAL command-line tools it calls.

1. **GDAL** (3.9 or newer; CI uses conda-forge, development used 3.13) on `PATH`: `gdalwarp`, `gdal_translate`, `gdaltransform`, `ogr2ogr`, `gdalinfo`, plus PROJ's data grids (`proj-data`) if you use vertical datum conversions. `conda install -c conda-forge gdal proj-data`, `brew install gdal`, or your distribution's package.
2. **The binary**, either
   - a release archive (`scripts/release.sh` builds them: linux, macOS and windows on amd64 and arm64) with its `SHA256SUMS`: `shasum -a 256 -c SHA256SUMS --ignore-missing`, or
   - from source: `go build -o bin/ocean ./cmd/ocean` (Go 1.22 or newer; `go.mod` says 1.22, tested with 1.26.1), or `go install github.com/ocean-compiler/ocean/cmd/ocean@<tag>` once the module is published.
3. `ocean version` prints the tool version, the git commit it was built from (and `uncommitted changes` if the tree was dirty), the world format schema, Go version and platform, and the GDAL it found. Put this output in bug reports.

No release has been published: there is no remote, so the checksums come from local builds only (see below).

## Releases and reproducibility

`scripts/release.sh [outdir]` cross-compiles six targets with `CGO_ENABLED=0 -trimpath -ldflags "-s -w"`, writes archives (binary, README, LICENSE, CHANGELOG, format spec, integration guide) and `SHA256SUMS`, then builds linux/amd64 twice and compares: the two binaries are byte-identical (checked 2026-11-02 on macOS arm64, Go 1.26.1). What that does not show: the same bytes on another machine or another Go version (the Go toolchain version is part of the binary), or from a dirty tree (the commit is embedded, with a `modified` flag). Tar archives are only byte-stable with GNU tar; compare the binaries, not the archives.

Worlds are reproducible independently of the binary: `manifest.build_id` hashes the config, tool version, algorithm versions, source hashes and the GDAL version; two builds with the same `input_key` give byte-identical files (tested: `TestStageCacheDeterministic`, `TestDeterministicBuild`, the golden tile hashes on arm64 and amd64 through CI).

## Pin a world's inputs

Network sources can change under you. `ocean build w.world.json -update-lock` writes `w.lock.json` (source URL, sha256, bytes, retrieval time); commit it. Later builds fail with the list of differences if a resolved source returned different bytes (`-lock off` ignores it). Pin the tool too: record `ocean version` output next to the lock file, or keep the release archive's checksum. A world built from a locked config by the same tool version and GDAL version is byte-identical.

## Cache

Downloads and warped intermediate rasters live in one cache directory: `-cache dir`, else `$OCEAN_CACHE`, else `./.ocean-cache`. It is content-addressed and every stage is sealed with hashes; an interrupted build resumes ([resume.md](resume.md)).

- `ocean cache stats` — downloaded files and rebuildable stage rasters, with sizes.
- `ocean cache clean -stages` — remove only the rebuildable stage rasters (keeps downloads, so no network is needed for the rebuild).
- `ocean cache clean` — remove everything (refuses `/` and `$HOME`).
- Several builds may share a cache concurrently (tested: 3 builds, 15 rounds).
- Sizes for real regions: [performance.md](performance.md).
