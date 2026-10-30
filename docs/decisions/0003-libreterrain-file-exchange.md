# 0003: LibreTerrain by file exchange, as an edit layer

Status: accepted, 2026-10-30. Evidence: `docs/research/libreterrain.md`.

## Decision

- LibreTerrain stays an optional external tool. OceanBuilder never links to it, copies its code or bundles it. The only coupling is `internal/libreterrain/mcp.go`, which speaks stdio JSON-RPC to `libreterrain-mcp` **if the binary is found**; without it, export still writes the heightmap and instructions.
- Exchange format: `heightmap.exr` (float32 metres, row 0 = north, vertex registered, padded to a square) + `exchange.json` (OceanBuilder-owned: CRS, origin, cell, padding, LibreTerrain world size and height range, checkpoints, source build id, hash of the EXR, the source world config). PNG16 is written for convenience but import prefers EXR32 (unclipped, exact).
- Import returns an **edit layer**, not a heightmap: `delta = LibreTerrain output − exported baseline` (float32, hash-checked), written to a new folder with `refinement.json` and a world config. `ocean build` applies it on the whole world grid (before masks and tiling) to cells that are **land in the original build**, ramping from 0 at the shoreline to full strength over `blend_cells` (default 4), never below 0 m.
- Water, nodata, the survey mask and the OSM coastline always come from the original sources. Edits on water are counted and ignored; land pushed below sea level is clamped and counted; both appear in the manifest (`refinement`). The edit layer is a provenance source with its own SHA-256.
- Import refuses (never guesses): resampled output, changed world size, cell size, height range, unknown orientation statement, ambiguous or missing heightfields, a flipped/mirrored/transposed raster, a changed baseline, or an output folder inside any input.

## Why a delta

Quantisation and float32 noise would otherwise perturb every land cell on an unedited round trip. With a delta, an unedited round trip is exactly zero, so the rebuilt tiles are byte-identical to the original (tested). It also makes "what did the artist change" auditable.

## Consequences

- Edited elevations use the world's own (unspecified) vertical reference, same as the source DEM. Nothing here adds datum information.
- Edits are only honoured on existing land. To reshape the coast, change the sources, not LibreTerrain.
- The refined config embeds the original `local` paths if the base world used them; those are relative to the original working directory.
