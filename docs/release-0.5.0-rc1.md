# 0.5.0-rc1: integration readiness and runtime consumption

Date: 2026-11-02. **Release candidate, not 0.5.0**: hosted CI (Linux, Windows, macOS) has never run because the repository has no remote and none was created; Windows and Linux are verified by cross-compilation only; the Unity runtime LOD ran on macOS arm64 only. Everything below says what was run; nothing is claimed from reading code.

## Environment of every run

macOS 27 (Darwin 27.0.0) arm64, 10 cores, Go 1.26.1 (`go.mod` says 1.22; `go vet` also passed under go1.22.12 in an earlier session), GDAL 3.13.0, PROJ from the same install, Unity 6000.4.0f1 (URP 17.4, Addressables 4.1.2, Mono), .NET SDK 10.0.103, Python 3.13 (reader and scripts, standard library only), about 4 GiB free disk. No Docker, no Windows or Linux machine, no remote.

## Gap analysis at the start of the session (0.4.0-rc1) and where each item ended

| Bucket | Item | End state |
|---|---|---|
| Implemented and verified | build pipeline, stage cache and resume, determinism, validator, Go reader, GeoTIFF export, antimeridian for GMRT/ETOPO/GEBCO, vertical policy | unchanged; `go test ./...` green again at 0.5.0-rc1 |
| Implemented, not independently verified | Unity importer/streamer on Falmouth only | now run on two more worlds, see below |
| Partial | Unity LOD "loaded in a player but never selected" | **done**: selector, streamer, skirts ([unity-lod.md](unity-lod.md)) |
| Partial | CI written, never run | still never run; local procedure and extra jobs added; cross-compile verified |
| Not implemented | consumer docs for non-Go readers | done: [integration.md](integration.md) + Python reference reader |
| Not implemented | packaging, version reporting, checksums | done locally: [install.md](install.md); nothing published |
| Not implemented | disk preflight, region ladder, profiling | done: [performance.md](performance.md) |

## What was added

1. **Unity runtime LOD** (priority A). Selection by distance with hysteresis, 2:1 balance, several viewers, missing levels; async streaming with `maxInFlight`, retry with backoff, cancel, unload delay, cover-until-loaded; Addressables loader behind `IOceanTileLoader`; static skirts hiding mixed-level cracks; colliders on level 0 only. Details, settings and limits: [unity-lod.md](unity-lod.md).
2. **Test tiers and cross-platform preparation** (B): `-short` tier, `scripts/verify-local.sh {fast,full,cross,scripts,lod,smoke}`, CI jobs `posix-scripts` and `unity-lod-logic`, [testing.md](testing.md) including a Windows hazard audit.
3. **Consumption without Unity** (C): [integration.md](integration.md), `examples/python-reader/ocean_reader.py` (standard library only: manifest, PNG decoder, transverse Mercator, bilinear sampling, conformance check), `scripts/reader_crosscheck.py`.
4. **Packaging** (D): `ocean version` (commit, Go, GDAL, platform), `scripts/release.sh` (six targets, `SHA256SUMS`, reproducibility check), pinning and cache docs.
5. **Performance** (E): `scripts/ladder.py`, `OCEAN_CPUPROFILE`, disk-space and GDAL preflights, measured constants ([performance.md](performance.md)).
6. **Data honesty fixes** (F): level 0 always complete; Overpass answers reused across mirrors (a warm build could silently take different coastline data); unique download temp names.
7. Documentation: install, integration, testing, performance, troubleshooting, unity-lod, ADR 0011, tutorials regenerated from real runs, README, roadmap, changelog, development log.

## Verification actually run

| What | Result |
|---|---|
| `go vet ./...` and `go test ./... -count=1 -timeout 30m` at 0.5.0-rc1 (macOS arm64) | pass, 15 packages (about 3.2 min; `internal/world` 187 s) |
| `go test -short ./...` | pass, about 75 s; also pass for darwin/amd64 under Rosetta (golden tile hashes included) |
| cross-compile (vet + build + test binaries) linux/windows/darwin x amd64/arm64 | pass; tests executed only on darwin/arm64 and (short tier) darwin/amd64 under Rosetta |
| `dotnet run --project tools/lodtests -c Release` | 6635 checks, 0 failures; steady-state Select+Cover allocates 40 bytes per step |
| Python reader `--check` on 7 real worlds + 2 offline worlds (UTM and local tmerc, schema 2 and 3) | all OK (hashes, checkpoints, shared edges, parent samples) |
| Python reader vs `ocean sample`, 40 points x levels 0 and 2 per world (same rounded coordinates to both) | worst difference 2.3e-9 m over 7 real + 2 offline worlds, nodata agrees (an earlier run with unrounded coordinates showed 9.9e-4 m on steep geiranger: the test, not the reader) |
| Python tmerc projection vs `gdaltransform` | below 1e-8 m at 3 points |
| Interrupt/resume (SIGINT, SIGKILL) and 3 concurrent builds, `scripts/verify-local.sh scripts` | pass |
| Unity Editor PlayMode (`OceanLodSmokeTests`, runtime tests) on a Falmouth-derived 5-level world and on kona (real, levels 0-3) | pass, exit 0 |
| Unity package version 0.5.0-rc1 loaded by Unity 6000.4 (re-exported, imported, PlayMode and player re-run after the version and the allocation changes, kona) | pass |
| Unity standalone macOS arm64 player, graphics on, same two worlds | `OCEAN_PLAYER_RESULT failures=0`; synthetic seam 4317 -> 0 background pixels with skirts |
| Region ladder, 7 regions, cold and warm | all built and validated; numbers in [performance.md](performance.md) |
| Disk preflight with a 400 M-cell config on 1.9 GiB free | refused immediately (before any download), nothing written |

