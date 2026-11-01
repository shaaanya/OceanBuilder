# Sailing game integration audit (2026-11-01)

Audit of OceanBuilder (OB) against the actual sailing game. Evidence classes: **R** = read in code/docs, **M** = measured by a script kept in `evidence/`, **T** = test/build run in this audit, **A** = assumed (not verified). The game repo was treated as read-only.

Companions: [requirements-matrix.md](requirements-matrix.md) · [production-workflow.md](production-workflow.md) · [implementation-plan.md](implementation-plan.md).

## 0. Headline

1. **The sailing game does not use OceanBuilder.** ALONE has its own parallel world pipeline (`tools/world-pipeline/build_world_pack.py` → `WorldTileImporter` → `WorldTileAsset` → `WorldScroller`). OB's Unity package has never been installed in `AloneV2` (R: `Packages/` holds only `com.alone.crest` and `manifest.json`). Every OB capability is therefore *implemented only in the OB demo*; none is verified in the game.
2. **The real gap OB can fill is bathymetry.** ALONE's world is coastline-only. Every sampled terrain vertex in all nine Act packs has y = 0 (M, `evidence/pack_relief_and_clusters.out`), no pack used a DEM or bathymetry (`fallbackDemPaths` etc. empty, `hasShoreDepthTextures: false`, no `sources`), the Crest sea-floor depth input is off (`World.prefab:131 _createSeaFloorDepthData: 0`), and `BoatGrounding` tests only land colliders. `docs/decisions/boat-physics.md:288` lists "Keel grounding in shallows" as not done; `design/hazards.md` says depth is not modelled.
3. **OB cannot be dropped in as is.** Four structural mismatches (section 5): projection/zone model, placement frame, engine representation (Terrain prefabs vs mesh assets), and whole-grid-in-memory. Each has a cheaper solution than a rewrite.
4. **The integration milestone is one coastal cluster (Falmouth), not the world.** Land clusters per Act are small (110 to 200 km), fit one UTM zone (one exception) and fit OB's in-memory model (M).

## 1. The game as it exists

Project: `~/Documents/projects/Unity/Alone/AloneV2` (git root `Alone/`, branch `audit/2026-11-01`, with uncommitted edits by the owner; the Editor, pid 1753, holds the project lock). Other Unity projects on the machine (`CrestUrp`, `CrestBirp`, `Seams`, `dotdungeon`, `fuel_chaos`) are not sailing games with a world; `CrestUrp` is the Crest fork's test project.

