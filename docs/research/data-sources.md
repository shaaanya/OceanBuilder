# Data sources in use (verified in this repo)

See [ecosystem.md](ecosystem.md) for the wider survey. This file covers only what the code uses today.

## GMRT GridServer (bathymetry + land)

- Endpoint: `https://www.gmrt.org/services/GridServer` (GET, no key). Parameters used: `minlongitude`, `maxlongitude`, `minlatitude`, `maxlatitude`, `format=geotiff`, `resolution=max`, `layer`.
- `layer=topo`: unmasked grid. Per the GridServer documentation, it is filled with GEBCO_08 where high-resolution ocean data do not exist.
- `layer=topo-mask`: masked grid with only high-resolution (≈100 m) ocean data, NaN elsewhere. We use "finite in mask" as "survey-backed" and report the fraction of water cells that are.
- `resolution=max` delivers ≈100 m/node at most per the docs; observed delivered cells around Falmouth were ≈39 m (N–S/E–W mean), so the nominal grid spacing is not the same as true data resolution. The manifest records both the delivered cell size and the GEBCO fill resolution (30″).
- Vertical reference: a mosaic of contributed grids; not a single datum. Manifest says so.
- Licence (verified 2026-10-30 at https://www.gmrt.org/about/terms_of_use.php): CC BY 4.0, acknowledgment with byline/credit/link required, **data should not be used for navigation purposes**. Cite Ryan et al. (2009), doi:10.1029/2008GC002332; data doi:10.1594/IEDA.100001.
- GEBCO_08 (the fill under `topo`): GEBCO says its grid is in the public domain and free to use, and that use implies acceptance of its conditions of use (https://www.gebco.net/data-products/gridded-bathymetry-data, checked 2026-10-30). Credited in every `ATTRIBUTION.txt`.

## OpenStreetMap coastline (Overpass)

- Query: `way["natural"="coastline"](bbox); out geom;` as POST to `overpass-api.de`, then public mirrors. A `User-Agent` is required (empty UA returned HTTP 406).
- ODbL 1.0: attribution "© OpenStreetMap contributors" and share-alike for derived databases. `ATTRIBUTION.txt` is written with every world. Whether a built tile set is a "derived database" of the coastline is a legal question we have not settled; the coastline is only used for metrics and the optional geojson copy.
- Overpass is a shared public service; the cache avoids repeat queries. Heavy use should go through a local extract (roadmap).

## User-provided data

`"local": {"topo": "x.tif", "mask": "m.tif", "coastline": "c.geojson", "license": "...", "attribution": "..."}` in the world config bypasses downloads. Licence and attribution are mandatory and flow into the manifest.
