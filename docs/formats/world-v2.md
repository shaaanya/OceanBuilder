# OceanBuilder world format, schema versions 2 and 3 (format revision 1)

This is the contract between OceanBuilder and anything that consumes a built world: engine adapters, GIS, scripts, other languages. A reader needs only this document, JSON, PNG and little-endian float32; it needs neither OceanBuilder nor GDAL. The Go package `pkg/oceanworld` is a reference implementation (about 300 lines) and `ocean validate` is the conformance check.

A world is a directory.

```text
world.manifest.json                     index, grid, encodings, sources, statistics (required)
ATTRIBUTION.txt                         credits the sources require; ship it with any redistribution (required)
coastline.wgs84.geojson                 coastline vectors used for checks/conforming (optional; ODbL when from OSM; `no_coastline_copy` omits it)
seamarks.geojson                        OpenSeaMap features (optional, "seamarks": true)
tiles/<level>/<id>.height.f32           float32 heights
tiles/<level>/<id>.depth.png            depth class per sample
tiles/<level>/<id>.shore.png            shore flag per sample
tiles/<level>/<id>.source.png           source code per sample
tiles/<level>/<id>.dataset.png          which dataset supplied each sample (format revision 1)
```

## Versioning

`schema_version` is an integer. Adding optional manifest fields or new source/depth codes does not change it; a change that makes an old reader misread data (encoding, axis order, id scheme) bumps it. A reader must refuse a `schema_version` it does not know. Version 1 (uint16 `.r16` heights quantised over a world-wide range) is not readable by version 2 tools; rebuild instead.

`build_id` identifies the produced bytes (config, tool version, algorithm versions, source hashes). `input_key` identifies the inputs: two builds with equal `input_key` produce byte-identical files (tested; the GDAL version is part of the key).

## Coordinates and the grid

**Schema 3 (new in 0.3.0).** Identical to schema 2 except for `crs`. A world that does not fit one UTM zone is built in a local WGS 84 transverse Mercator grid and carries `schema_version` 3 and `crs = {epsg: 0, kind: "tmerc", lon_0, k_0, false_easting, false_northing, proj4}`. Everything below that says "UTM" then means "this grid". UTM worlds stay at schema 2 with `crs.kind` "utm". Northings can be negative (south of the equator with false northing 0), so compute tile indices with `floor`. Longitudes must be wrapped around `lon_0` (a world across 180 has `bounds_wgs84` east above 180). Decision: [ADR 0006](decisions/0006-local-transverse-mercator-grid.md). The Go reader implements the projection; other readers can use `proj4` with PROJ.

- Horizontal CRS: WGS 84 / UTM, `crs.epsg` = 326zz (north) or 327zz (south). Units are grid metres, not ground metres: the UTM point scale factor `k` (reported as `stats.utm_scale_min/max`) makes 1 ground metre equal k grid metres; k is 0.99984 to 0.99990 at Falmouth (zone 30N) and 1.0006 to 1.0008 at Moorea (zone 6S). The builder refuses bounding boxes whose scale deviates more than 0.5 % from 1.
- Samples are **vertices**, not pixel areas. Level 0 has cell size `grid.cell_m`. Sample `k` east of a tile's west edge is at `west + k * cell`.
- Tiles hold `S = tile_cells + 1` samples per edge. The last row and column duplicate the first row and column of the neighbouring tile, so adjacent tiles share edge samples exactly (byte for byte) and the surface has no cracks.
- Rows run **north to south**; columns west to east. Sample `(row i, col j)` of a tile with bounds `[minE, minN, maxE, maxN]` is at easting `minE + j * cell`, northing `maxN - i * cell`.
- Tile index at level `L`: `x = floor(E / (tile_span_m * 2^L))`, `y = floor(N / (tile_span_m * 2^L))`. These are absolute, independent of the bounding box, so the same place has the same id in every world built with the same cell size and tile size. Tile id: `L<level>_<x>_<y>`.
- `bounds_utm` of a tile is `[x*span, y*span, (x+1)*span, (y+1)*span]` with `span = tile_span_m * 2^level`.
- Neighbours: the edge neighbour to the east is `(x+1, y)`, north `(x, y+1)` at the same level. The manifest lists `neighbours` (`n`,`s`,`e`,`w`) for tiles that exist, but they follow from the rule.

