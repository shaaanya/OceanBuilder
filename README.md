# OceanBuilder

A Go command-line tool that turns a geographic bounding box into a tiled ocean world. It puts bathymetry, land elevation and coastline on one metric grid, cuts the grid into tiles with an LOD pyramid, and records where every sample came from. `ocean validate` then checks the result against the data it was built from.

The output format is documented and engine-independent ([docs/formats/world-v2.md](docs/formats/world-v2.md)). There is a Go reader, a GeoTIFF export and a Unity (URP) adapter. You don't need a game engine to build, inspect or use a world.

Status: 0.5.0-rc1. The offline tests pass in CI on Linux, Windows and macOS; the Unity importer and runtime LOD have only been run on macOS arm64. The [release report](docs/releases/0.5.0-rc1.md) lists what was verified and what is partial or deferred.

![Falmouth, Cornwall: shaded relief and bathymetry, OSM coastline in red, tile edges in white](docs/img/falmouth-preview.png)

*Falmouth and Carrick Roads, built with `ocean world init falmouth && ocean build falmouth.world.json`: 16 tiles of 6.1 km at 48 m spacing. Red is the OpenStreetMap coastline. The land/water colour break is the DEM's own zero contour, and the validator reports how far apart the two are.*

## Quick start

You need Go 1.22+ (CI uses 1.26; see [install](docs/install.md)) and the GDAL command-line tools (`gdalwarp`, `gdal_translate`, `gdaltransform`, `ogr2ogr`, `gdalinfo`) on `PATH`. Tested with GDAL 3.13.

```bash
go build -o bin/ocean ./cmd/ocean

bin/ocean world init falmouth               # writes falmouth.world.json (more regions in examples/regions/)
bin/ocean world check falmouth.world.json   # grid, memory, projection distortion; no download
bin/ocean build falmouth.world.json         # downloads GMRT + OSM, writes output/falmouth
bin/ocean info output/falmouth              # region, levels, quality, every source with licence
bin/ocean validate output/falmouth
bin/ocean sample output/falmouth -lonlat -5.05,50.14    # depth, elevation, source; no GDAL needed
bin/ocean preview output/falmouth           # preview.png + preview.html
bin/ocean export output/falmouth -target geotiff -dest tif
bin/ocean export output/falmouth -target unity -dest path/to/UnityProject
bin/ocean diff before after                 # what changed between two builds
```

`ocean help` lists every command and `ocean <command> -h` shows its flags. Progress goes to stderr (`-q` silences it, `-log-json` structures it). Ctrl-C cancels cleanly, and a failed or cancelled build never damages an existing output.

Rebuilding with unchanged inputs returns immediately (`-force` overrides). Changing only derived settings reuses the cached warps. Downloads and warped rasters live in `.ocean-cache` (`ocean cache stats|clean [-stages]`).

For scripting, `build`, `validate`, `info`, `sample` and `diff` take `-json`, and `validate` exits 1 on errors.

First time? Follow the [tutorial](docs/tutorials/first-world.md), which goes from an empty directory to an inspected world. The [multi-source tutorial](docs/tutorials/multi-source.md) covers combining datasets.

## Data sources

- GMRT for bathymetry and topography, or your own GeoTIFFs.
- OpenStreetMap for the coastline, OpenSeaMap for seamarks.
- EMODnet bathymetry as an optional regional fill.

A world can combine several elevation datasets with explicit precedence and recorded vertical datums. The grid is UTM, or a local transverse Mercator for wide boxes.

`ocean sources explain <id>` prints a source's datum, resolution, whether it is measured or interpolated, and when it was last checked live.

Options for these sources are in [docs/options.md](docs/options.md): your own data, EMODnet regional fill, coastline conforming, seamarks and the source lock.

## What a build writes

```text
output/falmouth/
  world.manifest.json      grid, CRS, encodings, LOD levels, sources (URL, time, sha256, licence),
                           per-tile stats and file hashes, build id, input key
  ATTRIBUTION.txt          required credits for the sources used
  coastline.wgs84.geojson  OSM coastline as fetched
  seamarks.geojson         OpenSeaMap points (only with "seamarks": true)
  tiles/<level>/L<level>_<x>_<y>.height.f32   float32 metres, (N+1)² samples, edges shared with neighbours
  tiles/<level>/..depth.png     class per sample: 0 land, 1..n depth bands, 255 nodata
  tiles/<level>/..shore.png     255 where land touches water
  tiles/<level>/..source.png    survey, background grid, land DEM, regional, conformed or refined
```

Level 0 is full resolution. Each higher level keeps every second vertex of the one below, with no averaging, until a single tile covers the world. Features narrower than 2^k cells can vanish at coarse levels. Falmouth has 16 + 6 + 4 + 4 + 1 tiles over 5 levels, from 48 m to 768 m cells.

Tile ids are absolute indices on the UTM grid (`floor(easting / tile span)`), so the same place gets the same id whatever bounding box you ask for. Identical config and inputs give byte-identical output, including from a clean cache.

## Validation

This is real output from the Falmouth build:

```text
WORLD: falmouth

Tiles: 16
Elevation coverage: 100.0%
Water cells backed by survey data: 91.9%
OSM coastline to DEM shoreline offset: median 101 m, p90 268 m
Neighbour edge mismatches: 0, level mismatches: 0

WARNINGS:
  [low-res-bathymetry] 1 tiles are mostly (>50%) interpolated/background-grid bathymetry rather than survey-backed
  [background-fill] 8.1% of water cells have no survey data and come from a coarse background grid (≈758 m resolution) resampled to 48 m
  [coast-misaligned] OSM coastline is 101 m (median) / 268 m (p90) from the nearest DEM shoreline; 31% of points have none within 384 m. ...

RESULT: BUILT WITH WARNINGS
```

Mindelo (Cabo Verde) is the opposite case. Only 3.7% of its water cells are survey-backed. The rest is the roughly 0.9 km GEBCO grid resampled to 48 m, and the validator says so (`docs/img/mindelo-preview.png`).

## Unity

`ocean export <world> -target unity -dest <project>` copies the world into `Assets/OceanWorld/<name>/` and installs the UPM package `Packages/com.oceanbuilder.world`, which is embedded in the binary. `Ocean > Import World` (or batchmode) then generates terrains, prefabs, an index asset with neighbour links, and a scene with every tile under a translucent sea plane.

```bash
Unity -batchmode -projectPath <project> -executeMethod OceanWorldImporter.ImportFromCommandLine -quit
```

![Falmouth in Unity 6000.4.0f1 under URP 17.4: 16 prefab tiles, shore sand and seabed painted by depth under a translucent sea plane](docs/img/falmouth-unity-urp.png)

URP only (Unity 6, URP 17.x, tested on 17.4). The importer refuses to run under another pipeline. It has no water shader, textures or nautical features yet. Addressables, runtime streaming and the checks the importer runs are described in [docs/unity.md](docs/unity.md). Runtime LOD is in [docs/unity-lod.md](docs/unity-lod.md).

## Refining land in LibreTerrain

LibreTerrain is a separate node-graph terrain editor (not part of this repository). OceanBuilder only exchanges files with it.

```bash
ocean libreterrain export output/falmouth      # heightmap.exr + exchange.json (+ .lterrain if libreterrain-mcp is found)
# edit in LibreTerrain, export EXR32
ocean libreterrain import output/falmouth-libreterrain <libreterrain output dir> -out refined/falmouth
ocean build refined/falmouth/falmouth-refined.world.json
```

Export writes EXR through GDAL, so it needs a GDAL built with the EXR driver. Import writes an edit layer, not a replacement. Only cells that are land in the original build change. Sea floor and coastline stay as sourced, and the manifest records the layer. Flipped, resampled or resized output is refused. Measured behaviour is in [docs/research/libreterrain.md](docs/research/libreterrain.md).

## Limits

- Not for navigation. GMRT's terms forbid using its data for navigation, and this tool is for game worlds.
- Where GMRT has no multibeam survey (about 100 m), its `topo` layer is filled from GEBCO_08 at about 0.9 km. Output cells finer than that are interpolation. The manifest and validator report the surveyed fraction per tile.
- Vertical datums are recorded. Where PROJ ships a verified grid operation (EGM96, EGM2008, WGS 84 ellipsoid) the build converts them. Tidal datums (MLLW, LAT and so on) and NAVD88 are not converted, and the build refuses them unless you give a documented offset or accept a mix. Land/water is still decided at 0 m of the world datum, and the horizontal offset to the OSM coastline is reported.
- Projection scale deviation is recorded in the manifest. A bounding box beyond 0.5% is refused.
- LOD levels are plain vertex decimation by default and can alias thin features. `lod_method: "shoal"` keeps the shallowest water of each 3x3 block and never makes water deeper. `ocean lod-report` measures both.
- Seamark and harbour data are volunteer-mapped OSM points.

## Docs

- Using it: [install and pin](docs/install.md), [tutorial](docs/tutorials/first-world.md), [multi-source tutorial](docs/tutorials/multi-source.md), [options](docs/options.md), [troubleshooting](docs/troubleshooting.md), [resume and the stage cache](docs/stage-cache.md), [performance and disk](docs/performance.md)
- Output: [world format](docs/formats/world-v2.md), [integration guide](docs/integration.md), [Python reference reader](examples/python-reader/ocean_reader.py), [LOD quality](docs/lod-quality.md)
- Unity: [import and streaming](docs/unity.md), [runtime LOD](docs/unity-lod.md)
- Data: [sources](docs/sources.md), [licensing](docs/research/licensing.md)
- Project: [architecture](docs/architecture/overview.md), [decisions](docs/decisions/), [testing tiers](docs/testing.md), [ecosystem research](docs/research/ecosystem.md), [roadmap](docs/roadmap.md), [development log](docs/development-log.md), [contributing](CONTRIBUTING.md)

## Licence

MIT for the code ([LICENSE](LICENSE)). Data keeps its own licence: GMRT is CC BY 4.0 (citation required, see `ATTRIBUTION.txt`), GEBCO is public domain, OpenStreetMap is ODbL. Built worlds carry `ATTRIBUTION.txt` and must not be redistributed without it. The fixtures in `testdata/` are small crops of those sources.