| Item | Value | Evidence |
|---|---|---|
| Game | ALONE: first-person solo offshore sailing, nine Acts Falmouth → Falmouth, boat Tern/VO65 | R `CLAUDE.md` |
| Unity / pipeline | 6000.4.0f1, URP 17.4 (Render Graph), Addressables 2.9.1, New Input System | R `ProjectVersion.txt`, `manifest.json` |
| Ocean | Crest 4.22.4 own URP fork, embedded `Packages/com.alone.crest` | R |
| World scale | 1:1 (D-022). `WorldScale` exists, legs resolve to `Real`. Travel compression is geo-only: `GeoCoord` (double) advances, `FloatingOrigin.Slide()` scrolls the world (ADR 0002). | R `World/WorldScale.cs`, `FloatingOrigin.cs`, CLAUDE.md invariants |
| Geo model | `GeoCoord` double lat/lon is the truth. `FloatingOrigin.GeoToWorldPos` uses flat-earth ENU (`GeoUtility.GeoToWorld`, 111320 m/deg). Rebase at 4096 m (power of 2 for Crest). | R |
| Tile model | 10 km mesh tiles per **leg**, in an azimuthal-equidistant (AEQD) plane centred on the pack origin (Act I: 50.14 N, 5.04 W). Key `world/{legId}/tile_{x}_{z}`, one Addressables group per leg. `WorldPackData.Tiles` lists only tiles that have land; open ocean has no tile. | R `WorldScroller.cs`, `WorldPackData.cs`, `Packs/act1.../manifest.json` |
| Placement | Loaded tiles form one rigid block anchored at the boat, rotated by az2−az1 of AEQD, re-anchored after 15 km or a teleport (D-289). | R `WorldScroller.cs:45,224` |
| Streaming | `_loadRadius` 3 (7×7 = up to 49 tiles of 10 km), coroutine load by Addressables key, unload when outside. LOD0/LOD1 mesh swap at 12 km. Colliders = mesh colliders, subdivided so no edge > 450 m. | R `WorldScroller.cs:29`, `WorldTile.cs:9,172` |
| Data per tile | LOD0/LOD1 land mesh, coast ribbon, coast skirt, coast-band mesh (10 m samples, 500 m band), 256 px mask/normal textures. Mask RGBA = coast distance, slope, height, land mask. | R `WorldTileAsset.cs`, `manifest.json` |
| Coastline source | GSHHG full-resolution L1 shapefile, corridor 40 km around a route JSON. | R `local-only.md §2.1` |
| Land relief | **None for Act I** (built "GSHHG only, no DEM", `local-only.md`). All sampled terrain y = 0, mask height channel = 0 (M). | M |
| Bathymetry | **None.** A `ShoreDepthMap` slot exists on `WorldTileAsset` and the shader, never populated. | R, M |
| Grounding/collision | `BoatGrounding`: hull box vs tile/harbour colliders each physics step, cancels speed into shore, push-out capped at 3 m/s, 3500 m sphere-cast look-ahead feeding `CoastAheadM` (the passage director stops time compression within `coastClearM` 1500 m). Land = any collider under `WorldTile`/`HarbourKit`. | R `BoatGrounding.cs` |
| Keel | Keel foil span 4.78 m (hydro profile). No draught value is used by any grounding code. | R `Vo65KeelFoilProfile.asset`; no draught parameter found in physics |
| Seamarks | None placed from data. `NavLightSet` = boat/ship lights. Lighthouses are `blocked: data` (B5): "take them from a real List of Lights... never invent them" (`environment.md:106`). Harbours are procedural `HarbourKit` primitives. | R |
| Chart | `NavChartRenderer` with layers (route, boat, AIS, wind, current). No depth or marks layer. `NavMaps/VOR2017_Land.json` is a legacy PoC land mask. | R |
| Tide | Tide zones (stream circles) only; no tidal height or depth. | R `hazards.md` |
| Save | Atomic write, no version field. World tiles are derived, not saved; saves hold voyage state and `GeoCoord`. | R `docs/audit/project-audit.md` |
| Perf evidence | G7, **Editor** frame times, not Player: Falmouth 24 tiles, 1.56 M tris, 5.5 ms avg; +270 MB over six placements (leak not excluded); one 247 ms hitch on first tile load. No Player build exists (owner ruling: no Player builds). | R `slice1-gaps.md` G7 |
| Maturity | "Engineering-complete prototype of a vertical slice... pre-alpha and unproven." No human has sailed an Act. | R `docs/audit/project-audit.md` |
| World size | 905 land tiles across nine packs; 13 GB generated data on disk (4.7 GB packs, 8.5 GB tiles), git-ignored "local-only", with a rebuild recipe (`rebuild_packs.py`, 5 to 90 min per pack). | R, `du` |

Voyage scale (M, `evidence/leg_sizes_and_placement_error.out`): Acts run 2,300 to 10,700 km; their 80 km-wide corridors would be 81 M to 309 M cells at 48 m. **A corridor-wide OB world is not feasible; a per-land-cluster world is** (B2).

## 2. OceanBuilder as it exists

34 commits. Go CLI, GDAL command-line wrappers (GDAL 3.13 at `/opt/homebrew/bin`).

