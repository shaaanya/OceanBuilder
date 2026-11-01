# Requirements matrix: OceanBuilder vs the sailing game (ALONE)

Date 2026-11-01. Detail and evidence: [sailing-game-audit.md](sailing-game-audit.md). Tasks: [implementation-plan.md](implementation-plan.md) (IDs `P-n` OB side, `G-n` game side).

**Status.** `V-game` verified in the sailing project · `V-demo` implemented, verified only in the OB demo · `Insuff` implemented but insufficient · `Partial` · `Missing` · `Blocked` (data, licence, tooling, owner decision) · `N/R` not required.
**Evidence.** R read · M measured (script in `evidence/`) · T test run · A assumed.
**Priority.** `M0` blocks the first milestone (Falmouth slice) · `M1` minimum usable region set · `P` production · `X` advanced.
**No row is `V-game`.** The game has never loaded OB output.

## Geographic world

| ID | Game needs | OB provides | Status | Gap | Evidence | Build / change | Validate | Pri | Deps |
|---|---|---|---|---|---|---|---|---|---|
| G1 | Geo ↔ world in the game's frame (per-leg AEQD plane, rigid block anchored at the boat) | UTM per world, SW-corner Unity coords | Insuff | Frames differ; true-geo data is 67 to 673 m off the rendered tiles at 1 to 10 km from the Mindelo anchor | M `leg_sizes_and_placement_error.out`; R `WorldScroller.cs` | OB export in the game's frame (local AEQD option) + frame in manifest | A manifest checkpoint lands within 1 m of the rendered tile vertex in Unity | M0 | P-3 |
| G2 | Nine Acts, 2.3k to 10.7k km each | One world = one zone, whole grid in memory | Insuff | Corridor-wide infeasible (81 to 309 M cells); per land cluster feasible (2 to 7 per Act, 110 to 200 km, one zone except one Act VII cluster) | M `pack_relief_and_clusters.out` | One OB world per cluster, listed from the game's pack manifests | Every Act I land tile falls in one cluster world with ≥ 20 km sea margin | M1 | P-8 |
| G3 | One coastline | OSM line; DEM zero contour; opt-in `coast_conform` | Insuff | Game uses GSHHG. GSHHG vs OSM median 111 m, p90 259 m at Falmouth | M `coast_alignment_and_projection.out` | Decide R3; if GSHHG stays, conform OB land/water to it | Game coast vs OB 0 m contour ≤ 48 m (1 cell) at p90 | M0 | R3, P-4 |
| G4 | Land elevation | GMRT/GEBCO 39 to 758 m; EMODnet opt-in | V-demo | Game land is flat (y = 0 in all nine packs, M); value unproven, may be deliberate | M | Optional height export (R2) | 20 named summits within 10% or marked unreliable | P | R2 |
| G5 | Bathymetry at useful resolution | 39 m surveyed, 758 m GEBCO fill, per-tile fractions only | Partial | No per-cell source layer | R README, roadmap A | Per-cell source layer + export flag | Layer counts equal manifest counts; validator cross-check | M0 | P-1 |
| G6 | Vertical datum / depth convention | Unspecified datum, no conversion | Insuff | Depth is "below mean sea level ± tide"; game has no tide height | R manifest `vertical`, `hazards.md` | State convention in manifest and in-game text; datum conversion later | Statement in manifest and in-game help; no chart-depth claim | P | none |
| G7 | Islands, harbours, bays, channels | none beyond terrain/coast | Missing | Game harbours are procedural `HarbourKit` | R | Keep `HarbourKit` for M0 | n/a | X | none |
| G8 | Accuracy / source statement | `ATTRIBUTION.txt`, validator warnings | V-demo | Not shown in the game | R | In-game sources/limits text from `ATTRIBUTION.txt` | Screen present | P | P-12 |

## Sailing and navigation

