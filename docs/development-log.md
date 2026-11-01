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
