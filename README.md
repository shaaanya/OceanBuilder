# Ocean Compiler

A Go CLI that turns a geographic bounding box into a tiled, validated sailing world for game engines. It fetches bathymetry and coastline data, puts them on one metric grid, cuts crack-free tiles, writes depth and shoreline masks, and checks the result against the data it was built from.

Status: **0.1, first vertical slice.** One bathymetry source (GMRT), one coastline source (OpenStreetMap), one engine (Unity). See [docs/roadmap.md](docs/roadmap.md) for what is missing.

![Falmouth, Cornwall: shaded relief and bathymetry, OSM coastline in red, tile edges in white](docs/img/falmouth-preview.png)

*Falmouth and Carrick Roads, built with `ocean world init falmouth && ocean build falmouth.world.json`: 16 tiles of 6.1 km at 48 m spacing. Red is the OpenStreetMap coastline; the land/water colour break is the DEM's own zero contour. Where they differ, the validator reports by how much.*

## Try it

Requirements: Go 1.22+, the GDAL command-line tools (`gdalwarp`, `gdal_translate`, `gdaltransform`, `ogr2ogr`, `gdalinfo`) on `PATH`. Tested with GDAL 3.13.

```bash
go build -o bin/ocean ./cmd/ocean

bin/ocean world init falmouth          # writes falmouth.world.json
bin/ocean build falmouth.world.json    # downloads GMRT + OSM, writes output/falmouth
bin/ocean validate output/falmouth
bin/ocean preview output/falmouth      # preview.png + preview.html
bin/ocean export output/falmouth -target unity -dest path/to/UnityProject
```

Everything is non-interactive and scriptable: `build -json` and `validate -json` print machine-readable output, `validate` exits 1 on errors, downloads are cached in `.ocean-cache` (`ocean cache stats|clean`).

### Validation output (real, Falmouth)

```text
WORLD: falmouth

Tiles: 16
Elevation coverage: 100.0%
Water cells backed by survey data (GMRT mask): 91.9%
OSM coastline to DEM shoreline offset: median 101 m, p90 268 m
Neighbour edge mismatches: 0

WARNINGS:
  [low-res-bathymetry] 1 tiles are mostly (>50%) interpolated/global-grid bathymetry rather than survey-backed
  [gebco-fill] 8.1% of water cells have no survey data and come from the GEBCO_08 grid (≈758 m resolution) resampled to 48 m
  [coast-misaligned] OSM coastline is 101 m (median) / 268 m (p90) from the nearest DEM shoreline; 31% of points have none within 384 m. ...

RESULT: BUILT WITH WARNINGS
```

Mindelo (Cabo Verde) is a useful contrast: only 3.7% of its water cells are survey-backed, the rest is the ~0.9 km GEBCO grid resampled to 48 m, and the validator says so (`docs/img/mindelo-preview.png`).

## What a built world contains

```text
output/falmouth/
  world.manifest.json      grid, CRS, height encoding, sources (URL, time, sha256, licence), per-tile stats, build id
  ATTRIBUTION.txt          required credits for the sources used
  coastline.wgs84.geojson  OSM coastline as fetched
  seamarks.geojson         OpenSeaMap points (only with "seamarks": true in the world config)
  tiles/0/L0_<x>_<y>.height.r16   uint16 heights, (N+1)² samples, shared edges with neighbours
  tiles/0/L0_<x>_<y>.depth.png    class per cell: 0 land, 1..n depth bands, 255 nodata
  tiles/0/L0_<x>_<y>.shore.png    255 where land touches water
```

Tile ids are absolute indices on the UTM grid (`floor(easting / tile span)`), so the same place gets the same id whatever bounding box you ask for. Outputs are byte-identical for identical config and inputs (tested).

### Seamarks

`"seamarks": true` in the world config fetches every OSM element tagged `seamark:type` in the bbox (buoys, lights, harbours, wrecks, anchorages...) and writes `seamarks.geojson`: WGS84 points (ways use their centre) with `seamark_type`, `name`, all raw tags, UTM `easting`/`northing` and the DEM elevation under each. Falmouth: 60 features. The query covers the whole tile grid, which reaches beyond the configured bbox. `stats.seamarks` in the manifest counts them by type and cross-checks floating marks (buoys) against the DEM: 8 of 31 sit on DEM land, all in the intertidal upper Penryn/Truro creeks (DEM 0 to 3 m at 48 m cells), while the other 23 are on water where expected, so that is data resolution/tide, not misplacement. Volunteer-mapped tagging, ODbL, not a nautical chart. Unity: `ocean export -target unity` copies the file; the importer fills `OceanWorldIndex.seamarks` (id, type, name, Unity position, tile id; floating marks at y = 0) and adds one empty marker GameObject per mark under `Seamarks` in the scene (Falmouth: 60, also read back through Addressables in the PlayMode test). No marker prefabs or light characteristics yet.

## Unity

`ocean export <world> -target unity -dest <project>` copies the world into `Assets/OceanWorld/<name>/` and installs the UPM package `Packages/com.oceancompiler.world` (embedded in the binary). `Ocean > Import World` (or batchmode) generates, under `Generated/`:

- `Terrains/<tile id>.asset`: TerrainData per tile with 5 flat-colour layers painted from the depth-class and shore masks (land, shore, shallow, mid, deep; no texture assets needed, so no render-pipeline dependency in the data).
- `Prefabs/<tile id>.prefab`: Terrain + TerrainCollider, already positioned in world space.
- `OceanWorldIndex.asset`: tile ids, UTM bounds, Unity-space position/size, elevation range, water/survey fractions, **neighbour ids** and an Addressables address per tile, plus UTM↔Unity conversion (`OceanWorldIndex.UtmToUnity`, `TileAt`, `LinkNeighbours`).
- `<name>.unity`: a scene with every tile and a translucent sea plane at y = 0.

Unity space: x = east, z = north, y = metres above the world's zero; the south-west corner of the world is (0,0).

**Addressables.** If `com.unity.addressables` (≥1.19) is installed, the import creates the group `Ocean_<world>` and registers every tile prefab at `ocean/<world>/<tile id>` (labels `ocean-tile`, `ocean-<world>`) and the index at `ocean/<world>/index` (label `ocean-index`). Your own groups and build settings are untouched; move the entries wherever your game wants them. At runtime load the index, pick tiles near the ship by `TileAt`, `Addressables.InstantiateAsync(entry.address)`, then `index.LinkNeighbours(entry, terrain, id => loadedTerrains[id])`. `Ocean > Addressables > Build And Check` builds the bundles and verifies every address and label (batchmode: `-executeMethod OceanCompiler.World.Editor.OceanAddressables.BuildAndCheckFromCommandLine`).

```bash
Unity -batchmode -projectPath <project> -executeMethod OceanWorldImporter.ImportFromCommandLine -quit
```

![Falmouth in Unity 6000.4.0f1 under URP 17.4: 16 prefab tiles, shore sand and seabed painted by depth under a translucent sea plane](docs/img/falmouth-unity-urp.png)

Rendered by the importer itself (`-oceanShot`, no `-nographics`) on Unity 6000.4.0f1. The importer checks six manifest checkpoints (highest and lowest vertices, four corners) against `Terrain.SampleHeight` through its own UTM→Unity conversion; Falmouth's largest error is 3 mm under both pipelines and any flip or offset fails the import. **Render pipeline: URP only** (Unity 6, URP 17.x, tested on 17.4; the package depends on it and the importer refuses to run under another pipeline). The demo project is URP. The PlayMode test below finds no pink shader pixels when viewing the terrain from 3.5 km (the probe frame is 77% terrain, asserted above 20% so a blank frame cannot pass). Terrains use the active pipeline's default terrain material (5 layers: land, shore, shallow, mid, deep). The sea plane uses URP Lit (transparent). Caveat: in a batchmode screenshot URP's far-LOD terrain draws magenta because the editor never ticks (the screenshot code raises `basemapDistance` in memory to avoid it); in play mode it renders correctly. `docs/img/falmouth-unity-seabed.png` shows the seabed with the sea plane hidden (`-oceanHideSea`): flat colour bands with hard edges and no visible relief, because the layers are flat colours and the water elevation is GMRT (39 m native at Falmouth) with GEBCO fill (758 m) where there is no survey; it is a data and placeholder-material view, not a finished underwater look. Not implemented: LOD levels above 0 (the index has a `level` field, only 0 exists), water shader, textures, nautical features. A PlayMode test in the package (`Tests/`, needs `testables` in the project manifest) loads the index and tiles through Addressables from the **built bundles** (asserts `BundledAssetProvider`), links neighbours and checks the six checkpoints against `SampleHeight` within 5 cm (Falmouth: passes, 4 tiles loaded). That runs in the editor's play mode, not in a standalone player build.

## Refining land in LibreTerrain (optional)

[LibreTerrain](../LibreTerrain) is a separate node-graph terrain editor; OceanBuilder only exchanges files with it.

```bash
ocean libreterrain export output/falmouth      # heightmap.exr + exchange.json (+ .lterrain if libreterrain-mcp is found)
# ...edit in LibreTerrain, build exports (EXR32)...
ocean libreterrain import output/falmouth-libreterrain <libreterrain output dir> -out refined/falmouth
ocean build refined/falmouth/falmouth-refined.world.json
```

Import writes an edit layer, not a replacement: only cells that are land in the original build change, sea floor and coastline stay as sourced, and the layer is recorded in the manifest. It refuses flipped, resampled or resized output. Measured behaviour: `docs/research/libreterrain.md`.

## Honest limits

- Not for navigation. GMRT's terms say its data must not be used for navigation, and this tool is for game worlds.
- GMRT's `topo` layer is filled with GEBCO_08 (~0.9 km) wherever no ~100 m multibeam survey exists. Output cells finer than that are interpolation. The manifest and validator report the surveyed fraction per tile.
- There is no vertical-datum reconciliation. Land DEMs, multibeam, and the OSM coastline use different references; the tool treats 0 m as the waterline and reports the horizontal offset to the OSM coastline instead of hiding it.
- Single resolution level (no LOD pyramid), single UTM zone, no harbour or seamark features yet.

## Docs

[Architecture](docs/architecture/overview.md) · [Decisions](docs/decisions/) · [Data sources](docs/research/data-sources.md) · [Ecosystem research](docs/research/ecosystem.md) · [Roadmap](docs/roadmap.md) · [Development log](docs/development-log.md) · [Contributing](CONTRIBUTING.md)

## Licence

MIT for the code (see [LICENSE](LICENSE)). Data keeps its own licence: GMRT CC BY 4.0 (citation required, see `ATTRIBUTION.txt`), GEBCO public domain, OpenStreetMap ODbL. Built worlds carry `ATTRIBUTION.txt`; do not redistribute them without it. Test fixtures in `testdata/` are small crops of those sources.
