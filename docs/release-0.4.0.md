# Ocean Compiler 0.4.0-rc1: release report

Date: 2026-11-02. Machine: macOS arm64, GDAL 3.13.0, PROJ 9.x with `us_nga_egm96_15.tif` / `us_nga_egm08_25.tif`, Go 1.26.

**Why a release candidate and not 0.4.0.** The release criteria ask for a CI path on three operating systems. The workflow
exists (`.github/workflows/ci.yml`) but the repository has no remote and has never run it; nothing was executed on Linux or
Windows. Everything below marked "verified" was verified on macOS arm64 only. All other criteria are met or bounded, with
the evidence listed here.

## Status table

| Item | State | Evidence |
|---|---|---|
| Unity LOD import in a standalone player | **verified, macOS only** | player smoke checks LOD tile positions, sizes, heights, shared edges, vertices against level 0, load/unload, Addressables by address; importer verifies files and levels; negative cases (missing, corrupt, unsupported level) refused. Commit 91446b6, [development log](development-log.md). Not run: Windows/Linux players, IL2CPP; no runtime LOD *selection* logic exists in the Unity adapter (levels are loadable, not chosen by distance) |
| Real build across a UTM zone edge, equator, both hemispheres | **verified** | Galapagos, 200 m, 52 tiles; independent pyproj check (`scripts/verify_geo.py`): tile bounds < 0.1 mm, scale factors match, elevation rms 5.8 m with position control |
| Antimeridian, network sources | **verified**: GMRT, ETOPO 2022, GEBCO. **refused with a clear message**: CUDEM, EMODnet, OSM coast, seamarks, remote GeoTIFF | Fiji 179.8-180.25: 328 833 / 328 833 cells, 800 vertices vs raw rms 0.41 m; ETOPO/GEBCO 60 points each rms 1.76 / 1.43 m (no shift control for these two); offline fake-server tests for both GMRT labelling conventions |
| EGM96 0.4 m discrepancy | **resolved, bounded** | installed grid = independent spherical-harmonic synthesis to 0.1 mm; PROJ gie expectation unexplained and not reproduced; residual 6 cm interpolation. [research/egm96-discrepancy.md](research/egm96-discrepancy.md); old 0.5 m test replaced by 2 mm / 0.10 m tests |
| Large real build | **one measured: 4 x 3 degrees** | Hawaii, GMRT, 6 pieces, 656 s of which 652 s download, Go RSS 285 MB, cache 581 MB, stages 37 MB, output 30 MB, validate 2.4 s / 134 MB. A 6 x 5 degree run was stopped for disk. No ETOPO/GEBCO large ladder (see deferred) |
| Source metadata reflects the actual download | **verified** | `datasets[].delivery` (reference cell, pieces, finest/coarsest cell, coarsened pieces); `source_res_m` = coarsest piece; validator `source-coarsened`; fake service that coarsens oversized requests |
| Resume across several stages, corrupt-cache detection, determinism | **verified** | warp, vertical shift, compose and every LOD level are sealed, hash-verified stages; tests cut a build at every stage boundary, damage every artifact kind, and compare all output bytes across cache/output dirs, GOMAXPROCS=1 and memory budgets; verification mutation-checked. [resume.md](resume.md). Found and fixed a cache-key bug (pointer printed as an address) |
| LOD quality: measured, thresholds, trade-offs | **implemented, measured on one real fixture + one synthetic scene** | [lod-quality.md](lod-quality.md); `ocean lod-report -max-*` |
| Source transparency | **implemented** | `ocean sources explain <id>` (datum, resolution, measured/interpolated, antimeridian, last live check, limits); NOAA mosaic rejected with live evidence ([ADR 0009](decisions/0009-noaa-dem-mosaic-not-adopted.md)); Seascape and direct GEBCO decided ([ADR 0010](decisions/0010-seascape-and-direct-gebco.md)); a validator message that printed "0 m resolution" now says what is known |
| CI for Linux / Windows / macOS | **written, never run** | `ci.yml`: offline matrix, cross-target vet and `test -c` for 5 targets, scheduled network tier. Locally: `GOOS=windows|linux go vet`, per-package `go test -c`, and the whole suite as `GOARCH=amd64` under Rosetta pass |
| `go vet ./...` and `go test ./...` | **pass** locally | see the final line of this document |

## Format and compatibility

- Additive: `datasets[].delivery`; `resampling.source_res_m` is now the coarsest delivered piece (a coarsened GMRT request now reports a larger value than 0.3.0 did; that is the correction); validator code `source-coarsened`.
- Behaviour: a world with east > 180 that uses GMRT, ETOPO or GEBCO is now fetched as two boxes (0.3.0 would have clamped at 180). The tool version is part of the build id, so every 0.3.0 world rebuilds once.
- CLI: `export unity` `-level N` also selects the highest LOD level copied; `lod-report` flags; `sources explain`. No removed flags.
- Cache: stage files gain `.seal` sidecars and `stages/<name>-<key>/` directories; 0.3.0 stage files without a seal are ignored and rebuilt. `ocean cache clean -stages` clears everything rebuildable.
- Breaking: none known. Output bytes change only through the version in the build id and the items above.

## Reproduce

```
go vet ./... && go test ./... -timeout 30m          # offline; ~10 min on a loaded laptop
OCEAN_SMOKE=1 go test ./internal/etopo ./internal/gebco ./internal/cudem -run Smoke -v   # live services
go build -o bin/ocean ./cmd/ocean
bin/ocean build <world.json> && bin/ocean validate <out> && bin/ocean lod-report <out>
python3 scripts/verify_geo.py <world dir> <gmrt cache dir>      # needs pyproj, numpy
```

Hawaii: `bbox [-157,18.5,-153,21.5], cell_m 200, tile_cells 256, coastline none`, `/usr/bin/time -l bin/ocean build`.

## Deferred, with reasons

| Item | Why |
|---|---|
| Linux / Windows execution, Unity players there | no such machine and no CI remote in this environment |
| Large ladder with ETOPO / GEBCO and a region above 4 x 3 degrees | disk budget on this machine (the 6 x 5 degree GMRT attempt filled it); the one measurement shows download time dominates |
| Sub-stage checkpoints; resume of tile writing and of the whole-grid steps | every stage that dominates cost on measured builds is cached; the rest is seconds (4.2 s after the last download on Hawaii) |
| Progress / ETA on large grids | not started |
| Unity runtime LOD selector, crack hiding between levels, schema 3 in Unity | outside this cycle's verification scope; the player smoke proves loading and consistency only |
| NOAA multibeam / BAG ImageServers | each has its own datum story; needs the same live probing as ADR 0009 |
| NAVD88 / MHW / tidal-datum transformations | no verified PROJ operation here; sources in those datums stay refusable or `allow_mixed` |
| Islet / channel *connectivity* metrics for LOD | `lod-report` counts vertices that change side of the waterline, not components |
| ETOPO / GEBCO shifted-position control across 180 | only absence of gap or bias at the seam was shown |

## Not done on purpose

No external game project was inspected, modified or integrated. No datum, coordinate or depth is converted silently: every
conversion carries its PROJ pipeline and grid hashes, and unverifiable ones are refused. Nothing is claimed navigation-safe.
