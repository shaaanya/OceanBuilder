# LibreTerrain: verified behaviour

Checked 2026-10-30 by running `../LibreTerrain/libreterrain-mcp --project <file>` (the headless MCP bridge of LibreTerrain 2026.2.0, dual MIT/Apache-2.0) and reading its output files. This replaces the string-based guesses in `ecosystem.md` §1. The GUI app was not run; everything below is the bridge's CPU build path, which writes the same exports.

## Facts, each measured

| Question | Result | How measured |
|---|---|---|
| Project file | Native `.lterrain` = `LTERRAIN` + u32 version + raw-deflate **MessagePack**. The bridge also accepts a JSON project (`format_version`, `app_version`, `world{size_m:[x,y],height_range_m:[min,max],seed}`, `nodes`, `links`, `next_node_id`, `frames`, `custom_nodes`, `exports`, `build{resolution,folder}`, `ui`). | inflated a project saved by `save_project`; loaded a hand-written JSON one |
| Driving it | MCP over stdio (newline-delimited JSON-RPC). Tools: `edit_graph`, `mark_export`, `start_build`, `get_build_status`, `save_project`, `render_preview`... Every edit must quote the graph `revision`. | `internal/libreterrain/mcp.go` |
| PNG16 import, same size | **Bit-identical** (129² pattern encoding `x + 129·y`, 0 of 16641 pixels differ), no flip, no transpose. | numpy/PIL diff |
| Raster registration | **Vertex-registered**: `cell_size_m = world_size / (resolution − 1)`; 129 px over 6144 m ⇒ 48 m. Same convention as OceanBuilder's grid. | `build.json` |
| Resolution change | Bilinear, corners preserved: at 257 px every even sample equals the 129 px input, odd samples are the mean of neighbours; PNG16 output truncates (≤1 LSB). Exact only when sizes match. | diff of 257² vs 129² |
| Non-square worlds | `build.resolution` is one integer. A 3072×1536 m world exported at 65 gives a 65×65 raster with `cell_size_m = [48, 24]`: **the short axis is stretched**. OceanBuilder therefore pads to a square (repeating the edge row/column) and crops on import. | `build.json` |
| EXR32 | Float32 **metres, unclipped** even outside `height_range_m` (input −150…506 m with range −100…300 m came back exact). Even rows exact in the stretched test. | gdalinfo + numpy |
| PNG16 export | `0..65535 = height_range_m min..max`, **clipped** outside the range (`build.json` `data_min/max` still report the true span, so clipping is detectable). Step = range/65535 (≈6 mm for 400 m). | numpy |
| `values` modes of `primitive.file` | `metres` (EXR; used here), `normalised` (0..1 → world range or `low_m..high_m`), `auto`. | node resource |
| Orientation | `build.json`: "row 0 = world Y 0, column 0 = world X 0". In `render_preview` (top view) row 0 is at the **top**. So an image that is north-up in row order shows north-up in LibreTerrain; its +Y/`rotation_deg 90` direction points south. | preview brightness of a row-ramp |
| Metadata that survives | `world_size_m`, `height_range_m`, `resolution`, `cell_size_m`, `seed`, per-file `data_min/max`. **No CRS, no geographic bounds, no vertical datum.** Those live in OceanBuilder's `exchange.json`. | `build.json` |
| Tiling | Export supports tile files `{name}_x{x}_y{y}`; not used (single full-size export keeps exact alignment). | strings, not run |
| Unity helper | None (`import-helpers/` has Unreal, Godot, Blender per the bundle prompts). | strings |
| Unattended build gotcha | A `start_build` asking for more than the project's `build.resolution` returns `started` but never runs (no error). `ocean` writes `build.resolution = raster side` and treats "idle with no files" as failure. | observed |

## Numerical results (real data)

- **Falmouth, 513×513, 48 m, EXR32 unedited**: max |error| 0 m, mean 0 m over 263,169 samples; all six manifest checkpoints identical (highest 255.341 m, lowest −71.710 m, four corners); rebuilding the refined config gives **16 of 16 tiles byte-identical** to the original (height, depth class and shore mask files).
- **Fixture world, non-square (33×65 padded to 65²)**: unedited round trip exact; edited with LibreTerrain's `adjust.blur` (400 m): all 2,145 samples changed, max 95.4 m (cliffs at the coast). After applying to land only: 2,062 land cells changed (max 24.2 m, mean 5.2 m), 83 edits on water ignored, shoreline cells unchanged. **Padding matters:** the first version padded with the world minimum (a ~70 m cliff at the edge); the same blur then moved land by up to 55.2 m (mean 6.3 m) because the blur pulled real cells toward the pad. Repeating the edge row/column fixed this (24.2 m / 5.2 m), so padding repeats the edge.
- **Flip/transpose detection**: east-west flip, north-south flip and transpose of the exchange raster are all refused with the transform named (tests).

## Not verified

- Opening the exported `.lterrain` in the GUI app (the bridge loads and saves it; the GUI was not run).
- Behaviour of LibreTerrain tile exports and masks round trip (masks are not used).
- Whether other LibreTerrain versions keep the same `build.json` fields (import checks `orientation` text and refuses anything else).
