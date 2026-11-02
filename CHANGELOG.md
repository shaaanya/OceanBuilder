# Changelog

## Unreleased

- CI: first hosted run (2026-11-02) found three problems, now fixed: Go 1.22.12 test binaries abort on the macOS runner (CI pins Go 1.26; `go.mod` still declares 1.22 as the minimum), `gdal_calc.py` could not be launched on Windows (GEBCO measured mask; `gdal.Run` now goes through `python`), and the LibreTerrain round-trip tests failed instead of skipping where GDAL has no EXR driver.
- Renamed everything still called "Ocean Compiler" to OceanBuilder. Go module: `github.com/shaaanya/OceanBuilder`. Unity: the UPM package is `com.oceanbuilder.world`, the C# namespace and assembly definitions are `OceanBuilder.World.*`, and batch entry points are `OceanBuilder.World.Editor.*`. Existing Unity projects: delete `Packages/com.oceancompiler.world`, re-run `ocean export -target unity`, and fix any of your own code that references the old namespace. The HTTP User-Agent, `ATTRIBUTION.txt` header and preview title use the new name.
- Repository: IDE settings, the generated root `*.world.json` files and an unused wheel are no longer tracked; unused helpers removed; the release reports moved to `docs/releases/`, `docs/resume.md` is now `docs/stage-cache.md`.
- Source option `valid_max`: a dataset supplies only cells at or below it (after the vertical offset); higher cells fall through to the next dataset. `valid_max: 0` on a bathymetry source ranked below a land DEM stops GMRT's land artefacts (isolated cells of 5-7 km found over Cape Verde, present in the raw GMRT download) from landing on cells the land DEM calls sea. Recorded per dataset in the manifest. Test: `TestValidMaxKeepsHighCellsOutOfFallbackDataset`.
- Documented: a land DEM with sea cells stored as 0 (Copernicus GLO-30) works as a `geotiff` source with `"nodata": 0`, ranked above `gmrt` ([options.md](docs/options.md#land-dem-over-gmrt)).

## 0.5.0-rc1 (2026-11-02)

See [docs/releases/0.5.0-rc1.md](docs/releases/0.5.0-rc1.md). Release candidate: no hosted CI has run (no remote), Windows and Linux are unverified.

- Unity runtime LOD: `OceanLodStreamer` (distance cut with hysteresis, 2:1 balance, multi-viewer, async load/unload with retry, cancel and unload delay, cover-until-loaded), loader abstraction with an Addressables loader, importer skirts that hide mixed-level cracks (measured by rendering: 4317 background pixels without, 0 with), 6635 pure-C# property checks, Editor PlayMode tests and a macOS player run on two worlds ([docs/unity-lod.md](docs/unity-lod.md), [ADR 0011](docs/decisions/0011-unity-runtime-lod.md)).
- Format: level 0 is always complete (all-nodata level-0 tiles are written; validator `empty-tile`). Earlier builds dropped them silently.
- Fixed: Overpass answers were cached per mirror, so a warm build waited out a dead mirror and could switch data between builds; downloads used a shared `.part` name that two builds on one cache could corrupt.
- `ocean version` prints commit, Go, GDAL and platform; the build checks GDAL tools and free disk space (estimates measured on seven regions) before downloading; `-no-disk-check`; `OCEAN_CPUPROFILE`.
- Consumers: [integration guide](docs/integration.md), stdlib Python reference reader (`examples/python-reader`), cross-checked against `ocean sample` and `gdaltransform`; found and fixed the tile-edge lookup rule on real worlds.
- Packaging: `scripts/release.sh` (six targets, `SHA256SUMS`, reproducibility check), [install guide](docs/install.md).
- Tests/CI: `go test -short` tier, `scripts/verify-local.sh`, CI jobs for POSIX scripts and the Unity logic, [testing.md](docs/testing.md); [performance.md](docs/performance.md) with the region ladder; [troubleshooting](docs/troubleshooting.md); tutorials regenerated from real output.

## 0.4.0-rc1 (2026-11-02)

See [docs/releases/0.4.0.md](docs/releases/0.4.0.md). Release candidate: CI for Linux and Windows is written, never run.

- Stage cache: warp, vertical shift, compose and every LOD level are sealed, hash-verified stages; an interrupted build resumes at the first unfinished stage ([docs/stage-cache.md](docs/stage-cache.md)). Fixed: a source with an explicit nodata value never hit the warp cache.
- Antimeridian: GMRT, ETOPO 2022 and GEBCO worlds across 180 are fetched as two boxes (verified live); CUDEM, EMODnet, OSM coast, seamarks and remote GeoTIFFs are refused with a message.
- GMRT: delivered cell size is measured; oversized requests are split; `datasets[].delivery`, `source_res_m` is the coarsest piece, validator warning `source-coarsened`.
- EGM96: discrepancy resolved against an independent synthesis ([research](docs/research/egm96-discrepancy.md)); regression tests tightened from 0.5 m to 2 mm / 0.10 m.
- `ocean lod-report`: land/water loss, shallow-water rms, depth-class disagreement, gradient error, `-max-*` thresholds ([docs/lod-quality.md](docs/lod-quality.md)).
- `ocean sources explain <id>`; ADRs 0009 (NOAA mosaic not adopted) and 0010 (Seascape, GEBCO).
- Unity: LOD player smoke (macOS), importer file verification, exporter copies LOD levels (`export unity -level N`), checkpoint tolerance scaled to the 16-bit step.
- Validator: the `background-fill` warning no longer prints a 0 m resolution for sources that document none.
- Tests/CI: independent verification scripts (`scripts/`), `.github/workflows/ci.yml`.

## 0.3.0 (2026-11-01)

See [docs/releases/0.3.0.md](docs/releases/0.3.0.md) for what was verified, partial, unverified and deferred.

- Several elevation datasets per world with `listed`/`auto` precedence and a per-sample dataset layer; vertical datum policy with PROJ grid conversions and explicit refusals; `licensing` record in the manifest and `no_coastline_copy`.
- Local transverse Mercator grids (schema 3) for wide boxes and boxes across the antimeridian; windowed disk-backed pipeline with `-mem`.
- LOD: `ocean lod-report`, `lod_method: "shoal"`.
- Sources: ETOPO 2022, GEBCO (with TID survey mask), CUDEM; `ocean world check` shows per-source datum and conversion offline.
- Unity: coarser LOD levels imported (`-oceanMaxLevel`), standalone player build helper and headless smoke component.
- Fixed: `grid.bbox_wgs84` is the level-0 extent (it was the union over all LOD levels); vertical double conversion; unbounded GDAL cache.
- Changed: boxes over the UTM scale limit are planned as a local grid instead of refused (`projection: "utm"` restores the refusal).

## 0.1.0 (unreleased)

- First vertical slice: GMRT + OSM ingestion, UTM normalisation, shared-edge tiling, depth/shore masks, manifest, validator, preview, Unity export and importer.
