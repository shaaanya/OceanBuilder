# Licensing of source data and built worlds

Not legal advice. Records what each source requires as far as it was checked, what the tool does about it, and what is open. Checked dates are when a page was read; terms can change.

| Source | Terms | Checked | What the tool does |
|--------|-------|---------|--------------------|
| GMRT GridServer | CC BY 4.0; credit with link; **not for navigation** (https://www.gmrt.org/about/terms_of_use.php) | 2026-10-30 | Licence and attribution in every provenance entry and `ATTRIBUTION.txt`; "not for navigation" repeated in the spec and README. |
| GEBCO (fill under GMRT `topo`) | Public domain per GEBCO; use implies acceptance of its conditions (https://www.gebco.net/data-products/gridded-bathymetry-data) | 2026-10-30 | Credited in `ATTRIBUTION.txt`. |
| OpenStreetMap (coastline, seamarks via Overpass) | ODbL 1.0: attribution "© OpenStreetMap contributors"; share-alike for derived databases | 2026-10-30 | Attribution always written. Coastline used for checks and an optional GeoJSON copy; with `coast_conform` the land/water decision of some cells derives from OSM (source code 5); `seamarks` copies OSM points. |
| EMODnet bathymetry (opt-in `regional_bathymetry`) | Licence not read: the survey page names none and links EMODnet Terms of Use (unverified) | — | Provenance records the request and the licence string set in code; **verify before redistributing a world built with it**. |
| User files (`local`) | Whatever the user declares | — | `license` and `attribution` are mandatory config fields; a missing one fails the build. Local paths are not written to the manifest. |
| Seascape / NOAA CUDEM / Copernicus DEM | Surveyed in `ecosystem.md`; not used by the build | — | Not applicable yet. |

## Built worlds

- A world is a collection of values derived from the sources. The least restrictive reading is attribution (all sources) plus "not for navigation" (GMRT). Ship `ATTRIBUTION.txt` with every copy.
- ODbL share-alike: when a world contains OSM-derived data (conformed cells, seamark points), it may be a *derivative database* that must be offered under ODbL. The tool does not decide this; it makes the OSM-derived share measurable (`source` code 5 per cell, `stats` counts) so a consumer can judge. Worlds built without `coast_conform` and `seamarks` use OSM only for checks.
- Third-party redistribution of tiles (a game shipping them, a public tile server) needs the same review per source; the manifest lists every source with URL, licence and retrieval time to support it.

## Open

- A formal ODbL analysis of mask-only versus conformed use.
- EMODnet terms (read the Terms of Use page).
- A machine-readable `redistribution` summary per source in the manifest; today the licence is free text.
