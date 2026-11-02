# Development log

## 2026-10-30: first vertical slice

Done (all verified by running it):

- Repo, Go module `github.com/ocean-compiler/ocean`, CLI: `build`, `validate`, `preview`, `export`, `sources list`, `world init`, `cache stats|clean`, `version`.
- Real data: GMRT topo + topo-mask and OSM coastline for Falmouth (16 tiles) and Mindelo (20 tiles).
- Normalisation onto a UTM vertex grid, shared-edge tiles, depth classes, shore masks, manifest with provenance, attribution file.
- Validator: missing/corrupt tiles (hash), ids, bounds, edge cracks, nodata, 60° slope steps, survey-backed fraction, upsampling, coastline offset, provenance/licence.
- Unity 6000.4.0f1: batchmode import of Falmouth produced 16 linked terrains and a saved scene (`OCEAN_IMPORT_OK`).
- Tests (offline, real-data fixtures): config, bilinear/nodata, shoreline search, UTM zones, projection scale, OSM conversion, build + validate, determinism, tamper detection, crack detection, nodata reporting. Benchmarks for build and validate.

Findings:

- GMRT `topo-mask` is NaN, not a byte mask (my first reading of it as bytes gave 0% surveyed; fixed after reading the GridServer docs).
- Overpass returned HTTP 406 without a User-Agent. It is reachable from Go; some sandboxed tools had no network.
- LibreTerrain: not found on the public web, but later found installed locally in ../LibreTerrain; see ecosystem.md §1.
- Falmouth: OSM coastline is a median 101 m from the DEM shoreline (p90 268 m). Mindelo: 142 m / 331 m. The DEM is 39–58 m resolution and includes a tidal range, so this is expected order of magnitude, but not yet separated into datum vs. horizontal causes.

Known problems: see [roadmap](roadmap.md).

## 2026-10-30 (later): review fixes

- Validator no longer says "upsampled" when the output cell is coarser than the source; split into `upsampled` (cell < delivered source cell) and `gebco-fill` (share of water cells without survey data, computed from the mask).
- Verified GMRT terms (CC BY 4.0, citation required, **not for navigation**) and added GEBCO credit; ATTRIBUTION.txt and README updated. Cached provenance now takes licence text from code, not from stale sidecars.
- Manifest carries `checkpoints` (highest, lowest, four corners). Unity importer converts them to Unity space independently and fails the import on >5 cm error. Falmouth: max error 3 mm. A north/south or east/west flip would have passed the first version of the importer.
- Importer: shader fallback, translucent sea, in-engine screenshot (`docs/img/falmouth-unity.png`).
- Manifest no longer contains local absolute paths.
- Local git history re-authored to the configured user identity.

## 2026-10-30: project moved to Tools/OceanBuilder

Repo moved to `GameDev/Tools/OceanBuilder` next to `Tools/LibreTerrain`. Git history intact, `go vet` and `go test ./...` pass from the new location (no absolute paths in the repo).

## 2026-10-30 (session 3): LibreTerrain round trip

Ran LibreTerrain's headless MCP bridge and measured it (`docs/research/libreterrain.md`). Built `ocean libreterrain export|run|inspect|import` and a `refinement` edit layer in `ocean build` (ADR 0003).

- Falmouth 513², EXR32, unedited: max |error| 0 m, 6/6 checkpoints exact, 16/16 tiles byte-identical after rebuild.
- Real edit (LibreTerrain `adjust.blur`) on a non-square fixture: land edited, 83 water edits ignored, shoreline cells unchanged, validator clean.
- Findings that shaped the design: LibreTerrain is vertex-registered like us; non-square worlds are stretched (so we pad to a square); EXR is unclipped metres, PNG16 clips; project files are MessagePack (we let LibreTerrain write them through the bridge); a build larger than the project resolution silently never runs.
- Gotcha (fixed): cached provenance stored cwd-relative file paths, so `ocean build` had to run from the directory holding `.ocean-cache`. A cache hit now takes the real path; a build from another directory with `-cache <abs>` gives 16/16 identical Falmouth tiles.

## 2026-10-30 (session 3, later): Unity package, Addressables

- Importer moved to a UPM package (`internal/export/unitypkg`, installed to `Packages/com.oceancompiler.world`): runtime `OceanWorldIndex` (tiles, neighbours, UTM↔Unity, `LinkNeighbours`), editor importer, optional Addressables module compiled only when the package exists (`defineConstraints`).
- Verified in Unity 6000.4.0f1 batchmode: 16 tiles imported, 6 checkpoints max error 3.2 mm (corner checkpoints exposed an edge bug in `TileAt`: the east/north world edge belongs to the last tile), depth-painted layers, prefabs, index; Addressables 4.1.2 installed in the demo project: 17 entries registered and `BuildPlayerContent` succeeded with addresses and labels re-verified.
- Screenshot regenerated from the new scene (`docs/img/falmouth-unity.png`).
- The sailing game was not found on this machine (searched `~/Documents/projects/Unity`); nothing was tested against its Addressables settings.
- Gotchas found: a new Unity scene unloads the in-memory ScriptableObject (reload it from the asset); an asmdef referencing a missing assembly breaks compilation unless gated by `defineConstraints`.

- Review fixes: Unity importer now updates assets in place (GUIDs identical across re-imports, 47/47 metas unchanged; it used to delete `Generated/`). LibreTerrain export pads with the edge row/column instead of the minimum: blur edit on the non-square fixture dropped from max 55.2 m to 24.2 m, i.e. the cliff pad was contaminating real land. Export refuses worlds wider than 8193 samples (LibreTerrain builds above that need confirmation and silently do not run headless).

- Runtime check: PlayMode test in the Unity package loads `ocean-index` and tile prefabs through Addressables from built bundles (`BundledAssetProvider` asserted), links neighbours with `OceanWorldIndex.LinkNeighbours`, and matches 6 index checkpoints to `SampleHeight` within 5 cm: pass. First attempts found 0 tests (PlayMode asmdef must not be Editor-only; test-framework had to be installed and the package listed under `testables`). `BuildAndCheck` now switches the Addressables play mode script to the packed build.

