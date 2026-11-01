# 0006: Local transverse Mercator grid for wide worlds (schema 3)

Status: accepted, 2026-11-01. Supersedes the "multi-zone worlds are unsupported" rule of 0.2.0.

## Context

0.2.0 used the UTM zone of the bbox centre and refused any box whose scale error exceeded 0.5 % (about 6.8 degrees from the central meridian). Boxes that straddle a zone edge, sit on the antimeridian or are simply wide could not be built.

## Decision

- `projection` in the config: `auto` (default), `utm`, `tmerc`.
- `auto` keeps UTM whenever the box fits within 0.5 % scale error, so every 0.2.0-valid config builds the same grid, manifest schema 2, byte-identical tiles (golden tests). Otherwise the world uses a WGS 84 transverse Mercator centred on the bbox (`lon_0` rounded to 0.001 degrees, `k_0 = 1 - d_max^2 / 4` so the scale error is balanced about 1, false easting 500000, false northing 0). The same 0.5 % limit applies; a wider box is refused with advice to split it.
- Manifest schema 3 is written only for such worlds. It differs from schema 2 only in `crs`: `epsg` is 0 and `kind`, `lon_0`, `k_0`, `false_easting`, `false_northing`, `proj4` describe the grid. UTM worlds stay at schema 2.
- `projection: "utm"` restores the 0.2.0 refusal. A 20 degree box at 50N used to be refused and is now planned as a local grid (the old test now sets `projection: "utm"`).
- The reader (`pkg/oceanworld`) reads schema 2 and 3, implements the transverse Mercator itself and wraps longitudes, so 179.95 and -179.95 land on the same side of the grid.
- Bounding boxes may have `east > 180` (width at most 180 degrees) to cross the antimeridian.

## Limits

- Antimeridian boxes are accepted only with local GeoTIFF sources and `coastline: "none"`. No network fetcher has been verified across 180 (GMRT answered such a request in a probe, see research/source-probes.md, but the repo's own client was not exercised), so the config check refuses the rest instead of guessing.
- Scale error is the same limit as before; a local grid does not make a continental world metric.
- Engine adapters that assumed an EPSG code must use `crs.proj4` when `crs.kind` is `tmerc`. The GeoTIFF export and the preview do. The LibreTerrain exchange and the Unity importer have not been checked with schema 3 and are untested.

## Consequences

Old tools reading `crs.epsg` see 0 on schema 3 worlds and must stop; `schema_version` is the signal.
