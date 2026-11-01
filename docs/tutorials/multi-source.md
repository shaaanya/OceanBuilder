# Tutorial: several bathymetry sources in one world

A world can compose more than one elevation dataset. Every level-0 sample comes from exactly one dataset (the first one in precedence order that has a valid value there; nothing is blended across datasets), and the world records which: `ocean info` prints cells per dataset, `ocean sample` prints the dataset at a point, and the tile files carry a `*.dataset.png` layer.

## 1. Write the sources

```json
{
  "name": "falmouth2",
  "bbox": [-5.2, 50.05, -4.95, 50.22],
  "cell_m": 50,
  "coastline": "osm",
  "precedence": "auto",
  "vertical": { "datum": "EGM2008" },
  "sources": [
    { "id": "gebco", "type": "gebco", "quality": "compiled",
      "vertical": { "datum": "MSL", "offset_m": 0.0,
                    "offset_source": "GEBCO states MSL; treated as equal to EGM2008 for a game world, risk accepted" } },
    { "id": "etopo", "type": "etopo2022", "quality": "compiled" }
  ]
}
```

GEBCO states its heights are relative to mean sea level and ETOPO 2022 states EGM2008. The tool has no verified conversion between "MSL" and a geoid, so it refuses to combine them silently. The world-level `vertical` block names the target. GEBCO keeps its true datum (`MSL`) in the manifest; the offset of 0 m with a stated source is you declaring, in the config and in the manifest, that you accept the two as the same reference. Without it the build stops and says why.

Source types: `gmrt`, `emodnet`, `etopo2022`, `gebco`, `cudem`, and `geotiff` (your own file; it needs `path`, `license`, `attribution` and `vertical.datum`). `ocean sources list` shows them with licences. `quality` is `survey`, `compiled` or `regional`; `year` and `accuracy_m` are optional documented properties of the data.

## 2. Check before downloading

```bash
ocean world check falmouth2.world.json
```

Besides grid size and memory it prints, for every source, its datum and what the build would do to reach the world's vertical datum (`identity`, `proj-grid`, `user-offset`, or `refused` with the reason). Nothing is downloaded. Datasets whose heights cannot be related are refused: set a world `"vertical": {"datum": "EGM2008"}` where every source has a verified conversion (EGM96, EGM2008, WGS 84 ellipsoid), give a documented per-source `vertical.offset_m` with `offset_source`, or accept the mix with `vertical.allow_mixed` (then no height is converted and the manifest says so).

## 3. Precedence

`"precedence": "listed"` (default) tries the sources in the order written. `"auto"` ranks by quality class, then documented accuracy, then finer native resolution, then newer `year`, then list order. The ranking and every key it used are in `ocean info`, so the choice can be explained afterwards. Where the winner has no data (nodata, outside its coverage) the next dataset fills the cell; nodata that nobody covers stays nodata.

## 4. Build and look

```bash
ocean build falmouth2.world.json
ocean info falmouth2        # datasets table: precedence, datum, cells
ocean validate falmouth2    # recounts cells per dataset from the tile files
ocean sample falmouth2 -lonlat -5.07,50.13
```

The `survey` label on a water cell means the source's mask says a direct measurement lies somewhere in that source cell. At a finer `cell_m` than the source, the world warns that detail is interpolation; it does not become a measurement.

What was tested against real services, and when, is in [../sources.md](../sources.md).