- URP 17.4 test (scratch copy of the demo project, pipeline settings copied from a working URP project; the user's own projects were only read): first screenshot was magenta. Not the pipeline (a plain URP Lit cube rendered correctly in the same context) and not the shaders (`isSupported`, no errors): distant terrain uses URP's far-LOD base map, which a batchmode editor never generates. Raising `basemapDistance` fixed the screenshot; a PlayMode test (loads from Addressables bundles, camera 3.5 km away, counts pink pixels) gets 0.0000 under both Built-in and URP. Also dropped the hand-made URP `Terrain/Lit` template material (the pipeline's default is used). An earlier hypothesis (5 layers need an add pass, so use 4) was disproved: the screenshot stayed magenta at 4 layers, so 5 layers were restored and pass the same tests. Review also found the pink check could pass on an empty frame; the test now requires >20% terrain coverage (measured 0.766 Built-in, 0.765 URP) and a re-run passes under both.

- Mindelo (real, non-square, 5x4 tiles) unmodified LibreTerrain round trip through the real app: 0 cells changed, 20/20 tiles byte-identical. Committed a small real LibreTerrain output sample (`testdata/libreterrain/`).

- Seabed view (`-oceanHideSea`, URP): water renders as two flat tones with a hard shallow/deep edge from the depth classes. Fine as a data check; a real underwater look needs a depth-gradient material and finer bathymetry (roadmap). Screenshots regenerated from the current importer (5 layers); none contains magenta pixels; basemapDistance and the hidden sea plane do not reach the saved scene.

- Decision (user): Unity support is URP only. Removed the HDRP/Standard shader fallbacks; importer throws unless the active pipeline is a URP asset; package.json depends on com.unity.render-pipelines.universal; demo project converted to URP (committed `Assets/Settings`). Earlier Built-in results above are historical. Re-verified in the URP demo: checkpoints 3.2 mm, Addressables bundles built, PlayMode pass (terrain coverage 0.765, pink 0.0000).

- Seamarks: Overpass `nwr[seamark:type]` (needs the tool User-Agent, plain curl gets 406), parsed to points, projected to UTM, DEM elevation sampled, `seamarks.geojson` + manifest stats. Falmouth 58 features. Cross-check found 8/30 buoys on DEM land, all in intertidal creeks; reported, not corrected. Unit-tested parse (ways use centre, untagged/centreless dropped, stable order); the build integration was verified by a live Falmouth build only (needs network).

- Unity seamarks: index `seamarks[]`, `Seamarks` marker objects, export copies/removes `seamarks.geojson`. URP demo: import ok, 58 seamarks read back from the built bundles in the PlayMode test (pass), Go export test covers copy and stale removal.

- Seamark query now uses the tile grid's WGS84 envelope instead of the config bbox (advisor catch): Falmouth 58 -> 60 features, buoys on DEM land 8 of 31. Earlier 58/30 numbers in this log are from the bbox query.

- Coast conform (advisor-planned, opt-in). First design (flood-fill regions labelled by side seeds) failed on real data: coastline fragments clipped at the bbox leave land and sea connected, so the big land region was ambiguous and only tiny creek regions were labelled, which tripped the direction guard (12% agreement on a fixture window). Replaced by nearest-coastline-sample side (local, always defined). Guard now 30% over all labelled cells. Real Falmouth: 81% agreement, 433 to land, 6182 to water, 656 skipped, buoys on DEM land 8 -> 1 of 31 (independent of the algorithm), highest checkpoint unchanged (255.3 m). Pre-conform offset kept as the headline in stats/validator; post-conform offset (median 24 m) is reported but labelled as not evidence. Unit tests: moves shoreline to the OSM line, refuses a reversed way, skips deep/far cells, open fragment only affects its neighbourhood. LibreTerrain: unedited round trip with conform is byte-identical (the test asserts the fixture really conformed cells). MultiLineString coastlines are accepted. Note: `build_id` hashes config and inputs, not the algorithm, so it does not change when conform logic changes within an unreleased tool version.
- Found while screenshotting: the demo's `Sea.mat` dated from the Built-in era (Standard shader) and rendered pink under URP because the importer only created it when missing. It is now rebuilt and copied into the existing asset on every import (GUID stable).

- Mindelo with coast_conform: 69% agreement, 6330 to water, 408 to land, 2771 skipped (steep cliffs trip the 30 m guard); buoys on DEM land 6/6 -> 1/6; offset before 142/331 m. One buoy still on land is not investigated.

- `OceanTileStreamer` + `OceanStreamingPlan`: PlayMode 4/4 pass on URP (index test, plan tests x2, streaming move test: centre loads 5 tiles with neighbours linked, corner leaves 3, Terrain objects destroyed, destroy releases all). The plan hysteresis test initially asserted the opposite of its own comment; fixed before the real run.

- Advisor review fixes. (1) Conform guard was asymmetric: reversed agreement is exactly 1 - a, so a 30% threshold passed Mindelo's reversed 31%. Now refuses unless a > 0.5; real-fixture test reverses the real Falmouth coastline (reversed 15-16%, refused; forward 85%/84%). Mindelo still conforms at 69%, same cell counts. (2) `build_id` now hashes per-step algorithm versions (conform 2, seamarks 1, refinement 1) only when the step is active: plain Falmouth keeps e8cae523a144, the conformed world changed from 1b8dc99863d5 (shared by two different outputs) to 13e467e6ecf2. Bump the constants whenever the logic changes. (3) Root `unity/` was an empty leftover, removed; no non-URP references remain. (4) Doc wording: buoys corroborate, they are not independent; conform makes OSM-derived heights (ODbL note); batchmode screenshot note. (5) Streamer retries a failed tile after 10 s instead of every frame.

- Regional bathymetry (EMODnet WCS `emodnet__mean`, coverage from GetCapabilities). Probe on Falmouth: EMODnet vs GMRT survey water 1.8 m rms (median -0.5 m), vs GEBCO fill 3.0 m rms; GMRT is finer than EMODnet (39 vs 115 m) so EMODnet only fills. First guard (median over all shared survey cells <= 5 m) refused Mindelo at -5.5 m; investigation by depth bin showed the survey cells there are all 200-1500 m deep on steep slopes where p10/p90 are -80/+30 m: resolution scatter, not a datum offset. Changed the estimator to flat cells only (slope < 5% in both grids, min 200 cells): Mindelo then has 871 flat cells, median +2.74 m, and fills. Unit tests: fill touches only non-survey water; refuses an 8 m offset and too few cells (DEM untouched); offset comes from the flat shelf while a steep +-40 m scatter is ignored. `regional-algo=1` in the build id when active. Mindelo replaced 62.8% of the grid at up to 574 m change: documented as unproven improvement.

- EMODnet seam check (Falmouth, numeric + Unity seabed screenshot): max neighbour step in water 21.8 m vs 22.4 m baseline, p99 3.5 vs 2.8 m; 4,915 of 85,908 water cells changed (max 17.7 m); import checkpoints still 3.2 mm.

- Advisor review 3 (EMODnet). (1) Seam analysis restricted to water pairs where one cell was replaced and its neighbour kept, against the same pairs in the baseline: hard swap tripled the median seam step on Falmouth (0.46 -> 1.43 m) and raised Mindelo's p90 from 10.6 to 35.8 m; added an 8-cell blend ramp from kept cells; now 0.40 vs 0.49 m (Falmouth) and 1.48 vs 1.27 m (Mindelo). (2) The guard now reports what it measures: flat cells' median depth (Falmouth 49 m, Mindelo 370 m), relative offset, and aligned vs one-cell-shift rms. A first 5% shift threshold refused real Falmouth (1.62 vs 1.42 m); a sub-cell scan against GMRT survey cells showed the rms is nearly flat across +-70 m (1.75 -> 1.58), i.e. resolution mismatch, not misregistration, so the threshold is 25% (a 2-cell synthetic shift is caught, small shifts are not detectable, documented). "Alignment" is no longer claimed for the flat-offset check. (3) Validator keeps the gebco-fill warning when more than 5% of water is still GEBCO after the fill. (4) `base_build_id` was stored but never compared: the build now refuses a refinement whose base id differs from the world's id without the refinement (config name normalised to the export's world); test with a tampered id. EMODnet coverage is unversioned: documented. `RegionalAlgo` bumped to 2 (the blend changed output without changing the id until it was bumped). End to end on real Falmouth with regional + conform + seamarks through the real LibreTerrain: 0/16 tiles differ.

- Reproducibility (2026-10-30 session 2): cache hits are now re-hashed against the stored sha256 (corrupt/replaced file triggers a refetch); `<world>.lock.json` pins every resolved source by URL/sha256, checked on each build (`-update-lock` to accept, `-lock off` to disable). Locks generated from the existing cache for falmouth and mindelo (includes EMODnet only if the config enables it; the shipped configs do not). Tests: httptest cache-corruption refetch, lock diff (changed/new/unused, local files keyed by basename), end-to-end tamper test on the Falmouth fixture. Existing deterministic-build test already covers equal inputs -> equal tile hashes.

- EMODnet release pinning: GetCapabilities (2026-10-30) lists `emodnet__mean` plus dated `_2016/_2018/_2020/_2022`; config `regional_release` selects one (validated, in build id via config, recorded as `source` in stats). Whether `emodnet__mean` equals a newer undated release is unknown, so it stays the unversioned default. Real build with `2022` against Falmouth: see below.
- Regional fill acceptance is now explicit: `stats.regional_bathymetry.status` (`accepted` / `accepted_with_caveats`) and `caveats[]` (flat-cell median depth > 100 m, > 50% of water replaced, any cell moved > 100 m); the validator emits each caveat as a warning. Real Mindelo: `accepted_with_caveats`, all three caveats fire (370 m, 95% of water, 574 m). Falmouth: no caveats (49 m, 5%, 8.6 m). Unit-tested thresholds. Still unvalidated: no independent shallow-water checkpoint exists in the repo; the fill for Mindelo is NOT recommended (README).
- Negative result: Falmouth with `regional_release: 2022` is REFUSED by the misregistration guard (rms 0.7 m when moved one cell vs 1.2 m aligned). Both WCS files carry the same geotransform (origin -5.303125, 50.286458, 0.0010417 deg, AREA), which shows only how they are georeferenced, not that their content is registered the same: the measurement says the 2022 content is offset against GMRT by about one 48 m cell, while the current coverage is not, so `emodnet__mean` is a different compilation from `_2022` at Falmouth. Cause not established; hypothesis to test: a pixel-centre vs corner convention change (half an EMODnet pixel is ~58 m N-S, ~37 m E-W at 50N). The guard was not loosened: dated releases are selectable but a release can fail validation, which is the intended behaviour. The current `emodnet__mean` passes (1.62 vs 1.42 m).
- Advisor review 4 fixes: the lock was applied before the EMODnet/refinement sources joined `m.Sources`, so it pinned only GMRT+coastline; `applyLock` now runs last (also no lock for a failed build) and a test asserts lock entries == manifest sources. Regional caveats were dropped when >5% GEBCO water remained; now always emitted. README notes committed locks fail on fresh clones.

## 2026-11-01: sailing game integration audit (superseded)

An audit of one particular game and an integration plan were written and then removed the same day: the tool is engine-independent and must not be designed around one project ([ADR 0004](decisions/0004-engine-agnostic-scope.md)). The text remains in git history (commit `adabe69` and earlier). Nothing in the code or format depends on it.


## 2026-11-01: engine-agnostic 0.2 (format v2, LOD, incremental builds, reader)

Done, each tested (`go vet ./... && go test ./...`):

- **Format v2** ([spec](formats/world-v2.md), [ADR 0005](decisions/0005-format-v2-float32-source-layer-decimated-lod.md)): float32 heights, per-sample source layer, per-file sha256, LOD pyramid by exact vertex decimation (`L<k>_<x>_<y>`, parent/neighbour links). Validator checks layers against heights, shared edges at every level, parent/child samples, statistics recomputed from files.
- **Builds**: `input_key` skip, warp stage cache, atomic output swap, SIGINT/SIGTERM cancellation, UTM point-scale limits (warn 0.15 %, refuse 0.5 %, `utm_epsg` override), `PlanGrid` used by `world check`.
- **Reader** `pkg/oceanworld` (no GDAL, pure-Go UTM series): agrees with GDAL to 1 mm; GeoTIFF export agrees with `gdallocationinfo`.
- **CLI**: `info`, `sample`, `diff`, `world check`, `export -target geotiff|unity`, `build -force -q -log-json`, `cache clean -stages`, per-command help.
- **Ground-truth tests** with an analytic cone island: southern hemisphere (UTM 56S, worst error 0.24 m over 600 points), the same terrain delivered in Web Mercator (0.37 m), and a nodata hole that must stay nodata (class 255, source `nodata`).
- **Unity** (6000.4.0f1, URP 17.4), float32 importer, level 0 only: batchmode import of Falmouth (31 manifest tiles, 16 imported), six checkpoints max error 2.8 mm, Addressables build (16 tiles), PlayMode tests 4/4 pass. Editor only: no standalone player build was run.

### Measurements (Apple silicon, 10 cores, GDAL 3.13, Go 1.26, network live)

Region builds from `examples/regions/` (each `ocean build ... -lock off`, cold download):

| region | EPSG | cell | level 0 tiles | LOD levels | survey share of water | cold build | peak RSS | disk |
|--------|------|-----:|--------------:|-----------:|----------------------:|-----------:|---------:|-----:|
| geiranger (fjord, N. hemisphere, steep) | 32632 | 32 m | 20 | 6 | 28.1 % | 81.4 s | 55 MB | 3.1 MB |
| moorea (reef, S. hemisphere, 17.5°S) | 32706 | 48 m | 25 | 7 | 63.3 % | 470.5 s (first run: 6 min lost to Overpass; rerun with warm GMRT cache 72.7 s, coastline fetched in 33 s) | 55 MB | 4.1 MB |
| kona (volcano flank) | 32605 | 100 m | 9 | 6 | 93.6 % | 79.6 s | 59 MB | 1.8 MB |
| bahamas-bank (shallow bank) | 32618 | 100 m | 20 | 5 | 29.7 % | 78.2 s | 71 MB | 3.3 MB |

All four: `validate` 0 errors, 0 edge mismatches, 0 level mismatches; warnings are the intended ones (background-fill share, steep-slope discontinuity 1.56 % in the fjord, upsampling at Moorea where 48 m is finer than the ≈58 m delivered cell). Cold time is almost entirely GMRT/Overpass network time; CPU time is 1.5–2.0 s user.

Cache effectiveness and incremental behaviour (geiranger, bahamas-bank):

| case | time |
|------|-----:|
| cold | 81.4 s / 78.2 s |
| `-force` rebuild with warm download cache | 1.9 s / 1.7 s |
| rebuild with unchanged inputs (`input_key` match) | 0.53 s |
| Falmouth, only `depth_bands` changed (warp stage cache hit) | 1.4 s vs 1.9 s for the first build of that world (download cache warm both times) |

A forced rebuild into a different output directory reproduced the identical `build_id` and manifest (and the tests assert byte-identical tiles). Download cache for all runs: 60 MB.

Reader query cost (Falmouth fixture, `go test ./pkg/oceanworld -bench . -run x`): warm `SampleUTM` 40 ns, 0 allocs; cold tile load (file + 3 PNG decodes, 17×17 samples) 0.32 ms; 100-point profile 6.6 µs. Fixture tiles are tiny; a real 129×129 tile decode was not benchmarked.

### Findings

- Overpass was unreliable: for Moorea all three mirrors timed out at the old 3 min client timeout (9 min worst case). The build correctly continued without a coastline and said so (`no-coastline` in `validate`). Each mirror attempt is now capped at 100 s (the query asks the server for 60–90 s). The same query succeeded on a rerun 15 minutes later, so the failure was transient (cause unknown; mirror load is a guess). The cap is covered by a unit test with a silent server (`TestGetTimeoutGivesUpOnSilentServer`); the real-mirror failure path was not reproduced after the change.
- `info`-level `coast-coverage` is expected: Overpass returns whole ways, so many coastline samples lie outside the bbox.
- Repeated `gdalinfo --version` calls cost about 0.5 s each; the version is now memoised.
- `validate` and the libreterrain tests got slower (validate ≈55 s, libreterrain ≈75 s for the full package tests); not diagnosed beyond the memoised version fix, possibly machine load.

### Review follow-ups (same day)

- Spec checked against reality: the Python reader in the spec was run against Moorea (zone 6S) at levels 0, 1, 2 and matched `ocean sample` to 1e-3 m; every manifest key in the spec table was checked against a real manifest (`stats.notes` is omitted when empty, now stated); the tutorial commands were run verbatim and their outputs pasted.
- GeoTIFF export on a southern-hemisphere world: Moorea height.tif is EPSG:32706, and `gdallocationinfo` matches `ocean sample` exactly at 9 vertices (levels 0 and 2), after the half-cell registration was taken into account.
- Bug found by that comparison and fixed: `SampleUTM` reported a vertex exactly on the world's north or east boundary as outside the world (no tile beyond it). It now uses the last tile; `TestReaderAnswersOnWorldBoundary`.
- `ocean version` printed `schema 2 )`; fixed. `ocean sources list` now lists seamarks, EMODnet and local files. User-Agent says 0.2.
- Remote sources: `local.topo|mask|coastline` accept http(s) URLs; the file is downloaded once into the cache with sha256, retrieval time and the declared licence (`TestRemoteURLSourceMatchesLocalFiles` against a local test server). This is the route for a single NOAA CUDEM or other national GeoTIFF; no NOAA tile was downloaded in this session, so that use is untested against the real service.

### Not done / unverified

- NOAA CUDEM, Seascape, GEBCO direct: surveyed only ([research/ecosystem.md](research/ecosystem.md)); not implemented.
- Multi-source merge with precedence, multi-zone worlds, windowed processing, vertical datum conversion.
- Unity standalone player build; Unity importer reads level 0 only.
- EMODnet licence text was not read.

## 2026-11-01: 0.3.0 work, projection and tests

- Added tests for composition (precedence, overlap statistics, nodata hole, per-sample dataset layer, `auto` precedence), vertical refusals, EGM96→EGM2008 shift against `gdaltransform`, window-size independence (0/1/2/3 tiles per window: byte-identical tiles) and validator tamper detection.
- Bug found by the vertical test: a compound-CRS shift left the vertical CRS on the shifted file, so the later warp converted heights again (EGM96 −50 m became +3.1 m). The shifted file now carries only the horizontal CRS. Files that embed a vertical CRS are warped with an explicit horizontal source CRS.
- Open: the installed EGM96 grid (`us_nga_egm96_15.tif`) gives N = 36.021 m at 12.5E 55.5N, PROJ's regression suite (`egm96_15.gtx`, tolerance 1 cm) has 36.394 m, and at 180E 0N 21.153 vs 20.757 m. A direct `cct +proj=vgridshift` lookup of the installed file gives 36.0213 m too, so the builder's compound-CRS path matches the file; the difference is between the files. Not established which is closer to NGA's official values. The reference-point test only checks sign and magnitude within 0.5 m.
- Local transverse Mercator (ADR 0006). Tested: single zone stays UTM; zone boundary; wide box; equator (negative northings); antimeridian planning; the reader's projection against PROJ at 3 CRSs (< 1 cm); an end-to-end synthetic build across 180 that validates and samples correctly on both sides. Not tested: any network source across 180, Unity, LibreTerrain with schema 3.
- Source probes written to research/source-probes.md (dated 2026-11-01, by a research subagent; not re-verified by me).

### Measured: build time and peak memory vs. region size (2026-11-01, one Apple-silicon Mac, GDAL from Homebrew)

Synthetic flat -40 m GeoTIFF source (EGM2008, no network), 50 m cells, 256-cell tiles, `-mem 256`, wall time from `/usr/bin/time -l`, peak RSS sampled with `ps` every 0.1-0.2 s (so peaks can be missed by up to that interval). One run each; not a benchmark of real data (flat data compresses, so tile output sizes are not representative).

| Region | Samples | Tiles | Time | `ocean` peak RSS | gdal child peak RSS | Output |
|---|---|---|---|---|---|---|
| 3° × 2° | 22 M | 492 | 6.5 s | not sampled | not sampled | 133 MB |
| 6° × 4° | 85 M | 1741 | 19 s | 247 MB | 290 MB (678 MB before the GDAL cache cap) | 472 MB |
| 12° × 8° | 325 M | 6174 | 72 s | 277 MB | 200 MB | 1.6 GB |

- The Go process stays at the `-mem` budget while the grid grows 15x. `/usr/bin/time` alone reported 262 MB → 694 MB for 22 M → 85 M samples at `-mem 256`; that figure is the largest *child*, gdalwarp, with its default block cache (5 % of RAM). The pipeline now sets `GDAL_CACHEMAX` to MemoryMB/8 (64..512 MB). Output is unaffected (golden hashes and window-independence tests pass).
- `-mem 1024` vs `-mem 256` on 22 M samples: same output, 426 MB vs 262 MB total peak.
- Not measured: builds with the in-memory global steps (coast conform, regional fill), real survey data, more than 325 M samples, cancellation latency.

### LOD alternatives, measured (2026-11-01)

Synthetic scene (`internal/world/lod_quality_test.go`): 10 m source, 20 m output, 16-cell tiles, 2 coarse levels. Features: a ~30 m channel through a land mass, a ~25x35 m islet, a ~30x45 m shoal (-3 m in -30 m water), cliff coast (+10 m beside -30 m). `ocean lod-report`, error of the bilinear level surface against level 0 at level-0 vertices:

| method | level | rms m | p95 m | max m | too deep max m | land/water flips | uncovered |
|---|---|---|---|---|---|---|---|
| decimate | 1 (40 m) | 1.22 | 0.00 | 13.35 | 12.75 | 89 | 233 |
| decimate | 2 (80 m) | 2.44 | 2.38 | 25.30 | 25.30 | 204 | 509 |
| shoal | 1 | 1.95 | 0.00 | 28.63 | **0.00** | 112 | 233 |
| shoal | 2 | 4.21 | 8.17 | 34.43 | **0.00** | 225 | 509 |
| binomial smooth (removed) | 1 | 1.38 | 0.42 | 18.06 | 18.06 | 113 | 233 |
| binomial smooth (removed) | 2 | 2.58 | 3.08 | 25.33 | 24.01 | 241 | 509 |

- Decimation has the lowest rms and no safety property: the shoal vanishes (too deep by up to 25 m).
- Shoal removes the unsafe direction completely (0 m at every level, tested) and pays with a higher rms and wider shoals. It does not stop land/water flips of the interpolated surface near the coast (112 vs 89); the waterline of the *samples* never moves.
- Binomial smoothing was worse than decimation on rms and max on this scene and has no safety property, so it was removed rather than shipped as an option nobody should pick. Not measured: rough natural seabed, where low-pass behaviour might matter for visual shimmer.
- Not solved: channel connectivity. A channel narrower than two coarse cells can still disappear (the waterline is not moved to protect it).

### Adapters verified live, licensing record (2026-11-01)

- ETOPO 2022, GEBCO (REST + TID) and CUDEM read from the real services (numbers in [sources.md](sources.md)). A two-source world (GEBCO + ETOPO, `precedence: auto`, 50 m, Falmouth) built in 21 s and validated; GEBCO won every cell (same resolution, list order breaks the tie), 99 % of its water cells carry TID 10-17 and are labelled `survey`. That label means "direct measurement somewhere in a ~380 m cell", not a measurement at 50 m: the `upsampled` warning says so.
- A test using a fake download API caught nothing wrong in the adapter but a real bug in the builder: tile envelopes were wrapped relative to the bbox west edge, which made coarse (L4) tiles that reach far past the bbox 358° wide. Now wrapped around the bbox centre; the validator rejects envelopes wider than 90°.
- The manifest has a `licensing` record (what was read on 2026-11-01, per source and per output part). OSM data stays ODbL and separable: `no_coastline_copy` leaves the OSM coastline file out of the output. Default unchanged for compatibility.

### Unity standalone player (2026-11-01)

New in the Unity package: `Ocean > Build Standalone Player` / `OceanStandaloneBuild.BuildFromCommandLine` and the runtime `OceanPlayerSmoke`. Run on the Falmouth demo (Unity 6000.4.0f1, macOS, batchmode, URP): the player built in 42 s wall time (Unity reported 31 s, 117 MB, 0 errors). Running `unity-demo -batchmode -nographics` printed `OCEAN_PLAYER_SMOKE world=falmouth build=f1492fddaf19 tiles=16 ok=16 outside_range=0 missing_terrain=0` and exited 0: all 16 imported terrains were present in the player and their centre heights lay inside the manifest's per-tile range. Not tested: rendering (`-nographics`), Addressables streaming in the player, any other platform, schema 3 worlds (the exporter refuses them).

## 2026-11-01: Unity imports coarser LOD levels

`OceanWorldImporter.MaxLevel` (batch: `-oceanMaxLevel N`, default 0) imports LOD levels 1..N as tile prefabs. They go to `OceanWorldIndex.lodTiles` (`LodTiles(level)` helper, `maxLevelImported`), not to `tiles`, so the scene, streamer, `TileAt` and the smoke component keep seeing level 0 only and behave as before. A coarse terrain has the same sample count as a level-0 one and `size = span * 2^level`; its position comes from `bounds_utm` minus the index origin; neighbour ids are per level. Prefabs are registered with Addressables like level-0 tiles and `Build And Check` verifies them.

Measured (Unity 6000.4.0f1, macOS, batchmode, URP; a scratch copy of the demo project; `lodw`, a 4x4-tile offline world built from the Falmouth fixture with `lod_method: shoal`, 6 levels): `-oceanMaxLevel 2` printed `OCEAN_IMPORT_OK lodw tiles=16 (level 0; 6 coarser tiles of 27 in the manifest, max level 2)`; every coarse tile's `position` and `size` equals its manifest `bounds_utm` offset from the index origin to 0.00 m (script over the generated index asset); `OCEAN_ADDRESSABLES_OK tiles=22`. Not tested: rendering of mixed levels, streaming coarse tiles at runtime (no streamer support: nothing selects levels), seams between a coarse and a fine tile (Unity terrain neighbours stitch only same-size tiles; the library does not hide the crack), schema 3 worlds (still refused by the exporter).

## 2026-11-01: five real regions rebuilt with the 0.3.0-dev tool (GMRT, live)

`examples/regions/*.world.json` built from the live GMRT service and the OSM Overpass/OpenSeaMap services, each into a fresh cache, then `validate` and `lod-report -stride 2`. Machine: macOS, GDAL 3.13.0, a home connection; the wall time is dominated by the services (the `ocean` process used 1.6-1.9 s of CPU and 53-82 MB peak RSS in every build; GDAL children are not included in that RSS figure). **Correction:** the first run of this table (times 248-433 s) was made before the seamark query bug below was fixed; the table now shows a second run with a fresh cache after the fix. Which service was slow in the 400 s Bahamas run was not broken down. All five validate (`BUILT WITH WARNINGS`, no errors).

| region | cell | tiles (L0 / all) | wall time | output | water | water cells survey-measured | UTM zone |
|---|---|---|---|---|---|---|---|
| geiranger (fjord) | 32 m | 20 / 38 | 89 s | 3.3 MB | 3.1 % | 28.4 % | 32N |
| kona (volcano flank) | 100 m | 9 / 20 | 87 s | 1.9 MB | 19.6 % | 93.7 % | 5N |
| moorea (reef, southern hemisphere) | 48 m | 25 / 51 | 88 s | 5.0 MB | 82.8 % | 63.3 % | 6S |
| mindelo (near a zone edge, -25 deg) | 48 m | 20 / 35 | 48 s | 3.0 MB | 66.8 % | 3.8 % | 26N |
| bahamas-bank (shallow bank) | 100 m | 20 / 36 | 400 s | 3.0 MB | 84.5 % | 29.7 % | 18N |

Default LOD (vertex decimation), error against level 0 at level-0 vertices (rms / largest water-deeper-than-level-0 amount): level 1 is exact (0.000: every level-1 vertex is a level-0 vertex). Level 2: Moorea 8.8 m / 264 m, Kona 6.1 m / 114 m, Mindelo 6.6 m / 99 m, Bahamas 4.7 m / 98 m, Geiranger 16.9 m / 56 m. Level 5 (about 1.5 km cells): rms 41-111 m. These are measurements of the terrain being coarsened (steep flanks), not a defect to be fixed by the pyramid; a consumer that needs conservative shoals at distance should use `lod_method: "shoal"`.

Shoal on Moorea and Kona (same cached inputs, 53 s and 44 s): too-deep max is 0.000 m at every level except Moorea level 5 (0.139 m). That small figure comes from bilinear sampling across the waterline at a coarse level, not from a deeper vertex; the validator's per-vertex rule (shoal never deeper) holds. The price is accuracy: rms at level 1 is 20.7 m (Moorea) and 16.6 m (Kona) against 0 for decimation, because the shallowest water of each 3x3 on a steep slope is far from the centre value. Shoal is a safety bias, not an accuracy method.

Findings from this run:
- `grid.bbox_wgs84` was the union over all LOD levels, so `ocean info` printed regions up to 100 km wider than the grid (Geiranger "6.92..9.47 E" for a 0.25 degree box). The format document always said "level-0 grid". Fixed: the pipeline uses level 0 only and `validate` checks it (error for 0.3 builds, warning for 0.1/0.2 builds, which have the union). Test: `TestBBoxWGS84IsLevelZeroExtent`. The build id does not depend on the field.
- The OSM seamark query used the same all-levels union as `bbox_wgs84`: the Overpass request covered an area up to 100 km wider than the grid (found in the pre-release review: Mindelo, the only example without `seamarks`, was also the only fast build). Fixed with one shared `level0Extent()`. Kona with seamarks, fresh cache: 254 s before, 102 s after (GMRT 19 s, OSM coastline 42 s, seamarks 41 s), and the seamark list is now clipped to the grid area. Output differs for builds with `seamarks: true` (fewer, grid-local points; the build id changes).
- Mindelo: `[coast-coverage] 2533 of 5669 coastline samples fall outside the built area`. Expected, the island coast is cut by the box; the validator reports it as a warning.
- Not covered: a region across a UTM zone boundary (Mindelo sits at -25.08..-24.86, zone 26, about 0.9 degree from the -24 edge), large-area real data (the scale runs are synthetic), real NOAA/Seascape data.

## 2026-11-01: 0.3.0

Version set to 0.3.0; release report [release-0.3.0.md](release-0.3.0.md) replaces the rc1 report. Last measurements before release: Ctrl-C during an OSM fetch of a changed config exited 130 ("cancelled") and left the existing output directory byte-identical (checksum of all files before and after); GMRT large-request probe in [sources.md](sources.md).

## 2026-11-02: 0.4.0 cycle (in progress; entries are added as each item is verified)

Starting state verified: `go vet ./...` and `go test ./...` pass on 0.3.0 (macOS arm64, GDAL 3.13.0, Go 1.26). Disk is the binding constraint on this machine (8 GiB free at the start; Unity `Library/` is 2.2 GB), so large runs are sized to it. No git remote and no container runtime: CI is written, not run (see below).

### EGM96 discrepancy resolved
The installed grid matches an independent spherical-harmonic synthesis of the EGM96 coefficients to 0.1 mm at both test nodes (36.0213 m at 12.5E 55.5N, 21.1533 m at 180E 0N) and NGA's own test points to <= 6 cm (bilinear interpolation of a 15' grid). PROJ's gie file expects 36.394 / 20.757 and fails against this install by 0.37 / 0.40 m; no EGM96 computation tried reproduces those values. Full evidence and a reproduction recipe: [research/egm96-discrepancy.md](research/egm96-discrepancy.md). The old 0.5 m-tolerance test is replaced by `TestEGM96IndependentReferences` (2 mm at nodes, 0.10 m at NGA points).

