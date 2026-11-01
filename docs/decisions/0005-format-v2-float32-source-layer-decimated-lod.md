# 0005: Format v2: float32 heights, per-sample source layer, LOD by vertex decimation

Date: 2026-11-01. Status: accepted. Supersedes the height encoding of 0002.

## Context

Version 1 stored heights as uint16 scaled over the world's global min/max. Consequences: one changed extreme sample rewrote every tile (no tile-level change detection), precision depended on the highest peak in the world, the format carried Unity's 16-bit terrain limit into the core, and a consumer could not tell measured depth from resampled background grid or from coast-conformed cells (only per-tile counts existed). There were no LOD levels.

## Decision

1. **Heights are float32 metres per tile**, nodata -9999, no world-wide quantisation. 4 bytes per sample is cheap next to the PNG layers; engines quantise in their importer (the Unity importer does).
2. **A source layer (`.source.png`) per tile** codes where every sample came from (survey, background grid, land DEM, regional grid, coast-conformed, edit layer, nodata). Statistics and the validator read it; codes are append-only.
3. **Each tile file has its own SHA-256 in the manifest.** Two builds can be compared from manifests alone (`ocean diff`) and a consumer can verify a download.
4. **LOD levels are exact vertex decimations of level 0** (every 2^k-th vertex), tile ids `L<k>_<x>_<y>` with `x_k = x_0 >> k`, same samples per tile at every level. Alternatives considered: area averaging (smoother, but invents values that were never measured, breaks equality at shared vertices, and would need a new 'interpolated' source category per level) and min-depth filtering (conservative for navigation, wrong for terrain). Decimation keeps the invariants exactly checkable: every parent sample equals the child sample at the same ground position. Cost: features narrower than 2^k cells can alias away at coarse levels; documented in the spec and `lod.note`. Class and shore masks are recomputed per level.
5. **Atomic output and an input key.** A build is assembled in a sibling directory and swapped in; `input_key` (config + tool/GDAL/algorithm versions + source hashes) lets an unchanged build return immediately; warped rasters are cached by content.

## Consequences

Version 1 worlds are not readable (rebuild). Heights take twice the space they did. Smoothing-based LOD can be added as another `lod.method` without a schema bump if a consumer needs it.
