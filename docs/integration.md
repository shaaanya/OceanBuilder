# Integration guide: consuming a world without OceanBuilder, Go or Unity

A world is a directory of JSON, PNG and little-endian float32 files. [docs/formats/world-v2.md](formats/world-v2.md) is the contract; this guide is the checklist for writing a reader in any language. A complete non-Go reader, standard library only, is [examples/python-reader/ocean_reader.py](../examples/python-reader/ocean_reader.py) (about 200 lines, includes the projection and a PNG decoder). It was checked against `ocean sample` (40 random points on levels 0 and 2, UTM and local-tmerc worlds: worst difference 1.1e-4 m, float32 rounding) and its projection against `gdaltransform` (< 1e-8 m) — `scripts/verify-local.sh scripts` repeats it.

## 1. Open and gate

1. Read `world.manifest.json`. **Refuse** a `schema_version` you do not know (2 and 3 exist). Ignore manifest keys you do not know; additive fields never change the schema number. `format_revision` (1 = has `.dataset.png`) only adds optional layers.
2. Check `height_encoding`: `type` "float32", `byte_order` "little", `row_order` "north_to_south", `nodata` -9999.
3. Optionally verify `sha256` of each file you read against the tile entry. `build_id` identifies the produced bytes: use it as a cache key for anything you derive.

## 2. Byte layouts

| File | Layout |
|---|---|
| `*.height.f32` | `S*S` IEEE-754 binary32, **little-endian**, row-major, row 0 = north edge, column 0 = west edge. Metres, up positive, 0 = land/water boundary. `S = grid.samples_per_tile` (= `tile_cells + 1`). Exactly `S*S*4` bytes. No header, no padding. |
| `*.depth.png` | 8-bit greyscale PNG, `S x S`, same order. 0 land, 1..n water depth band (`depth_classes[]`), 255 nodata. |
| `*.shore.png` | 8-bit grey: 255 land sample next to water (4-connected), else 0. |
| `*.source.png` | 8-bit grey: source code table in the manifest `source_layers[]` (0 nodata, 1 survey = measured, 2 background, 3 land, 4 regional, 5 conformed, 6 refined, 7 lod-derived). An unknown code means "not measured". |
| `*.dataset.png` | 8-bit grey: `datasets[].index` that supplied the sample; 0 = none. Optional. |

A PNG reader must support filter types 0-4 and 8-bit greyscale; interlacing is not used. Nodata is exactly -9999 in the float file and 255/0 in the byte layers; treat a bilinear sample as invalid if any of its four vertices is nodata (do not fill).

## 3. Coordinates

- Heights are on a **metric grid**, not degrees. `crs.kind` "utm": WGS 84 / UTM zone from `crs.epsg` (326zz north, 327zz south; false easting 500000, k0 0.9996, false northing 0 or 10 000 000). `crs.kind` "tmerc" (schema 3): WGS 84 transverse Mercator with `lon_0`, `k_0`, `false_easting`, `false_northing` (zero values may be omitted from the JSON) or use `crs.proj4` with PROJ. Grid metres differ from ground metres by the point scale `k` (`stats.utm_scale_min/max`, bounded to 0.5 %).
- Longitude: wrap around `lon_0` (a world across 180° has `bbox_wgs84` east above 180).
- Vertex `(row i, col j)` of tile `(L, x, y)`: easting `x*span_L + j*cell_L`, northing `(y+1)*span_L - i*cell_L`, where `span_L = tile_span_m * 2^L`, `cell_L = cell_m * 2^L`. Samples are vertices, not pixel areas: do not offset by half a cell.
- Tile index at level `L`: `x = floor(E / span_L)`, `y = floor(N / span_L)`; use `floor` (northings can be negative). Ids are `L<level>_<x>_<y>`. The last row/column of a tile duplicates the first row/column of its east/north neighbour at the same level, so a position exactly on a tile edge is a vertex of both tiles, and on the world's north or east edge only the tile before exists: look up `floor`, and if that tile is missing and the position is exactly on its west/south edge, use the tile before (the reference reader's `_locate`). The manifest checkpoints at the world corners hit exactly this case.
- Manifest `col`/`row` count from the north-west of the level and are for convenience only; `x`/`y` are authoritative.
- `checkpoints[]` are named vertices (highest, lowest, corners) with expected elevation: read them back as a one-minute test of your orientation and axis handling.

## 4. Levels of detail

