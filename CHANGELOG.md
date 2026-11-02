# Changelog

## 0.4.0-rc1 (2026-11-02)

See [docs/release-0.4.0.md](docs/release-0.4.0.md). Release candidate: CI for Linux and Windows is written, never run.

- Stage cache: warp, vertical shift, compose and every LOD level are sealed, hash-verified stages; an interrupted build resumes at the first unfinished stage ([docs/resume.md](docs/resume.md)). Fixed: a source with an explicit nodata value never hit the warp cache.
- Antimeridian: GMRT, ETOPO 2022 and GEBCO worlds across 180 are fetched as two boxes (verified live); CUDEM, EMODnet, OSM coast, seamarks and remote GeoTIFFs are refused with a message.
- GMRT: delivered cell size is measured; oversized requests are split; `datasets[].delivery`, `source_res_m` is the coarsest piece, validator warning `source-coarsened`.
- EGM96: discrepancy resolved against an independent synthesis ([research](docs/research/egm96-discrepancy.md)); regression tests tightened from 0.5 m to 2 mm / 0.10 m.
- `ocean lod-report`: land/water loss, shallow-water rms, depth-class disagreement, gradient error, `-max-*` thresholds ([docs/lod-quality.md](docs/lod-quality.md)).
- `ocean sources explain <id>`; ADRs 0009 (NOAA mosaic not adopted) and 0010 (Seascape, GEBCO).
- Unity: LOD player smoke (macOS), importer file verification, exporter copies LOD levels (`export unity -level N`), checkpoint tolerance scaled to the 16-bit step.
- Validator: the `background-fill` warning no longer prints a 0 m resolution for sources that document none.
- Tests/CI: independent verification scripts (`scripts/`), `.github/workflows/ci.yml`.

## 0.3.0 (2026-11-01)

See [docs/release-0.3.0.md](docs/release-0.3.0.md) for what was verified, partial, unverified and deferred.

- Several elevation datasets per world with `listed`/`auto` precedence and a per-sample dataset layer; vertical datum policy with PROJ grid conversions and explicit refusals; `licensing` record in the manifest and `no_coastline_copy`.
- Local transverse Mercator grids (schema 3) for wide boxes and boxes across the antimeridian; windowed disk-backed pipeline with `-mem`.
- LOD: `ocean lod-report`, `lod_method: "shoal"`.
- Sources: ETOPO 2022, GEBCO (with TID survey mask), CUDEM; `ocean world check` shows per-source datum and conversion offline.
- Unity: coarser LOD levels imported (`-oceanMaxLevel`), standalone player build helper and headless smoke component.
- Fixed: `grid.bbox_wgs84` is the level-0 extent (it was the union over all LOD levels); vertical double conversion; unbounded GDAL cache.
- Changed: boxes over the UTM scale limit are planned as a local grid instead of refused (`projection: "utm"` restores the refusal).

## 0.1.0 (unreleased)

- First vertical slice: GMRT + OSM ingestion, UTM normalisation, shared-edge tiling, depth/shore masks, manifest, validator, preview, Unity export and importer.
