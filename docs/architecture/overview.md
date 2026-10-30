# Architecture overview

```text
 world.json ──► fetch (GMRT topo + topo-mask, OSM coastline) ──► cache/ (+ provenance sidecars)
                    │ or local files (GeoTIFF / GeoJSON)
                    ▼
        gdalwarp → one vertex-registered UTM grid (explicit resampling)
                    ▼
   derive world-wide grids: depth class, shoreline, survey mask
                    ▼
        slice into (N+1)² tiles sharing edge samples ──► tiles/0/*.r16 + png masks
                    ▼
   coastline check, world.manifest.json, ATTRIBUTION.txt
                    ▼
      validate · preview · export unity (thin C# importer)
```

## World model

The engine-independent contract is `world.manifest.json` (`internal/world/world.go`, `Manifest`):

- **CRS**: WGS 84 / UTM zone chosen from the bbox centre. All distances are metres on this grid.
- **Grid**: vertices at `origin_easting + k·cell_m`. `tile_cells` (power of two) cells per tile, `tile_cells+1` samples per edge, so heightmaps are 2ⁿ+1 as Unity needs.
- **Tile id**: `L0_<x>_<y>` with `x = floor(E / tile_span)`, `y = floor(N / tile_span)`: absolute, bbox-independent, deterministic. Level is always 0 so far.
- **Heights**: uint16 little-endian, rows north to south, `z = min + u/65535·(max−min)`. Nodata cells are written 0 and flagged class 255.
- **Depth classes**: 0 land (z ≥ 0), 1..n by `depth_bands` (default 5/20/50/200 m), 255 nodata.
- **Shore mask**: land cells with a 4-neighbour water cell, computed on the whole world grid so tile borders agree.
- **Provenance**: each source with URL, retrieval time, sha256, licence, attribution. `build_id` hashes config, tool version, input hashes.
- **Quality**: per-tile nodata, water and survey-backed fractions; world-level coastline offset (OSM line to nearest DEM shoreline cell, within 8 cells).

## Why tiles never crack

One raster is warped once; tiles are slices `[r·N, r·N+N] × [c·N, c·N+N]`, so a shared edge is the same samples, not two independent resamples. `validate` compares every shared edge and the manifest hash of each file.

## Build pipeline notes

Tiles are written in parallel into fixed slots, so output order is deterministic. Working rasters are ENVI files in a temp dir; the whole world grid is held in memory (fine to a few thousand samples per side; windowed processing is on the roadmap).

## Extension points

Add a source by producing a GeoTIFF plus provenance (`fetch.Provenance`) and a case in `world.Build`; add an engine by consuming the manifest (`internal/export`). Interfaces will be introduced when a third source or second engine exists, not before.
