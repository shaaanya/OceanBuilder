# 0009: NOAA DEM_global_mosaic is not adopted as a source

Status: accepted, 2026-11-02. Evidence: live calls to the service on that date.

## Context

`https://gis.ngdc.noaa.gov/arcgis/rest/services/DEM_mosaics/DEM_global_mosaic/ImageServer` is a global single-band float32 WGS 84 mosaic of NCEI DEMs (max request 20000 x 20000 px, 20 items). It is the obvious way to reach NOAA coastal DEMs for any US bbox with one adapter.

## Findings

- The catalog (`/query`, field `VerticalDatum`) overlays items with different vertical datums at the same point. Key West (-81.8, 24.55): `key_west_fl_mhw` (MHW), `key_west_fl_navd88` (NAVD 88), `crm_vol3_2023` (EGM 2008), ETOPO 2022 (datum field empty). Nantucket (-70.62, 41.55): `nantucket_ma` (MHW), `ncei19_n41x75_w070x75_2018v1` (NAVD 88), CRM vol 1 (EGM 2008). Hawaii (-155.5, 19.7): UH multibeam synthesis ("Sea Level"), two NCEI 1/9" tiles (NAVD 88), CRM vol 10 (EGM 2008).
- One `exportImage` returns one band with no per-pixel item or datum. A 0.02 x 0.02 degree window at Key West (120 x 120 px): default mosaic mean -0.253 m, the MHW item alone (`esriMosaicLockRaster`) mean +0.004 m, the NAVD 88 item alone mean -0.060 m. The default export matches neither: which item supplied which cell, and in which datum, cannot be recovered from the response.
- ADR 0008 forbids mixing datums silently and has no verified NAVD88/MHW/"Sea Level" transformation.

## Decision

No `noaa` mosaic adapter. NOAA's high-resolution coastal DEMs stay reachable through `cudem` (one declared datum, NAVD88, from the file CRS and ISO metadata). A future adapter could query the catalog, pick items itself, download them one at a time and declare each item's datum; it would still need a verified NAVD88/MHW transformation to be combined with anything else, so it is deferred, not rejected.

## Consequences

`docs/sources.md` lists the mosaic as "not implemented, mixed datums, evidence in this ADR". Nothing in the tool calls the service.
