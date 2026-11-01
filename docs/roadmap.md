# Roadmap

Ocean Compiler is a standalone, engine-independent tool ([ADR 0004](decisions/0004-engine-agnostic-scope.md)). Status labels: **done** = implemented and tested; **partial** = works with stated limits; **open** = not started.

## State (2026-11-01, tool 0.2.0, format schema 2)

Done:
- Format v2 with LOD pyramid, per-sample source layer, per-file hashes, documented in [formats/world-v2.md](formats/world-v2.md).
- Sources: GMRT (topo + survey mask), local GeoTIFF/GeoJSON, OSM coastline, OpenSeaMap seamarks, opt-in EMODnet regional fill, opt-in OSM coast conform, LibreTerrain edit round trip.
- Reproducible builds, up-to-date skip, warp stage cache, atomic output, cancellation, UTM distortion limits.
- CLI: `build`, `validate`, `info`, `sample`, `diff`, `preview`, `export`, `cache`, `world init/check`, `sources`, `libreterrain`.
- Reader package `pkg/oceanworld`; GeoTIFF export; Unity URP adapter with Addressables and streamer.
- Tests: synthetic ground truth (southern hemisphere, Web Mercator source, nodata hole), LOD and determinism, incremental and cancel, reader vs GDAL, export. Measured numbers: [development-log.md](development-log.md).

## Next (highest value first)

1. **Multiple sources per world with explicit precedence** (survey-grade national grids over GMRT over GEBCO), each sample keeping its source id. Today: GMRT or one local pair, plus EMODnet fill. Needs a source-provider interface; design it with the second real provider (NOAA CUDEM tiles over S3, public bucket) rather than before.
2. **Windowed processing** so a world larger than memory can be built; `world check` estimates memory but refuses nothing.
3. **Multi-zone worlds** (bbox beyond 0.5 % UTM distortion, or crossing a zone boundary). Today: refused with advice to split or set `utm_epsg`. Options: per-zone sub-worlds with a shared seam rule, or a local transverse Mercator centred on the bbox.
4. **Vertical datum handling**: record each source's datum; apply geoid/tide conversion where the transform is published. Today zero is the waterline and the offset to the OSM coastline is only reported.
5. **Smoothed LOD method** (alternative `lod.method`) for consumers that prefer averaged coarse levels over exact decimation; needs a source-code rule for blended samples.
6. **Seascape** (Terrarium PNG tiles, CC BY 4.0) as an optional background source; see [research/ecosystem.md](research/ecosystem.md).
7. Unity: LOD levels in the importer, standalone player build test, floating origin for the streamer, water/seabed material.
8. Unreal Landscape exporter (2ⁿ−1 sizes, Z-scale in the manifest): only once a second consumer of the format asks.
9. Vector products: polygonised depth contours and coast from the grid (grid and masks exist; no vector output yet).
10. Local OSM extract (PBF or GeoJSON) as an Overpass alternative: Overpass was unreliable during the 2026-11-01 region builds.

## Open questions

- ODbL: a world built with `coast_conform` or `seamarks` contains OSM-derived cells and points. Treated as a derivative database; attribution is written to `ATTRIBUTION.txt`, share-alike is not legally analysed.
- Vertical datum of GMRT: a mosaic of contributed grids; how to quantify per region.
- Whether decimated-LOD aliasing of narrow channels matters to real consumers (documented, not measured).

## Known problems

- One UTM zone per world; the whole grid in memory.
- Overpass mirrors can be slow or down; the build then continues without a coastline and says so (`no-coastline` warning). Each mirror attempt is capped at 100 s.
- Unity importer reads level 0 only; verified in the editor (import, Addressables build, 4 PlayMode tests) but not in a standalone player.
- Coastline-to-DEM offset is reported, not corrected. Preview colours are a fixed ramp, not a chart.
- Unity batchmode screenshots need a GPU session; URP's far-LOD terrain base map never generates there.
