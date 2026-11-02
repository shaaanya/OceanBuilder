# Resume, cancellation and the stage cache

Status: implemented and tested (`internal/world/resume_test.go`), 0.4.0. Tested on macOS arm64 only; the CI matrix
(Linux, Windows) has not run yet.

## Model

A build is a chain of stages. Each stage is a pure function of its **key**, a sha256 over everything it reads:

| Stage | Cache artifact (`<cache>/stages/`) | Key covers |
|---|---|---|
| fetch | `<cache>/<source>/…` + provenance sidecar (sha256) | URL and request box |
| vertical shift | `vshift-<src sha>-<shift>.tif` + `.seal` | source sha256, grid/shift identity |
| warp (per dataset, `dem` and `mask`) | `<key>-dem.bin` / `-mask.bin` + `.seal` | source sha256s, resampling, target extent, size, SRS, GDAL version, vertical key, nodata, source SRS |
| compose (level 0) | `compose-<key>/` (`l0.dem l0.kind l0.prov composition.json STAGE.json`) | sealed sha256 of every warped input in rank order, dataset ids, parts, qualities, offsets |
| LOD level k | `lod<k>-<key>/` (`l.dem l.kind l.prov STAGE.json`) | key/hash of level k-1, both level plans, tile cells, LOD method |

Not cached, recomputed on every build: the whole-grid steps (regional fill, coast conform, refinement), the tile
files and PNGs, coastline/seamark checks, the manifest. After a whole-grid step the edited level 0 is hashed and that
hash seeds the LOD keys, so LOD stages are still reused when the edit is byte-identical.

## Guarantees

- **Atomic.** A directory artifact is written in a `*.tmp-*` sibling, sealed (`STAGE.json`: sha256 and size of each
  file) and renamed. A single-file artifact is renamed into place and then sealed with `<file>.seal`. A crash at any
  point leaves temporary files or an unsealed file, which the next build ignores and replaces.
- **Verified on reuse.** Every byte of a reused artifact is hashed against its seal. A flipped bit, truncation, missing
  seal or wrong key is logged (`failed verification`), the artifact is deleted and rebuilt. Cost: one sequential read
  per reused artifact (about 6 bytes per level-0 cell, 4 per warp).
- **Deterministic.** Output is byte-identical for cold cache, warm cache, resumed build, different cache and output
  directories, `GOMAXPROCS=1`, and any `-mem` budget (tests compare a hash of every output file).
- **Output is never partial.** The world is assembled in a `.building-` directory and swapped in only when complete;
  a failed or interrupted build leaves the previous output untouched.
- **Concurrent builds** may share a cache: the loser of a rename race verifies and uses the winner's artifact.
- **Cancellation** (Ctrl-C, context cancel) stops GDAL children and the row loops; temporary stage directories are
  removed on the way out. Directories left by a killed process (`kill -9`, power loss) are swept at the next build once
  older than two hours.

## What resumes, what does not

Resumes: everything above. After an interruption the next identical build skips finished fetches, shifts, warps,
composition and LOD levels and restarts at the interrupted stage. Interrupting *inside* a stage loses that stage's
work (no sub-stage checkpoints): a 6 h composition restarts from its beginning.

Does not resume: tile writing (restarts), whole-grid steps, validation. A build whose finished output already holds
the same inputs is skipped entirely (`input_key`), unless `-force`.

A changed config, source, tool or GDAL version changes the keys: only the affected stages and those after them are
redone (for instance changing `lod_method` reuses warps and composition).

## Disk use

The cache holds warp rasters (4 B/cell per dataset and kind), the compose layer (6 B/cell) and the LOD layers (about
1/3 more). `ocean cache stats` shows it; `ocean cache clean -stages` deletes everything rebuildable and keeps
downloads. Nothing evicts automatically.

## Testing hook

`world.StageHook` (tests only) is called at `start:`, `mid:` (data written, not sealed) and `done:` of every stage and
may return an error to simulate a crash. `TestResumeAfterInterruptAtEveryStage` cuts a build at every such point and
checks the resumed output byte for byte.