| Area | State | Evidence |
|---|---|---|
| Commands | `build`, `validate`, `preview`, `export -target unity`, `libreterrain export/import`, `sources`, `world init`, `cache`, `version` | R `cmd/ocean/main.go` |
| Sources | GMRT topo+mask (39 m survey / 758 m GEBCO fill), OSM coastline via Overpass, optional EMODnet DTM fill (opt-in, guarded), optional local GeoTIFF/GeoJSON, optional LibreTerrain edit layer | R README |
| Grid | One UTM zone chosen from the bbox centre; one raster warped once and sliced into (N+1)² uint16 height tiles sharing edges; depth-class PNG (0 land, 1..n bands, 255 nodata), shore mask PNG; fixed 5/20/50/200 m bands | R `docs/architecture/overview.md` |
| Provenance | URL, time, sha256, licence, attribution per source; source lock file; deterministic output | R |
| Validation | `go vet` + `go test ./...` pass (T, ~46 s, mostly LibreTerrain). Falmouth rebuild is byte-identical to the stored output for all `.r16` tiles with the same `build_id` (T). Neighbour edge mismatches 0 (R). | T |
| Unity | UPM package: importer (Terrain per tile, flat-colour layers, prefabs, index, scene), Addressables registration, `OceanTileStreamer`, PlayMode tests on built bundles (editor Play, not Player). URP only. | R README |
| Not implemented | LOD above 0, floating origin, water shader, textures, Player build test, light characteristics, harbour geometry, multi-zone, windowed processing, per-cell source layer, vertical datum conversion | R roadmap |

OB's own limits: GMRT terms forbid navigation use; no vertical datum reconciliation; zero is the waterline.

## 3. Side by side

| Need (from ALONE) | ALONE today | OB today |
|---|---|---|
| Real coastline | GSHHG f, 10 m densified, whole route | OSM (ODbL) at 48 m grid; optional `coast_conform` |
| Land relief | none (flat) | GMRT/GEBCO DEM (39 to 758 m) |
| Bathymetry | none | GMRT 39 m where surveyed, else GEBCO fill; optional EMODnet 115 m |
| Depth under keel at the boat | no | data exists as uint16 height tiles; **no runtime query** |
| Seamarks | none | OSM `seamark:type` points GeoJSON (60 at Falmouth); no light characters |
| Tile format | mesh ScriptableObject, AEQD, 10 km | Terrain prefab, UTM, 6.1 km (128 × 48 m) |
| Streaming | `WorldScroller` (floating-origin aware) | `OceanTileStreamer` (no floating origin) |
| Whole-voyage scale | yes (nine packs) | no (single zone, in memory) |

## 4. Measured: do OB and ALONE agree on the coast?

Falmouth (`evidence/coast_alignment_and_projection.out`, M). ALONE pack coastline vertices projected AEQD → WGS84 → UTM 30N and compared with OB's OSM coastline inside the OB bbox (25,856 vertices; 14,832 remain after dropping vertices on tile-cut edges, with median 132 m to OSM):

- 14,364 of 25,856 vertices lie within 500 m of an OSM coastline sample; for those, median **111 m**, p90 259 m. The rest (about 11,500) include inland tile-border cut vertices, which are not coastline, and coast in creeks and estuaries where OSM and GSHHG may tag different features; the two causes were not separated (A).
- 82.9% of OSM coastline samples have a GSHHG vertex within 300 m; median 140 m, p90 383 m.
- OB's own DEM-to-OSM offset is 101 m median / 268 m p90 (R, manifest).

So **three coastline representations (GSHHG, OSM, GMRT zero contour) differ by 100 to 140 m median and a few hundred metres at p90** at Falmouth. Fine for open-ocean visuals; not fine if depth/grounding says "water" where the visible coast says "land" (boat on rendered land) or the reverse. Reconciling them is a design decision (R3), not a bug.

## 5. Integration findings (blockers)

**B1 Placement frame disagrees by hundreds of metres far from the pack origin (M).** ALONE draws tiles in a rigid block rotated by az2−az1; data placed by true geodesy (`GeoToWorldPos(geo)`: seamarks, key areas, a depth query by boat geo) disagrees with those tiles by 67 / 337 / 673 m at 1 / 5 / 10 km east of the Mindelo anchor (26 / 132 / 264 m north). At the pack origin the error is ~0. D-289 already records up to ~900 m at a re-anchor snap. OB data consumed by ALONE must be exported **in the same plane as the tiles**, or ALONE's placement must change. Recommendation: export in ALONE's frame (OB gains a local azimuthal-equidistant frame option), because ALONE's geometry is already decided and the depth grid then lies in the plane of the rendered tiles. That is necessary but not sufficient: the rendered tiles are also rotated and shifted by the boat-anchored rigid block, so the lookup must go through the same transform as `WorldScroller.TileWorldPos` (inverse: grid = anchorXY + R⁻¹·(worldPos − GeoToWorldPos(anchorGeo)); rebase-safe because `TileWorldPos` re-reads `GeoToWorldPos(anchorGeo)`), not through true geodesy. A geo-only lookup would sit off the rendered coast by the B1 gap (337 m at 5 km).

