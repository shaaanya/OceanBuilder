# Real LibreTerrain 2026.2.0 output sample

`build.json` + `file-n_0001_out_65.exr` are unmodified output of `libreterrain-mcp` (CPU build) for a 3072 x 1536 m
world built at `build.resolution` 65. The short axis is stretched (`cell_size_m` = [48, 24]); this is the measured
reason `ocean libreterrain export` pads to a square. Used to check that `Inspect` parses real `build.json` files.
