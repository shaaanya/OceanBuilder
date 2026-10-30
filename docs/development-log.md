# Development log

## 2026-10-30: first vertical slice

Done (all verified by running it):

- Repo, Go module `github.com/ocean-compiler/ocean`, CLI: `build`, `validate`, `preview`, `export`, `sources list`, `world init`, `cache stats|clean`, `version`.
- Real data: GMRT topo + topo-mask and OSM coastline for Falmouth (16 tiles) and Mindelo (20 tiles).
- Normalisation onto a UTM vertex grid, shared-edge tiles, depth classes, shore masks, manifest with provenance, attribution file.
- Validator: missing/corrupt tiles (hash), ids, bounds, edge cracks, nodata, 60° slope steps, survey-backed fraction, upsampling, coastline offset, provenance/licence.
- Unity 6000.4.0f1: batchmode import of Falmouth produced 16 linked terrains and a saved scene (`OCEAN_IMPORT_OK`).
- Tests (offline, real-data fixtures): config, bilinear/nodata, shoreline search, UTM zones, projection scale, OSM conversion, build + validate, determinism, tamper detection, crack detection, nodata reporting. Benchmarks for build and validate.

Findings:

- GMRT `topo-mask` is NaN, not a byte mask (my first reading of it as bytes gave 0% surveyed; fixed after reading the GridServer docs).
- Overpass returned HTTP 406 without a User-Agent. It is reachable from Go; some sandboxed tools had no network.
- No repository named LibreTerrain turned up in web searches; see ecosystem.md.
- Falmouth: OSM coastline is a median 101 m from the DEM shoreline (p90 268 m). Mindelo: 142 m / 331 m. The DEM is 39–58 m resolution and includes a tidal range, so this is expected order of magnitude, but not yet separated into datum vs. horizontal causes.

Known problems: see [roadmap](roadmap.md).