### Cross-platform
`GOOS=windows|linux go vet ./...` and `go test -c` per package compile. The whole suite also passes as `GOARCH=amd64` under Rosetta (no FMA fusion), so the golden tile hashes do not depend on the architecture. `.github/workflows/ci.yml` runs vet + tests on ubuntu/windows/macos with conda-forge GDAL, a cross-target compile job, and a scheduled/manual network tier. **It has not run**: the repository has no remote. Linux and Windows execution remain unverified.

### Real build across a UTM zone edge and the equator (GMRT, live)
`galapagos-zone-edge`: bbox -90.5..-89.5 E (the zone 15/16 edge is at -90), -0.3..0.5 N, 200 m cells, EPSG:32616 chosen by the planner (centre latitude 0.1 N, so the southern part has negative northings and tile row -2..2). 48 s, 97 MB peak RSS of the Go process, 52 tiles in 9 levels, validate: no errors. Independent check (`scripts/verify_geo.py`: pyproj/numpy, shares no code with the tool): tile `bounds_wgs84` match pyproj's transform of the tile corners to < 0.1 mm; ground distance per grid metre is 0.99845..0.99950, the manifest's point scale predicts 0.99854..0.99945; elevation at 600 random level-0 vertices against the raw GMRT file at pyproj's lon/lat: rms 5.8 m, median 0.54 m at zero offset, 21-26 m rms when shifted by one cell in any direction (the position is right to within a cell; the residual is the 61 m source resampled to 200 m). The same box forced into the southern zone (`utm_epsg: 32716`, false northing 10 000 000) gives rms 4.7 m with the same offset test: no hemisphere dependence. The region sits on land-poor ocean (98.8 % water), so land/water behaviour at the edge is not exercised.

