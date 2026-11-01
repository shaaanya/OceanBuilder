# OceanBuilder world format, schema version 2

This is the contract between OceanBuilder and anything that consumes a built world: engine adapters, GIS, scripts, other languages. A reader needs only this document, JSON, PNG and little-endian float32; it needs neither OceanBuilder nor GDAL. The Go package `pkg/oceanworld` is a reference implementation (about 300 lines) and `ocean validate` is the conformance check.

A world is a directory.

```text
world.manifest.json                     index, grid, encodings, sources, statistics (required)
ATTRIBUTION.txt                         credits the sources require; ship it with any redistribution (required)
coastline.wgs84.geojson                 coastline vectors used for checks/conforming (optional)
seamarks.geojson                        OpenSeaMap features (optional, "seamarks": true)
tiles/<level>/<id>.height.f32           float32 heights
tiles/<level>/<id>.depth.png            depth class per sample
tiles/<level>/<id>.shore.png            shore flag per sample
tiles/<level>/<id>.source.png           source code per sample
```

## Versioning

`schema_version` is an integer. Adding optional manifest fields or new source/depth codes does not change it; a change that makes an old reader misread data (encoding, axis order, id scheme) bumps it. A reader must refuse a `schema_version` it does not know. Version 1 (uint16 `.r16` heights quantised over a world-wide range) is not readable by version 2 tools; rebuild instead.

`build_id` identifies the produced bytes (config, tool version, algorithm versions, source hashes). `input_key` identifies the inputs: two builds with equal `input_key` produce byte-identical files (tested; the GDAL version is part of the key).

## Coordinates and the grid

- Horizontal CRS: WGS 84 / UTM, `crs.epsg` = 326zz (north) or 327zz (south). Units are grid metres, not ground metres: the UTM point scale factor `k` (reported as `stats.utm_scale_min/max`) makes 1 ground metre equal k grid metres; k is 0.99984 to 0.99990 at Falmouth (zone 30N) and 1.0006 to 1.0008 at Moorea (zone 6S). The builder refuses bounding boxes whose scale deviates more than 0.5 % from 1.
- Samples are **vertices**, not pixel areas. Level 0 has cell size `grid.cell_m`. Sample `k` east of a tile's west edge is at `west + k * cell`.
- Tiles hold `S = tile_cells + 1` samples per edge. The last row and column duplicate the first row and column of the neighbouring tile, so adjacent tiles share edge samples exactly (byte for byte) and the surface has no cracks.
- Rows run **north to south**; columns west to east. Sample `(row i, col j)` of a tile with bounds `[minE, minN, maxE, maxN]` is at easting `minE + j * cell`, northing `maxN - i * cell`.
- Tile index at level `L`: `x = floor(E / (tile_span_m * 2^L))`, `y = floor(N / (tile_span_m * 2^L))`. These are absolute, independent of the bounding box, so the same place has the same id in every world built with the same cell size and tile size. Tile id: `L<level>_<x>_<y>`.
- `bounds_utm` of a tile is `[x*span, y*span, (x+1)*span, (y+1)*span]` with `span = tile_span_m * 2^level`.
- Neighbours: the edge neighbour to the east is `(x+1, y)`, north `(x, y+1)` at the same level. The manifest lists `neighbours` (`n`,`s`,`e`,`w`) for tiles that exist, but they follow from the rule.

## LOD levels

`lod.levels[]` lists levels 0..n. Level `L` has cell size `cell_m * 2^L` and tile span `tile_span_m * 2^L`, with the same `S` samples per tile, so a coarse tile covers four tiles of the level below. Parent of `(L, x, y)` is `(L+1, x >> 1, y >> 1)`. The pyramid ends when one tile covers the world (or at `lod_levels` from the config, at most 8); `lod_levels: -1` builds level 0 only.

Method (`lod.method = "vertex-decimation"`): level `L` keeps every `2^L`-th vertex of level 0. A level-`L` sample therefore *is* a level-0 sample at the same ground position: same height, same source code. Nothing is averaged, so no value is invented, and shared edges between tiles and across levels agree exactly. The cost is aliasing: features narrower than `2^L` cells (a channel, a spit) can vanish at coarse levels. Depth class and shore are recomputed at each level from that level's heights. Tiles of upper levels can extend past the world; samples outside it are nodata and a tile with no valid sample is omitted.

## Tile files