**B2 Zones and size (M, `evidence/pack_relief_and_clusters.out`).** Corridor-wide: Act III crosses 22 UTM zones, Act V crosses the antimeridian. Per land cluster (tiles within 3 tiles of each other): 2 to 7 clusters per Act, extent 110 to 200 km, one UTM zone each except one Act VII cluster (2 zones). At 48 m a 200 km cluster is ~17 M cells, or ~25 M (~50 MB of heights) with the 20 km margin on each side, and fits OB's in-memory model; Falmouth is 0.26 M cells. This sidesteps multi-zone and windowing for the first milestone. Caveat: clusters come from land tiles; shoal water extends beyond them, so each cluster needs a margin (assume 20 km, A).

**B3 Representation.** OB emits `Terrain` prefabs with SW-corner Unity coordinates and a UTM index. ALONE streams `WorldTileAsset` meshes by AEQD address with `FloatingOrigin` shifts and its own colliders. Using OB's terrain prefabs would duplicate the land and conflict with `BoatGrounding`'s land test. **What ALONE needs from OB is a data product (depth/height grids), not geometry.** Recommendation: OB exports tile grids + an index in ALONE's frame, plus a small C# runtime sampler in the UPM package; ALONE feeds Crest's sea-floor input and grounding from it.

**B4 Runtime queries do not exist.** OB offers `TileAt` and Terrain `SampleHeight`. ALONE needs a sampler over the leg-frame grid (`TryDepthGrid(x, y)`) safe to call every physics step, fed by a game-side world→grid inverse of the tile transform (B1); a geo overload exists only for chart and far-field use and is labelled with its off-anchor error. It must give a defined result outside loaded tiles ("unknown/deep", never a silent 0).

**B5 Datum.** GMRT zero ≈ waterline; no tidal datum. Depth here is not chart depth (chart datum is lowest astronomical tide). Springs range at Falmouth is about 5 m (A, not measured). Until the game models tidal height (hazards.md: not yet), state "depth below mean sea level, ± tidal range".

**B6 Resolution vs hull.** Keel span 4.78 m against 48 m cells and 39 m native GMRT (best). A 48 m cell cannot resolve inshore channels or shoals a keelboat cares about. OB's resolution suits "deep water / shoaling coast" gameplay, not pilotage. That is a scope decision (R1); do not fix it by interpolation (AGENTS.md: never present interpolated data as measured).