### Antimeridian for network sources
Changed: a world with east > 180 now requests GMRT, ETOPO 2022 and GEBCO as two boxes, `[west-pad, 180]` and `[-180, east+pad-360]` (`fetchBoxes`), and mosaics them in the warp. The old code clamped the padded box at 180 and would have dropped the far side. CUDEM, EMODnet, OSM coastline, seamarks and remote GeoTIFFs stay refused before any download, each with its own message. GMRT returns the western piece labelled with longitudes above 180 (observed: a request in -180..-179.6 came back as 179.9997..180.395), which GDAL handles, and the pieces overlap by one pixel. Verified live: `taveuni-180` (Fiji, bbox 179.8..180.25 E, -17..-16.6, 100 m cells, UTM 1S): 328 833 of 328 833 cells filled (no gap), validate clean, 800 vertices on both sides of 180 against the two raw files: rms 0.41 m, the 81 within 0.03 degree of the seam rms 0.31 m. Offline: `TestAntimeridianGMRTSplit` runs the same through a fake GridServer for both labelling conventions and checks no request is wider than 1 degree, the nodata fraction is 0, and heights at 179.99, 179.9995, 180.0005 and -179.99 equal the analytic seabed. ETOPO and GEBCO across 180 are implemented the same way but **not** verified live.

