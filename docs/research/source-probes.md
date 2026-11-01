# Source probes: bathymetry candidates, called live

Probe date: **2026-11-01** (UTC, all requests made that day). Test areas: Falmouth UK (lon -5.20..-5.00, lat 50.05..50.20), Key West (lon -81.9..-81.6, lat 24.4..24.6), Fiji antimeridian (lon 179.5..180 and -180..-179.5, lat -17..-16).

Labels: **OBSERVED** = I made the request and looked at the result. **DOCUMENTED** = read in the named page. **UNKNOWN** = not established. Probe files are in `/private/tmp/claude-501/-Users-shaaaanya-Documents-projects-GameDev-Tools-OceanBuilder/ae8ad161-b745-41f1-8fb6-d442aa3c517f/scratchpad/probes/` (small fixtures in `fixtures/`). Total downloaded was about 115 MB.

## Summary table

| # | Source | On-demand bbox? | Native res (observed) | Vertical ref | Measured-vs-interpolated layer | Licence | Verdict |
|---|---|---|---|---|---|---|---|
| 1 | NOAA gis.ngdc ImageServer (DEM_global_mosaic) | yes, exportImage | mosaic: 1/9" (US, CUDEM) down to 15" (ETOPO 2022) elsewhere | **mixed per item** (NAVD 88, MHW, MHHW, sea level, ...) | none in output; per-item catalog via `identify` | no licence text in service; underlying NCEI = public domain | usable but datum-ambiguous |
| 2 | NOAA CUDEM 1/9" tiles (S3 COG) | yes, `/vsicurl` window reads | 3.0864e-5 deg (1/9") | NAVD88 height (OBSERVED in file CRS) | none (1 band) | "Not subject to copyright protection within the United States" | best US source |
| 3 | GEBCO_2026 | yes via undocumented `download.gebco.net/api` | 15" (0.0041667 deg) | "assumed MSL", WGS84 | **TID grid** (OBSERVED, per cell; the only per-cell measured-vs-indirect layer in this report) | public domain, attribution required | best global provenance |
| 4 | ETOPO 2022 | yes, `/vsicurl` on 15 deg tiles | 15" | EGM2008 geoid (~MSL) | **SID tile** (OBSERVED; dataset provenance only, not a measured/interpolated flag; codes not decoded) | NOAA/NCEI, cite DOI | stable, simple fallback |
| 5a | Seascape tiles | tile fetch | 512 px tiles, z0..z18 | varies by source (not normalised) | coverage.json tiles only | CC BY 4.0 + per-source | lossy, mixed datum |
| 5b | AWS Terrarium | tile fetch | 256 px; ocean from ETOPO1 1' (DOCUMENTED) | not stated | none | attribution required | not suitable |
| 6 | EMODnet DTM WCS | yes (in use) | 1/16' = 0.0010417 deg | **LAT** documented for the DTM; an MSL variant exists only as downloadable ESRI ASCII tiles; which one the WCS `emodnet__mean` serves is not stated in capabilities (UNKNOWN, page says LAT) | none in WCS (full DTM download has std dev, count, interpolation flag, source ref) | CC BY 4.0 | keep (Europe) |
| 7 | GMRT GridServer | yes (in use) | ~61 m at equator zoom, equal-metre nodes | **not stated** on pages read | `topo-mask` | CC BY 4.0 | keep, with caveats |

---

## 1. NOAA NCEI ImageServer services (gis.ngdc.noaa.gov)