| ID | Game needs | OB provides | Status | Gap | Evidence | Build / change | Validate | Pri | Deps |
|---|---|---|---|---|---|---|---|---|---|
| N1 | Depth under keel at the boat, every physics step | uint16 height tiles, no runtime query | Missing | No grid sampler (`TryDepthGrid`) and no world→grid transform matching the rendered tiles | R OB `Runtime/` | Runtime grid sampler (frame x,y → bilinear), unknown-safe; game-side world→grid via the inverse tile transform (B1) | Equals Go-side sample at 1,000 random points within 1 cm; ≥ 1 M queries/s on one core (need ≈ 50/s, so ≥ 20,000× headroom) | M0 | P-3, P-5 |
| N2 | Grounding on shoals (keel draught) | none | Missing | Game grounds on land colliders only; no draught parameter | R `BoatGrounding.cs`, `boat-physics.md:288` | Game: draught param + depth test in `BoatGrounding` (G-1) | Scripted: boat onto a measured shoal shallower than draught grounds within 1 boat length of the contour; deeper passage does not | M0 | N1 |
| N3 | Water mask / coastline collision | shore mask + depth class PNG | Insuff | Game collision = GSHHG mesh colliders; OB mask must agree with them | M | OB mask for routing aids only; keep game colliders for physics | Mask vs collider disagreement ≤ 1 cell at p90 | P | G3 |
| N4 | Seamarks (buoys, beacons) | OSM points GeoJSON; importer markers; 60 at Falmouth | V-demo | Not in the game; no prefabs; ODbL; frame (G1) | R README | Export in game frame + marker data; game prefabs | 60 Falmouth marks within 1 m of GeoJSON UTM position after frame transform; none on land after conform | M0 | P-3, P-6 |
| N5 | Lights and characteristics | type only | Blocked | Owner rule: real List of Lights only | R `environment.md:106` | Owner chooses source (R4) | n/a | X | R4 |
| N6 | Harbours/ports as navigation features | `harbour` seamark points | Partial | Points only | R `seamarks.go` | Use as key-area anchors | Each Act port within 500 m of the game's key area | P | P-6 |
| N7 | Chart layer: depth, marks | none | Missing | Chart has route/wind/AIS only | R `NavChartRenderer` | Game chart layer from depth classes + marks (G-3) | 20 m contour matches the OB class boundary | P | N1 |
| N8 | Echo-sounder readout | none | Missing | No UI | R | Game HUD from N1 (G-2) | Readout equals N1 at boat position | P | N1 |
| N9 | Tide height | none | N/R | Game has stream circles only | R | n/a | n/a | X | none |

## Streaming and runtime