## LOD levels

**Method (`lod.method`).** `vertex-decimation` (default): every level keeps every second vertex of the previous one, values are exact level-0 samples. `shoal-biased` (config `lod_method: "shoal"`, 0.3.0): a water sample is replaced by the shallowest water height in its 3x3 neighbourhood of the finer level, so a coarse level is never deeper than the water it stands for; land, nodata and the waterline are unchanged; changed samples carry source code 7. Both build each level as one raster and then tile it, so tiles of one level share edge samples exactly. `ocean lod-report` measures a built world's levels against level 0.

`lod.levels[]` lists levels 0..n. Level `L` has cell size `cell_m * 2^L` and tile span `tile_span_m * 2^L`, with the same `S` samples per tile, so a coarse tile covers four tiles of the level below. Parent of `(L, x, y)` is `(L+1, x >> 1, y >> 1)`. The pyramid ends when one tile covers the world (or at `lod_levels` from the config, at most 8); `lod_levels: -1` builds level 0 only.

Method (`lod.method = "vertex-decimation"`): level `L` keeps every `2^L`-th vertex of level 0. A level-`L` sample therefore *is* a level-0 sample at the same ground position: same height, same source code. Nothing is averaged, so no value is invented, and shared edges between tiles and across levels agree exactly. The cost is aliasing: features narrower than `2^L` cells (a channel, a spit) can vanish at coarse levels. Depth class and shore are recomputed at each level from that level's heights. Tiles of upper levels can extend past the world; samples outside it are nodata and an upper-level tile with no valid sample is omitted. **Level 0 is always complete** (0.5.0-rc1): every tile of the level-0 rectangle is written even when all its samples are nodata (the validator reports `empty-tile`), so a missing level-0 tile can only mean a damaged world. Earlier builds omitted all-nodata level-0 tiles.

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
| 7 | lod-derived | LOD levels above 0 only (`lod_method` "shoal"): height computed from the 3x3 neighbourhood of the finer level, not a level-0 sample | no |

`source_layers[]` in the manifest repeats this table. Codes are never renumbered; new ones are appended. `measured` means "a measurement at or near output resolution"; `resampling.source_native_res_m` and `resampling.unmeasured_res_m` give the real resolutions. No sample is marked measured because it was interpolated.

### `.dataset.png` (format revision 1)
8-bit greyscale `S x S`: `datasets[].index` of the dataset that supplied the height of each sample, `0` for nodata (and for any sample no dataset supplied). Added in 0.3.0 as an optional layer; a tile entry has `dataset` and `sha256.dataset` when it exists, `manifest.format_revision` is 1, and a reader that does not know the layer can ignore it. In an LOD level k the value is that of the level-0 vertex the decimation kept (same rule as `.source.png`). Conformed and refined cells keep the index of the dataset that supplied the underlying sample.

## Manifest

