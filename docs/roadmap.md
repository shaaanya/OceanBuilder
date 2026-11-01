# Roadmap

Ocean Compiler is a standalone, engine-independent tool ([ADR 0004](decisions/0004-engine-agnostic-scope.md)). Status labels: **done** = implemented and tested; **partial** = works with stated limits; **open** = not started.

## State (2026-11-01, tool 0.3.0-rc1, format schema 2 / 3, format revision 1)

Done (implemented and tested offline):
- Everything of 0.2.0 (format with LOD pyramid and per-sample source layer, GMRT / local / OSM / OpenSeaMap / EMODnet sources, reproducible incremental builds, reader, GeoTIFF export, Unity URP adapter).
- Several elevation datasets per world with listed or `auto` precedence, per-sample dataset identity (`.dataset.png`), overlap and seam statistics ([ADR 0007](decisions/0007-dataset-composition.md)).
- Vertical datum policy: declared datums, PROJ grid conversions with recorded grids, explicit refusal otherwise ([ADR 0008](decisions/0008-vertical-datum-policy.md)).
- Windowed disk-backed pipeline: output independent of window size, Go process memory follows `-mem`, GDAL cache capped. Measurements in the [development log](development-log.md).
- Local transverse Mercator grids for wide boxes and boxes across 180 (schema 3, [ADR 0006](decisions/0006-local-transverse-mercator-grid.md)); reader implements it.
- Validator checks the dataset layer and warns about mixed datums, systematic offsets and seams.

Partial:
- Antimeridian worlds: local GeoTIFF sources only, no coastline. Network fetchers refused until verified across 180.
- Progress and cancellation exist (0.2.0) but were not re-measured on large grids. Resume of an interrupted build reuses only the warp stage cache.

## Next (highest value first)

1. **LOD quality**: measure vertical error, land/water flips and channel connectivity of exact decimation on narrow channels, islands, banks and coasts; then add an alternative `lod.method` with a source-code rule for blended samples. Nothing here was started in 0.3.0.
2. **Source adapters** verified against the real services, with fixtures: ETOPO 2022 tiles, GEBCO (undocumented REST route plus its TID layer), NOAA CUDEM COGs, then the NOAA mosaic. Findings and cautions in [research/source-probes.md](research/source-probes.md). None implemented.
3. **Licensing metadata in the manifest** (`licensing.sources[]`) from [research/licensing-analysis.md](research/licensing-analysis.md); decide whether the OSM coastline copy becomes opt-in.
4. Verified antimeridian fetch for GMRT; GMRT size cap (a 10 degree request is silently coarsened to 122 m).
5. Unity: LOD levels in the importer, schema 3 CRS, Addressables streaming inside a player, floating origin, water/seabed material. (A standalone macOS player build and headless smoke run pass, see the development log.)
6. Unreal Landscape exporter only when a second consumer asks.
7. Vector products (depth contours, coast) from the grid.
8. Local OSM extract as an Overpass alternative.

## Open questions

- ODbL: a world built with `coast_conform` or `seamarks` contains OSM-derived cells and points. Treated as a derivative database; attribution is written to `ATTRIBUTION.txt`, share-alike is not legally analysed.
- Vertical datum of GMRT: a mosaic of contributed grids; how to quantify per region.
- Whether decimated-LOD aliasing of narrow channels matters to real consumers (documented, not measured).

## Known problems

- Installed EGM96 grid differs from PROJ's published values by about 0.4 m at two points; cause unknown.
- Overpass mirrors can be slow or down; the build then continues without a coastline and says so (`no-coastline` warning).
- Global steps (coast conform, regional fill, refinement) hold the grid in memory and are skipped or refused above the memory budget.
- Unity importer reads level 0 only and has not been checked with schema 3 worlds; not verified in a standalone player.
- LibreTerrain exchange assumes an EPSG code (UTM worlds only).
- Coastline-to-DEM offset is reported, not corrected. Preview colours are a fixed ramp, not a chart.