- Level `L` has cell `cell_m * 2^L` and span `tile_span_m * 2^L`; every level has the same `S` samples per tile. The parent of `(L, x, y)` is `(L+1, x>>1, y>>1)`; its four children are `(L-1, 2x+{0,1}, 2y+{0,1})`.
- `lod.method` "vertex-decimation": a level-L vertex **is** a level-0 vertex at the same position (same height and source code), so shared edges also agree across levels. "shoal-biased": water vertices may be shallower than the level-0 vertex (source code 7).
- Neighbouring tiles of the **same level** share edge vertices exactly. Tiles of **different levels** do not align edge-to-edge in a rendering engine (a coarse edge is a straight line between every second fine vertex): meshes at mixed levels crack unless you add skirts, stitch or constrain the edge. The Unity adapter uses skirts ([unity-lod.md](unity-lod.md)). Pick a level per tile by distance and keep neighbouring levels within one step if you can.
- Choosing a level for a point query: use the finest level whose tile exists at that point.

## 5. Missing tiles and levels

- **Level 0 is complete**: every tile in the rectangle `first_tile_x..last_tile_x` x `first_tile_y..last_tile_y` of `lod.levels[0]` exists, even when it holds only nodata (the validator reports `empty-tile`). A point outside that rectangle is outside the world: unknown, **not** sea.
- Levels above 0 omit tiles with no valid sample. A missing coarse tile means "nothing known here at this level"; use level 0 or a finer level, never assume water.
- A reader that wants a continuous surface should treat nodata as a hole (leave it, or draw it as deep unknown water if the game wants) and keep the `depth_class` 255 flag visible to the player; the tool never fills nodata.
- The pyramid may stop below the level where one tile covers the world (`lod_levels` in the config, at most 8). `lod.levels[]` lists exactly what exists.

## 6. Data honesty

`source.png` says what each sample is. Only code 1 is a measurement at or near output resolution. `resampling.upsampled` true means the output grid is finer than the source: detail below `resampling.source_native_res_m` is interpolation. Surface it in your UI or documentation if your product presents depth as fact. Nothing here is for navigation; the licences require saying so.

## 7. Attribution and licences

Ship `ATTRIBUTION.txt` with any redistribution (it is generated from `sources[]` and the licensing record). `manifest.attribution[]` and `licensing.required_notices[]` list what must be shown: source credits, the not-for-navigation notice, and for OSM-derived parts (`coastline.wgs84.geojson`, `seamarks.geojson`, coastline-conformed cells) the ODbL notice (share-alike is the consumer's to evaluate; the tool records terms as read, not legal advice). `licensing.components[]` says which output files contain which sources and whether they are separable. A world with any `sources[]` entry missing `license`, `attribution`, `sha256` or `retrieved_at` fails `ocean validate`.

## 8. Version compatibility

| Feature | First tool version | How a reader sees it |
|---|---|---|
| schema 2 (float32 heights, UTM) | 0.2.0 | `schema_version` 2 |
| schema 3 (local transverse Mercator, antimeridian) | 0.3.0 | `schema_version` 3, `crs.kind` "tmerc" |
| `.dataset.png`, `datasets[]`, `vertical`, `licensing` | 0.3.0 | `format_revision` 1; a tile without `dataset` has no such layer |
| `datasets[].delivery` (GMRT delivered resolution) | 0.4.0 | optional key |
| level 0 always complete (all-nodata tiles written) | 0.5.0-rc1 | older worlds may lack all-nodata level-0 tiles: tolerate missing level-0 tiles or rebuild |
| schema 1 (`.r16` quantised heights) | 0.1.0 | refuse; rebuild |

Dates and contents are from the [CHANGELOG](../CHANGELOG.md); `tool.version` in the manifest is the writer. A reader written for schema 2 + `format_revision` 1 keeps working with every later 0.x tool unless `schema_version` changes; when it does, the old reader must refuse.

## 9. Checklist before you ship an adapter

- [ ] Refuses unknown `schema_version`.
- [ ] Reads the checkpoints back within a few mm of `elevation_m`.
- [ ] Little-endian float32 on big-endian hosts too (explicit byte order).
- [ ] North-to-south rows and `floor` tile indices (negative northings, southern hemisphere).
- [ ] Treats nodata as nodata, outside-world as unknown, missing coarse tiles as "use another level".
- [ ] Mixed-level seams handled (skirts, stitching or equal levels).
- [ ] Attribution and not-for-navigation notice reachable by the end user.