### GMRT delivered resolution
Probed 2026-11-02 at the equator with `resolution=max`: 0.25, 0.5, 1, 2 and 3 degree boxes all came back at 61 m cells (3 x 3 degrees: 5462 x 5464 nodes, 119 MB, 102 s); `mresolution=500` returned 122 m (the service snaps to power-of-two levels, it does not honour the number); the GridServer documentation says GeoTIFF has a lower node limit than NetCDF but does not give it. The metadata endpoint with `format=geotiff` returns binary, not JSON. 0.3.0's 6 x 5 degree request (about 99 M nodes) was coarsened. Now `gmrt.Plan` makes a 0.02 degree probe at the centre to learn the finest cell the service offers, splits any request above 16 M nodes into pieces (4 reference cells of overlap on interior edges), and records in `datasets[].delivery` the reference cell, the pieces, the finest and coarsest cell received and the number of pieces more than 25 % coarser than the reference. `resampling.source_res_m` is now the coarsest piece. The validator warns `source-coarsened`. Tests: `TestGMRTDeliveryAndSplitting` (fake server that coarsens oversized requests). The 16 M budget is below the largest size seen to work (30 M), not a measured threshold.

### Unity LOD in a standalone player (macOS only)
Commit 91446b6. The player smoke (`OceanPlayerSmoke`, batchmode `-nographics`, absolute `-logFile`) loads the LOD tiles of an imported world in a built macOS player and checks, against the manifest and level 0: tile positions and sizes, heights, shared edges, level-k vertices equal to level-0 vertices, load and unload, and loading by Addressables address. The importer verifies tile hashes and levels before import; the exporter copies LOD levels (`-level N`) and keeps `.meta`; the 16-bit checkpoint tolerance scales with the quantisation step (range / 65535) instead of a fixed figure. A watchdog ends a hung player; failures write the report before exit. Fixed on the way: Addressables init handle released before read, content built before the scene object is destroyed, relative `-logFile` ignored by Unity. Not verified: Windows and Linux players, IL2CPP, URP rendering beyond the screenshot capture.