### `.height.f32`
`S * S` IEEE-754 binary32 values, little-endian, row-major, rows north to south. Metres, positive up; **0 is the land/water boundary** (no vertical datum reconciliation, see Limits). A sample without a value is exactly `height_encoding.nodata` (-9999). Values are not quantised: any scaling an engine needs (for example Unity's 16-bit terrain) is the importer's job, using `height_encoding.min_m/max_m`.

### `.depth.png`
8-bit greyscale `S x S`, same sample order. `0` land (height >= 0); `1..n` water by depth band (`depth_classes[]` gives each band's `min_depth_m`/`max_depth_m`; default 0-5, 5-20, 20-50, 50-200, >200 m); `255` nodata.

### `.shore.png`
8-bit greyscale `S x S`. `255` on a land sample with a 4-connected water neighbour, else `0`. At tile edges the neighbour lies in the adjacent tile, so shore flags are computed on the whole level raster, not per tile.

### `.source.png`
8-bit greyscale `S x S`: where each value came from.

| code | name | meaning | measured |
|-----:|------|---------|:--------:|
| 0 | nodata | no value | no |
| 1 | survey | water sample inside the primary source's survey mask | **yes** |
| 2 | background | water sample from a coarse or unattributed grid, resampled to the output grid | no |
| 3 | land | land elevation from the primary source's DEM | no |
| 4 | regional | water sample replaced or blended from a regional bathymetry grid | no |
| 5 | conformed | land/water decision made from the coastline; height is a synthetic +-0.5 m | no |
| 6 | refined | land elevation changed by an external edit layer | no |

`source_layers[]` in the manifest repeats this table. Codes are never renumbered; new ones are appended. `measured` means "a measurement at or near output resolution"; `resampling.source_native_res_m` and `resampling.unmeasured_res_m` give the real resolutions. No sample is marked measured because it was interpolated.

## Manifest

| key | content |
|-----|---------|
| `schema_version`, `name`, `build_id`, `input_key` | identity |
| `tool` | `version`, `gdal` |
| `config` | the resolved world config that produced this build |
| `crs` | `epsg`, `name` |
| `vertical` | `unit`, `datum`, `note`: states what is *not* known about the vertical reference |
| `grid` | `cell_m`, `tile_cells`, `samples_per_tile`, `tile_span_m`, `tiles_x`, `tiles_y`, `origin_easting` (west edge of tile column 0), `origin_northing_top` (north edge of tile row 0), `first_tile_x`, `first_tile_y` (absolute index of the southernmost row), `bbox_wgs84` [W,S,E,N] of the level-0 grid |
| `height_encoding` | `type` "float32", `byte_order`, `row_order`, `unit`, `nodata`, `min_m`, `max_m` (range of valid level-0 samples) |
| `depth_classes`, `source_layers` | code tables |
| `lod` | `method`, `levels[]` {`level`,`cell_m`,`tile_span_m`,`first/last_tile_x/y`,`tiles`}, `note` |
| `resampling` | `method`, `source_native_res_m`, `unmeasured_res_m`, `output_cell_m`, `upsampled` (output finer than source: the extra detail is interpolation) |
| `tiles[]` | per tile: `id`, `level`, `x`, `y`, `col`, `row` (0 = north, within the level), `bounds_utm`, `bounds_wgs84` (envelope), `min_m`, `max_m`, `nodata_fraction`, `water_fraction`, `measured_water_fraction`, file paths `height`, `depth_class`, `shore_mask`, `source`, `sha256` {the four file hashes}, `parent`, `neighbours` |
| `stats` | world totals, `cells_by_source`, coastline check, conforming/regional/seamark statistics, `utm_scale_min/max`, `notes` (omitted when empty) |
| `sources[]` | provenance, see below |
| `checkpoints[]` | named vertices (highest, lowest, corners) with expected elevation, so an importer can verify its own orientation and placement |
| `attribution[]`, `files` | credits; optional extra files |

### Provenance and licensing

Every `sources[]` entry has `source`, `url`, `retrieved_at` (UTC), `sha256`, `bytes`, `license`, `attribution`. A manifest missing any of these fails validation. Local files appear as `file://<basename>` (no machine paths are written). `ATTRIBUTION.txt` is generated from the same entries and must travel with the data. Licence terms are those of the sources: GMRT CC BY 4.0 (and "not for navigation"), OpenStreetMap ODbL (a derived coastline or seamarks make a world a derivative database; share-alike is not analysed here), EMODnet as its service states. See `docs/research/licensing.md`.

## Reading it: the whole algorithm

File paths in the manifest are relative to the world directory. This code was run against a Moorea world (zone 6S, levels 0 to 2) and agreed with `ocean sample` to 1e-3 m at all six points.

```python
import json, os, struct
root = "."                                      # the world directory
m = json.load(open(root + "/world.manifest.json")); assert m["schema_version"] == 2
S, cell, span = m["grid"]["samples_per_tile"], m["grid"]["cell_m"], m["grid"]["tile_span_m"]
def height(E, N, level=0):                      # E, N in the world's UTM zone
    sp, c = span * 2**level, cell * 2**level
    x, y = int(E // sp), int(N // sp)           # on the world's north/east boundary, take the tile before (E - 1e-6)
    t = next(t for t in m["tiles"] if (t["level"], t["x"], t["y"]) == (level, x, y))
    raw = open(os.path.join(root, t["height"]), "rb").read()
    u, v = (E - x * sp) / c, ((y + 1) * sp - N) / c   # columns east, rows south
    j, i = min(int(u), S - 2), min(int(v), S - 2)
    h = lambda r, q: struct.unpack_from("<f", raw, (r * S + q) * 4)[0]
    fu, fv = u - j, v - i
    return (h(i, j) * (1 - fu) + h(i, j + 1) * fu) * (1 - fv) + (h(i + 1, j) * (1 - fu) + h(i + 1, j + 1) * fu) * fv
```

Treat the result as invalid if any of the four samples equals `nodata`.

## Guarantees checked by `ocean validate`

Every file hash matches the manifest; each tile has `S x S` samples in all four layers; depth class and source code agree with the height of every sample (nodata, land, water band); shore flags agree inside tiles; per-tile and world statistics match the files; adjacent tiles share identical edge samples at every level; every parent sample equals the child sample at the same position; provenance fields are complete.

## Limits (not hidden)

- No vertical datum reconciliation: GMRT mixes contributed grids with heterogeneous references, and OSM coastlines follow a high-water convention that varies by country. `vertical.note` says so; the validator reports the horizontal coastline-to-zero-contour offset.
- One UTM zone per world. The projection scale deviation is reported and bounded.
- Cells finer than the source are interpolated (`resampling.upsampled`); cells coded `background` are as coarse as the background grid.
- Not for navigation.
