# Sources: what each adapter does, and what was checked

Dated notes. "Checked live" means a call to the real service on 2026-11-01 whose result is recorded here; offline tests use local stand-ins and never touch the network. Service probes (more detail, other candidates) are in [research/source-probes.md](research/source-probes.md). Licences are as far as they were read, not legal advice ([research/licensing-analysis.md](research/licensing-analysis.md)).

Run the live checks yourself: `OCEAN_SMOKE=1 go test ./internal/etopo ./internal/gebco ./internal/cudem -run Smoke -v`.

| `sources[].type` | Data | Declared datum (override with `vertical`) | Measured-vs-interpolated | Checked live |
|---|---|---|---|---|
| `gmrt` | GMRT GridServer, topography + survey mask | `GMRT-mosaic` (the service states none) | survey mask → `survey` (1) | 0.2.0 builds (Falmouth, Moorea); not re-run |
| `emodnet` | EMODnet DTM via WCS (Europe, ~115 m) | `unknown` (published DTM is LAT, the WCS declares nothing) | none | 0.2.0 |
| `etopo2022` | ETOPO 2022 15" surface tiles | `EGM2008` (User Guide) | none (SID tile exists; its codes were not decoded) | Falmouth window 48 x 36, min -63.113 / max 201.471 m, identical to the independent probe |
| `gebco` | GEBCO_2026 15" subset + TID | `MSL` (GEBCO: "assumed", shallow sources may differ) | TID 10-17 → `survey`; everything else `background` | Falmouth: 48 x 36 cells, min -63 / max 199 m, 33.1 % of cells TID 10-17 |
| `cudem` | NOAA CUDEM 1/9" COG tiles (US) | `NAVD88` (file CRS and ISO metadata) | none | Key West 0.02 x 0.02 degree window, 648 x 648 px, min -43.17 / max 5.95 m, 23 s |
| `geotiff` | your files, local path or URL | whatever you declare (required) | optional mask file | tests |

## Behaviour worth knowing

- **GEBCO** uses an undocumented API found in the download app's JavaScript. It can change without notice; failures name the step (queue, status, download) and the manifest records the request. GEBCO also publishes the global grid as a NetCDF file, usable as a `geotiff` source after conversion. The mask is computed from TID codes 10-17 (direct measurements: singlebeam, multibeam, seismic, isolated sounding, ENC sounding, lidar, optical, combination). Codes 40+ (gravity-predicted, interpolated, contours) are not measurements at output resolution.
- **ETOPO 2022** tiles are 15 degrees square and selected by arithmetic from the file name (`..._N60W015_surface.tif` covers lon -15..0, lat 45..60). Only the Falmouth tile was read live; the hemisphere prefixes for other tiles follow the observed naming and the offline test, not a live listing.
- **CUDEM** needs the tile index (`urllist8483.txt`, fetched and hashed once into the cache). Window reads are slow (about 0.3 MB/s measured), so windows are capped at 0.5 degrees per side. Heights are NAVD88; the planner has no verified transformation to anything else, so a CUDEM world is NAVD88 unless you set `vertical.allow_mixed`.
- All three drop the files' compound vertical CRS when cutting the window, so GDAL cannot change the heights; the datum is the declared one.
- **Antimeridian**: none of these adapters has been verified across 180; such configs are refused.
- Not implemented (probed, ranked in the research notes): NOAA `DEM_global_mosaic` ImageServer (mixed datums per item), Seascape, AWS Terrarium.
- GMRT GridServer, probed 2026-11-01 with `resolution=max`: the five example regions (0.15-0.4 degree boxes) came back at about 55-58 m native cells (Kona, Moorea, Mindelo, Bahamas) and 29 m (Geiranger) and were built; a 2 x 2 degree box came back as a 3642 x 5668 raster with anisotropic pixels (0.00055 x 0.00035 degrees) after 97 s and 83 MB; a 6 x 5 degree box returned a raster at about twice that pixel size, i.e. the service coarsens large requests without saying so, and the download did not finish within a 120 s probe. OceanBuilder reads the native cell size from the file it receives (`resampling.source_res_m`), so a coarsened answer shows up as `upsampled` and a coarser source resolution in the manifest; it does not know the request was coarsened. Whole-request fetch timeout is 3 minutes. Not tested: boxes over 10 degrees, the antimeridian.

## Example

```json
{
  "name": "falmouth-multi", "bbox": [-5.20, 50.05, -4.95, 50.22], "cell_m": 50,
  "precedence": "auto",
  "sources": [
    {"type": "gebco", "id": "gebco", "vertical": {"datum": "EGM2008", "offset_m": 0.0, "offset_source": "GEBCO says MSL; treated as EGM2008 for this demo, accepted risk"}},
    {"type": "etopo2022", "id": "etopo"}
  ]
}
```
Mixing `MSL` and `EGM2008` without such a statement is refused. Keep the declaration honest.
