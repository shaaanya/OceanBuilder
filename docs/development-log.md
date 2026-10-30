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
- Negative result: Falmouth with `regional_release: 2022` is REFUSED by the misregistration guard (rms 0.7 m when moved one cell vs 1.2 m aligned). Both coverages share the same geotransform (origin -5.303125, 50.286458, 0.0010417 deg, AREA), so the grids are registered identically; the 2022 content simply fits GMRT better after a shift. Cause not established (different compilation inputs vs. a real cell offset in that release). The guard was not loosened: dated releases are selectable but a release can fail validation, which is the intended behaviour. The current `emodnet__mean` passes (1.62 vs 1.42 m).
