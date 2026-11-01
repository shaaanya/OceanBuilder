# Production workflow: OceanBuilder in the sailing game

Target workflow once [implementation-plan.md](implementation-plan.md) M1 is done. Today (2026-11-01): **[exists]** works in OceanBuilder alone; **[planned: ID]** does not exist yet; **[manual today]** is a hand step. Nothing here has run inside the game.

## Responsibilities

| Layer | Owns | Does not own |
|---|---|---|
| OceanBuilder CLI (Go) | fetching, caching, locking sources; warping to the game's frame; depth/height/class/source grids; seamark extraction; validation; manifest and attribution | rendering, physics, Unity assets, boat data |
| OB Unity package (`com.oceancompiler.world`, runtime only) | `DepthSampler` (leg-frame x,y → depth, double precision, unknown-safe), data container types | streaming policy, Terrain generation (the old importer stays in the demo only) |
| Game editor tools (G-7) | importing OB output into the game's Addressables groups, idempotently | geography |
| The game | draught, grounding rule, echo sounder, Crest depth input, chart layer, seamark prefabs, streaming, save/load, rendering | geodesy, data fetching |

Rule: the game references places by **geo coordinates**, never by OB tile ids. Rebuilds can then change tile layout without breaking saves or scenes.

## The twelve steps

1. **Define a region.** Edit `regions/<leg>.json` (cluster list, margin, frame origin = the leg's pack origin) **[planned: P-8]**. Today: edit `<name>.world.json` with a bbox **[exists]**.
2. **Acquire and manage sources.** `ocean build` fetches GMRT and OSM into `.ocean-cache` with sha256 provenance; `-update-lock` writes `<region>.lock.json`; commit it **[exists]**. Fresh clone: run `-update-lock` once or `-lock off`.
3. **Configure processing and resolution.** `cell_m` (48 default), `tile_cells`, `coast_conform` (to the game's coast **[planned: P-4]**), `regional_bathymetry` (only where guards pass **[exists]**), `seamarks`. One config per cluster.
4. **Build.** `ocean region build regions/act1.json` **[planned: P-8]**; per cluster `ocean build` **[exists]**. Output includes the per-cell source layer **[planned: P-1]**.
5. **Generate terrain, underwater data, coastline, features.** Height/class/shore/source grids in the game frame **[planned: P-3]**; coastline and `seamarks.geojson` **[exists]**, in-frame coordinates **[planned: P-6]**. No geometry is generated; the game keeps its own coast meshes.
6. **Tiles and LOD.** Level 0 only; the game samples these tiles, it does not render them. LOD is not needed for M1.
7. **Import into Unity.** `Unity -batchmode -executeMethod ...ImportOceanBuilderRegion <dir>` creates `OceanRegionData` and bakes the Crest depth cache **[planned: G-7, G-4]**. Today the OB importer builds Terrains for the demo project only **[exists; wrong shape for the game]**.
8. **Addressables.** The import registers the asset under `world/<legId>/ocean/<cluster>` in the existing `World_<legId>` group; the normal Addressables build ships it **[planned: G-7]**. The game's own tile import needs a menu click per pack **[manual today]**.
9. **Load the world.** Region data is small (tens of MB per cluster); `WorldScroller.SetRaceStart` also loads the active leg's clusters within the load radius **[planned: G-7]**. No OB streamer.
10. **Gameplay access.** `DepthSampler.TryDepthGrid(x, y)`, fed through the game's world→grid inverse of the tile transform, from `BoatGrounding` (draught), the HUD (echo sounder), the chart layer, bots and tests **[planned: P-5, G-1, G-2, G-3]**. Unknown means "do not ground, show no reading".
11. **Update a region.** Change the region file or sources → `ocean region build` → compare `build_id`s → import. Unchanged clusters reproduce byte-identically (per world **[exists]**, per region **[planned]**), so the Git diff and Addressables delta are exactly the changed clusters. Saves hold geo, so they survive.
12. **Debug, inspect, validate.** `ocean validate` and `ocean preview` per cluster **[exists]**; per-cell source/class overlay in the game's dev tooling **[planned: G-5]**; EditMode tests for frame and sampler **[planned: G-6]**; `docs/integration/evidence/*.py` re-run to check coastline and projection agreement.

## Manual operations today and what to do about them

| Manual operation | Where | Decision |
|---|---|---|
| Choose bbox, write `world.json` | OB | Automate from the game's pack manifests (`ocean region plan`, P-8) |
| Run `ocean build` per region | OB | CLI is right; scripted by `region build` |
| `ocean export -target unity -dest <project>` installs the package and imports Terrains | OB → Unity | Replace with data-only export (P-3) for the game; keep for the demo |
| Install the UPM package into the game | Unity | One-time, relative path (P-15) |
| Click `Ocean > Import World` | Unity | Batch method (G-7) |
| Move/rename generated assets | Unity | Not needed; import writes final locations |
| Create/choose Addressables group, register tiles | Unity | Automate in import (G-7) |
| Click `ALONE/World/Import World Pack` per pack for the game's own tiles | Unity | Out of scope; the same automation could be reused later |
| Build Addressables | Unity | Existing `AddressablesBuildUtility`; leave |
| Assign pack/region to `LegData` | Unity | Leave manual (authoring) or fold into import later |
| Decide draught, lights source, coast source | Owner | Intentionally manual (D-1, R4) |
| Licence review | Owner | Intentionally manual (T8) |

## Failure behaviour the workflow must have

- Build refuses on source-lock mismatch, a failed regional guard, or a failed conform guard, and says why **[exists]**.
- Import refuses a region whose `frame` differs from the leg's origin, instead of placing it wrongly **[planned: G-7]**.
- Sampler returns `false` outside coverage; callers never treat that as depth 0 **[planned: P-5]**.
- Every depth carries its source class; the HUD may mark GEBCO-fill cells as estimated **[planned: P-1, G-2]**.
