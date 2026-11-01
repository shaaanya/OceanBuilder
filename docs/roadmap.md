# Roadmap

Ocean Compiler is a standalone, engine-independent tool ([ADR 0004](decisions/0004-engine-agnostic-scope.md)). Status labels: **done** = implemented and tested; **partial** = works with stated limits; **open** = not started.

## State (2026-11-01, tool 0.3.0, format schema 2 / 3, format revision 1)

Done (implemented and tested; see [release report](release-0.3.0.md) for what was run against real services):
- Everything of 0.2.0 (format with LOD pyramid and per-sample source layer, GMRT / local / OSM / OpenSeaMap / EMODnet sources, reproducible incremental builds, reader, GeoTIFF export, Unity URP adapter).
- Several elevation datasets per world with listed or `auto` precedence, per-sample dataset identity, overlap and seam statistics ([ADR 0007](decisions/0007-dataset-composition.md)).
- Vertical datum policy with recorded PROJ grids and explicit refusals ([ADR 0008](decisions/0008-vertical-datum-policy.md)); `ocean world check` shows it offline.
- Windowed disk-backed pipeline; output independent of window size; memory follows `-mem`.
- Local transverse Mercator grids for wide boxes and boxes across 180 (schema 3, [ADR 0006](decisions/0006-local-transverse-mercator-grid.md)).
- LOD measurement (`ocean lod-report`) and the `shoal` method.
- ETOPO 2022, GEBCO (with TID), CUDEM adapters read from the live services; licensing record in the manifest.
- Unity: coarser LOD levels imported (`-oceanMaxLevel`), standalone player build and headless smoke run.

Partial: antimeridian worlds (local GeoTIFF only); resume (warp stage cache only); datum conversions (EGM96, EGM2008, ellipsoid); Unity LOD levels are present but not selected at runtime.

## Next (highest value first)

1. **Unity runtime LOD**: a level selector and a way to hide the cracks between tiles of different sizes; then schema 3 worlds and floating origin.
2. **Verified antimeridian fetch** for GMRT (and the GMRT silent coarsening of large requests: detect it from the returned cell size and warn).
3. A real build across a UTM zone boundary and over a large real region; Linux/Windows runs.
4. **Mid-pipeline resume** and progress/ETA on large grids.
5. NOAA DEM mosaic and Seascape adapters, `ocean sources explain` (coverage per source).
6. Unreal Landscape exporter only when a second consumer asks.
7. Vector products (depth contours, coast) from the grid; local OSM extract as an Overpass alternative.

## Open questions

- ODbL: a world built with `coast_conform` or `seamarks` contains OSM-derived cells and points. Treated as a derivative database; attribution is written to `ATTRIBUTION.txt`, share-alike is not legally analysed.
- Vertical datum of GMRT: a mosaic of contributed grids; how to quantify per region.
- Whether the accuracy cost of `shoal` (rms 16-21 m at level 1 on steep real terrain) is acceptable to consumers who need conservative shoals; both methods are measured, neither is the right default for everyone.

## Known problems

- Installed EGM96 grid differs from PROJ's published values by about 0.4 m at two points; cause unknown.
- Overpass mirrors can be slow or down; the build then continues without a coastline and says so (`no-coastline` warning).
- Global steps (coast conform, regional fill, refinement) hold the grid in memory and are skipped or refused above the memory budget.
- Unity: coarse levels are imported but never selected; schema 3 worlds are refused; rendering was not checked in the standalone player.
- LibreTerrain exchange assumes an EPSG code (UTM worlds only).
- Coastline-to-DEM offset is reported, not corrected. Preview colours are a fixed ramp, not a chart.
