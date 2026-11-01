# Tutorial: from an empty directory to an inspected maritime world

About 15 minutes, most of it downloading. You need Go 1.22 or newer (the `go` line in `go.mod`) and the GDAL command-line tools; no game engine.

## 1. Install

```bash
# macOS: brew install go gdal      Debian/Ubuntu: apt install golang gdal-bin
git clone <this repository> && cd OceanBuilder
go build -o bin/ocean ./cmd/ocean
export PATH="$PWD/bin:$PATH"
ocean version        # ocean 0.3.0 (world format schema 2; 3 for worlds on a local transverse Mercator grid)
gdalinfo --version   # any recent GDAL; tested with 3.13
```

## 2. Choose a region and write the config

```bash
mkdir my-project && cd my-project
ocean world init falmouth          # writes falmouth.world.json
cat falmouth.world.json
```

```json
{ "bbox": [-5.2, 50.05, -4.95, 50.22], "cell_m": 48, "coastline": "osm",
  "name": "falmouth", "resampling": "bilinear", "tile_cells": 128 }
```
(the file is written pretty-printed with keys in alphabetical order; shown compact here)

`bbox` is `[west, south, east, north]` in degrees. `cell_m` is the grid spacing in metres; `tile_cells` (a power of two) is the cells per tile edge. Pick `cell_m` near the finest data you will use: finer than the source is interpolation, and the validator says so. Other regions to copy from are in `examples/regions/` (a Norwegian fjord, a southern-hemisphere reef, a volcanic flank, a shallow bank).

Check it before downloading anything:

```bash
ocean world check falmouth.world.json
# falmouth: valid
#   EPSG:32630, 4 x 4 tiles of 6144 m (513 x 513 samples, 0.3 M cells)
#   working memory about 6 MiB, tile data about 3 MiB before PNG compression
#   UTM point scale 0.99984..0.99990
```

A bounding box that is too wide for one UTM zone is refused here with advice (split it, or set `utm_epsg`).

## 3. Build

```bash
ocean build falmouth.world.json
```

It downloads GMRT bathymetry/topography and the OpenStreetMap coastline into `.ocean-cache/` (hashed, with provenance), warps them onto one metric grid, cuts crack-free tiles, builds the LOD levels and writes `output/falmouth/`. Run it again: *up to date*, nothing is touched. Add `-force` to rebuild, `-q` to silence progress, `-log-json` for machine-readable progress, Ctrl-C to cancel safely.

To pin your sources so teammates get the same bytes: `ocean build falmouth.world.json -update-lock`, then commit `falmouth.world.lock.json`.

## 4. Inspect what you got

```bash
ocean info output/falmouth        # region, grid, levels, quality, every source with licence
ocean validate output/falmouth    # recomputes everything from the tile files; exit 1 on errors
ocean preview output/falmouth     # preview.png and preview.html (shaded relief, tile grid)
```

Read the warnings: *background-fill* says what share of the water is a coarse global grid resampled to your cell size, not measurement. `ocean info` prints the share of every source per cell.

Ask the data questions directly (no GDAL, no engine):

```bash
ocean sample output/falmouth -lonlat -5.05,50.14
# -5.05000,50.14000  water, depth 10.5 m      elevation   -10.55 m  class 2  source survey (measured)  tile L0_57_904
ocean sample output/falmouth -line=-5.08,50.12:-5.02,50.18 -steps 12     # a depth profile: one such line per step
```

## 5. Use it

- **Any GIS** (QGIS, GDAL): `ocean export output/falmouth -target geotiff -dest tif -level 0` writes `tif/height.tif` (32-bit float), `tif/depth_class.tif` and `tif/source.tif`.
- **Your own code**: Go programs import `github.com/ocean-compiler/ocean/pkg/oceanworld`; any other language implements the short algorithm in [the format spec](../formats/world-v2.md).
- **Unity (URP)**: `ocean export output/falmouth -target unity -dest path/to/UnityProject`, then menu *Ocean > Import World*.

## 6. Change something, rebuild only what changed

Edit `falmouth.world.json`: add `"depth_bands": [3, 10, 40]`, rebuild. The log shows `stage cache hit: dem`, so nothing is re-warped; compare the result with the previous build:

```bash
cp -r output/falmouth /tmp/before
ocean build falmouth.world.json
ocean diff /tmp/before output/falmouth
# config (1):  depth_bands: [5 20 50 200] -> [3 10 40]
# tiles changed (22): L0_57_905 ...      (tiles with no water are byte-identical)
```

Add `"coast_conform": "osm"`, `"seamarks": true` or `"regional_bathymetry": "emodnet"` (Europe, 115 m) the same way; each is documented in the README with what it does and does not establish.

## 7. Things to know

- Not for navigation. GMRT forbids it; this is a game-development dataset.
- Zero metres is the waterline; no vertical datum reconciliation is done.
- Keep `ATTRIBUTION.txt` with anything you redistribute; OSM-derived content is ODbL.
- `ocean cache stats` / `ocean cache clean [-stages]` manage disk use.
