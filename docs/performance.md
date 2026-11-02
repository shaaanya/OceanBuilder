# Performance, memory, disk, download volume

Measured 2026-11-02 on one machine (macOS 27 arm64, 10 cores, Go 1.26.1, GDAL 3.13.0, home broadband, 4 GiB free disk), by `scripts/ladder.py`: each region is built twice with `-force`, first with an empty cache ("cold": real downloads from GMRT and Overpass) and then with the cache it just filled ("warm"). One run per cell of the table, so there is no variance estimate: network timings in particular vary from run to run. Peak memory is the `ocean` process plus the GDAL processes it waited for (`wait4` rusage). Peak disk is the size of everything under the scratch directory sampled every 0.25 s (a shorter spike can be missed). "Download" is the cache without the rebuildable stage rasters.

| Region | Why | Cell | Level-0 cells | Tiles (levels) | Cold | Warm | Peak RSS | Peak disk | Download | Output | Validate |
|---|---|---|---|---|---|---|---|---|---|---|---|
| kona | volcanic flank | 100 m | 0.15 M | 20 (6) | 104 s | 2.0 s | 56 MiB | 14 MiB | 8.7 MiB | 1.6 MiB | <0.1 s |
| geiranger | fjord | 32 m | 0.33 M | 38 (6) | 99 s | 41 s (2.4 s on a re-run) | 53 MiB | 15 MiB | 5.8 MiB | 2.6 MiB | 0.1 s |
| moorea | coral island | 48 m | 0.41 M | 51 (7) | 73 s | 2.0 s | 78 MiB | 19 MiB | 4.2 MiB | 4.1 MiB | <0.1 s |
| mindelo | harbour | 48 m | 0.33 M | 35 (6) | 53 s | 1.7 s | 50 MiB | 11 MiB | 2.6 MiB | 2.4 MiB | <0.1 s |
| taveuni-180 | antimeridian, schema 2 UTM 1S | 100 m | 0.33 M | 35 (6) | 21 s | 2.1 s | 59 MiB | 18 MiB | 13.3 MiB | 2.4 MiB | <0.1 s |
| bahamas-bank | shallow bank | 100 m | 0.33 M | 36 (5) | 81 s | 2.0 s | 68 MiB | 22 MiB | 11.8 MiB | 2.8 MiB | <0.1 s |
| azores-regional | large regional, 4 x 1.7 degrees | 250 m | 1.38 M | 123 (6) | 1659 s (27.6 min) | 46 s (5.6 s on a re-run) | 320 MiB | 410 MiB | 383 MiB | 8.1 MiB | 0.1 s |

All seven built, validated (exit 0, "BUILT WITH WARNINGS": the usual `nodata`, `low-res-bathymetry` and `upsampled` notes, see each world's report). The 6 x 5 degree Hawaii attempt that filled the disk was **not** repeated.

## What the numbers say

- **Time is the network and GDAL, not Go.** CPU profiles (`OCEAN_CPUPROFILE=file ocean build ...`, then `go tool pprof`) of warm geiranger and warm azores show 140 ms and 680 ms of Go CPU in 2.5 s and 5.7 s of wall time; what is left is reading and hashing cached files and waiting for GDAL. Cold time is dominated by GMRT (up to 27 minutes for the large request) and Overpass.
- **Two warm outliers are unexplained.** The first warm geiranger took 41 s and the first warm azores 46 s; re-running right afterwards took 2.4 s and 5.6 s. The cache is the same in both runs. Likely causes are an Overpass retry and cold filesystem reads of the 383 MiB cache, but neither was confirmed.
- **GMRT is downloaded at the service's finest cell whatever `cell_m` is.** Azores asks for 250 m cells and downloads 383 MiB at 47-48 m, three pieces, topo and mask (float32 each). Output is 8 MiB. This is the dominant cost of a large region; asking GMRT for a coarser `resolution` would cut it by 25 x for this case but would change what "measured" means for the survey mask, so it is **not** implemented (roadmap).
- **Memory**: 44-320 MiB. A linear fit to the seven builds is `50 MiB + 220 bytes x level-0 cells` (azores: 340 MiB predicted, 320 measured). Above 1.4 M cells nothing was measured; processing is windowed to the `-mem` budget (default 1024 MB), and `TestWindowSizeDoesNotChangeOutput` shows the output does not depend on the window size, but the 1 GB bound itself was not verified on a large world.
- **Disk apart from downloads**: 14-39 bytes per level-0 cell (5-30 MiB here); output 1.6-9 MiB, i.e. 5-12 bytes per cell including the whole pyramid.
- **Validation** takes 0.1 s on 8 MiB of output (it hashes and reads every tile: linear in output size). Benchmarks (`go test -bench`, one iteration, noisy): `validate` 4.7 ms on the 40-tile Falmouth fixture, `oceanworld` warm sample 0.6 microseconds, cold load 0.6 ms, 100-point profile 0.4 ms.

## Disk-space preflight

`ocean build` refuses to start, before any download, when the disk cannot hold the estimate (`internal/world/disk.go`, `ocean world check` prints it):

- work disk (stages, tiles, masks, pyramid): `48 bytes x level-0 cells + 16 MiB`, checked on the output's and the cache's filesystem;
- GMRT download, checked after the service has been probed and the request split into pieces: `12 bytes x nodes` at the service's finest cell, i.e. 1.5x the topo+mask float32 size (measured on the 3-piece azores request; the extra factor is overlap, probe and the other files);
- both must leave a 25 % margin plus 256 MiB free.

Both constants are above every measurement here but come from seven builds on one machine; they are not a guarantee. `-no-disk-check` or `OCEAN_SKIP_DISK_CHECK=1` skips the check (e.g. a filesystem that misreports free space). A resumed build re-checks with the full estimate, which can refuse a build that mostly needs nothing new: use the flag then. Verified: a 400 M-cell config on a machine with 1.9 GiB free was refused in under a second with the numbers and nothing written (`TestDiskCheckRefusesWhatCannotFit` covers the function; the CLI refusal was run by hand). Windows free-space code (`GetDiskFreeSpaceExW`) compiles but has never run.

## Reproduce

```bash
go build -o bin/ocean ./cmd/ocean
python3 scripts/ladder.py bin/ocean /path/to/scratch examples/regions/*.world.json   # stops below 2 GiB free
```

Go test timings (macOS arm64): `go test ./...` about 2.5 min (`internal/world` 160 s of it), `-short` about 75 s ([testing.md](testing.md)).
