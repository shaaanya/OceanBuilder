# Changelog

## 0.3.0 (2026-11-01)

See [docs/release-0.3.0.md](docs/release-0.3.0.md) for what was verified, partial, unverified and deferred.

- Several elevation datasets per world with `listed`/`auto` precedence and a per-sample dataset layer; vertical datum policy with PROJ grid conversions and explicit refusals; `licensing` record in the manifest and `no_coastline_copy`.
- Local transverse Mercator grids (schema 3) for wide boxes and boxes across the antimeridian; windowed disk-backed pipeline with `-mem`.
- LOD: `ocean lod-report`, `lod_method: "shoal"`.
- Sources: ETOPO 2022, GEBCO (with TID survey mask), CUDEM; `ocean world check` shows per-source datum and conversion offline.
- Unity: coarser LOD levels imported (`-oceanMaxLevel`), standalone player build helper and headless smoke component.
- Fixed: `grid.bbox_wgs84` is the level-0 extent (it was the union over all LOD levels); vertical double conversion; unbounded GDAL cache.
- Changed: boxes over the UTM scale limit are planned as a local grid instead of refused (`projection: "utm"` restores the refusal).

## 0.1.0 (unreleased)

- First vertical slice: GMRT + OSM ingestion, UTM normalisation, shared-edge tiling, depth/shore masks, manifest, validator, preview, Unity export and importer.
