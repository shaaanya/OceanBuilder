# Roadmap

## Next (highest value first)

1. Regional high-resolution bathymetry as an additional source (EMODnet for Europe, NOAA CUDEM for the US) merged over GMRT with per-cell source id and confidence. Needs a merge step with explicit vertical-datum handling.
2. Coastline-driven land/water mask: use OSM coast polygons (not DEM zero) for the shore mask, keep DEM zero as a cross-check.
3. Remaining build time on small worlds is the GDAL subprocesses (2× gdalwarp, ogr2ogr); profile before optimising further. Tile-corner transforms are already batched (fixture build 1.97 s → 1.60 s).
4. Windowed processing so global-scale areas do not need the full grid in memory.
5. LOD pyramid (levels > 0) with deterministic ids and tests that levels agree at shared vertices.
6. Unity: depth-class texture overlay, simple water shader, Terrain layers from shore mask; screenshots in README from Unity itself.
7. Harbour and seamark features (OpenSeaMap tags) as GeoJSON in the manifest.
8. Unreal Landscape exporter (resampled to 2ⁿ−1 components).
9. Incremental builds keyed on input hashes and config (the cache already keys downloads).
10. Multi-zone worlds.

## Open questions

- Does LibreTerrain exist as a usable tool? Not found so far; revisit if a repo is identified.
- ODbL treatment of built worlds containing OSM-derived masks.
- Vertical datum for GMRT land vs. multibeam: how to quantify per region.

## Known problems

- Unity URP/HDRP are unverified (importer falls back URP → HDRP → Standard shaders; only Built-in has been run). Test with a URP template project.
- Coastline-to-DEM offset is reported but not corrected.
- Preview colours are a fixed ramp; not meant as a chart.
- `-nographics` Unity batchmode cannot take screenshots; Unity visuals still need a manual or MCP capture.