### Large region: Hawaii 4 x 3 degrees, GMRT, live (2026-11-02)
`hawaii-4x3`: bbox -157..-153 E, 18.5..21.5 N, 200 m cells, tile 256 cells, UTM 5N, 2305 x 2049 samples, 111 tiles in 5 levels. The request is split into 6 GMRT pieces of about 1.7 x 2.06 degrees (16 M node budget); all came back at the finest cell (56.9..57.6 m against a 58.8 m reference, 0 coarsened pieces), `source_res_m` 57.47.
- wall time 656.5 s, of which 652 s are the six GMRT downloads (85-126 s each for topo+mask); everything after the last download (warp, compose, 5 levels, tiles, manifest) took 4.2 s;
- peak RSS of the Go process 285 MB (GDAL children not included), 8.1 s user + 5.4 s system CPU;
- download cache 581 MB (GeoTIFF topo + mask), warped stage rasters 37 MB, output 30 MB;
- `ocean validate`: 2.4 s, 134 MB, no errors; 92.8 % water, 93.6 % of it survey-measured.
Conclusion: the download dominates; the tool's own work is under 1 % of the wall time. A 6 x 5 degree attempt was stopped for lack of disk, not for a tool limit.

### Stage cache and resume
New `stagecache.go`, [resume.md](resume.md). Warp, vertical-shift, composition and every LOD level are now stages keyed by their inputs' sha256 and sealed; reuse re-hashes the artifact; interrupted stages leave nothing complete-looking. Found while doing it: the old warp key printed `d.spec.Nodata` (a pointer) with `%v`, i.e. an address, so a source with an explicit nodata value never hit the cache (and the key differed between runs); now the value is hashed. Tests: `TestResumeAfterInterruptAtEveryStage` (cuts at start/mid of every stage and after compose; resumed output byte-identical to an uninterrupted build; finished stages reused; no temp artifacts), `TestStageCacheDeterministic` (warm cache, new cache and output directories, GOMAXPROCS=1, 64 MB budget with 1-tile windows give the same bytes), `TestCorruptStageCacheIsDetected` (bit flip, truncation, lost seal on warp and directory artifacts; mutation-checked: with the hash comparison disabled the test fails). The suite got slower (about 30 more GDAL builds); CI sets `-timeout 25m` and a loaded laptop needs `-timeout 30m`.

