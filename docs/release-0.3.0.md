# OceanBuilder 0.3.0 release report

Date: 2026-11-01. Statuses use these words, and nothing is listed as verified that was not run.

- **Verified**: run in this repository by `go test ./...`, by a command whose output is in the [development log](development-log.md), or against the named real service on the stated date.
- **Partial**: works with stated limits.
- **Unverified**: written, not exercised against the real thing.
- **Deferred**: not started.

All measurements were made on one macOS machine (GDAL 3.13.0, PROJ from the same install, Unity 6000.4.0f1) and are single runs unless stated. Nothing was run on Linux or Windows.

## Verified

- **Multi-source composition** (Area A): precedence `listed`/`auto`, per-sample dataset layer, overlap/seam statistics, nodata hole fall-through, validator recount and tamper detection (`internal/world/compose_test.go`). Legacy configs reproduce 0.2.0 tile hashes (`testdata/golden`).
- **Vertical datums** (B): refusals for mixed, unknown, tidal, MSL-vs-geoid and missing offset source; EGM96 to EGM2008 shift agrees with `gdaltransform` to 0.02 m on a synthetic grid. This shows the builder uses the PROJ grid as PROJ does; it is not a check of the geoid values. The test found and fixed a double-conversion bug. `ocean world check` prints each source's datum and planned conversion without downloading.
- **Wide and antimeridian worlds** (C): planning at a zone boundary, a wide box, the equator and 180; reader against PROJ below 1 cm at three CRSs; end-to-end synthetic builds across 180 with UTM zone 1 (schema 2) and with a forced local transverse Mercator (schema 3); network configs across 180 refused before any fetch.
- **Large worlds** (D): window-size independence (byte-identical output for 0/1/2/3 tiles per window); a synthetic flat source at 85 M samples in 19 s and 325 M samples in 72 s with `-mem 256` (Go process 247-277 MB, GDAL 200-290 MB, 1.6 GB output).
- **LOD** (E): `ocean lod-report` measures every level against level 0; `lod_method: "shoal"` never makes a water vertex deeper (validator rule plus a hard-feature test scene). Real-region numbers are in the log: decimation has the lower rms (Moorea level 2: 8.8 m), shoal trades accuracy for safety (Moorea level 1: 20.7 m rms, too-deep 0). A smoothing method was tried, measured worse and removed.
- **Source adapters** (F), read from the real services on 2026-11-01: ETOPO 2022 (Falmouth window), GEBCO REST with its TID layer (Falmouth), CUDEM (a 0.02 degree window at Key West, 23 s), and a two-source GEBCO + ETOPO world built and validated in 21 s. Optional smoke tests run with `OCEAN_SMOKE=1`. Details and cautions: [sources.md](sources.md).
- **Real regions** (quality): Geiranger, Kona, Moorea, Mindelo and Bahamas-bank built from live GMRT/OSM and validated; time, size, memory, survey fractions and LOD error in the log. 1.6-1.9 s CPU and 53-82 MB peak RSS for the Go process in each; wall time 48-400 s, dominated by the services (a first run before the seamark-bbox fix took 248-433 s; the log records the correction).
- **Licensing record** (G): `licensing` in the manifest (sources, components, required notices, unresolved items), `no_coastline_copy`, validator checks (`TestLicensingRecord`). It is a record of what the tool knows about published terms, not legal advice.
- **Unity** (H), in batchmode: import, Addressables build and check, a standalone macOS player that builds (117 MB) and runs headless with all 16 terrains within their manifest ranges (this run predates the `lodTiles` change to the index asset; the player was not rebuilt afterwards); import of coarser LOD levels into `lodTiles` (`-oceanMaxLevel`), with positions matching the manifest to 0.00 m and 22 tiles verified in Addressables.
- **Cancellation**: Ctrl-C during a fetch exits 130 with "cancelled" and leaves an existing output directory byte-identical.
- **Bugs found and fixed late**: `grid.bbox_wgs84` and the OSM seamark query area were the union over all LOD levels (`ocean info` printed regions up to 100 km too wide; seamark requests were that wide); both now use the level-0 extent, and the validator checks `bbox_wgs84` (error for 0.3.0 builds, warning for 0.1/0.2/0.3.0-dev builds). Builds with `seamarks: true` change their output and build id accordingly.

## Partial

- Antimeridian: local GeoTIFF sources only, no OSM coastline; network fetchers are refused because they were not tested across 180.
- Resume: only the warp stage cache is reused; there is no mid-pipeline resume. A cancelled build was only tested during the fetch stage.
- Datum conversions: EGM96, EGM2008 and the WGS 84 ellipsoid only. MSL, NAVD88 and the tidal datums are recorded but not converted (refused without a documented offset or `allow_mixed`).
- GMRT coarsens large requests silently (a 2 degree box came back at ~55 m, a 6 x 5 degree box at about twice that); OceanBuilder reports the native cell size of what it received, but cannot tell that the request was coarsened.
- Unity: coarse LOD tiles are imported and addressable, but nothing selects levels at runtime and Unity terrain does not stitch tiles of different sizes (cracks between levels are not handled). No rendering was checked (`-nographics`). Schema 3 worlds are refused by the exporter and the LibreTerrain exchange.
- Scale measurements use a synthetic flat source.

## Unverified

- Any network source across 180; any source over a box wider than ~10 degrees.
- CUDEM beyond one small window; GEBCO's REST route is undocumented and could change.
- The installed EGM96 grid gives 36.021 m at 12.5E 55.5N where an older PROJ regression value is 36.394 m. The builder matches the installed grid; which is closer to NGA's values was not established.
- `gdal.CacheMB` is process-wide: concurrent builds with different `-mem` would share one value, and an exported `GDAL_CACHEMAX` is overridden.
- [research/source-probes.md](research/source-probes.md) was gathered by a research subagent and not re-checked by the maintainer.

## Deferred

- NOAA DEM mosaic and Seascape adapters (probed, ranked low; the probe fixtures were lost with the session scratch directory).
- A real build across a UTM zone boundary or over a large (million-sample) real region.
- `ocean sources explain` (the offline `world check` covers per-source datum and plan, not coverage).
- Unity: diagnostics overlay, runtime LOD selection and seam handling, schema 3 worlds, floating origin, water/seabed material.
- Mid-pipeline resume; progress/ETA re-measurement on large grids.
- Linux and Windows runs.

## Compatibility

0.2.0 configs build byte-identical tiles. Behaviour changes: a box wider than one UTM zone's 0.5 % limit used to be refused and is now planned as a local grid unless `projection: "utm"` ([ADR 0006](decisions/0006-local-transverse-mercator-grid.md)); manifests gain `datasets`, `licensing`, `format_revision`, `tiles[].dataset` and LOD source code 7 (shoal builds only); `bbox_wgs84` is now the level-0 extent; schema 3 only for local-grid worlds. Readers that ignore unknown fields are unaffected for UTM worlds.

## Release criteria

`go vet ./...` and `go test ./...` pass. Real-data builds and Unity batchmode runs are recorded in the log. Determinism: tested (window size, repeated builds). Docs: spec, ADRs 0006-0008, sources, roadmap, tutorials updated. Clean tree: yes after the release commit.
