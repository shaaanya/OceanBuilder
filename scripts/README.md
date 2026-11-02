# Verification scripts

Independent checks of built worlds. They use pyproj, numpy and the GDAL command-line tools and share no code with the Go tool, so they are evidence, not part of `go test`.

- `verify_geo.py <world dir> <gmrt cache dir>`: tile bounds against pyproj, ground distance against the manifest's point scale, elevation against the raw GMRT file at positions computed by pyproj, with one-cell shifts as a control (the error must be smallest at zero offset).
- `verify_antimeridian.py <world dir> <cache dir>`: samples vertices on both sides of 180 against the raw GMRT pieces (a piece labelled with longitudes above 180 is handled).

Requires `pip install pyproj numpy`. The 2026-11-02 runs are in the [development log](../docs/development-log.md).
- `interrupt_resume.py <ocean> <world.json> <scratch>`: kills a real `ocean build` with SIGINT and SIGKILL as soon as each kind of stage artifact appears in the cache, rebuilds on the same cache and requires a byte-identical output tree (docs/resume.md).
- `concurrent_builds.py <ocean> <world.json> <scratch>`: three simultaneous builds on one fresh cache, 15 rounds; all must succeed with identical output and leave no temporary stage directories.
Both use an offline config (a `local` source over `testdata/falmouth`); POSIX only (signals).