### ETOPO 2022 and GEBCO across 180, live (2026-11-02)
`bbox 179.7..180.3 E, -17..-16.6 N`, 200 m cells, 64-cell tiles, UTM 1S. Both adapters fetched two pieces (ETOPO: tiles covering 179.556..180 and -180..-179.556; GEBCO: two subset requests, 107 x 162 px each), built 43 tiles in 6 levels, validate: built with warnings (expected for 460 m sources at 200 m cells: low-res-bathymetry, upsampled, background-fill, no-coastline, vertical-datum note), no errors. 60 random positions in 179.76..180.24 E sampled with `ocean sample` against the raw cached pieces read bilinearly at the same lon/lat with `gdallocationinfo`: ETOPO rms 1.76 m (median 0.28, max 7.0), GEBCO rms 1.43 m (median -0.01, max 4.2); the 6 positions within 0.03 degree of 180: 1.6 m and 1.0 m rms, i.e. no seam bias. Differences are the 460 m source resampled to 200 m by the world warp versus bilinear on the source. There is no shifted-position control in this check (unlike the GMRT one), so it shows no gap or offset at the seam, not sub-cell positioning. GEBCO took 66 s (queue latency), ETOPO 6.8 s.

### Sources: decisions
NOAA `DEM_global_mosaic` rejected on live evidence ([ADR 0009](decisions/0009-noaa-dem-mosaic-not-adopted.md)); Seascape and direct GEBCO evaluated ([ADR 0010](decisions/0010-seascape-and-direct-gebco.md)). Smoke tests against the real ETOPO, GEBCO and CUDEM services pass today.

