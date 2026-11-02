# 0010: Seascape not adopted; GEBCO stays on the subset API

Status: accepted, 2026-11-02. Evidence: live calls on that date and the probes in [research/source-probes.md](../research/source-probes.md).

## Seascape (Open Waters, `tiles.openwaters.io/seascape`)

- Reachable today: `raster.json` 200 in 1.4 s; Falmouth z10/z12, Key West z12 and a mid-Atlantic z8 tile returned 200 WebP in 0.2-1.7 s. Terrarium-encoded, 512 px, z0-z18, CC BY 4.0, attribution `© Open Waters`.
- Not usable under this repository's rules: land has no elevation (category codes only), the depth datum varies per source and is not recorded per tile (README: LAT/MLLW for some, about MSL for others), the data is "interpolated and merged ... then smoothed during tiling", and values are quantised (half-metre steps at z12). It cannot report source resolution or the surveyed fraction, which `AGENTS.md` requires to be kept in the manifest, and it would present smoothed, merged data as one source.
- Decision: no adapter. It remains a useful visual cross-check and a catalogue of regional open bathymetry (its `sources/README.md`).

## GEBCO direct

- The adapter uses the Grid Subsetting App's undocumented REST API (queue, status, download). Live today: `TestSmokeFalmouth` passes (6.4 s); a 0.6 x 0.4 degree antimeridian world took 66 s for two pieces. It can change without notice; every failure names the step and the manifest records the request.
- A direct global NetCDF download exists, but its URL and size were not established from GEBCO's published pages, and a global grid cannot be fetched on this machine's disk budget. Not evaluated beyond that; not adopted.
- The TID layer is what makes GEBCO worth using: per-cell measured (TID 10-17) vs indirect. A subset gets it too; a direct file would need the matching TID file.
- Decision: keep the API adapter, flagged "undocumented" in `docs/sources.md`; revisit a local-file route (`type: geotiff` already accepts a user-downloaded GEBCO subset, with the declared datum) when GEBCO publishes stable per-region URLs.

## NOAA multibeam / BAG ImageServers

Not probed. They are possible measured-only sources but each is a separate service with its own datum story; deferred.
