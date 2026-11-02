# 0011: Unity runtime LOD: quadtree cut, static skirts, loader abstraction

Status: accepted (0.5.0-rc1).

**Context.** The format has an LOD pyramid but the Unity adapter imported only level 0. Unity `Terrain` stitches only same-size neighbours, so mixed levels crack (measured: 4317 background pixels in the synthetic probe).

**Decision.**
- Selection is pure C# (no UnityEngine) so it is property-tested with `dotnet` outside Unity. Distances are in tile spans (scale independent), with hysteresis and a 2:1 balance.
- Cracks are hidden by static skirts built by the importer, switched per side by the cover logic. Rejected: edge-snapping (measured: does not close the cracks), a custom mesh terrain (loses Unity Terrain tooling), runtime-generated skirts (GC, hitches).
- Loading goes through `IOceanTileLoader`; Addressables is the default implementation. Tests use a prefab loader and a counting/failing wrapper so streaming logic is tested without bundles.
- Level 0 always writes the full grid, even when every sample is nodata (explicit nodata, validator reports `empty-tile`). Levels above 0 still skip all-nodata tiles. Reason: a silently missing level-0 tile looked like a broken world.

**Consequences.** Skirt geometry is extra import output (`Generated/Skirts`). Margin values are unmeasured at real distances. Schema 3 worlds stay unsupported in Unity.
