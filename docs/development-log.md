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