### Validation matrix (7 regions, scripts/validate_matrix.py)

| Region | Kind | Validate | Warnings (data notes) | Provenance fields | ATTRIBUTION | LOD report | Python reader | Rebuild x2 |
|---|---|---|---|---|---|---|---|---|
| geiranger | fjord | exit 0 | background-fill, discontinuity, low-res-bathymetry | complete | yes | exit 0 | OK, agrees | byte-identical |
| kona | volcanic coast | exit 0 | background-fill | complete | yes | exit 0 | OK, agrees | byte-identical |
| moorea | coral island | exit 0 | background-fill, low-res-bathymetry, upsampled | complete | yes | exit 0 | OK, agrees | byte-identical |
| bahamas-bank | shallow bank | exit 0 | background-fill, low-res-bathymetry | complete | yes | exit 0 | OK, agrees | byte-identical |
| mindelo | harbour | exit 0 | background-fill, coast-misaligned, low-res-bathymetry, upsampled | complete | yes | exit 0 | OK, agrees | byte-identical |
| azores-regional | large regional, 1.4 M cells | exit 0 | background-fill, low-res-bathymetry | complete | yes | exit 0 | OK, agrees | byte-identical |
| taveuni-180 | antimeridian (UTM 1S, 179.75 to 180.35 E) | exit 0 | background-fill, low-res-bathymetry, no-coastline | complete | yes | exit 0 | OK, agrees (points on both sides) | byte-identical |

LOD quality of the same worlds (`ocean lod-report -json`, vertex-decimation; level 1 doubles the cell; "flip" = a sample whose land/water class differs from level 0 at the same place):

| Region | Cell | L1 rms (m) | L1 shallow-water rms (m) | L1 land/water flips | L1 depth-class disagreement | L2 rms (m) | L2 flips |
|---|---|---|---|---|---|---|---|
| geiranger | 32 | 7.5 | 7.0 | 0.03 % | 0.10 % | 17.6 | 0.09 % |
| kona | 100 | 2.9 | 7.4 | 0.17 % | 0.53 % | 6.3 | 0.40 % |
| moorea | 48 | 3.3 | 2.4 | 0.11 % | 0.27 % | 9.2 | 0.45 % |
| bahamas-bank | 100 | 2.2 | 1.3 | 0.28 % | 0.83 % | 4.9 | 0.63 % |
| mindelo | 48 | 2.6 | 1.0 | 0.08 % | 0.24 % | 6.7 | 0.27 % |
| azores-regional | 250 | 6.4 | 8.5 | 0.02 % | 0.09 % | 16.0 | 0.05 % |
| taveuni-180 | 100 | 12.3 | 4.5 | 0.53 % | 1.04 % | 22.7 | 1.30 % |

These are the costs of coarse levels, not pass/fail results: no threshold was set. Coarse samples are exact level-0 samples, so the errors come from aliasing of steep relief (geiranger, taveuni, azores slopes); shallow-water rms of 7-8 m at level 1 on kona and azores means coarse tiles can show water deeper than the fine ones near shoals (`lod_method: "shoal"` trades that, see [lod-quality.md](lod-quality.md)).

"Rebuild" = two further builds from the cache; their trees are identical to each other and to the first build. "LOD report exit 0" only means `ocean lod-report` ran: no `-max-*` threshold was set, so this column is **not** a quality gate; the numbers are in the report. Unity (runtime LOD) was run on **two** of these worlds' kind only: kona (volcanic coast, 9 level-0 tiles) and a Falmouth-derived world. The fjord, coral island, shallow bank, large regional and antimeridian worlds were **not** imported into Unity. Schema 3 cannot be imported at all.

## Known limits

- No hosted CI, no Windows or Linux execution, no IL2CPP/mobile; interrupt behaviour on Windows unverified.
- Skirt margin derived from a synthetic test; not tuned at game view distances; no player frame-time or GC profile; no floating origin.
- Memory figures cover up to 1.4 M cells; the 6 x 5 degree attempt that filled the disk was not repeated; disk constants come from seven builds on one machine.
- GMRT is always downloaded at its finest resolution (25 x more than needed in the large case).
- `SkirtSides` ignores a neighbour whose same-level tile is missing while finer ones exist.
- Cross-machine reproducibility of the release binaries is untested (same machine, same tree only).
- Datum handling unchanged: no NAVD88/MHW transformations were added (none can be verified here).
- Two warm-build outliers (geiranger 41 s, azores 46 s) are probably Overpass retries and cold file reads; the Overpass part is fixed, the azores one is not explained.

## Deferred

CI execution (needs a remote), Windows/Linux runs, Unity on the other regions and platforms, schema 3 in Unity, player profiling, GMRT resolution matching, larger-than-1.4 M-cell regions, NAVD88/MHW.

## Recommendation

Ship **0.5.0-rc1** as a release candidate. Promote to 0.5.0 after (1) the CI matrix has run green on Linux and Windows, (2) the Unity player smoke has run on Windows or Linux, and (3) one larger region (above 2 M cells) has been built on a machine with room. Do not call it 0.5.0 before those.
