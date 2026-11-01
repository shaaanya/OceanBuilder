# Implementation plan: OceanBuilder into ALONE

Date 2026-11-01. Grounded in [sailing-game-audit.md](sailing-game-audit.md); requirement IDs from [requirements-matrix.md](requirements-matrix.md). `P-n` = OceanBuilder repo, `G-n` = game repo (ALONE; its rules require the owner's approval of a plan before code). Complexity: S < 1 day, M 1 to 3 days, L about a week, XL more.

## The milestones

**M0, "Falmouth slice" (first real proof).** In ALONE's Falmouth chapter, Play from `Boot`: the boat's keel grounds on a shoal taken from OB bathymetry and not on deep water; the echo sounder reads OB depth; Crest shows shoaling; the 60 Falmouth seamarks stand at their positions; all rebuilt from one command and imported with no hand steps. Stop here and judge by playing before building more.

**M1, "minimum complete set" (the answer to the central question).** M0 plus: a **second cluster far from the Act I origin (Mindelo, UTM 26N, about 4,100 km away)** built from the same region file with no manual edits, proving the frame fix (B1) and the expansion workflow; the cluster list for all of Act I generated from the game's pack manifests; memory and latency measured in the game; the licence/attribution decision recorded. With M1 the owner can add any later region by editing one region file and running one command. Production and advanced items follow from play evidence.

Excluded from M1 on purpose: LOD pyramid, multi-zone worlds, windowed processing, Unreal, OB texture/shader work, harbour geometry, tidal height, OB's own streamer and Terrain importer.

## Dependency order

`D-1` → `P-1`, `P-3` → `P-5` → (`G-7`, then `G-1`, `G-4`; `P-4`, `P-6` in parallel) → `P-7` Falmouth gate (= M0) → `P-8`, `P-9`, `P-12`, `G-2`, `G-6` → Mindelo run (= M1) → production → advanced.

## Tier 0: Blockers (before meaningful integration)

| ID | Deliverable | Why | Existing code | Deps | Acceptance | Validation | Size |
|---|---|---|---|---|---|---|---|
| D-1 | `docs/decisions/0004-game-integration-defaults.md` recording R1 (offshore + coastal awareness, no pilotage), R2 (relief optional), R3 (GSHHG stays the game coast; OB follows it), R4 (lights blocked until the owner picks a source); owner confirms or overrides | Four plan branches depend on them; defaults are in audit section 7 | audit §7 | none | File exists; each R has default, alternative, and what changes | Owner read | S |
| P-3 | Game-frame grid: `world.json` option `frame: {kind: "aeqd", lat0, lon0}` (the leg's pack origin, 50.14/-5.04 for Act I) so heights/classes are cut in the plane of the game's tiles; manifest `frame` block; **data-only export** (`-target data`: tiles + index JSON + frame; no Terrains/prefabs/scene) | B1, B3, T3: data must agree with the rendered tiles; no duplicate geometry | `internal/gdal` (UTM selection), `internal/world/world.go`, `internal/export` | D-1 | (a) Go test: 200 random WGS84 points map to pyproj `+proj=aeqd +datum=WGS84` within 0.01 m and to the game's `AeqdProjection.Forward` within 0.01 m (reference table generated once from the game's C# and committed as a fixture); (b) deterministic rebuild; (c) UTM mode output byte-identical to today | `go test`; fixture; byte-compare with `output/falmouth` | L |
| P-5 | Runtime `DepthSampler` in `com.oceancompiler.world`: `bool TryDepth(double lat, double lon, out float depthM)`, double-precision geo, bilinear over shared-edge tiles, false outside coverage; asmdef free of Addressables/Terrain; loads grids from bytes | N1, S2, S3 | `OceanWorldIndex.cs` | P-3 | (a) matches the Go-side sampler at 1,000 random points within 0.01 m; (b) ≥ 1 M queries/s single core (need about 50/s); (c) continuous across joins to < 1 mm; (d) identical after 100 simulated origin rebases; (e) never returns 0 for unknown | EditMode tests in the package; Go golden file | L |
| P-1 | Per-cell source layer (roadmap A): uint8 (survey, GEBCO fill, regional, regional-blend, conformed, refined, nodata), per-tile hashes, validator checks vs manifest stats, exported with the grids | G5: AGENTS.md forbids presenting interpolated data as measured; the game must be able to say so | `internal/world/build.go` (`meas`, `replace`/`dist`), `internal/validate` | none | Layer for all tiles; class counts equal manifest figures to the cell; validator fails on a doctored tile (test) | Go tests; Falmouth + Mindelo rebuild | M |

## Tier 1: Minimum usable integration (M0, then M1)

| ID | Deliverable | Why | Existing code | Deps | Acceptance | Validation | Size |
|---|---|---|---|---|---|---|---|
| P-4 | `coast_conform` accepting an external coast source (GeoJSON from the game's GSHHG pack, or the shapefile via GDAL) in place of OSM | G3/R3: OB land/water must follow the coast the player sees | `internal/world/conform.go`, `internal/gdal` | D-1, P-3 | Falmouth: p90 distance from the game's GSHHG vertices to OB's 0 m contour ≤ 48 m (record the pre-change baseline first; against OSM it is 259 m); same guards pass (deep/high never flipped; refusal when the reversed reading fits better); unedited LibreTerrain round trip stays byte-identical | `evidence/coast_alignment_and_projection.py` re-run; `go test` | M |
| P-6 | Seamarks in the game frame: x,y in frame in `seamarks.geojson`; data-only export writes `seamarks.json`; floating marks y = 0 | N4 | `internal/world/seamarks.go`, `internal/osm/seamarks.go` | P-3 | 60 Falmouth marks within 0.01 m of the frame transform; counts per type equal manifest stats; marks on DEM land reported (8 of 31 buoys today; expected to fall after P-4) | Go test | S |
| G-7 | Game editor tool `ALONE/World/Import OceanBuilder Region` + batch method: reads OB data-only output, creates `OceanRegionData`, registers it in Addressables group `World_<legId>` under `world/<legId>/ocean/<cluster>`; refuses a frame that differs from the leg origin; idempotent | T3, T4: zero manual steps | `WorldTileImporter.cs` (group creation ~137 to 160, 426), `AddressableGroups.cs` | P-3, P-5 | Re-run with unchanged input changes no asset bytes; headless run exits 0; the asset loads by key in Play | Editor batch run (Editor must be closed or a second working copy used) | M |
| G-1 | Boat draught parameter (VO65 value supplied or approved by the owner, not guessed) and a depth test in `BoatGrounding`: grounded when depth < draught at the keel position; reuse speed-cancel and `InterruptReason.Aground`; add `ShoalAheadM` beside `CoastAheadM` | N2; `boat-physics.md:288` lists it as not done | `BoatGrounding.cs`, `Vo65KeelFoilProfile.asset` (span 4.78 m is not draught) | P-5, G-7, owner | Scripted: boat at 6 kn onto a measured shoal grounds within 1 boat length of the draught contour; same speed in 30 m never grounds; a report lists Falmouth channel cells shallower than 5 m so data limits are visible, not hidden | Existing bots (`PlayerShot`/`SailSim`) + EditMode test of the pure depth rule | M |
| G-4 | Crest sea-floor depth from OB data: bake the cluster's depth into an `OceanDepthCache` (or registered input) at import; enable `_createSeaFloorDepthData` only with a region loaded | V2 | `World.prefab:131`, Crest `OceanDepthCache.cs` | P-5, G-7 | Texture value equals `TryDepth` at 20 points within interpolation; visible shoaling at Falmouth (screenshot pair); ≤ 0.3 ms frame cost at the G7 Falmouth site (Editor, same method) | Screenshots; `PerfLog` | M |
| P-7 | **M0 gate:** Falmouth end to end in ALONE; short report `docs/integration/m0-report.md` with measured numbers and failures | The only proof that counts | all above | P-1, P-3..P-6, G-1, G-4, G-7 | Every M0 statement true | Play in the Editor | M |
| G-2 | Echo-sounder readout from `TryDepth`; hidden when unknown | N8 | `GuideHud` / UI Toolkit HUD | G-7 | Readout equals `TryDepth` at the boat within display rounding | Dev screenshot | S |
| P-8 | Region file + generator: `ocean region plan <game pack manifest>` writes `regions/<leg>.json` (clusters as in `evidence/pack_relief_and_clusters.py`, 20 km margin, frame = leg origin); `ocean region build` builds each cluster | G2, T1 | `cmd/ocean`, `internal/world` | P-3 | Act I plan = 7 clusters, each ≤ 17 M cells at 48 m; second run reproduces every `build_id`; deleting `output/` and rebuilding gives byte-identical data | Go test on a manifest fixture; real run on Act I | M |
| P-9 | Measurement pass: build time and peak RSS for a 200 km cluster; in-game memory with a region loaded; sampler cost in a physics step | S5: no numbers exist | n/a | P-8, G-7 | Report at 3 sites; check against the derived 120 MB cap | Memory Profiler | M |
| P-12 | Attribution text importable by the game (`ATTRIBUTION.txt` exists); shown on a credits/data screen | G8, B7 | `ATTRIBUTION.txt` | none | Text identical to the built world's, GMRT citation included | Visual | S |
| G-6 | EditMode tests in ALONE: frame fixture vs `AeqdProjection`, sampler golden values, grounding rule | T7 | `Assets/Tests/EditMode` (73 tests) | P-5 | Green with the existing 73 | Unity test run | S |

**M1 gate:** `P-8` plan for Act I; build and import **Mindelo** and **Falmouth** from one region file; at 5 km from the Mindelo anchor, seamark and depth positions agree with the rendered tiles to ≤ 25 m (unfixed frame error there is 337 m, so this tests B1); `G-6` green; `P-9` report accepted; `T8` licence decision recorded.

## Tier 2: Production readiness

| ID | Deliverable | Why | Deps | Acceptance | Size |
|---|---|---|---|---|---|
| P-10 | Lock policy for game regions: commit locks, document fresh-clone `-update-lock`; `scripts/rebuild-check.sh` rebuilds a fixture region and compares hashes | T2 | P-8 | Identical `build_id` on a second machine (assumed; verify when one is available) | S |
| G-3 | Chart depth/marks layer in `NavChartRenderer` | N7, N4 | G-7 | 20 m contour coincides with the OB class boundary within 1 cell | M |
| G-8 | Seamark prefabs and placement from `OceanRegionData`; buoys ride Crest height | N4, V5 | G-7, P-6 | 60 Falmouth marks within 1 m of data | M |
| G-5 | Dev depth/source overlay | V6 | P-1, G-7 | Toggles in Play | S |
| P-13 | EMODnet/NOAA regional fill decided per region, only where guards pass (README: Mindelo fill is unvalidated) | G5 | P-1, T8 | Per-region decision with measured offset recorded | M |
| P-14 | Standalone macOS Player check of region load (when the owner lifts the no-Player ruling) | S7 | G-7 | Region loads, depth values as expected | M |
| T8 | Licence decision record: ODbL (seamarks, `coast_conform` output), EMODnet, GSHHG, GMRT | B7 | owner | `docs/decisions/` entry | S + owner time |
| P-15 | Relative `file:` package reference for the OB runtime package in the game; note in `local-only.md` | B9 | G-7 | Fresh clone resolves packages | S |

## Tier 3: Advanced (only if play evidence asks)

| ID | Deliverable | Why | Size |
|---|---|---|---|
| P-11 | Light characteristics ingest after the owner approves a source (R4); `NavLightSet` flash patterns | N5 | M |
| P-16 | Windowed processing (roadmap 4) | unneeded while clusters ≤ 17 M cells | L |
| P-17 | Multi-zone worlds (roadmap 10) | only one Act VII cluster spans two zones, and the AEQD frame (P-3) avoids it; likely never | XL |
| P-18 | Coarse overview levels with honest source flags | S4 | M |
| P-19 | Harbour polygons / pilotage-grade national data | G7, R1 | XL |
| P-20 | Tidal height and datum conversion | G6, B5 | L |
| P-21 | Unreal exporter (roadmap 8) | not a game requirement | L |

## Risks

1. **R3 may flip.** If the owner prefers the OSM coast, P-4 turns into a game-side coast change (far larger).
2. **Resolution may disappoint.** 48 m cells may make Falmouth's channels read as shoals. G-1 reports cells that ground a 5 m draught; if noisy, revisit R1 before M1.
3. **Editor lock.** The owner's Editor holds the project; G-tasks need a second working copy or a closed Editor.
4. **Game process.** Each G-task needs the owner's plan approval; this plan is input, not approval.
5. **Flat land.** If the owner wants relief (R2), a game-side mesh-height task (M to L) is needed that is not listed, because the game's tile meshes are y = 0.
