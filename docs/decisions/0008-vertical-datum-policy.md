# 0008: Vertical datum policy

Status: accepted, 2026-11-01.

## Decision

- Every elevation dataset declares a vertical datum from a closed vocabulary (EGM96, EGM2008, WGS84-ellipsoid, MSL, NAVD88, MLLW, MLW, MHW, MHHW, LAT, HAT, GMRT-mosaic, unknown). The world has one target datum (`vertical.datum`, default: the datum shared by all datasets).
- A conversion is applied only when PROJ resolves a real grid operation for the pair (`projinfo -o PROJ`) with the grids installed; `+proj=noop` and ballpark results are refused. The pipeline, grid file names and sha256, and the PROJ version go into `datasets[].vertical`. Only EGM96, EGM2008 and the WGS 84 ellipsoid are transformable today. NAVD88, MSL and tidal datums are refused because there is no verified transformation (EPSG:5714 MSL needs a grid that is not installed; NAVD88→ellipsoid resolved to a no-op here).
- A user `offset_m` is accepted only with `offset_source` and is marked `verified: false`.
- Datasets that do not share a verified reference are refused unless `vertical.allow_mixed` is true; the manifest then says `mixed` and the validator warns.
- The geoid shift is applied on the source's own grid before warping, and the resulting file carries only a horizontal CRS so the world warp does not convert heights a second time (a bug the first version of the test caught).
- GDAL runs with `PROJ_NETWORK=OFF` so a build never silently downloads grids.

## Limits

The tool does not know the datum of GMRT, the mixed datums of the NOAA mosaic, or which datum an EMODnet WCS coverage serves (see research/source-probes.md); these must be declared by the user or stay `unknown`. The installed EGM96 grid differs from PROJ's published regression values by about 0.4 m at two points; the cause is not established (dev log).
