# OceanBuilder 0.3.0-rc1 release report

Date: 2026-11-01. This is a release candidate, not a finished 0.3.0: Areas E (LOD) and F (source adapters) and most of H (Unity) were not started. Statuses use the words below, and nothing is listed as verified that was not run.

- **Verified**: run in this repository by `go test ./...` or by a command whose output is in the development log.
- **Partial**: works with stated limits.
- **Unverified**: written, not exercised against the real thing.
- **Deferred**: not started.

## Verified

- Multiple elevation datasets per world, precedence listed/auto, per-sample dataset layer, overlap and seam statistics, nodata hole fall-through (`internal/world/compose_test.go`). Legacy configs reproduce 0.2.0 tile hashes (`testdata/golden`, plain and LOD builds).
- Vertical policy: refusals for mixed/unknown/tidal/MSL/unknown names/missing offset source. The EGM96→EGM2008 shift agrees with `gdaltransform` to 0.02 m on a synthetic grid: this checks that the builder and PROJ use the grid the same way (and the EGM96 value at 12.5E 55.5N from the compound-CRS path, 36.021 m, equals a direct `cct` vgridshift lookup of the same grid file), not that the geoid values are geodetically correct. The test found and fixed a double-conversion bug.
- Window-size independence (0/1/2/3 tiles per window), byte-identical output.
- Local transverse Mercator: planning at a zone boundary, wide box, equator, antimeridian; the reader's projection against PROJ at three CRSs (< 1 cm); end-to-end synthetic builds across 180 with the default projection (UTM zone 1, schema 2) and with a forced local grid (schema 3): both validate, tile envelopes stay under 1 degree (corner longitudes are unwrapped onto the bbox-west branch, `bounds_wgs84` east can exceed 180), the reader answers on both sides. Network configs across 180 are refused before any fetch (offline test). The GeoTIFF export of a schema 3 world carries the transverse Mercator CRS.
- Validator: dataset layer hashes, recount of `cells_by_dataset`, tamper detection.
- Memory/time on a synthetic source at 85 M and 325 M samples with `-mem 256` (numbers and caveats in the development log): Go process 247-277 MB, gdal 200-290 MB, 72 s for 325 M samples.
- Bug fixes: conformed/refined source codes (0.2.0), vertical double conversion, GDAL block cache unbounded.

## Partial

- Antimeridian: local GeoTIFF only, no OSM coastline.
- Resume: only the warp stage cache is reused; no mid-pipeline resume.
- Datum conversions: EGM96, EGM2008, WGS 84 ellipsoid only. The EGM96 grid on this machine gives 36.021 m at 12.5E 55.5N where PROJ's regression suite (older `.gtx` file, which its own comments call a "widespread bad egm96 file") has 36.394 m; the builder agrees with the installed grid, and which file is closer to NGA's official values was not established.
- Measurements are synthetic flat data on one machine, single runs.

## Unverified

- Anything involving a network source across 180 (the config refuses it).
- Unity importer and LibreTerrain exchange with schema 3 worlds: both now refuse such worlds with an explanation (tested), as does refinement on a local-grid world.
- `gdal.CacheMB` is process-wide (set per build; concurrent builds with different `-mem` would share one value, and an exported `GDAL_CACHEMAX` is overridden).
- The source-probe findings ([research/source-probes.md](research/source-probes.md)) were gathered by a research subagent on 2026-11-01 and not re-checked by the maintainer.

## Deferred

- LOD alternatives, error measurements, narrow-channel/island tests (Area E).
- Adapters for ETOPO, GEBCO, CUDEM, NOAA mosaic, Seascape (Area F). The probe fixtures stayed in the session scratch directory and are not preserved; re-fetch them if an adapter is started.
- Licensing metadata in the manifest and the OSM-coastline opt-in decision (Area G); the analysis is in [research/licensing-analysis.md](research/licensing-analysis.md) and is not legal advice.
- Unity LOD import, diagnostics, standalone player build (Area H).
- Tutorials for multi-source worlds and the `sources explain` command.
- Real-region measurements (fjord, volcano, coral, bank) with real data; only synthetic and the 0.2.0 Falmouth/Moorea numbers exist.

## Compatibility

0.2.0 configs build byte-identical tiles. Behaviour changes: a box wider than one UTM zone's 0.5 % limit used to be refused and is now planned as a local grid unless `projection: "utm"` ([ADR 0006](decisions/0006-local-transverse-mercator-grid.md)); manifests gain `datasets`, `format_revision`, `tiles[].dataset`; schema 3 only for local-grid worlds. Readers that stop at unknown fields are unaffected for UTM worlds.

## Release criteria status

go vet, go test: pass (run after the last code change). Real-data tests: not re-run (offline suite uses the Falmouth fixtures). Determinism: tested. Export: tested (GeoTIFF). Unity tests: not run this session. Docs/ADRs: updated (spec, ADR 0006-0008, roadmap, dev log). Clean tree: yes after this commit.
