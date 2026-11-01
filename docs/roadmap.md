# Roadmap

## State at last stop (2026-10-30)

Done and verified on real data (Falmouth, Mindelo): GMRT + OSM worlds, LibreTerrain file round trip, Unity URP package with Addressables, tile streamer, seamarks, opt-in coast conform, opt-in EMODnet regional fill. All Go tests pass; PlayMode tests (4) pass on the URP demo. Unverified or not started: see Next and Known problems. Highest-value next steps: (a) LOD pyramid (5) so distant tiles are cheap in the sailing game; (b) incremental builds (9) now that `build_id` tracks algorithm versions; (c) a depth-gradient URP water/seabed material; (d) first test inside the user's own Unity project (found 2026-11-01: ALONE, see Integration below; it has its own pipeline and has never loaded OB output); (e) floating origin for the streamer.

## Integration with the sailing game (audit 2026-11-01, evidence-backed)

The game is ALONE (`~/Documents/projects/Unity/Alone/AloneV2`, Unity 6000.4.0f1, URP 17.4, Crest fork). Full analysis: [integration/sailing-game-audit.md](integration/sailing-game-audit.md); matrix: [integration/requirements-matrix.md](integration/requirements-matrix.md); workflow: [integration/production-workflow.md](integration/production-workflow.md); plan: [integration/implementation-plan.md](integration/implementation-plan.md). Verified = measured or run in the audit; everything else is read or assumed (see the audit, section 9).

- Verified: ALONE's nine packs are flat (all sampled terrain y = 0) with no bathymetry; Crest sea-floor depth is off; `BoatGrounding` is land-only. OB bathymetry is the real gap it can fill.
- Verified: ALONE tiles use a per-leg azimuthal-equidistant frame; true-geo data sits 67 to 673 m off the rendered tiles at 1 to 10 km from the Mindelo anchor. OB data must be exported in the game's frame (P-3).
- Verified: GSHHG (game) vs OSM (OB) coast at Falmouth: median 111 m, p90 259 m. Needs the coast decision R3.
- Verified: per land cluster (110 to 200 km) a world fits one UTM zone (one exception) and OB's in-memory model; corridor-wide worlds do not (81 to 309 M cells).
- Next, in order (plan IDs): D-1 decisions, P-1 (this is item A above), P-3 game-frame grid + data-only export, P-5 runtime `DepthSampler`, P-4 conform to the game's coast, P-6 seamarks in frame, then game-side G-7/G-1/G-4, then the Falmouth gate P-7 (M0). M1 = M0 + Mindelo from one region file.
- Re-ranked: floating origin for OB's streamer, LOD pyramid, OB Terrain importer improvements and water shader drop in value for the game (not needed for M1).

## Next (highest value first, re-ranked 2026-10-30 session 2)

A. Per-cell source layer per tile (uint8: GMRT survey, GEBCO fill, regional, regional-blend, conformed, refined, nodata) with tile hashes and validator checks against manifest stats; prerequisite for honest LOD downsampling and water shading. Inputs (`meas`, `replace`/`dist`, conform and refine decisions) already exist in `Build`.
B. Standalone player build of the URP demo with Addressables (untested).
C. LOD design (after A).
D. Mindelo offline regression fixture (needs local regional-source support, currently rejected).
Done this session: source lock (`-update-lock`), verified cache hits, `regional_release`, regional status/caveats.


0. ~~LibreTerrain round trip~~ done 2026-10-30 (`ocean libreterrain ...`, ADR 0003, numbers in research/libreterrain.md). Remaining: open the exported `.lterrain` in the GUI app; masks/tiles exchange not attempted.
1. ~~EMODnet regional fill~~ done as opt-in `regional_bathymetry` (README): fill-only, offset measured on flat shared cells, refused when it cannot be validated. Remaining: NOAA CUDEM for the US, per-cell source id and confidence layer (currently only counts), vertical datum conversion (offset is reported, not applied), a better offset story where survey cells are all steep (Mindelo).
2. ~~Coastline-driven land/water mask~~ done as opt-in `coast_conform` (README). Remaining: apply on the 20-tile Mindelo world, decide default, handle sub-cell creeks, tidal range.
3. Remaining build time on small worlds is the GDAL subprocesses (2× gdalwarp, ogr2ogr); profile before optimising further. Tile-corner transforms are already batched (fixture build 1.97 s → 1.60 s).
4. Windowed processing so global-scale areas do not need the full grid in memory.
5. LOD pyramid (levels > 0) with deterministic ids and tests that levels agree at shared vertices.
6. Unity: ~~depth-class Terrain layers, tile prefabs, index, Addressables registration~~ done (README). Runtime load from built bundles is tested in editor play mode. Remaining: a standalone player build test, (streaming: `OceanTileStreamer` done, needs floating origin and timing/memory measurements),  water shader, LOD tiles, real textures.
7. ~~Harbour and seamark features as GeoJSON~~ done (`seamarks` config flag, README). Unity index entries and scene markers done. Remaining: marker prefabs, light characteristics parsing.
8. Unreal Landscape exporter (resampled to 2ⁿ−1 components).
9. Incremental builds keyed on input hashes and config (the cache already keys downloads).
10. Multi-zone worlds.

## From the ecosystem research (docs/research/ecosystem.md, 2026-10-30)

- Seascape (openwatersio, BSD-3 code, CC BY 4.0 tiles, Terrarium PNG tiles over HTTPS) as an optional fallback bathymetry source; its source catalogue is the regional-source list. Datum varies per source.
- EMODnet DTM 2024 (~115 m, from research, not re-measured here) via WCS and NOAA CUDEM via the public S3 bucket, fetched through GDAL. EMODnet licence text still to read; ~115 m will not beat GMRT's delivered 39 m at Falmouth, so value is elsewhere.
- Engine-exact tile sizes: Unreal needs 2ⁿ−1-style sizes and a Z-scale (default 100 covers only ±256 m) written to the manifest.
- OSM seamarks via the existing Overpass client; verify the `seamark:type` list first. Seamap is GPL-3.0: reference only.
- Cesium plugins are Apache-2.0: possible later output target, not a core dependency.

## Open questions

- gen-map: no repo found; ask the user for a URL.
- ODbL treatment of built worlds containing OSM-derived data: now also OSM-derived heights at coast-conformed cells (`coast_conform`) and seamark points, not only masks.
- Vertical datum for GMRT land vs. multibeam: how to quantify per region.

## Known problems

- Unity: URP only by decision (Built-in and HDRP are not supported). Verified on URP 17.4.
- Coastline-to-DEM offset is reported but not corrected.
- Preview colours are a fixed ramp; not meant as a chart.
- Unity screenshots work in batchmode without `-nographics` (needs a GPU session); with `-nographics` there is no rendering. URP's far-LOD terrain base map never generates in batchmode, so the screenshot code raises `basemapDistance` in memory.
