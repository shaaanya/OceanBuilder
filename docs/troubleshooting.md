# Troubleshooting

Messages are quoted from the tool. Run `ocean version` first and include its output in a bug report.

## Build refuses or fails

| Message (start) | Cause | What to do |
|---|---|---|
| `GDAL command-line tools not found on PATH: gdalwarp, ...` | GDAL is not installed, or not on `PATH` in this shell (conda environments are per shell) | Install GDAL 3.9 or newer ([install.md](install.md)); check `gdalinfo --version`. |
| `not enough disk space for building X ...` / `... for the GMRT download ...` | The preflight ([performance.md](performance.md)) estimated more than the free space | Free space, point `-cache` / output at a larger disk, shrink the bbox or raise `cell_m` (it shrinks the work and the output, not the GMRT download), or `-no-disk-check` if you know better. |
| `bbox reaches N% scale distortion in UTM zone Z ...` | `projection: "utm"` and the box is too wide for the zone | Use `projection: "auto"` (local transverse Mercator, schema 3) or split the region. |
| `bbox reaches N% scale distortion even in a local transverse Mercator` | The region is too wide for one metric grid | Split it. |
| `datasets do not share a verified vertical reference` / `PROJ cannot relate ...` / `PROJ offers only a ballpark ...` | Sources use datums the tool has no verified conversion between | Set a world `vertical.datum` every source can reach, give a documented `vertical.offset_m` with `offset_source`, or accept the mix with `vertical.allow_mixed` (the manifest then says heights are unreconciled). The tool never guesses. |
| `transformation grid ... not found in the PROJ data path` | PROJ data grids missing (the tool runs PROJ with network off) | `conda install proj-data` or install your distribution's PROJ data. |
| `sources differ from X.lock.json (rebuild with -update-lock to accept)` | A source returned different bytes than when the lock was written (upstream data changed, or a different mirror) | Inspect the listed differences; if the change is expected, `-update-lock` and commit it. |
| `all Overpass mirrors failed` | The public Overpass servers are busy or unreachable | Retry later. A coastline already cached from any mirror is reused first, so a repeat build does not hit the network. Without OSM data use `"coastline": "none"` (the validator then warns `no-coastline`). |
| `source "..." (...): the world crosses the antimeridian and this source cannot be requested across 180` | CUDEM, EMODnet, OSM coastline, seamarks and remote GeoTIFFs are refused across 180 | Use `gmrt`, `etopo2022`, `gebco` or a local GeoTIFF, `"coastline": "none"`. |
| `gmrt resolution probe: ...` / HTTP 5xx / timeouts from GMRT | The service is slow or down (a 4 x 1.7 degree request took 27 minutes) | Retry; completed downloads are cached and kept, a resumed build continues at the first unfinished stage ([resume.md](resume.md)). |
| `cancelled` | Ctrl-C or SIGTERM | Nothing is half-written: the previous output is untouched; re-run to resume. |
| The build ends in a rename error on Windows | Another program holds a file of the previous output open (viewer, antivirus, Explorer) | Close it and re-run; stages are reused. |

A build with identical inputs prints *up to date* and touches nothing; add `-force` to rebuild.

## The world builds but looks wrong

| Symptom | Look at |
|---|---|
| Large areas flat at one deep value | `ocean validate`: `background-fill` (water cells come from the 758 m GEBCO fill, not survey), `low-res-bathymetry`. `ocean info` prints the cells per source. This is the data, not a bug. |
| Coastline drawn off the shore | `coast-misaligned`: OSM coastline vs the DEM zero contour differ horizontally or in datum. Try `"coast_conform": "osm"` ([README](../README.md)) and read `vertical.note`. |
| Spikes or steps at tile or dataset borders | `dataset-seam`, `discontinuity`, `dataset-offset` in the validator: different sources disagree by metres where they meet. |
| Detail looks smeared | `upsampled`: `cell_m` is finer than the source. Raise `cell_m` or accept interpolation. |
| Holes | Depth class 255 / source 0 = nodata, kept on purpose. Level 0 writes all-nodata tiles too (`empty-tile`). Upper levels omit them. |
| Different result from yesterday | Compare `input_key` and `ocean diff dirA dirB`; the GDAL version is part of the key. |

## Validator findings

`ocean validate` exits 1 on any error. Errors are inconsistencies in the files (`tile-hash`, `tile-size`, `tile-stats`, `layer-mismatch`, `lod-mismatch`, `missing-tile`, `duplicate-tile`, `tile-gap`, `height-range`, `build-id`, `licensing-missing`...): rebuild with `-force` from a clean output, and if it persists report it. Warnings describe the data (see above). `-json` gives machine-readable findings.

## Unity

| Symptom | Cause / fix |
|---|---|
| Import fails: schema 3 | The Unity importer supports schema 2 (UTM) only. Rebuild the region so it fits one zone, or use another consumer. |
| Import fails: file missing from the project | Export with enough levels: `ocean export <world> -target unity -dest <project> -level N` and import with `-oceanMaxLevel N`. |
| Coarse tiles never appear | The world was imported with level 0 only (`-oceanMaxLevel 0`), or the world has `lod_levels: -1`. |
| Cracks between tiles of different size | Skirts are built when `-oceanMaxLevel` is above 0. Check that `Generated/Skirts` exists and the streamer's `skirts` is true. See [unity-lod.md](unity-lod.md). |
| Pink terrain | Not URP. The package supports URP only and refuses another pipeline. |
| Code edited in `internal/export/unitypkg` has no effect | The demo project embeds a copy in `Packages/com.oceancompiler.world`; re-export. |
| Player log shows `OCEAN_PLAYER_RESULT failures=N` | The detail follows on the same line. Re-run with graphics (the seam probe needs a GPU; `-nographics` skips it). |

## Reading a world in your own code

See [integration.md](integration.md): refuse unknown `schema_version`, read the checkpoints back, use `floor` for tile indices, treat nodata as unknown, and handle positions exactly on a tile edge.
