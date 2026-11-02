# Roadmap

OceanBuilder is a standalone, engine-independent tool ([ADR 0004](decisions/0004-engine-agnostic-scope.md)). Status labels: **done** = implemented and tested; **partial** = works with stated limits; **open** = not started.

## State (2026-11-02, tool 0.5.0-rc1, format schema 2 / 3, format revision 1)

What each release added is in the [changelog](../CHANGELOG.md); what was run, and against what, is in the [release reports](releases/).

Partial:
- CI runs the offline tier on Linux, Windows and macOS ([testing.md](testing.md)). The Unity importer and runtime LOD have only been run on macOS arm64; the LibreTerrain round trip is not covered in CI (no EXR driver in the CI GDAL).
- CUDEM, EMODnet, OSM and remote GeoTIFF across 180 are refused.
- Datum conversions cover EGM96, EGM2008 and the ellipsoid only.
- Unity LOD skirts are tuned on a synthetic 1-2 m cell test and two real worlds at 24-100 m, not at game view distances.
- Resume has no sub-stage checkpoints; tile writing always restarts.

## Next (highest value first)

1. Unity player run on Linux and Windows; IL2CPP; an EXR-capable GDAL in CI for the LibreTerrain round trip.
2. Unity: schema 3 worlds, floating origin, frame-time/GC profile of the streamer in a player, skirt margin at real distances, partial-world neighbour skirts (`SkirtSides` ignores a neighbour whose same-level tile is absent).
3. Larger real regions (above 1.4 M cells; the 6 x 5 degree attempt that filled the disk) on a machine with room; ask GMRT for a resolution near `cell_m` instead of the finest (the 383 MiB download for 8 MiB of output); progress/ETA on large grids.
4. Verified NAVD88 / MHW transformations (unlocks CUDEM combined with other sources) and NOAA multibeam/BAG probes ([ADR 0009](decisions/0009-noaa-dem-mosaic-not-adopted.md)).
5. Connectivity metrics (islands, channels) for LOD quality; shifted-position control for ETOPO/GEBCO across 180.
6. Unreal Landscape exporter only when a second consumer asks.
7. Vector products (depth contours, coast) from the grid; local OSM extract as an Overpass alternative.

## Open questions

- ODbL: a world built with `coast_conform` or `seamarks` contains OSM-derived cells and points. Treated as a derivative database; attribution is written to `ATTRIBUTION.txt`, share-alike is not legally analysed.
- Vertical datum of GMRT: a mosaic of contributed grids; how to quantify per region.
- Whether the accuracy cost of `shoal` (rms 16-21 m at level 1 on steep real terrain) is acceptable to consumers who need conservative shoals; both methods are measured, neither is the right default for everyone.

## Known problems

- PROJ's own gie file expects EGM96 values 0.37 / 0.40 m away from the installed grid at two points; the grid agrees with an independent synthesis, the gie value is unexplained ([research](research/egm96-discrepancy.md)).
- Overpass mirrors can be slow or down (a cached answer from any mirror is reused first since 0.5.0-rc1); the build then continues without a coastline and says so (`no-coastline` warning).
- Global steps (coast conform, regional fill, refinement) hold the grid in memory and are skipped or refused above the memory budget.
- Unity: schema 3 worlds are refused; only macOS arm64 players were run.
- LibreTerrain exchange assumes an EPSG code (UTM worlds only).
- Coastline-to-DEM offset is reported, not corrected. Preview colours are a fixed ramp, not a chart.
