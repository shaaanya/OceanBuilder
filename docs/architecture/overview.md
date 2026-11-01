# Architecture overview

```text
 world.json ─► PlanGrid (UTM zone, distortion check, grid size) ─► fetch / local sources ─► cache/ (+ provenance sidecars)
                                   │
                                   ▼
      gdalwarp → one vertex-registered UTM grid per input   (cache/stages/, keyed on input hashes + warp params)
                                   │
                                   ▼
   derive: depth class, shore, source code  (+ opt-in: EMODnet fill, OSM coast conform, LibreTerrain delta)
                                   │
                                   ▼
   level plan: level 0 = full grid, level k = every 2^k-th vertex ─► tiles/<k>/*.height.f32 + 3 PNG layers (sha256 each)
                                   │
                                   ▼
   manifest (input_key, build_id, lod, tiles, stats, sources) ─► atomic swap into output/<name>/
                                   │
             ┌─────────────────────┼───────────────────────────┐
             ▼                     ▼                           ▼
        validate / info / diff   pkg/oceanworld (reader)    export: geotiff · unity (URP)
```

The contract is the on-disk format in [../formats/world-v2.md](../formats/world-v2.md). Go owns processing; adapters and readers consume the manifest and tile files only ([ADR 0004](../decisions/0004-engine-agnostic-scope.md)).

## Packages

| package | role |
|---------|------|
| `cmd/ocean` | CLI dispatch, flags, help, signal handling. No logic beyond argument handling. |
| `internal/gdal` | GDAL CLI wrappers (warp, translate, transform, ogr2ogr, info), UTM zone choice and point-scale, raw raster readers, cancellable `Ctx`. |
| `internal/fetch`, `gmrt`, `osm`, `emodnet` | Downloads into a content-addressed cache with provenance sidecars; Overpass mirrors tried in turn with a per-mirror timeout. |
| `internal/world` | Config, manifest types, `PlanGrid`, the build pipeline, LOD (`layers.go`), input key and stage cache, `Diff`. |
| `internal/validate` | Recomputes every metric from the tile files; hash, edge, layer, LOD, statistics and provenance checks. |
| `internal/libreterrain` | Optional edit-layer exchange with LibreTerrain (export/import, MCP client). |
| `internal/preview`, `internal/export` | PNG/HTML renderer; GeoTIFF and Unity adapters (Unity C# is embedded). |
| `pkg/oceanworld` | Public, GDAL-free reader: open, sample at UTM or lon/lat and level, profiles, pure-Go UTM. |

## World model (summary; the spec is authoritative)

- **CRS**: WGS 84 / UTM, zone from the bbox centre or `utm_epsg`. Point-scale deviation is measured; warn at 0.15 %, refuse at 0.5 %.
- **Grid**: vertices at `origin + k·cell_m`, `tile_cells` (power of two) cells per tile, `tile_cells+1` samples per edge, tiles share edge samples.
- **Tile id**: `L<level>_<x>_<y>`, absolute and bbox-independent, `x = floor(E / (tile_span·2^level))`.
- **LOD**: level k keeps every 2^k-th level-0 vertex. Parent of `(k,x,y)` is `(k+1, x>>1, y>>1)`. No averaging ([ADR 0005](../decisions/0005-format-v2-float32-source-layer-decimated-lod.md)).
- **Layers per sample**: float32 height, depth class, shore flag, source code.
- **Provenance**: every source with URL, retrieval time, sha256, licence, attribution; `ATTRIBUTION.txt` generated from it.

## Why tiles never crack

One raster is warped once; tiles are slices `[r·N, r·N+N] × [c·N, c·N+N]`, so a shared edge is the same samples, not two resamples. Coarser levels are decimations of the same raster, so edges also agree across levels. `validate` checks both.

## Reproducibility, incremental builds, cancellation

- `input_key = H(config, tool + GDAL version, algorithm versions, source hashes)`. If the output already carries it, `build` returns at once. `-force` bypasses.
- `build_id` additionally covers the produced content. Equal `input_key` gives byte-identical output (tested from a clean cache, a stage-cache hit and a forced rebuild).
- Warps are cached under `cache/stages/` by input hash and warp parameters, so changing only derived settings (depth bands, LOD levels, conform) skips them.
- Output is assembled in a sibling directory and swapped in; SIGINT/SIGTERM cancel GDAL subprocesses and leave an existing output untouched. A lock file (`*.world.lock.json`) can pin source hashes.

## Memory and time

The whole world grid is held in memory. `ocean world check` prints the estimate before any download. Small builds are dominated by GDAL subprocess start (about 0.4–0.5 s per tool call); measured numbers are in [../development-log.md](../development-log.md). Windowed processing for continental extents is not implemented.

## Extension points

A new source is a GeoTIFF/GeoJSON plus `fetch.Provenance` and a case in the build; a new engine adapter reads the manifest and tile files (`internal/export`) or uses `pkg/oceanworld`. No interfaces until a third source or second engine exists.
