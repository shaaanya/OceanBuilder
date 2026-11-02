# Unity runtime LOD

Status: implemented and verified in the Unity Editor (PlayMode) and in a standalone macOS arm64 player (Mono, URP 17, Unity 6000.4.0f1). Not verified: Windows, Linux, IL2CPP, mobile, schema 3 (the importer refuses it). Decision record: [0011](decisions/0011-unity-runtime-lod.md).

The world format carries a pyramid (level L has cells of `cell_m * 2^L`, tiles of `tile_span_m * 2^L`, the same S samples per tile). The Unity adapter turns it into a streamed, camera-driven terrain cut. Water, vessels and gameplay are not part of it.

## Pieces

| Piece | File | Needs Unity? |
|---|---|---|
| Selection: quadtree cut, hysteresis, 2:1 balance, cover-until-loaded, skirt sides | `Runtime/OceanLod.cs` | no (pure C#) |
| Streamer (`OceanLodStreamer`): async load/unload, retry, events | `Runtime/OceanLodStreamer.cs` | yes |
| Loader abstraction `IOceanTileLoader` | `Runtime/OceanTileLoader.cs` | yes |
| Addressables loader (default when Addressables is installed) | `Streaming/OceanAddressablesTileLoader.cs` | yes |
| Skirts (geometry + per-side switch) | `Runtime/OceanSkirtBuilder.cs`, `OceanTileSkirts.cs` | yes |
| Importer (levels, skirts) | `Editor/OceanWorldImporter.cs` | yes |

## Use

1. Build the world with an LOD pyramid (`lod_levels` in the world config) and export with the levels you want: `ocean export -target unity -level N ...`.
2. Import with `-oceanMaxLevel N` (menu: the importer window). Level 0 only is imported by default and then the streamer has nothing to coarsen.
3. Add `OceanLodStreamer` to an empty object at the world origin. Set `index` (the `OceanWorldIndex` asset) and add the viewers (cameras or any transform) to `viewers`.

## Settings (`OceanLodSettings`, field `lod`)

| Field | Default | Meaning |
|---|---|---|
| `splitFactor` | 2 | A tile splits into its four children while the nearest viewer is closer than `splitFactor * tile span`. Scale independent: a tile always splits at the same number of tile spans, whatever the world's size. |
| `splitDistancesM` | empty | Optional explicit distance per level (metres). When set it replaces `splitFactor`. |
| `hysteresis` | 0.25 | An already-split tile merges only beyond `(1 + hysteresis)` times its split distance. Stops flicker on the boundary. |
| `viewDistanceM` | 0 (off) | Tiles farther than this from every viewer are not selected. |
| `maxLevelDelta` | 1 | 2:1 balance: neighbouring drawn tiles differ by at most this many levels. Bounds the seam work. |
| `minLevel`, `maxLevel` | 0, all | Clamp the cut. |

Streamer fields: `maxInFlight` (2), `updateIntervalSeconds` (0.1), `unloadDelaySeconds` (2, real time), `retryAfterSeconds` (5, doubling), `maxRetries` (3), `collidersMaxLevel` (0: only level-0 tiles get colliders), `skirts` (true).

Several viewers: a tile splits if **any** viewer is within its distance. A world with no viewers draws the coarsest cut that covers it.

## Behaviour

- **Missing levels or tiles.** Selection works on the tiles that exist. A tile whose children do not exist is a leaf. A tile that failed to load (`unavailable`) keeps its parent drawn.
- **Cover until loaded.** A tile that is wanted but not yet loaded is replaced on screen by its loaded ancestor, else by its loaded descendants; the new tile is shown only when loaded, and its ancestors/descendants are hidden then. The tests assert no hole opens while the viewer moves (random walks with asynchronous loads).
- **No duplicate loads, no churn.** One load per tile id; a load for a tile that stops being wanted is cancelled (flagged, released on completion); if it is wanted again before it completes the cancel is withdrawn. Unloads wait `unloadDelaySeconds`.
- **Failures.** A failed load raises `TileFailed(entry, message, final)`, is retried with doubling delay up to `maxRetries`, then marked unavailable. `Retry()` clears the state.
- **Addressables lifecycle.** Instances are created with `InstantiateAsync` and released with `ReleaseInstance` (or `Release` of a failed handle); destroying the streamer releases everything. A synchronous failure (unknown address) is reported from the next tick so callers never see a callback inside `Begin`.
- **GC.** `Select` + `Cover.Compute` reuse their buffers: measured 40 bytes per step in steady state (dotnet, 142 leaves of a 341-tile tree, 2 viewers; `tools/lodtests` asserts under 256 bytes). The streamer's own per-step allocations (events, dictionaries on load/unload) and the player's GC were **not** measured.

## Seams (cracks)

Unity `Terrain` stitches neighbours only when they have the same size. A coarse tile beside a fine one therefore cracks. Measured by rendering with a pure-green camera clear colour and counting green pixels:

| Case (synthetic: 2 fine 256 m tiles beside one 512 m tile, 12 low views) | Background pixels |
|---|---|
| all fine (reference) | 0 |
| mixed sizes, no skirts | 4317 |
| mixed sizes, skirts (0.5 m margin) | 0 |

Same numbers in the Editor and in the standalone player. Edge-snapping the fine edge to the coarse samples did **not** remove the cracks (Terrain tessellates its own patches), so it is not used.

**Strategy: static skirts.** For every tile the importer saves four vertical strips (north, east, south, west) hanging from the edge. Top: edge height + `SkirtUpM` (0.5 m, batch arg `-oceanSkirtUp`). Bottom: the edge's own relief plus two cells of that tile's level below it (at least 1 m). The strips are switched on per side only where the drawn neighbour has a different level (`OceanLodCover.SkirtSides`). Material: URP Lit, two-sided, shallow-layer colour.

On real tiles (Falmouth fixture ladder, 5 levels) the same render finds background pixels in 13 views with and without skirts (693203 vs 698318): those are mostly off-world edges of the test world, so that number is **not** a crack measurement; only the synthetic probe is.

## Coastline and shallow water

Decimation keeps coarse samples equal to level-0 samples, so coastlines move at most by coarse-cell aliasing: thin spits and shoals can vanish at coarse levels. `lod_method: "shoal"` biases the other way (keeps shallowest water). See [lod-quality.md](lod-quality.md). Skirts are in the terrain colour and never carry depth-class information; the 0.5 m lift above the edge can show as a thin wall where a coarse tile's edge sits lower than a fine neighbour's coast. Not tuned at real view distances.

## Verification (what was actually run)

- `dotnet run --project tools/lodtests -c Release`: 6635 checks (pure C#, property tests with random walks; mutation-checked for the 2:1 balance rule).
- Editor PlayMode, `OceanLodSmokeTests` (2 tests) and the older runtime tests: pass (Unity 6000.4.0f1, macOS arm64).
- Standalone macOS arm64 player, 5-level world (25 level-0 tiles + 14 coarse), graphics enabled: `OCEAN_PLAYER_RESULT failures=0`. Scenarios: calm (12 of 39 tiles loaded, 3 levels drawn), move (7 unloads), 150 rapid teleports (no duplicate load, max 2 in flight, no leaks), interruption, failure injection with retry, corrupt address, missing levels, release, seam.

## Limits

- Skirt margin 0.5 m comes from a synthetic 1-2 m cell test; at real 1-8 km view distances with 30-500 m cells the margin and depth are unmeasured.
- `SkirtSides` ignores a neighbour whose same-level tile is absent but whose finer tiles exist; that seam is not skirted (rare: needs a partial world with a missing tile).
- Colliders only at level `<= collidersMaxLevel`.
- No floating origin. Large worlds lose float precision far from the origin.
- Windows, Linux, IL2CPP, mobile, HDRP/Built-in (not supported by design) and schema 3 worlds are not covered.
- Frame time and GC were not profiled in a player (only the pure selection code was measured, see Behaviour).
