# 0001: Go core that shells out to GDAL CLIs

Status: accepted, 2026-10-30

## Context

The core must be Go, handle CRS transforms and raster resampling correctly, and stay easy to install. Options: cgo bindings (godal, gdal), pure-Go geo libraries, GDAL command-line tools.

## Decision

Go owns the pipeline, data model, cache, validation and CLI. For warping, reprojection and vector reprojection it runs `gdalwarp`, `gdaltransform`, `ogr2ogr` and `gdalinfo` as subprocesses. Raw float32/byte rasters come back as ENVI files read in Go. The GDAL version is recorded in the manifest.

## Consequences

- No cgo: `go build` works anywhere; cross-compiling needs no GDAL headers.
- Users must install GDAL (Homebrew, apt, conda). The README states this.
- Subprocess cost: ~tens of ms per call. Tile bounds currently spawn one `gdaltransform` per tile; batching is on the roadmap.
- Correct projections and resampling come from PROJ/GDAL rather than our own code.
- If GDAL becomes a distribution problem, a godal backend can sit behind `internal/gdal`.
