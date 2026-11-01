# 0007: Multi-dataset composition with per-sample dataset identity

Status: accepted, 2026-11-01.

## Decision

- `sources[]` in the world config lists elevation datasets (gmrt, geotiff, emodnet) with quality, year, accuracy, licence and attribution. The legacy `local` and `regional_bathymetry` keys map onto the same list, so 0.2.0 configs build byte-identical tiles (golden hashes in `testdata/golden`).
- Selection is per cell, first valid in precedence order. No blending across datasets: blended cells would need a new source code and an error model nobody measured. Precedence is the listed order or `auto` (ranking documented in the format spec); it is recorded in the manifest.
- Each dataset is warped once onto the world grid (stage cache), then the composite is streamed to layer files and tiled in windows with a one-sample halo. Window size changes memory, never output (tested for 0, 1, 2 and 3 tiles per window).
- A new optional layer `.dataset.png` records the supplier of every sample (format revision 1, additive within schema 2). The manifest has `datasets[]`, `cells_by_dataset` and composition statistics (overlap median/rms, seam steps), computed from the data, not estimated.
- Fixed a 0.2.0 bug found while building this: conformed and refined cells kept pre-renumbering source codes. They now get 5 and 6.

## Consequences

Missing data stays visible (nodata is not filled across datasets unless a lower-ranked dataset actually has a value there). Overlap statistics make datum or registration disagreements visible; the validator warns above 1 m median offset. The quality ranking for `auto` is a heuristic; use `listed` when the order matters.
