# Roadmap

## Next (highest value first)

0. LibreTerrain round trip (see ecosystem.md §1): `ocean export -target libreterrain` writing PNG16 + metre range, then verify against checkpoints.
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

## From the ecosystem research (docs/research/ecosystem.md, 2026-10-30)

- Seascape (openwatersio, BSD-3 code, CC BY 4.0 tiles, Terrarium PNG tiles over HTTPS) as an optional fallback bathymetry source; its source catalogue is the regional-source list. Datum varies per source.
- EMODnet DTM 2024 (~115 m) via WCS and NOAA CUDEM via the public S3 bucket, fetched through GDAL. EMODnet licence text still to read; ~115 m will not beat GMRT's delivered 39 m at Falmouth, so value is elsewhere.
- Engine-exact tile sizes: Unreal needs 2ⁿ−1-style sizes and a Z-scale (default 100 covers only ±256 m) written to the manifest.
- OSM seamarks via the existing Overpass client; verify the `seamark:type` list first. Seamap is GPL-3.0: reference only.
- Cesium plugins are Apache-2.0: possible later output target, not a core dependency.

## Open questions

- LibreTerrain: present locally at ../LibreTerrain (MIT/Apache, macOS app + MCP bridge). Plan: file-exchange adapter (export our heightmap for its `primitive.file` node; ingest its PNG16/EXR output as a local DEM); facts are from binary strings, verify by running it. gen-map: no repo found; ask the user for a URL.
- ODbL treatment of built worlds containing OSM-derived masks.
- Vertical datum for GMRT land vs. multibeam: how to quantify per region.

## Known problems

- Unity URP/HDRP are unverified (importer falls back URP → HDRP → Standard shaders; only Built-in has been run). Test with a URP template project.
- Coastline-to-DEM offset is reported but not corrected.
- Preview colours are a fixed ramp; not meant as a chart.
- `-nographics` Unity batchmode cannot take screenshots; Unity visuals still need a manual or MCP capture.