**Authoritative docs:** none found for these services beyond the REST directory itself (https://gis.ngdc.noaa.gov/arcgis/rest/services). Per-item metadata links come from the service catalog (below).

**Services (OBSERVED, `GET .../rest/services/<folder>?f=json`):**
- `DEM_mosaics`: `CRM_mosaic`, `DEM_all`, `DEM_global_mosaic`, `DEM_global_mosaic_hillshade`, `DEM_tiles_mosaic`, `DEM_tiles_mosaic_hillshade`, `ETOPO1_bedrock`, `ETOPO1_ice_surface` (all ImageServer).
- `multibeam_mosaics`: `Multibeam_Backscatter_F32/U8`, `multibeam_mosaic_combined`, `_processed`, `_raw`, `_subsets`. `NOS_MBAB`: `NOS_MBAB_F32/U8`. Root also has MapServers (`bag_bathymetry`, `multibeam_datasets`, `csb`, `bathy_gap_analysis`, ...) and `wgs84/gebco_2019_contours`.
- Not probed in depth: the multibeam and NOS_MBAB mosaics.

**Service metadata (OBSERVED, `.../ImageServer?f=json`):**

| Service | Extent | Cell (deg) | pixelType | Notes |
|---|---|---|---|---|
| DEM_global_mosaic | global (-180..180, -90..90) | 3.086e-5 (= 1/9") | F32 | min -9982, max 6180, default resampling **Bilinear**, maxImage 20000x20000 |
| CRM_mosaic | lon -161..-64.25, lat 17..49 | 2.7778e-4 (1") | F32 | US coastal relief |
| DEM_tiles_mosaic | lon -171..146, lat -14.5..64.75 | 3.086e-5 | F32 | |

**Request that worked (OBSERVED):**
```
GET https://gis.ngdc.noaa.gov/arcgis/rest/services/DEM_mosaics/DEM_global_mosaic/ImageServer/exportImage
    ?bbox=-81.9,24.4,-81.6,24.6&bboxSR=4326&imageSR=4326&size=1000,667
    &format=tiff&pixelType=F32&interpolation=RSP_NearestNeighbor&f=image
```
200, `image/tiff`, Float32, EPSG:4326, 3,146,918 bytes. No auth. Rate limits: UNKNOWN. Size cap: requesting 30000x20000 returned JSON `{"error":{"code":400,...,"details":["The requested image exceeds the size limit."]}}` (matches maxImageWidth/Height 20000).

**What you actually get (OBSERVED):**
- No NoData tag in the GeoTIFF. Land is positive values (topobathy). Inside the world extent there were no NaN or sentinel values in any test.
- **Server adjusts the bbox to the aspect of `size`**: Key West origin came back y=24.60005; a 1-degree-square bbox with `size=200,100` returned an extent 179..181 at 0.01 deg/px. Always request a `size` whose aspect matches the bbox, and read the geotransform from the response.
- **Default interpolation is Bilinear** (service metadata). Falmouth with `RSP_NearestNeighbor`: 1,475 distinct values in 187,500 px (blocky source cells). Same request with `RSP_BilinearInterpolation`: 183,465 distinct values. So without `interpolation=` you get server-side interpolation presented as data.
- **The mosaic is a patchwork.** `identify?...&returnCatalogItems=true` (OBSERVED) at Falmouth (-5.05,50.12 and -5.10,50.08), Fiji (179.8 and -179.8, -16.5) and mid-Pacific (-150,0) returned only `ETOPO_2022_v1_15s_surface_elev`, 15" (0.0041667 deg), DateCompleted 2022-10-03. So **outside the US the 1/9" nominal grid is a resample of 15" ETOPO**: Falmouth min -63.113 equals ETOPO 2022's -63.113. At Key West (-81.78,24.555) the top item is `ncei19_n24x75_w082x00_2022v1`, 0.111", datum "NAVD 88", DateCompleted 2023-04-28, with MetadataURL https://www.ncei.noaa.gov/metadata/geoportal/rest/metadata/item/gov.noaa.ngdc.mgg.dem:999919/html; 18 raster values came back at that point (overlapping items).
- **Beyond lon 180 the value is 0, not NaN** (OBSERVED: `gdallocationinfo` at 180.5/-16.5 and 180.9/-16.5 returned 0; 179.2 returned 185.27). A request crossing 180 gets silent zeros. Separate requests for 179.5..180 and -180..-179.5 both returned real data (Fiji W min -2020, E min -1773).
- Vertical datum per catalog item is in the `VerticalDatum` attribute. Count of `Category=1` catalog items by datum (OBSERVED, `query` with `outStatistics`): NAVD 88 1477, Sea Level 168, MHW 106, (null) 104, MHHW 31, Instantaneous Sea Level 10, EGM 2008 5, Sea level 2, MSL 2, Mean Sea Level 2, PRVD02 1. The exported raster carries none of this.
- Measured vs interpolated: not in the exported raster. Only the catalog item name/cellsize/date via `identify`/`query`.

**Licence:** service `copyrightText` empty (OBSERVED). For the NCEI CUDEM content see section 2.

**Bytes for test area:** Key West 1000x667 = 3.1 MB; Falmouth 500x375 = 787 KB (F32 uncompressed, so about 4 B per pixel plus overhead).

**Suitability:** convenient, but violates the repo rules unless the adapter also calls `identify`/`query` to record per-cell source and datum, and passes `interpolation=RSP_NearestNeighbor`. Fixtures: `fixtures/noaa_exportImage_falmouth_100x75_nearest.tif`, `fixtures/noaa_exportImage_keywest_150x100_nearest.tif` (note: 0.002 deg pixels, coarse).

---

## 2. NOAA CUDEM (Continuously Updated DEM)

**Real location (OBSERVED):** AWS open-data bucket `noaa-nos-coastal-lidar-pds`, anonymous HTTPS, e.g.
`https://noaa-nos-coastal-lidar-pds.s3.amazonaws.com/?list-type=2&prefix=dem/&delimiter=/` (401 prefixes).
- 1/9 arc-second set: `dem/NCEI_ninth_Topobathy_2014_8483/` (NCEI id 8483, DOI 10.25921/ds9v-ky35). Regional subfolders by count: FL 132, LA_MS 122, TX 106, chesapeake_bay 88, southeast 80, MA_NH_ME 77, northeast_sandy 58, AL_nwFL 57, NC 48, CA 41, OR 27, wash_pugetsound 23, wash_outercoast 22, columbia_river 18, AK 17, rima 14, wash_bellingham 13, wash_juandefuca 10.
- 1/3 arc-second: `dem/NCEI_third_Topobathy_2014_8580/`; territory sets: `NCEI_ninth_Topobathy_{AmSam_9460,CNMI_9560,Guam_9462,Hawaii_9428,PuertoRico_9525,USVI_9529}` and `third_*` equivalents.
- The same bucket's other `dem/` folders are NOAA coastal-lidar DEMs (not CUDEM).

**Tile naming and finding tiles without a database (OBSERVED):**
- Example: `dem/NCEI_ninth_Topobathy_2014_8483/FL/ncei19_n24x75_w082x00_2022v1.tif`. `n24x75` = north edge 24.75, `w082x00` = west edge -82.00 (file origin -82.000185, 24.750185). Tiles are 0.25 x 0.25 deg plus a 6 px buffer (8112 x 8112 px at 3.0864e-5 deg).
- The region subfolder and the version suffix (`2014v1` vs `2022v1`) are not derivable from coordinates, so use the index files in the same folder: `urllist8483.txt` (942 lines, full URLs), `tileindex_NCEI_ninth_Topobathy_2014.zip`, `NCEI_ninth_Topobathy_2014_m8483_EPSG-4269.vrt`, per-region shapefiles (e.g. `southeast/southeast_topobathy_19.shp`), ISO metadata `ncei_nintharcsec_dem_m8483_met.xml`, and `CUDEM_Amante_et_al_2023.pdf`. A client can fetch `urllist8483.txt` once, parse tile edges from the filenames, and pick tiles by bbox.
- Tile sizes: median 203 MB, the Key West tile is 355 MB. **Do not download whole tiles.**

**Format (OBSERVED, `gdalinfo /vsicurl/<url>`):** Cloud Optimized GeoTIFF (`LAYOUT=COG`, LZW, 512x512 blocks, overviews 4056/2028/1014/507), Float32, **1 band**, NoData **-9999**, `AREA_OR_POINT=Area`, pixel 0.000030864197 deg. CRS: `COMPOUNDCRS["NAD83 + NAVD88 height"]`, TIFFTAG_IMAGEDESCRIPTION `Topography-Bathymetry; NAVD88 height`. Land is positive.

**Window read (OBSERVED):** `gdal_translate -projwin -81.95 24.60 -81.90 24.55 -co COMPRESS=DEFLATE /vsicurl/<url> out.tif` returned 1620x1620 px, 9.0 MB, in 34 s (min -4.41, max 0.496 m; 2,624,400 px, no NoData in this window). Range requests work, so bbox-on-demand is feasible. Latency scales with window size; 0.25 deg tiles would be about 250 MB.

**Vertical datum:** **NAVD88 height (EPSG:5703)**, OBSERVED in the file CRS and in the ISO XML (`urn:ogc:def:datum:EPSG::5703`). It is not MHW/MHHW. Geoid model used for NAVD88 is not stated in what I read (UNKNOWN).

**Vintage (OBSERVED/DOCUMENTED):** tile suffix `2022v1`, S3 LastModified 2024-01-06; service catalog DateCompleted 2023-04-28; ISO metadata publication date 2014-12-09, "Currentness: Publication Date 2018-01-01". Tiles in other regions carry other years (`2014v1`).

**Measured vs interpolated:** none in the tile (single band). No TID/source grid observed. The ISO metadata points to "DEM Development Reports" for provenance (UNKNOWN content).

**Licence/attribution (DOCUMENTED, ISO XML `ncei_nintharcsec_dem_m8483_met.xml`):** "Not subject to copyright protection within the United States." "Not to be used for navigation. Although these data are of high quality and useful for planning and modeling purposes, they are not suitable for navigation." Cite as: "Cooperative Institute for Research in Environmental Sciences (CIRES) at the University of Colorado, Boulder. 2014: Continuously Updated Digital Elevation Model (CUDEM) - 1/9 Arc-Second Resolution Bathymetric-Topographic Tiles. [indicate subset used]. NOAA National Centers for Environmental Information. https://doi.org/10.25921/ds9v-ky35. Accessed [date]."

**Coverage:** US coasts and territories only. Nothing for UK; American Samoa/CNMI/Guam are near, not at, the 180 meridian (not tested).

**Suitability:** best US source (3 m, explicit datum, COG). Needs urllist parsing and `/vsicurl`; GDAL is already a hard dependency.

---

## 3. GEBCO (direct)

**Docs:** https://www.gebco.net/data-products-gridded-bathymetry-data/gebco2026-grid (OBSERVED, fetched today); terms https://www.gebco.net/data-products/gridded-bathymetry/terms-of-use.

**Current release (DOCUMENTED):** **GEBCO_2026 Grid, published April 2026**: 15 arc-second, 43200 rows x 86400 columns, pixel-centre registered, "land and ice surface elevation" version plus a sub-ice-topography version for Greenland/Antarctica. Global NetCDF "single 7.0 GB file", TID grid "single 3.5 GB". NetCDF4 CF; elevation Int16, TID single-byte integer. DOI 10.5285/4f68d5c7-45eb-f999-e063-7086abc036fa.

**Scriptable access (OBSERVED):**
- Official page directs users to the "GEBCO Grid Subsetting App" at https://download.gebco.net (JS app). Direct global-file URLs: **UNKNOWN** (not on the page; the BODC `open_download/gebco/gebco_2026/zip/`, `..._tid/zip/`, `.../geotiff/` and `gebco_2025/zip/` patterns I tried all returned 404).
- The app talks to an **undocumented REST API** (found in its JS bundle; not in GEBCO's published docs, so it can change). It worked end to end today with no login and no e-mail:
  1. `GET https://download.gebco.net/api/grids` lists grids; GEBCO 2026 global is `grid_id 1`, data sources `1` Bathymetry, `2` Bathymetry (sub ice), `3` TID; `maximum_area 14400`; `x_min -180, x_max 360`. `GET /api/formats`: netcdf(1), geotiff(2), ascii(3), png/jpg colour-map and hillshade (4-7).
  2. `POST /api/queue` with JSON `{"id":"0","email":null,"submission_date":"2023-08-28T16:59:39.220334","processing_status":"new","items":[{"id":0,"grid_id":1,"data_source_ids":[1,3],"formats":[2],"left":-5.2,"right":-5.0,"top":50.2,"bottom":50.05}]}` returned 201 `{"message":"Basket added to queue","basketId":"GEBCO_01_Nov_2026_1d410854c234"}`. (A first attempt with the old flat body returned 422 pydantic errors, so the schema has changed before.)
  3. `GET /api/queue/status/<basketId>` returned `{"status":"finished"}` within seconds.
  4. `GET /api/queue/download/<basketId>` returned a 435,826 B zip containing `gebco_2026_n50.2_s50.05_w-5.2_e-5.0_geotiff.tif` (11,308 B, Int16), the matching `gebco_2026_tid_...tif` (11,988 B, Int8), `GEBCO_Grid_documentation.pdf` and `GEBCO_Grid_terms_of_use.pdf`.
- WMS: https://wms.gebco.net/mapserv (WMS 1.3.0, layers `GEBCO_LATEST`, `_2`, `_3` ("measured data only, interpolation transparent"), `_SUB_ICE_TOPO`, `GEBCO_LATEST_TID`, `_TID_2`; max 4096x4096). **Images only**: `format=image/tiff` returned a 3-band Byte RGB (OBSERVED), not elevation values. `service=wcs` returned 400 Bad Request. No WCS/ArcGIS data service found.

**Resolution/georef (OBSERVED, Falmouth GeoTIFF):** 48x36 px, origin (-5.2, 50.2), pixel 0.004166667 deg, EPSG:4326, `AREA_OR_POINT=Area`, no NoData tag; ~460 m E-W at 50N.

**Vertical reference (DOCUMENTED, GEBCO_2026 page):** "The GEBCO grid can be assumed to be relative to WGS84. GEBCO's global elevation models are generated by the assimilation of heterogeneous data types, assuming all of them to be referred to Mean Sea Level. However, in some shallow water areas, the grids include data from sources having a vertical datum other than mean sea level."

**TID (DOCUMENTED + OBSERVED):** per-cell Type Identifier grid. Direct measurements: 0 land, 10 singlebeam, 11 multibeam, 12 seismic, 13 isolated sounding, 14 ENC sounding, 15 lidar, 16 optical light sensor, 17 combination of direct methods. Indirect: 40 predicted from satellite gravity, 41 interpolated by algorithm, 42 contours from charts, 43 contours from ENCs, 44 multi-source gridded with gravity-guided interpolation, 45 flight gravity, 46 iceberg draft, 47 Argo, 48 animal loggers. Unknown: 70 pre-generated grid, 71 unknown, 72 steering points. Falmouth box (1,728 px): TID 0 x1118, 17 x552, 16 x20, 40 x28, 44 x10.

**Vintage:** GEBCO_2026, April 2026 (DOCUMENTED). Base is SRTM15+ V2.8 plus Seabed 2030 regional grids (DOCUMENTED).

**Licence (DOCUMENTED, terms page):** "The GEBCO Grid is placed in the public domain and may be used free of charge." Users may copy, adapt, and commercially exploit; must "Acknowledge the source of The GEBCO Grid" and not suggest official endorsement; "should NOT be used for navigation". Attribution: "GEBCO Bathymetric Compilation Group 2026 (2026). The GEBCO_2026 Grid - a continuous terrain model for oceans and land at 15 arc-second intervals. NERC EDS British Oceanographic Data Centre NOC. doi:10.5285/4f68d5c7-45eb-f999-e063-7086abc036fa". GEBCO also warns: "the resolution of The GEBCO Grid may be significantly different to that of the resolution of the underlying measured data."

**Antimeridian:** WMS and the app's grid use a -180..360 longitude range (DOCUMENTED on the WMS page; OBSERVED in `/api/grids`). A queued download across 180 was **not tested** (UNKNOWN).

**Bytes for test area:** 0.2 x 0.15 deg: 23 KB of rasters, 436 KB zip (mostly PDFs).

**Suitability:** best provenance (TID) for the "never present interpolated data as measured" rule, global. Risk: the download API is undocumented. Fallback is the 7 GB global NetCDF from the app.

---

## 4. ETOPO 2022 (NOAA NCEI)

**Docs:** https://www.ncei.noaa.gov/products/etopo-global-relief-model ; User Guide https://www.ngdc.noaa.gov/mgg/global/relief/ETOPO2022/docs/1.2%20ETOPO%202022%20User%20Guide.pdf (OBSERVED, read).

**Access (OBSERVED):** static GeoTIFF tiles, no auth:
`https://www.ngdc.noaa.gov/mgg/global/relief/ETOPO2022/data/15s/15s_surface_elev_gtif/ETOPO_2022_v1_15s_N60W015_surface.tif` (directory listing has 288 tiles; also `15s_bed_elev_gtif`, `15s_geoid_gtif`, `15s_surface_sid_gtif`, `15s_bed_sid_gtif`; 30s/60s sets exist). Tile = 15 x 15 deg, **name gives the top-left corner** (`N60W015` origin -15, 60; covers lon -15..0, lat 45..60). So tile selection is pure arithmetic.
- `/vsicurl` range reads work: `gdal_translate -projwin -5.2 50.2 -5.0 50.05 /vsicurl/<url>` produced a 48x36 px, 6,436 B file in 2.4 s (min -63.113, max 201.471).
- tile: 3600x3600, pixel 0.004166667 deg, Float32, **NoData -99999**, 256x256 blocks, `AREA_OR_POINT=Area`, `TIFFTAG_DATETIME=20220929125058`, CRS has `VERTCRS["EGM2008 height"]` (EPSG:9518). Land positive.
- THREDDS/NCSS: the page links catalogs under https://www.ngdc.noaa.gov/thredds/catalog/global/ETOPO2022/15s/... but my guessed NCSS URL (`.../thredds/ncss/global/ETOPO2022/15s/15s_surface_elev_netcdf/ETOPO_2022_v1_15s_N60W015_surface.nc?var=z&north=...`) returned **404**; the right NCSS path is UNKNOWN. Not needed given the tiles.

**Vertical (DOCUMENTED, User Guide):** "referenced in meters relative to the Earth Gravitational Model of 2008 (EGM2008) geoid surface (EPSG:3855)". "For most purposes, EGM2008 is an adequate approximation of mean sea level." A `geoid` tile converts to WGS84 ellipsoid heights.

**Source ID (OBSERVED + DOCUMENTED):** each 15" tile has an integer SID tile (`15s_surface_sid_gtif/..._sid.tif`, Byte, NoData 0, same 3600x3600 grid). 30"/60" grids have none (User Guide). Falmouth window SID values: 1 (316 px), 5 (262 px), 10 (1150 px) of 1,728. The meaning of those codes is in the User Guide section 5 table; I did not extract it (UNKNOWN here).

**Vintage:** 2022 release (citation year 2022; file timestamp 2022-09-29).

**Licence/citation:** product page citation: "NOAA National Centers for Environmental Information. 2022: ETOPO 2022 15 Arc-Second Global Relief Model. NOAA National Centers for Environmental Information. DOI: 10.25921/fd45-gt74. Accessed [date]." The page also states, in a data-classes list, "Full global coverage, no copyright or security distribution restrictions"; I did not verify which product class that sentence applies to. No other licence text found (UNKNOWN).

**Bytes:** 6.4 KB data + 0.9 KB SID for the Falmouth box. Fixtures: `fixtures/etopo_falmouth.tif`, `fixtures/etopo_falmouth_sid.tif`.

**Suitability:** easiest, most stable global source (static URLs, range reads, SID). Same 15" as GEBCO but with an explicit EGM2008 reference. It is also the fallback inside NOAA's DEM_global_mosaic.

---

## 5. Terrarium-encoded tiles

### 5a. Seascape (openwatersio)

`docs/research/ecosystem.md` section 5 describes it; I re-checked.
- TileJSON (OBSERVED): `https://tiles.openwaters.io/seascape/raster.json`: `"tiles":["https://tiles.openwaters.io/seascape/{z}/{x}/{y}.webp"]`, `minzoom 0`, `maxzoom 18`, `"encoding":"terrarium"`, bounds +/-85.0511. **The tiles are WebP, not PNG**, and **512x512** (OBSERVED, `gdal_translate` read them).
- Decode (DOCUMENTED, repo README): `v = R*256 + G + B/256 - 32768`. `v < 0` is depth below the winning source's datum ("shallow-biased"); non-negative values are category codes: 0 water of unknown depth, 1 drying foreshore, 2 land. So **land has no elevation**.
- OBSERVED decodes: Falmouth z10 tile (497/346): min -64, max 2 (land), 60 distinct values, 16,248 B. Falmouth z12: min -10.5. Key West z14: min -3.625. Values are quantised (integer steps offshore at z6, half-metre at z12). A Falmouth z14 tile (7959/5548) was 54 B, all value 2.0 (land).
- Ground resolution per pixel = 40,075,017 m x cos(lat) / (512 x 2^z): z10 at 50N about 49 m, z12 about 12 m, z14 about 3 m (arithmetic, not measured). Whether real data exists at those zooms depends on the source; Falmouth z12 showed depth, so regional sources exist.
- Datum (DOCUMENTED, README): "Depth datum varies by source. Some regional sources use a low-water chart datum (such as LAT or MLLW), while other sources are approximately MSL. The data is interpolated and merged ... then smoothed during tiling, so values are approximate."
- Licence (DOCUMENTED): tiles CC BY 4.0; attribution `© Open Waters (https://openwaters.io/charts/seascape#license)`; "The underlying data remains under each source's own open terms." Provenance footprints: `https://tiles.openwaters.io/seascape/coverage.json` (MVT; not decoded).
- Suitability: small hobby-scale service (best-effort endpoint). Quantised, mixed-datum, smoothed, interpolated-and-merged. Fails the repo's "keep source resolution and survey fraction" rule. Reference only.

### 5b. AWS Terrain Tiles (Mapzen), Terrarium

- URL (OBSERVED, 200): `https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png`, 256x256 RGB PNG, ~45-120 KB each.
- Formula (DOCUMENTED, joerd docs/formats.md): `(red * 256 + green + blue / 256) - 32768`, Web Mercator.
- Content: ocean bathymetry source is "ETOPO1 for ocean bathymetry, 1 arc-minute resolution globally" (DOCUMENTED, joerd docs/data-sources.md). OBSERVED (correct bip decode): Falmouth z10 min -67.8 max 240.5, 12,083 of 65,536 px negative; Key West z6 min -3480; but **Key West z12: 32,898 of 65,536 px are exactly 0** (flat zeros over half the tile; cause not investigated); Key West z10 659 zeros. So offshore is coarse and partly flat.
- Attribution (DOCUMENTED, joerd docs/attribution.md): "Attribution is required for many terrain tile data providers ... you are responsible for researching each project to follow their license terms", with a required list including "Global ETOPO1 terrain data U.S. National Oceanic and Atmospheric Administration". Service-level licence/SLA of the S3 bucket: UNKNOWN (not read).
- Suitability: no, ETOPO1 is superseded by sources 3 and 4.

Fixtures: `fixtures/sea_falmouth_z10.webp` (16 KB), `fixtures/aws_falmouth_z10.png` (120 KB).

---

## 6. EMODnet Bathymetry (WCS)

**Endpoint (OBSERVED, matches `internal/emodnet/emodnet.go`):** `https://ows.emodnet-bathymetry.eu/wcs`, WCS 2.0.1.
- GetCapabilities coverages (OBSERVED): `emodnet__mean`, `emodnet__mean_2016`, `_2018`, `_2020`, `_2022`, `emodnet__mean_atlas_land`, `_multicolour`, `_rainbowcolour`. **No source-reference, quality-index, standard-deviation or interpolation-flag coverage exists in this WCS.** The EMODnet page (DOCUMENTED) says the map viewer has a "sources" layer and a "Quality Index ... improved source reference layer"; whether WMS/other services expose them was not probed (UNKNOWN). Capabilities say `Fees NONE`, `AccessConstraints NONE`.
- DescribeCoverage `emodnet__mean` (OBSERVED): Lat 11..90, Long -70.5..43.0 (EPSG:4326), grid 108,960 x 75,840, offset 0.0010416667 deg (1/960 = 1/16 arc-minute), range field "Depth", nil value **NaN**.
- GetCoverage that worked (OBSERVED): `...wcs?service=WCS&version=2.0.1&request=GetCoverage&coverageId=emodnet__mean&format=image/tiff&subset=Lat(50.05,50.20)&subset=Long(-5.20,-5.00)` returned 192x144 Float32, 110,976 B, origin (-5.2, 50.2), pixel 0.0010417, min -63.85, max 205.13 (land included as positive). The same request for Key West returned an OWS ExceptionReport (404): outside coverage.
- Mean-depth layer description (OBSERVED in capabilities): "Mean depth based on source resolution of 1/16 arc minute (~125 meter)". Older coverages: `_2016`, `_2020`, `_2022` abstracts mention 1/16 or 1/8 arc minute; I did not pull the release-to-year mapping.

**Vertical datum (DOCUMENTED, https://emodnet.ec.europa.eu/en/bathymetry): LAT for the DTM; MSL exists only as a separate download.** "Mean depth full coverage with water depth to Lowest Astronomical Tide (LAT) reference from EMODnet DTM ... complemented by land DTM and GEBCO coverage." And: "The bathymetric data as collected from the data providers, are supposed to be referenced at the Lowest Astronomical Tide (LAT) (International Hydrographic Office (IHO) resolution 3/1919) ... the EMODnet Digital Terrain Model has been developed and is published for viewing and downloading in LAT." The same page states: "Note: The water depth values are in metres with reference to LAT", and that an MSL version (LAT plus a GTSM/Deltares tide-model offset) "applies for the DTM tiles in the ESRI ASCII format, which are downloadable both in LAT and MSL". So the MSL variant is a tile download, not a WCS product; the WCS capabilities and DescribeCoverage contain no LAT/MSL string (grep OBSERVED), so the WCS datum is **documented only indirectly (page: LAT)**. The vertical reference of the land part and the GEBCO fill (2024 30" + IBCAO V4) is not stated (UNKNOWN). The full DTM cell record (DOCUMENTED) has min/avg/max depth, standard deviation, number of values, an interpolation flag marking extrapolated cells, a smoothed depth and "reference to the prevailing source of data"; none of these are exposed by the WCS. Do not trust the DescribeCoverage unit field: it declares `swe:uom code="W.m-2.Sr-1"` (a radiance unit) for depth (OBSERVED).

**Licence/citation (DOCUMENTED, https://emodnet.ec.europa.eu/en/terms-use-emodnet-online-services-data-and-data-products):** "Unless indicated otherwise, data products created by EMODnet are owned by the EU and therefore licensed under Creative Commons CC-BY 4.0." Users should "verify any metadata ... to properly cite the data originator and consult any use restrictions or licences of individual data originators." Citation: "EMODnet Digital Bathymetry (DTM 2024). EMODnet Bathymetry Consortium https://doi.org/10.12770/cf51df64-56f9-4a99-b1aa-36b8d7b743a1".

Fixture: `fixtures/emod_falmouth.tif` (111 KB).

---

## 7. GMRT GridServer

**Docs (OBSERVED, read):** https://www.gmrt.org/services/gridserverinfo.php. Endpoint `https://www.gmrt.org/services/GridServer` (matches `internal/gmrt/gmrt.go`); metadata at `.../GridServer/metadata?...&mformat=json` (JSON includes `meters_per_node`, `width`, `height`, `nodes`, `file_size_netcdf`, `gmrt_version` 4.5.0, `gmrt_release_date` "June 2026").

**(a) Vertical reference:** **UNKNOWN.** I read the GridServer info page, Terms of Use, and About page, and grepped the XML metadata response (`GridServer/metadata?...&mformat=xml`) for "datum", "vertical" and "sea level"; nothing found. The only related text: unmasked `topo` is "filled with GEBCO_08 where high-resolution data do not exist in the ocean" (GEBCO's own statement is "assuming ... Mean Sea Level" for its grids, see section 3, for a different release).

**(b) `resolution=max` on large bboxes (OBSERVED):**

| bbox (lon, lat) | metadata (width x height, nodes) | meters_per_node / zoom_level | file size | delivered? |
|---|---|---|---|---|
| 0..2, 40..42 | 3641 x 4826, 17,571,466 | 61.15 / 1024 | 70,286,592 (metadata), **70,339,410 B actual** | **downloaded**: 3641 x 4827 px, pixel 0.000549467 x 0.000414608 deg, 36 s |
| 0..5, 40..45 | 9103 x 12357, 112,485,771 | 61.15 / 1024 | 449,943,812 | metadata only (budget) |
| 0..10, 40..50 | 9103 x 12923, 117,638,069 | **122.30 / 512** | 470,553,004 | metadata only (budget) |
| -180..180, -80..80 (metadata) | 10240 x 7942, 81,326,080 | 3913.58 / 16 | n/a | metadata only |

- 2x2 deg delivered what its metadata predicted, within one row (4827 vs 4826 rows). The 10x10 deg request is **silently coarsened by one zoom level (61 m to 122 m)** in the metadata; no error, `lessThanMaxNodes: true`. The only signal is `meters_per_node` / `zoom_level`. The node cap is only bracketed by my data: accepted 117.6M nodes (10x10 at zoom 512) and 112.5M (5x5 at zoom 1024); rejected (coarsened) about 450M (10x10 at zoom 1024) and the global box (which fell to zoom 16, finer zoom 32 would be about 325M). So the cap lies between about 118M and 325M nodes (about 470 MB), well under the documented "up to 2GB, ~20 by 20 degrees at 100 m". I did not download the 5x5 or 10x10 files, so their delivered pixel sizes are inferred from metadata (the 2x2 case matched exactly).
- `grid_bounds` expand past the request (2x2: west -0.000275, north 42.0005); read the geotransform.
- **Pixels are not square in degrees**: nodes are equal-metre. Falmouth lon step 0.000550826, lat step 0.000352998 (ratio 0.641 = cos 50.12 deg); 41N: 0.000549467 vs 0.000414608 (0.755 = cos 41 deg). So "61 m" is the equator figure; at Falmouth nodes are about 39 m. Any code assuming square degree pixels will be wrong.
- `mresolution=300` for the Falmouth box returned the **identical** 365x429 grid as `resolution=max` (docs say `mresolution` overrides `resolution`); why is UNKNOWN. `resolution=default` for the 1-deg Fiji box returned 914x951 (<1000 nodes, as documented).

**(c) Antimeridian (OBSERVED):** **works, no error.** `minlongitude=179.5&maxlongitude=-179.5` and `minlongitude=179.5&maxlongitude=180.5` returned byte-identical-size files (3,481,030 B at `resolution=default`), 914x951 px, origin lon 179.4974, i.e. continuous longitudes above 180 (metadata `grid_bounds` 179.4996..180.5004). `-180..-179.5` returned origin 179.9994, 457 px: western-hemisphere requests come back as 180..180.5. `179.5..180` returned 457 px from 179.4974. CRS is plain EPSG:4326 with longitudes > 180, so a tiler must wrap or reproject.

**(d) topo vs topo-mask (OBSERVED + DOCUMENTED):** DOCUMENTED: `topo` "The unmasked grid. Unmasked grids are filled with GEBCO_08 where high-resolution data do not exist in the ocean." `topo-mask` "contain only high-resolution data (about 100 m) in the ocean, NaNs elsewhere." OBSERVED Falmouth box (156,585 px): `topo` has 730 NaN, `topo-mask` has 14,434 NaN (9.2%); both min -63.889; masked mean differs (46.26 vs 42.17 incl. NaN handling in stats). Unexplained: 730 NaN in the unmasked grid. This is the only global source here that exposes a (binary) measured-fraction.

**Licence (DOCUMENTED, https://www.gmrt.org/about/terms_of_use.php):** "licensed under a Creative Commons Attribution 4.0 International (CC BY 4.0) License. Appropriate acknowledgment with a byline/credit/link must be given ... Data should not be used for navigation purposes." Citation: Ryan, W. B. F., S.M. Carbotte, J. Coplan, S. O'Hara, A. Melkonian, R. Arko, R.A. Weissel, V. Ferrini, A. Goodwillie, F. Nitsche, J. Bonczkowski, and R. Zemsky (2009), Global Multi-Resolution Topography (GMRT) synthesis data set, Geochem. Geophys. Geosyst., 10, Q03014, doi:10.1029/2008GC002332.

**Bytes for test area:** Falmouth 0.2x0.15 deg: 627,394 B (365x429 Float32).

Fixtures are over the 300 KB target (Falmouth 627 KB), so not recommended: `fixtures/gmrt_falmouth_topo_mres300.tif`, `fixtures/gmrt_falmouth_topomask_mres300.tif`, `fixtures/gmrt_antimeridian_179.5_to_-179.5_mres3000.tif` (3.5 MB). **Naming warning:** the `mres300` files are really the max grid (365x429, about 39 m at Falmouth, because `mresolution` was ignored) and the `mres3000` file is the `resolution=default` grid (914x951); do not write tests assuming 300 m or 3000 m. A stray 70 MB `gmrt/big2.tif` (2x2 deg download) also remains in the scratch dir.

---

## Other things noticed

- NOAA also runs `multibeam_mosaics/*`, `NOS_MBAB/*` (multibeam bathymetry and backscatter ImageServers) and `bag_bathymetry`/`csb` MapServers: possible measured-only multibeam sources; not probed.
- Seascape's `sources/README.md` (already in ecosystem.md) is a good catalogue of regional open bathymetry (INFOMAR, UK EA/CCO, DDM, Kartverket, Litto3D...).
- `download.gebco.net` also lists polar grids (IBCAO/IBCSO) as separate `grid_id`s.

---

## Vertical datum findings

| Source | Documented vertical reference | Where |
|---|---|---|
| NOAA DEM_global_mosaic | **Mixed per catalog item**: NAVD 88 (1477 items), Sea Level (168), MHW (106), MHHW (31), unspecified (104), others; ETOPO 2022 fallback items have `VerticalDatum` null (OBSERVED) though ETOPO itself is EGM2008 | `query ... outStatistics` on the ImageServer |
| NOAA CUDEM 1/9" | **NAVD88 height** (EPSG:5703), horizontal NAD83 | file CRS (OBSERVED) + ISO XML |
| GEBCO_2026 | WGS84; "assuming all of them to be referred to Mean Sea Level", with some shallow-water sources on other datums | gebco2026-grid page |
| ETOPO 2022 | **EGM2008 geoid** (EPSG:3855/9518); "adequate approximation of mean sea level" | User Guide + file CRS |
| Seascape | **Varies per source**, LAT/MLLW for some, approx MSL for others; not normalised | repo README |
| AWS Terrarium | Not stated in the pages read (underlying ETOPO1 etc.) | UNKNOWN |
| EMODnet DTM | **LAT** (IHO 3/1919) for the published DTM; an MSL version is offered only as ESRI ASCII tile downloads; the WCS coverage itself declares no datum (UNKNOWN); land/GEBCO fill reference not stated | emodnet.ec.europa.eu/en/bathymetry |
| GMRT | **Not stated** on the pages read | UNKNOWN |

Implication: nothing here shares one datum. The manifest should record `vertical_datum` per source (and per CUDEM/NOAA-catalog item) and refuse to merge sources with different datums without an explicit conversion; GEBCO/ETOPO are the closest to a common MSL-like surface, CUDEM is NAVD88, EMODnet is LAT.

---

## Recommendation

Ranked by user value against feasibility for an on-demand bbox adapter:

1. **ETOPO 2022 via `/vsicurl` tiles (+ SID tile).** Static, documented, stable URLs; tile choice is pure arithmetic; NoData -99999; explicit EGM2008; per-cell source ID; DOI citation. Best low-risk global baseline. Needs: tile math, window read, SID decode table from the User Guide.
2. **GEBCO_2026 via the download.gebco.net API (or the 7 GB file) with TID.** Best global bathymetry and the only global per-cell measured-vs-interpolated type grid, public domain. Risk: the API is undocumented and has changed its schema before (422 on the old body); design the adapter to also accept a local GEBCO NetCDF. Antimeridian behaviour untested.
3. **NOAA CUDEM 1/9" COG tiles via `/vsicurl`.** Highest resolution (3 m) and explicit NAVD88, public domain, but US-only, 200-350 MB tiles with slow windowed reads (34 s for 9 MB), and no source layer. Needs `urllist8483.txt` parsing. High value for US scenes (e.g. Key West).
4. **EMODnet WCS (already in repo).** Keep for Europe (115 m, CC BY 4.0, simple WCS, NaN nodata). Record datum as LAT; no quality layer available via WCS.
5. **GMRT GridServer (already in repo).** Keep, but: handle non-square degree pixels, handle silent coarsening on large bboxes (read metadata first and compare `meters_per_node` to what you asked for), wrap longitudes above 180, document that the vertical datum is unstated, and prefer `topo-mask` to compute the measured fraction.
6. **NOAA gis.ngdc `exportImage` mosaic.** Easy, but mixed datums, bilinear by default, zero-fill beyond 180, 15" ETOPO outside the US, and no source layer in the output. Only worth it as a US shortcut if combined with `identify`/`query` provenance; otherwise use CUDEM directly (#3).
7. **Seascape tiles.** Quantised, smoothed, mixed datum, land encoded as category codes; reference only.
8. **AWS Terrarium.** ETOPO1-based ocean, flat zeros seen; superseded.