| key | content |
|-----|---------|
| `schema_version`, `name`, `build_id`, `input_key` | identity |
| `tool` | `version`, `gdal` |
| `config` | the resolved world config that produced this build |
| `crs` | `epsg`, `name`, and for schema 3 `kind`, `lon_0`, `k_0`, `false_easting`, `false_northing`, `proj4` (a zero `false_easting` or `false_northing` may be omitted from the JSON: read a missing one as 0) |
| `vertical` | `unit`, `datum` (one datum of the closed vocabulary, `mixed` or `unknown`), `note`, `mixed` (datasets with different or unverified references were combined on explicit request) |
| `datasets[]` | one per source that supplied elevation: `index` (value in `.dataset.png`), `id`, `type`, `role`, `quality`, `year`, `accuracy_m`, `native_res_m`, `declared_datum`, `vertical` {`kind` identity\|proj-grid\|user-offset, `from`, `to`, `pipeline`, `grids[]` with sha256, `proj_version`, `offset_m`, `offset_source`, `verified`}, `precedence_rank` (1 = tried first), `sources` / `source_sha256` (links to `sources[]`) Added in 0.4.0 (additive, optional): `delivery` for `gmrt` datasets, what the service actually delivered: `request` (resolution parameter sent), `reference_cell_m` (finest cell the service offered at the centre of the area), `pieces`, `cell_m_min` / `cell_m_max` (finest and coarsest received, ground metres), `coarsened_pieces` (more than 25 % coarser than the reference), `note`. `resampling.source_native_res_m` is the coarsest piece, never the requested resolution. |
| `grid` | `cell_m`, `tile_cells`, `samples_per_tile`, `tile_span_m`, `tiles_x`, `tiles_y`, `origin_easting` (west edge of tile column 0), `origin_northing_top` (north edge of tile row 0), `first_tile_x`, `first_tile_y` (absolute index of the southernmost row), `bbox_wgs84` [W,S,E,N] of the level-0 grid |
| `height_encoding` | `type` "float32", `byte_order`, `row_order`, `unit`, `nodata`, `min_m`, `max_m` (range of valid level-0 samples) |
| `depth_classes`, `source_layers` | code tables |
| `lod` | `method`, `levels[]` {`level`,`cell_m`,`tile_span_m`,`first/last_tile_x/y`,`tiles`}, `note` |
| `resampling` | `method`, `source_native_res_m`, `unmeasured_res_m`, `output_cell_m`, `upsampled` (output finer than source: the extra detail is interpolation) |
| `tiles[]` | per tile: `id`, `level`, `x`, `y`, `col`, `row` (0 = north, within the level), `bounds_utm`, `bounds_wgs84` (envelope), `min_m`, `max_m`, `nodata_fraction`, `water_fraction`, `measured_water_fraction`, file paths `height`, `depth_class`, `shore_mask`, `source`, `sha256` {the four file hashes}, `parent`, `neighbours` |
| `stats` | world totals, `cells_by_source`, `cells_by_dataset`, `composition` (per dataset cells selected / overridden, pairwise overlap statistics with median and rms difference, and seam statistics where the supplying dataset changes), coastline check, conforming/regional/seamark statistics, `utm_scale_min/max`, `notes` (omitted when empty) |
| `sources[]` | provenance, see below |
| `checkpoints[]` | named vertices (highest, lowest, corners) with expected elevation, so an importer can verify its own orientation and placement |
| `attribution[]`, `files` | credits; optional extra files |
| `licensing` | what was read about each source's terms (`checked_at`), per source `spdx` (or `LicenseRef-...`), `terms_url`, `attribution_text`, `share_alike`, `navigation_disclaimer`, `endorsement_prohibited`; per output part (`components[]`) which sources it contains and whether it is separable (OSM-derived files are ODbL and separable); `required_notices[]` (attribution, navigation, odbl, no-endorsement) and `unresolved[]` open questions. A record of terms as read, not legal advice |

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


## Composition and vertical references (0.3.0)

- A world may combine several elevation datasets (`sources[]` in the config). For every output cell the first dataset in precedence order that has a valid value is used (`precedence: "listed"` keeps the config order; `"auto"` ranks by quality class, declared accuracy, native resolution, year, then list order; the order used is `stats.composition.order`). Cells are never averaged across datasets, and a nodata hole in a higher-ranked dataset is filled by the next one, which is visible in `.dataset.png`.
- Heights are in the world's single vertical datum, `vertical.datum`. A dataset in another datum is converted only by a PROJ grid operation that PROJ resolves with the grids installed (recorded with grid sha256 and PROJ version) or by a user offset that cites its source; a ballpark or missing transformation is refused. Tidal datums and MSL have no verified transformation and are refused unless `vertical.allow_mixed` is set, in which case `vertical.mixed` is true and `ocean validate` warns. Nothing about vertical accuracy is claimed beyond that.
- `ocean validate` recounts `cells_by_dataset` from the layers and warns about systematic offsets or large seams between datasets.
