# AGENTS.md

Guidance for AI agents and humans working in this repo.

## Build and test

```bash
go vet ./... && go test ./...                 # offline; uses testdata/falmouth fixtures
go test ./internal/validate -bench . -run x   # benchmarks
go build -o bin/ocean ./cmd/ocean
```

GDAL CLI tools must be on `PATH` (gdalwarp, gdal_translate, gdaltransform, ogr2ogr, gdalinfo). Tests never touch the network. Network runs: `bin/ocean build falmouth.world.json`.

## Layout

- `cmd/ocean`: CLI dispatch only.
- `internal/gdal`: GDAL CLI wrappers, UTM zone selection, raw raster readers.
- `internal/fetch`, `gmrt`, `osm`: downloads into a content-addressed cache with provenance sidecars.
- `internal/world`: config, manifest types, the build pipeline (normalise → derive masks → tile → manifest).
- `internal/validate`: validator; every metric is computed from tile files.
- `internal/preview`: PNG/HTML renderer. `internal/export`: engine adapters (Unity; C# is embedded).

## Rules

- Go owns processing. Engine adapters only read `world.manifest.json` and tile files.
- Never mix degrees and metres: process in the UTM grid, convert with GDAL.
- Neighbouring tiles share edge samples by construction (one world grid is sliced); `validate` re-checks it. Do not resample per tile.
- Never present interpolated data as measured. Keep resampling method, source resolution and survey fraction in the manifest.
- Do not hide missing data: nodata is flagged (255 in depth class, nodata_fraction), not filled.
- New sources need provenance (URL, retrieval time, sha256), licence and attribution, or validation fails.
- Real-data fixtures go in `testdata/` with a README naming source and licence.
- Record decisions in `docs/decisions/`, progress in `docs/development-log.md`, plans in `docs/roadmap.md`.
