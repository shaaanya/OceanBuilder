# OceanBuilder 0.3.0-rc1 release report

Date: 2026-11-01. This is a release candidate, not a finished 0.3.0: Areas E (LOD) and F (source adapters) and most of H (Unity) were not started. Statuses use the words below, and nothing is listed as verified that was not run.

- **Verified**: run in this repository by `go test ./...` or by a command whose output is in the development log.
- **Partial**: works with stated limits.
- **Unverified**: written, not exercised against the real thing.
- **Deferred**: not started.

## Verified

- Multiple elevation datasets per world, precedence listed/auto, per-sample dataset layer, overlap and seam statistics, nodata hole fall-through (`internal/world/compose_test.go`). Legacy configs reproduce 0.2.0 tile hashes (`testdata/golden`, plain and LOD builds).
- Vertical policy: refusals for mixed/unknown/tidal/MSL/unknown names/missing offset source; EGM96→EGM2008 shift agrees with `gdaltransform` to 0.02 m on a synthetic grid; a double-conversion bug was found by this test and fixed.
- Window-size independence (0/1/2/3 tiles per window), byte-identical output.
- Local transverse Mercator: planning at a zone boundary, wide box, equator, antimeridian; the reader's projection against PROJ at three CRSs (< 1 cm); an end-to-end synthetic build across 180 that validates and samples correctly (local GeoTIFF source).
- Validator: dataset layer hashes, recount of `cells_by_dataset`, tamper detection.
- Memory/time on a synthetic source at 85 M and 325 M samples with `-mem 256` (numbers and caveats in the development log): Go process 247-277 MB, gdal 200-290 MB, 72 s for 325 M samples.
- Bug fixes: conformed/refined source codes (0.2.0), vertical double conversion, GDAL block cache unbounded.

## Partial

- Antimeridian: local GeoTIFF only, no OSM coastline.
- Resume: only the warp stage cache is reused; no mid-pipeline resume.
- Datum conversions: EGM96, EGM2008, WGS 84 ellipsoid only. The EGM96 grid on this machine is 0.37-0.40 m off PROJ's published values at two points (cause unknown).
- Measurements are synthetic flat data on one machine, single runs.

## Unverified

- Anything involving a network source across 180 (the config refuses it).
- Unity importer with schema 3 worlds; LibreTerrain exchange with schema 3 (assumes EPSG).
- The source-probe findings ([research/source-probes.md](research/source-probes.md)) were gathered by a research subagent on 2026-11-01 and not re-checked by the maintainer.

## Deferred

- LOD alternatives, error measurements, narrow-channel/island tests (Area E).
- Adapters for ETOPO, GEBCO, CUDEM, NOAA mosaic, Seascape (Area F); fixtures from the probes are in scratch, not in `testdata/`.
- Licensing metadata in the manifest and the OSM-coastline opt-in decision (Area G); the analysis is in [research/licensing-analysis.md](research/licensing-analysis.md) and is not legal advice.
- Unity LOD import, diagnostics, standalone player build (Area H).
- Tutorials for multi-source worlds and the `sources explain` command.
- Real-region measurements (fjord, volcano, coral, bank) with real data; only synthetic and the 0.2.0 Falmouth/Moorea numbers exist.

## Compatibility

0.2.0 configs build byte-identical tiles. Behaviour changes: a box wider than one UTM zone's 0.5 % limit used to be refused and is now planned as a local grid unless `projection: "utm"` ([ADR 0006](decisions/0006-local-transverse-mercator-grid.md)); manifests gain `datasets`, `format_revision`, `tiles[].dataset`; schema 3 only for local-grid worlds. Readers that stop at unknown fields are unaffected for UTM worlds.

## Release criteria status

go vet, go test: pass. Real-data tests: not re-run (offline suite uses the Falmouth fixtures). Determinism: tested. Export: tested (GeoTIFF). Unity tests: not run this session. Docs/ADRs: updated (spec, ADR 0006-0008, roadmap, dev log). Clean tree: yes after this commit.