| ID | Game needs | OB provides | Status | Gap | Evidence | Build / change | Validate | Pri | Deps |
|---|---|---|---|---|---|---|---|---|---|
| S1 | Data available with the game's tiles (radius 3 × 10 km) | `OceanTileStreamer` (own grid, 8 km) | Insuff | Different grid, no floating origin | R | Do not reuse the streamer; one small always-loaded cluster asset, or load by the game's key scheme | Load adds ≤ 1 ms main-thread per frame; present before the boat is within 3 km (the game's look-ahead is 3.5 km) | M0 | P-5 |
| S2 | Floating origin | none | Insuff | Game rebases every 4096 m and slides under compression | R `FloatingOrigin.cs` | Lookup goes world → grid through the inverse of `TileWorldPos` (re-reads the rebased anchor); never through true geo | Result unchanged across 100 random rebases | M0 | P-5 |
| S3 | Tile boundary continuity | shared edge samples, validator 0 mismatches | V-demo | Sampler across joins untested in the game | T, R | Stitch by shared edge | Continuous across joins to < 1 mm | M0 | P-5 |
| S4 | LOD | level 0 only | N/R for M0 | Needed only if depth is rendered | R | none | n/a | X | none |
| S5 | Memory/latency budget | not measured | Missing | Derived budget: cluster ≤ 25 M cells (200 km + margins) × 2 B = 50 MB heights + 25 MB class; game's own G7 shows +270 MB unexplained growth | R, M | Measure; proposed cap: OB data ≤ 5% of the 2.4 GB Editor footprint (≈ 120 MB, derived from G7) | Memory capture at three sites | P | P-9 |
| S6 | Save/load | n/a | N/R | Depth derived from geo; saves hold geo | R | Rule: no OB tile id in saves | Load a save, depth query works | M0 | none |
| S7 | Player build behaviour | untested | Blocked | Owner ruling: no Player builds | R | Test when lifted | Addressables load in a Player | P | owner |

## Visual representation

| ID | Game needs | OB provides | Status | Gap | Evidence | Build / change | Validate | Pri | Deps |
|---|---|---|---|---|---|---|---|---|---|
| V1 | Seabed visible through water | Terrain prefabs, flat colours | N/R | Crest is the water; do not render OB terrain | R | none | n/a | N/R | none |
| V2 | Crest shoaling / shallow colour | none | Missing | `_createSeaFloorDepthData: 0`; needs an ocean depth cache fed from OB data | R `World.prefab:131`, Crest `OceanDepthCache.cs` | Game: bake cluster depth to a depth cache (G-4) | Cache value equals N1 at 20 points; visual A/B at Falmouth | M0 | N1 |
| V3 | Coast, beaches, shore transition | shore mask | N/R | Game has its own 10 m coast band | R | none | n/a | N/R | none |
| V4 | Island/harbour geometry | none | Missing | | R | game | n/a | X | none |
| V5 | Seamark objects | empty markers | Partial | No prefabs | R | game prefabs | n/a | P | N4 |
| V6 | Debug visualisation | `preview.png/html` | Partial | No in-game overlay | R | Depth overlay in game dev tooling (G-5) | Readable at 3 zoom levels | P | N1 |

## Pipeline, tooling, process

| ID | Game needs | OB provides | Status | Gap | Evidence | Build / change | Validate | Pri | Deps |
|---|---|---|---|---|---|---|---|---|---|
| T1 | One-command rebuild of a region | `ocean build` (config + lock) | V-demo | No link to game leg ids / cluster list | R | Build driven from a game-side region file | Delete outputs, one command, identical `build_id` | M0 | P-8 |
| T2 | Reproducibility | deterministic, source lock, byte-identical (T) | V-demo | Committed locks pin the author's cache | T, R | Document `-update-lock`; commit locks for game regions | Two clean-clone builds equal | P | none |
| T3 | Import with no hand steps | `ocean export -target unity` | Insuff | Installs a Terrain importer the game does not want | R | "Data only" export into the game's data folder | Zero manual Editor clicks | M0 | P-3 |
| T4 | Addressables | group `Ocean_<world>`, own addresses | Insuff | Game uses `World_<legId>` and `world/{leg}/...` | R | Register into a caller-named group and key prefix | Headless Addressables build; load by key in Play | M1 | T3 |
| T5 | Update a region without breaking references | absolute tile ids; build_id | V-demo | Game must not reference tile ids | R | Rule: reference by geo only | Rebuild with bbox +10%: sampler results unchanged inside the old bbox | P | none |
| T6 | Rebuild one tile/region | whole world only | N/R for now | Cluster worlds make this moot | R | none | n/a | X | none |
| T7 | Tests | Go suite 46 s; demo PlayMode tests | V-demo | No game-side tests | T | EditMode tests for sampler + frame (G-6) | `go test` + Unity EditMode | M0 | P-5 |
| T8 | Licence review | `ATTRIBUTION.txt` | Blocked | ODbL, EMODnet, GSHHG texts not analysed | R, A | Written decision in `docs/decisions/` | Decision recorded | P | owner |

## Summary

Counts by primary status (38 rows): `V-game` 0 · `V-demo` 8 · `Insuff` 9 · `Partial` 4 · `Missing` 8 · `Blocked` 3 · `N/R` 6. Reading: OB's data pipeline is sound and reproducible, but the integration layer (frame, sampler, depth consumers) does not exist. The game side has more missing pieces (N2, N7, N8, V2) than OB has.