**B7 Licences.** GMRT CC BY 4.0, GEBCO public domain, **OSM ODbL** (derivative-database obligations if `coast_conform`/seamarks are baked into shipped tiles; unanalysed), EMODnet licence text unread. GSHHG (ALONE's current coast) is LGPL (A, not rechecked). A shipped game needs an attribution screen and a licence review first.

**B8 Reproducibility.** ALONE packs are local-only (13 GB). OB is deterministic and source-locked, so OB-derived data could be rebuilt from `world.json` + lock, which is better than ALONE's current state. Caveat: the committed locks pin the author's cache; a fresh clone must run `-update-lock`.

**B9 Package coupling.** ALONE's hot-reload entry in `manifest.json` is an absolute `file:` path that already breaks fresh clones (game audit). The OB package must be added by relative path or git URL.

## 6. End-to-end trace (Falmouth, the one region both projects share)

| Stage | Format | Check |
|---|---|---|
| 1 GMRT fetch → topo, mask | GeoTIFF, WGS84 | cached, sha256 in manifest (R) |
| 2 OSM Overpass → `coastline.wgs84.geojson` | GeoJSON WGS84 | 742 KB, 59,354 samples at 5 m (M) |
| 3 gdalwarp → UTM 30N 48 m vertex-registered grid, bilinear | ENVI temp | resampling in manifest (R) |
| 4 derive depth class, shore, survey masks | uint8 | measured water 91.9%, GEBCO fill 8.1% (R) |
| 5 slice into 129² tiles, 4×4 | `.r16`, `.depth.png`, `.shore.png` | rebuilt byte-identical, same build_id (T) |
| 6 validate | manifest stats | edges 0 mismatches; coast offset 101/268 m (R) |
| 7 export unity → importer → Terrains, prefabs, index, Addressables | Unity assets | six checkpoint heights within 3 mm (R, demo project only) |
| 8 runtime load | PlayMode test from built bundles | editor Play only; **no Player, no game** (R) |

Silent-loss audit: heights are uint16 over the world min/max (Falmouth −71.7..255.3 m → 0.005 m step; −5000..+1000 m would be 0.09 m, still fine). Nodata is flagged 255, not filled (R). Resampling method is in the manifest (R). Vertical convention agrees (OB y = metres above sea level; ALONE ocean at y = 0). **Not preserved by current output:** per-cell source (survey vs GEBCO fill vs regional; only per-tile counts, roadmap A), LOD, light characteristics, harbour geometry.

ALONE side of the same trace (R/M): GSHHG shapefile → AEQD tile geojson (tile-local metres) → terrain.json/coastband.json (y = 0, M) → `WorldTileAsset` → Addressables key → `WorldScroller`. No step touches a depth value.

## 7. Decisions recorded with a default

The brief said not to ask; these are the points where the plan assumes a default and states what would change.

- **R1 Gameplay depth fidelity.** Default: offshore sailor with coastal-approach awareness, no pilotage. 39 to 115 m data is then enough; depth drives grounding on shoals, Crest shoaling/colour, an echo-sounder HUD and chart contours. If the owner wants harbour pilotage, OB's sources are insufficient nearly everywhere and no pipeline fixes that.
- **R2 Land relief.** Default: take OB DEM relief where it gives ALONE something it lacks (hills visible from sea). Flat land may be deliberate art direction (A).
- **R3 One coastline.** Default: keep GSHHG as the visual and collision coast and make OB's land/water decision follow it for depth purposes (reuse the `coast_conform` machinery with GSHHG polygons). Alternative: move the game to the OSM coast (ODbL). Needed before depth/coast agreement work.
- **R4 Seamarks and lights.** Owner rule: lights come from a real List of Lights, never invented. OSM `seamark:light:*` is volunteer data of mixed quality. Default: buoys/beacons from OSM for visuals; **lights stay blocked until the owner approves a data source**.

## 8. Findings outside the roadmap

1. ALONE's parallel pipeline (not in OB's roadmap).
2. Placement-frame mismatch (B1, M).
3. Cluster-scale worlds fit OB; corridor-wide do not (B2, M).
4. Runtime sampler API missing (B4).
5. Three-way coastline disagreement quantified (section 4, M).
6. Tide/datum scope (B5).
7. No keel draught value anywhere in ALONE (needs a boat parameter; VO65 draught is public but not in the repo, A).
8. OB's `OceanTileStreamer` lacks floating origin; irrelevant if ALONE keeps its own streamer.
9. Act I tile count: manifest lists 131, `local-only.md`/`backlog.md` say 127 (R). Game-doc drift.
10. No depth inspection tool on either side beyond `preview.png`; ALONE's `PlayerShot`/`PerfLog` dev tooling could host a depth overlay.

## 9. Verified and not verified

Verified (T/M): OB builds and tests pass; Falmouth rebuild identical; every number tagged M above; coastline comparison; projection error; cluster sizes; flat relief across nine packs (up to 25 `terrain.json` files sampled per pack).

**Not verified:** any OB output loaded in `AloneV2`; any Play or Player run (Editor held by the owner's session, project lock, owner ruling against Player builds); `BoatGrounding` against a depth source; Crest's sea-floor input fed by a runtime texture (code read only); an Addressables build of an OB-derived group in the game; the Act VI Nemo alternative pack; whether flat land is deliberate; real tide range at Falmouth; GSHHG and EMODnet licence text; the speed and memory of a 25 M-cell OB build (extrapolated, not run); every statement tagged A.