### LOD quality
`ocean lod-report` gained land/water loss, shallow-water rms, depth-class disagreement, gradient error and `-max-*` thresholds (non-zero exit). Measurements and trade-offs: [lod-quality.md](lod-quality.md). Finding: on the Falmouth fixture shoal removes the too-deep direction entirely but gives 1.7-4.4 times the shallow-water rms and more water vertices that read as land (127 vs 25 at level 1) than decimation.

### Resume with real processes, and a race found by it (2026-11-02)
The in-process tests (`StageHook` errors) cannot show what a killed process leaves. `scripts/interrupt_resume.py` sends SIGINT and SIGKILL to a real `ocean build` as soon as a warp, compose, lod1 or lod2 artifact appears: 9 of 10 triggers fired (the `compose-*.tmp-*` trigger was missed for SIGINT because the build finished first); each rebuild on the same cache was byte-identical to a clean build; a SIGKILL during compose left one `compose-*.tmp-*` directory and the rebuild succeeded. `scripts/concurrent_builds.py` runs three builds at once on a fresh cache: the first version failed 2 of 6 rounds (`...bin.seal: no such file or directory`, a shared `.seal.tmp` name, plus a vshift temporary with a fixed name). Fixed with unique temporary names and by never deleting an unsealed file; afterwards 15 of 15 rounds (45 builds) succeeded with identical output. Mutation re-check of `TestCorruptStageCacheIsDetected` against the committed test: with both hash comparisons disabled, all four `flip-*` cases fail.

## 0.5.0-rc1 work (2026-11-02), in the order it happened

### Unity runtime LOD
- **Cracks measured, not assumed.** A scratch Unity lab rendered terrain tiles of different sizes against a pure-green camera clear colour and counted green pixels. Mixed sizes: 4317 background pixels over 12 low views; all-fine reference: 0. Snapping the fine tile's edge to the coarse samples did not close them (Terrain tessellates its own patches). Static skirts did: 0 pixels. The skirt margin (0.5 m up, relief + 2 cells down) comes from that 1-2 m cell experiment.
- **Pure logic outside Unity.** `OceanLod.cs` has no UnityEngine reference, so `tools/lodtests` (dotnet) checks it with property tests: 6635 checks (cut covers each point once, 2:1 balance, hysteresis, missing levels, unavailable tiles, multi-camera, random-walk streaming with asynchronous loads never leaving a hole, skirt sides). One mutation (balance removed) first went undetected until the test forced a level jump of 2.
- **Streamer** with a loader abstraction (`IOceanTileLoader`): Addressables in production, a prefab loader and a counting/failing wrapper in tests. Scenarios (`OceanLodSmoke`) run in Editor PlayMode and in the player: calm, move, 150 rapid teleports, interruption/leak check, failure injection with retry, corrupt address, missing levels, release, seam.
- **Player run, two bugs.** (1) The player smoke range check flagged 9 tiles at -9.5 m: nodata cells are flattened to the world minimum by the importer, below a tile's own minimum; the check now allows that floor. (2) The synthetic seam in the player showed no benefit from skirts (4317 = 4317). Cause: the probe took the skirt material from a streamer instance it then destroyed, which released the Addressables bundle and destroyed the material. Fixed by keeping the instance (hidden) until the probe is done. A wrong run before that was caused by the git-ignored copy of the package inside the demo project being stale.
- Verified in a macOS arm64 standalone player (Mono, URP 17): falmouth-derived 5-level world and the kona volcanic flank (real GMRT, 100 m, levels 0-3). Both `OCEAN_PLAYER_RESULT failures=0`, synthetic seam `reference=0 mixed_no_skirt=4317 mixed_skirt=0`.

### Format
- Level-0 tiles that are entirely nodata were silently dropped by the tile writer; the validator then reported confusing errors. Level 0 is now always complete (`empty-tile`); upper levels still skip empty tiles. Test: `TestEmptyTileKeptAtLevel0DroppedAbove`.

### Real-world findings
- **Overpass caching.** Responses were cached under the mirror URL. A warm build tried the first mirror again, waited out its timeout when it was down (geiranger warm: 41 s instead of 2.4 s) and then used another mirror's answer, so two builds of one config could differ (seen in the tutorial run as "osm-coastline content changed"). Fix and test: any mirror's cached answer is used first.
- **Download temp name.** `fetch` wrote `<file>.part`, shared by concurrent builds on one cache. Now unique.
- **Python reference reader on real worlds.** The first version failed the manifest checkpoints at the world's north and east corners: a position exactly on a tile edge belongs to the tile before when the next one does not exist, and a vertex read with zero bilinear weight must not be invalidated by a nodata neighbour. Fixed in the reader and written into the integration guide; synthetic worlds had not hit it.
- **GMRT download volume** does not follow `cell_m`: 383 MiB for an 8 MiB world.
- **Disk.** The go build cache grew 1.6 GiB during the cross-platform release build and nearly filled the 4 GiB disk during the ladder; the disk preflight (`disk.go`) and the ladder's 2 GiB stop exist for this reason.

### Measurements
Region ladder and test timings: [performance.md](performance.md). Validation matrix (7 regions; validate, provenance, attribution, lod-report, Python reader check, Python vs `ocean sample`, deterministic rebuild): all pass; recorded in [release-0.5.0-rc1.md](release-0.5.0-rc1.md).
