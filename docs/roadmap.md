# Roadmap

Ocean Compiler is a standalone, engine-independent tool ([ADR 0004](decisions/0004-engine-agnostic-scope.md)). Status labels: **done** = implemented and tested; **partial** = works with stated limits; **open** = not started.

## State (2026-11-02, tool 0.5.0-rc1, format schema 2 / 3, format revision 1)

Done (implemented and tested; see [release report](release-0.3.0.md) for what was run against real services):
- Everything of 0.2.0 (format with LOD pyramid and per-sample source layer, GMRT / local / OSM / OpenSeaMap / EMODnet sources, reproducible incremental builds, reader, GeoTIFF export, Unity URP adapter).
- Several elevation datasets per world with listed or `auto` precedence, per-sample dataset identity, overlap and seam statistics ([ADR 0007](decisions/0007-dataset-composition.md)).
- Vertical datum policy with recorded PROJ grids and explicit refusals ([ADR 0008](decisions/0008-vertical-datum-policy.md)); `ocean world check` shows it offline.
- Windowed disk-backed pipeline; output independent of window size; memory follows `-mem`.
- Local transverse Mercator grids for wide boxes and boxes across 180 (schema 3, [ADR 0006](decisions/0006-local-transverse-mercator-grid.md)).
- LOD measurement (`ocean lod-report`) and the `shoal` method.
- ETOPO 2022, GEBCO (with TID), CUDEM adapters read from the live services; licensing record in the manifest.
- Unity: coarser LOD levels imported (`-oceanMaxLevel`), standalone player build and headless smoke run.

Done in 0.4.0-rc1 (see [release report](release-0.4.0.md)): stage cache with verified resume ([resume.md](resume.md)); antimeridian for GMRT / ETOPO / GEBCO; GMRT delivered-resolution detection and splitting; EGM96 discrepancy resolved; LOD quality metrics and thresholds ([lod-quality.md](lod-quality.md)); `sources explain`; Unity LOD player smoke on macOS; a real UTM-zone-edge build and a 4 x 3 degree build.

Done in 0.5.0-rc1 (see [release report](release-0.5.0-rc1.md)): Unity runtime LOD (distance selection with hysteresis and 2:1 balance, async streaming with retry/cancel/unload delay, cover-until-loaded, static skirts; [unity-lod.md](unity-lod.md), [ADR 0011](decisions/0011-unity-runtime-lod.md)) verified in the Editor and in a macOS player on two worlds; test tiers and a local verification script ([testing.md](testing.md)); a stdlib Python reference reader checked against `ocean sample` and GDAL ([integration.md](integration.md)); `ocean version` details, release script with checksums and a reproducibility check ([install.md](install.md)); disk-space preflight and a measured region ladder ([performance.md](performance.md)); level 0 always complete; Overpass answers reused across mirrors; GDAL preflight.

Partial: CI (written, never run; no remote; Linux/Windows unverified, macOS arm64 is the only platform tests ran on); CUDEM, EMODnet, OSM and remote GeoTIFF across 180 (refused); datum conversions (EGM96, EGM2008, ellipsoid only); Unity LOD skirts are tuned on a synthetic 1-2 m cell test and two real worlds at 24-100 m, not at game view distances; resume has no sub-stage checkpoints and tile writing always restarts.

## Next (highest value first)

1. Run the CI matrix (needs a remote) and fix what Linux/Windows show; Unity player run there; IL2CPP.
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
