# OceanBuilder: ecosystem research

Checked 2026-10-30. Method: GitHub API (`gh api`) for repo metadata, shallow clones for cgo checks, WebFetch/WebSearch for docs. Anything not read from a fetched page is marked "unverified". Star counts and push dates are snapshots from that day.

Context: OceanBuilder is a Go CLI that shells out to GDAL, uses GMRT GridServer (topo + topo-mask; topo is GEBCO_08-filled where no ~100 m survey exists) and OSM coastline via Overpass, and writes tiled worlds for Unity/Unreal.

---

## 1. LibreTerrain

**Correction (2026-10-30, later):** web/GitHub searches found nothing, but LibreTerrain is installed locally at `../LibreTerrain/` (a macOS app bundle plus `libreterrain-mcp`, dual MIT/Apache-2.0: `LICENSE-MIT`, `LICENSE-APACHE`). The agent's "no standalone project" finding (searches: `gh search repos/code`, `npm view`, WebSearch; only an unrelated `cismet/carma` map widget) was about public availability only. No source tree is present, so the facts below come from the binary's embedded strings and the bundle layout, **not from running it or reading docs**; treat them as unverified until exercised.

What the binary indicates:
- Rust node-graph terrain editor (`crates/terrain-core`, `terrain-nodes`); project file `.lterrain` with world size and `height_range_m`; CPU builds; GPU preview.
- Procedural nodes (fBm, ridged, canyon, dunes, plateau, terrace...), hydrology (`data.flow`, wetness), snow, polygons for flat areas (sea floor height outside polygons).
- **Heightmap import** node `primitive.file`: EXR or 8/16-bit PNG stretched over the whole world; values either metres (EXR) or 0..1 mapped to the world height range or to `low_m`/`high_m`; `flip_y` option. Also `colour.image` for aerial imagery.
- **Exports**: PNG16 (0..65535 = height range), EXR32, tiles with `{name}_x{x}_y{y}` patterns, masks (slope, flow, snow...), point files for vegetation, and `build.json` with world size and height range. Prompts mention import helpers for Unreal, Godot and Blender (Unreal helper noted as untested inside Unreal). No Unity helper mentioned.
- `libreterrain-mcp` is an MCP bridge (`--project file.lterrain` serves a project without the editor; tools such as `get_graph`, `edit_graph`, `mark_export`, `start_build`, `get_build_status`). Not a plain batch CLI.

| Item | Answer |
|---|---|
| Solves | Land shaping and erosion/hydrology on heightfields; fills in procedural detail where DEM data is coarse; exports engine-friendly heightmaps and masks. |
| Reuse | File exchange: (a) we emit a GeoTIFF/PNG16 plus metres range that a `primitive.file` node can ingest, so users refine land in LibreTerrain; (b) we ingest its exported PNG16/EXR as a "user-provided DEM" through our `local` source. |
| Cannot do | Real-world CRS, bathymetry merging, provenance, tile-edge validation across georeferenced tiles, coastline data. It stretches an image over the world, so georeferencing stays on our side. |
| Integration | File exchange first. Driving it automatically needs the MCP protocol (stdio JSON-RPC), a later experiment. |
| License | MIT or Apache-2.0: safe as an optional external tool; do not bundle the binary without checking redistribution of the app bundle. |
| Verdict | **Optional complementary tool via files; keep out of the core.** Next step: hand-build one Falmouth `.lterrain` project that imports our heightmap and check the PNG16 round trip against our checkpoints. |


---

## 2. Immersive Ocean / MSP-ProceduralOceanView

Repo: https://github.com/BredaUniversityResearch/MSP-ProceduralOceanView (README says "Immersive Ocean (formerly Procedural Ocean View)"). C++, 2 stars, pushed 2026-03-03. Developed by Breda University of Applied Sciences (Cradle Lab) under EU H2020 ILIAD; companion of the MSP Challenge serious game.

- What it is (README): Unreal Engine 5.3.x platform that generates 3D maritime scenes from 2D geodata via "Procedural Rule Packs" (PRPs). Inputs listed: GeoJSON, Shapefile, GeoTIFF (bathymetry/terrain), WFS/WMS/WCS, JSON. PC and VR (Quest 2/3). Data flow: MSP Challenge -> GeoServer -> JSON export -> IO via "POV Launcher".
- Limits stated in README: targets European regions (EPSG:3035); other regions "require additional projection configuration"; open-source drop omits paid Marketplace assets (Waterline PRO water shader, some meshes), so water visuals are degraded without them; VR retrofitted.
- **License discrepancy:** GitHub API reports `AGPL-3.0` for the repo; the README "Licence" section says GPL-3.0-only. I did not open the LICENSE file text. Treat as copyleft either way; unverified which is authoritative.

| Item | Answer |
|---|---|
| Solves | Runtime scene dressing (turbines, shipping, fish, weather) from GIS layers in UE5 for maritime spatial planning. |
| Reuse | Ideas only: the PRP concept (data-driven placement rules), EPSG:3035 handling, Open-Meteo weather feed. No code reuse. |
| Cannot do | Not a data compiler. It consumes GeoTIFF bathymetry; it does not fetch, mosaic or tile from GMRT/GEBCO. Unreal-only, version-pinned to 5.3. |
| Integration | File exchange only: our GeoTIFF/GeoJSON output could feed its POV Launcher (feasibility unverified; format specifics not read). |
| License | GPL-3/AGPL-3 (see above). Copying code into an MIT/Apache OceanBuilder would force relicensing. Calling it as a separate downstream consumer of our files carries no obligation. |
| Verdict | **Reference only.** Do not link or vendor code. Possibly document "output can be imported into IO" later. |

---

## 3. Cesium (Cesium for Unity / Cesium for Unreal / Cesium ion / 3D Tiles)

Sources: https://github.com/CesiumGS/cesium-unity , https://github.com/CesiumGS/cesium-unreal , https://github.com/CesiumGS/cesium-native , https://github.com/CesiumGS/3d-tiles , https://cesium.com/platform/cesium-ion/pricing/

- cesium-unity: Apache-2.0, C#, 544 stars, pushed 2026-10-29, latest release v1.26.1 (2026-10-29).
- cesium-unreal: Apache-2.0, C++, 1249 stars, pushed 2026-10-29, latest v2.31.0 (2026-10-29). README has an engine-version/plugin-version table (UE 5.0 -> v1.31.1, 5.1 -> v2.5.0, 5.2 -> v2.11.0, ...; rest not read).
- cesium-native: Apache-2.0, C++, shared core. README of both plugins: "free for both commercial and non-commercial use."
- 3D Tiles: open spec built on glTF; main branch is 1.1, 2.0 "in development"; 1.0 approved as an OGC Community Standard 2018-12-14 (page). Spec license not stated on the page I fetched (unverified).
- Cesium ion pricing page: Community tier free for personal/non-commercial/evaluation (10 GB storage, 15 GB/month streaming); Commercial from $149/month; paid license required if company has >$50K annual gross revenue or raised >$50K, or for government projects or funded research. Page says all plans include terrain; it does not name "Cesium World Bathymetry" (unverified whether that asset is covered). Page does not describe custom-data upload/tiling limits in detail.

| Item | Answer |
|---|---|
| Solves | Streaming a full-scale WGS84 globe into Unity/UE, with a standard tile format (3D Tiles) and quantized-mesh terrain. Handles floating origin, georeferencing, LOD streaming. |
| Reuse | (a) 3D Tiles as an optional output format. (b) Georeferencing conventions (ECEF/CRS-origin). (c) cesium-native is Apache-2.0 C++, not usable from Go without cgo/FFI and not worth it. |
| Cannot do | It does not compile bathymetry+coastline into game-ready local worlds; our users want flat local tiles with game physics, not a globe. Terrain in ion is hosted; ion bathymetry quality/resolution not verified. |
| Integration | File exchange: emit GeoTIFF/glTF and let users upload to ion, or ship our own tileset.json. Writing 3D Tiles from Go is a large task (unverified effort). Alternatively document a "use Cesium plugin with our heightmaps" recipe. |
| License | Plugins Apache-2.0: compatible with any license. ion service is a paid, rate-limited SaaS: do not make it a hard dependency of an open-source CLI. |
| Verdict | **Optional later output target, not a core dependency.** Low priority until local tiled export is solid. |

---

## 4. gen-map

**Finding: could not identify a project named "gen-map".**
- GitHub API repo search `gen-map in:name` returned only: Azgaar/Fantasy-Map-Generator, ProbableTrain/MapGenerator, MappingGenerator (Roslyn), MapTileGenerator, FantasyMapGenerator (rlguy), a Sketch plugin. `genmap in:name`: genome mappability tool, a Go integer-hash-map library, an nmap script, etc. None is a bathymetry/heightmap/game-world tool.
- WebSearch "gen-map GitHub heightmap generator real-world elevation Unity Unreal GDAL tiles": no repo of that name. Surfaced instead (not fetched in detail, summary from the search result text only): vazgriz/Mapper (Mapbox elevation -> 16-bit RAW heightmaps for Unity/Unreal, tiling support), pepijndevos/citiesheightmap (SRTM, Python), Bother (OpenTTD).
- Sources: https://github.com/vazgriz/Mapper , https://github.com/pepijndevos/citiesheightmap

| Item | Answer |
|---|---|
| Solves / Reuse / Integration | Unknown: project not found. |
| Verdict | **Unverified; no source URL was supplied.** Closest real comparator is vazgriz/Mapper (Mapbox-based, land only, per search summary); worth a look as prior art for tile stitching but license not checked. |

---

## 5. Open Waters / Seamap (and Seascape)

Org: https://github.com/openwatersio . Site: https://openwaters.io

- **openwatersio/seamap**: TypeScript, GPL-3.0, 16 stars, pushed 2026-10-29. "Nautical-themed map with depths, seamarks, hazards", rebuilt weekly from OSM seamark data. Vector tiles z0-14 at `https://tiles.openwaters.io/seamap/tiles.json`; style at `.../style.json`; whole-planet PMTiles per build at `.../<YYYY-MM-DD>.pmtiles`. Tiles/styles CC-BY 4.0 with attribution; derived from OSM (ODbL). Code GPL-3.0 except the `style/` npm package (`@openwaters/seamap`, MIT, sprites excepted). Fork of prozessor13/seamap (MIT, Java, pushed 2026-10-27). Source: https://github.com/openwatersio/seamap
- **openwatersio/seascape** (more relevant): Python, 21 stars, pushed 2026-10-30, code BSD-3-Clause; tiles CC BY 4.0, "underlying data remains under each source's own open terms". Global bathymetry as Terrarium-encoded raster DEM tiles (`https://tiles.openwaters.io/seascape/raster.json`) and MVT contours/soundings. Terrarium decode: `v = R*256 + G + B/256 - 32768` m; negative = depth, non-negative codes are categories (0 unknown-depth water, 1 drying, 2 land: so land carries no elevation). Global to z8 (~15"), regional detail up to z14-z16. Datum varies per source (GEBCO/EMODnet-MSL variants vs LAT/MLLW elsewhere); not normalized yet. Source: https://github.com/openwatersio/seascape
- Seascape's `sources/README.md` is a useful verified catalogue of open regional bathymetry (GEBCO 2026, EMODnet 2024, Danish DDM, Kartverket, NOAA CUDEM/S-102/NOS estuarine, Dutch Vaklodingen, INFOMAR, England SurfZone/EA/CCO multibeam, Litto3D Bretagne, German DGM-W, GSC Atlantic/Pacific, gbr30, AusBathyTopo, BATNAS, NZ LiDAR, Great Lakes, lakes). It states selection rule "resolution sets the zoom cap, an openly-redistributable license is the gate", and lists enumerate/materialize pipeline patterns. Read: https://github.com/openwatersio/seascape/blob/main/sources/README.md

| Item | Answer |
|---|---|
| Solves | Web chart rendering; a pre-merged open global-to-coastal bathymetry mosaic with provenance. |
| Reuse | (1) Seascape tiles as an *alternative bathymetry source* to GMRT: free, global, 512px Terrarium tiles, fetchable by plain HTTPS and decodable in Go with `image/png`. (2) Its source catalogue as our regional-source roadmap. (3) Its datum documentation. |
| Cannot do | Max resolution is whatever the best source is per region; tile quantization is shallow-biased (rounds toward shallower), so it is not survey-grade; land has no elevation; not for navigation; datums mixed. |
| Integration | HTTP tile fetch + stitch to GeoTIFF (we write that in Go or via `gdal_translate` on a WMTS/XYZ description). Or download the dated PMTiles planet archive (size unverified). |
| License | Tiles CC BY 4.0: attribution string required in our output metadata. OSM-derived layers carry ODbL. Seamap code GPL-3.0: do not copy; seascape code BSD-3 is safe to read/adapt. |
| Verdict | **Worth integrating as optional source "seascape"** (high value, low cost). Seamap: reference only. Note the service is a small project (21 stars); treat the endpoint as best-effort and cache. |

---

## 6. OpenSeaMap

Sources: https://wiki.openstreetmap.org/wiki/OpenSeaMap , https://www.openseamap.org , https://github.com/OpenSeaMap , https://github.com/k-yle/OpenSeaMap-vector

- Wiki: all OSM-hosted OpenSeaMap data is ODbL; "Chart tiles are available under CC-BY-SA". Data scheme follows IHO S-57 / S-101; tagging details are on the wiki "Seamarks" pages. I did not fetch the tagging page, so the exact `seamark:type=*` value list is **unverified here** (the namespace `seamark:` is well known but not read from a fetched page in this session).
- Seamark raster overlay endpoint is in the site's own JS: `https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png` (TMS-style, 18 zoom levels); a HEAD request returned HTTP 200 on 2026-10-30. No formal usage policy was found on the wiki page.
- Repos: OpenSeaMap/online_chart (PHP, 78 stars, last push 2026-01-26, no license field in API), k-yle/OpenSeaMap-vector (SVG, 15 stars, pushed 2026-10-16, no license field), OpenSeaMap/vectortiles-generator (last push 2016, stale).
- Our existing Overpass route can already pull seamarks: query `nwr["seamark:type"]` (query form is standard Overpass, but the tag name itself is unverified here).

| Item | Answer |
|---|---|
| Solves | Crowd-sourced aids to navigation, harbours, lights, buoys, wrecks, separation schemes. |
| Reuse | Seamark data as game-object placement layer (buoys, lighthouses, wrecks): a feature with strong value for maritime games. |
| Cannot do | Bathymetry (the raster depth layer is a separate, restricted contribution; not bulk-licensed here). Coverage is uneven; not authoritative. |
| Integration | Overpass query -> GeoJSON points -> placement manifest. No new dependency. Do not hotlink the tile server in a CLI that bulk-downloads (no policy found, so unverified). |
| License | ODbL: attribution plus share-alike on *derived databases*. Document it in the output manifest. Compatible with our OSM coastline use. |
| Verdict | **Integrate seamarks via Overpass** (cheap, same pipeline as coastline). Ignore the tile server. |

---

## 7. Data sources

### 7.1 GEBCO
- Site lists **GEBCO_2026** as the current grid (15 arc-second, elevation in m, plus a TID source-type grid); formats netCDF, Esri ASCII, Data GeoTIFF; OPeNDAP via CEDA. A GEBCO_2025 page also exists (netCDF, 43200x86400, global file 7.5 GB, TID 4 GB, DOI 10.5285/37c52e96-24ea-67ce-e063-7086abc05f29).
- Terms: "placed in the public domain and may be used free of charge"; commercial use allowed under site terms; not for navigation. Citation requested.
- Access: user-defined-area download app (interactive, no documented stable REST endpoint found; unverified), or the full global file. 
- Sources: https://www.gebco.net/data-products/gridded-bathymetry-data , https://www.gebco.net/data-products-gridded-bathymetry-data/gebco2025-grid
- Relevance: we already get GEBCO_08 indirectly via GMRT. Using the current GEBCO grid directly (local file, `gdalwarp -te`) would be newer. Datum is approximately MSL (per Seascape README), not a chart datum.

### 7.2 NOAA ETOPO 2022
- 15, 30, 60 arc-second; Ice Surface and Bedrock versions; GeoTIFF and NetCDF; Grid Extract web tool, direct HTTPS GeoTIFF downloads from ngdc.noaa.gov, THREDDS for NetCDF. Page does not mention S3, OPeNDAP or WCS. Vertical datum and license are not stated on that page (unverified; NOAA data is generally US-gov public domain but not confirmed here). DOI 10.25921/fd45-gt74.
- Source: https://www.ncei.noaa.gov/products/etopo-global-relief-model
- Relevance: duplicates GEBCO at global scale; skip unless we need a no-registration, plain-HTTPS fallback.

### 7.3 NOAA CUDEM / Coastal Relief Model
- CRM: 3 arc-second originals, newer volumes 1 arc-second; US coast (CONUS, Alaska, Hawaii, Puerto Rico); access via THREDDS (every volume), Bathymetry Map Viewer, Grid Extract. Vertical datum "either EGM2008 or Mean Sea Level"; pre-2013 volumes not converted to common datum. Products from NCEI data are "not subject to U.S. copyright protection"; cite NCEI; not for navigation.
- CUDEM data in S3: I verified an anonymous-listable public bucket `noaa-nos-coastal-lidar-pds` with prefix `dem/` containing folders such as `dem/NCEI_ninth_Topobathy_2014_8483/`, `.../AmSam_9460/`, `.../CNMI_9560/` (S3 XML listing returned 2026-10-30). That these are the "CUDEM 1/9 arc-second" tiles is inferred from the names; the CRM page does not mention S3 (unverified mapping). Seascape records CUDEM at 3.4 m / 10 m, datum MLLW, VDatum-based.
- Sources: https://www.ncei.noaa.gov/products/coastal-relief-model , https://github.com/openwatersio/seascape/blob/main/sources/README.md
- Relevance: highest-resolution free US nearshore data. Needs a datum decision (MLLW vs MSL).

### 7.4 EMODnet Bathymetry (Europe)
- DTM 2024, 1/16 x 1/16 arc-minute (~115 m; page text, the arc-second conversion is mine), covering 36W,15N to 43E,90N; separate 2024 Caribbean DTM. Tiles download as ESRI ASCII, XYZ, CSV, NetCDF, GeoTIFF, SD; LAT and MSL versions available. WMS, WFS, WCS services exist; WMTS for the base layer. ERDDAP not mentioned.
- License: page names no specific license; links to EMODnet Terms of Use (not read, unverified). Seascape ingests it as an openly redistributable source. Citation DOI 10.12770/cf51df64-56f9-4a99-b1aa-36b8d7b743a1. Many underlying CDI survey sets need owner permission.
- Source: https://emodnet.ec.europa.eu/en/bathymetry
- Relevance: strongest programmatic upgrade over GMRT/GEBCO in Europe, via WCS (GDAL has a WCS driver, so we stay within the CLI-shell model).

### 7.5 Copernicus DEM / SRTM (land)
- Copernicus DEM GLO-30 and GLO-90: AWS Open Data, buckets `copernicus-dem-30m` and `copernicus-dem-90m`, region eu-central-1, Cloud Optimized GeoTIFF, anonymous (`aws s3 ls --no-sign-request s3://copernicus-dem-30m/`). License wording: free "for the general public under the terms and conditions of the Licence" (full Copernicus licence not read; attribution requirement unverified). The Copernicus Space Data page itself failed to load (connection refused), so vertical datum/acquisition period are unverified from that source. Source: https://registry.opendata.aws/copernicus-dem/
- SRTM 1 arc-second (USGS): ~30 m, worldwide void-filled, EarthExplorer download (login typically required, unverified), WGS84/EGM96, GeoTIFF/DTED/BIL. Page labels sample image "Public Domain" but states no explicit license for the dataset. Source: https://www.usgs.gov/centers/eros/science/usgs-eros-archive-digital-elevation-shuttle-radar-topography-mission-srtm-1
- Relevance: GEBCO land cells are coarse (15"). For islands/coast we want 30 m land. Copernicus GLO-30 COG over `/vsicurl/` + `gdalwarp` is the cleanest route. Caveat: DSM, not terrain; includes buildings/trees (unverified from fetched page, widely documented).

### 7.6 Other notable (per Seascape catalogue; Seascape's license gate means each is claimed openly redistributable, not independently verified by me)
- Ireland INFOMAR 10 m/25 m (LAT), England EA/CCO multibeam 0.25-4 m and SurfZone 2 m (ODN), Denmark DDM 50 m, Norway Kartverket 50 m, Netherlands Vaklodingen 20 m, Brittany Litto3D 5 m, Germany DGM-W 2 m, Canada GSC Pacific 10 m, Australia gbr30 30 m, AusBathyTopo 250 m, Indonesia BATNAS ~180 m.
- UK: Defra Marine DEM 1 arc-second (north/south) and 6 arc-second on data.gov.uk; Defra multibeam 50 cm as Esri ASCII in 5 km zips via the Defra Data Services Platform. Search result text says input data was licensed by UKHO to Defra, attribution "Crown Copyright"; the south set is "SFTP via large data request". OGL not confirmed (unverified). Source: https://www.data.gov.uk/dataset/e05d3f72-8e67-4cc0-aa2e-abb79e14243c/defras-marine-digital-elevation-model-dem-1-arc-second-south
- UKHO/Admiralty own open data and Marine Scotland: not verified (no page fetched). INFOMAR data-access page returned 404 so license/method unverified.
- GMRT (existing): not re-verified in this pass.

---

## 8. Infrastructure

### 8.1 C/C++ libraries (verified via GitHub API and license files)
| Lib | Latest release (checked) | License | Notes |
|---|---|---|---|
| GDAL | v3.13.3 (2026-08-18); repo pushed 2026-10-30 | MIT-style (LICENSE.TXT: "In general GDAL/OGR is licensed under an MIT style license"; also contains notices for bundled third-party code; GitHub shows NOASSERTION) | We shell out, so there is no linking obligation. Distribute no GDAL binaries and no obligation; if we ever bundle, ship LICENSE.TXT. |
| PROJ | 9.9.0 (2026-09-15) | MIT-style (COPYING) | Pulled in by GDAL. |
| GEOS | 3.15.0 (2026-09-01) | **LGPL-2.1** | Dynamically linked by GDAL; shelling out avoids any link-time question. If we ever cgo-link GEOS statically into the Go binary, LGPL obligations apply. |

Sources: https://github.com/OSGeo/gdal , https://github.com/OSGeo/PROJ , https://github.com/libgeos/geos

### 8.2 Go libraries (cgo checked by shallow clone + grep for `import "C"` in `*.go`)
| Lib | License | Last push | Release | cgo | Notes |
|---|---|---|---|---|---|
| airbusgeo/godal | Apache-2.0 | 2026-09-04 | tag v0.0.18 (2026-05-22) | **Yes** (2 files with `import "C"`; README: "groups often-used calls in a single cgo function"; needs GDAL >= 3.0 headers) | Pre-1.0. Binding to libgdal; breaks the "just shell out" model, makes cross-compilation harder. 184 stars. |
| paulmach/orb | MIT | 2026-03-30 | v0.13.0 | No | Geometry types, GeoJSON, MVT/WKB/WKT, clipping, simplify. 1141 stars. Good fit for coastline polygons. |
| twpayne/go-geom | BSD-2-Clause | 2026-10-18 | none (no GH releases) | No | WKB/WKT/GeoJSON codecs, topology helpers. 986 stars, actively maintained. |
| golang/geo | Apache-2.0 | 2026-10-29 | none | No | S2 spherical geometry (cells, caps). Useful for spherical tiling, not planar polygon clipping. |
| tidwall/geojson | MIT | 2026-08-16 | none | No | GeoJSON with spatial predicates; built for Tile38. |
| go-spatial/geom | MIT | 2026-02-23 | none | Only inside `vendor/` (mattn/go-sqlite3); no `import "C"` in its own non-vendor files | Quieter (last push Feb 2026). |

Sources: respective GitHub repos (airbusgeo/godal, paulmach/orb, twpayne/go-geom, golang/geo, tidwall/geojson, go-spatial/geom). Not found in the standard checked set: any pure-Go polygon boolean-ops library was not evaluated (unverified).

**Take:** keep GDAL as a subprocess (MIT, no cgo). For in-process coastline handling use orb (MIT, pure Go) or go-geom. Avoid godal unless benchmarks show process-spawn overhead matters.

---

## 9. Engine heightmap formats (official docs)

### 9.1 Unity Terrain RAW
Sources: https://docs.unity.com/en-us/engine/6000.0/manual/creating-environments/script-terrain/terrain-heightmaps , https://docs.unity.com/en-us/engine/current/script-reference/unityengine/terraindata/heightmapresolution
- Import dialog has Depth = Bit 16 (2 bytes) or Bit 8, Byte Order ("mainly applies to bit-16 ... platform-dependent": Windows / Mac options), Resolution (width and height of the imported file), Terrain Size. Docs recommend RAW as "a 16-bit grayscale format".
- Scripting reference: `TerrainData.heightmapResolution` is clamped to **33, 65, 129, 257, 513, 1025, 2049, 4097** (2^n + 1). The manual page itself states no 2^n+1 rule for RAW file dimensions.
- Which byte order each menu choice writes is not defined on the fetched pages; the import dialog assumes a choice by the user. **Little-endian is conventional on both x86 and Apple Silicon**, but the doc's "Mac" label historically meant big-endian (unverified here): emit little-endian and also state this in the manifest; verify empirically with a test tile.
- Heights: Unity maps 0..65535 linearly onto 0..Terrain Size.y; there is no signed range, so ocean worlds must offset (sea level = a chosen non-zero value) and set `terrainData.size.y` accordingly. (Standard behavior; not quoted from a fetched page.)
- Terrain tiles must share edge rows/columns for seam-free neighbors (2^n + 1 means tiles overlap by one sample); our tiler should be built around that.

### 9.2 Unreal Landscape
Sources: https://dev.epicgames.com/documentation/en-us/unreal-engine/importing-and-exporting-landscape-heightmaps-in-unreal-engine , https://dev.epicgames.com/documentation/en-us/unreal-engine/landscape-technical-guide-in-unreal-engine
- Import formats: 16-bit grayscale PNG, 8-bit grayscale `.r8`, 16-bit grayscale `.r16`, and RAW via a **JSON sidecar** (sidecar schema not on that page; the technical guide link was not followed for this, unverified). Export: 16-bit grayscale `.png` or `.r16`.
- Byte order of `.r16`: not stated on the Epic pages; a third-party MathWorks page says "16-bit, grayscale RAW file in little-endian byte order" (secondary source).
- Z scale formula on the import page: `(max height of heightmap in meters) x 100 x 0.001953125`. At the default Z scale of **100**, heights run **-256 m to +256 m**; 16-bit values encode -256..255.992 m. Mid-grey (32768) is 0 m (my inference); step size is 512 m / 65536 = ~7.8 mm at default scale (computed).
- Recommended square sizes (vertices): **8129, 4033, 2017, 1009, 505, 253, 127**. Technical guide: section size "(in number of vertices) must be a power of two value", max 256x256; components use 1 section or 2x2 sections. Overall resolution is derived from the file, so non-recommended sizes are not rejected but waste area/components.
- Implication for oceans: a -256..+256 m default window is too narrow for deep ocean; use a larger Z scale (e.g. world depth 11000 m needs Z ~ 11000/256*100 ~ 4300) at the cost of vertical precision (~0.34 m/step at 11 km range, computed). Write the Z scale into the output manifest.

---

## Recommendations (ranked)

1. **Direct high-res regional bathymetry via GDAL web services: EMODnet WCS first, then NOAA CUDEM from the public S3 bucket.** Both are free, programmatic, and read with `/vsicurl/` or the WCS driver within the existing shell-to-GDAL design. They remove the biggest quality gap (GMRT falls back to ~450 m GEBCO outside surveys) in the two regions with the most maritime-game demand (Europe, US). Needs a vertical datum policy (MSL vs LAT/MLLW).
2. **Engine-exact export hardening (Unity 2^n+1 with shared edges; Unreal sizes 1009/2017/4033/8129 + selectable Z scale + manifest of datum/sea-level/Z).** These are verified constraints from official docs, and wrong sizes/ranges silently ruin worlds. No new dependency.
3. **Copernicus DEM GLO-30 (COG, anonymous AWS) as the land layer**, merged over GEBCO land cells using the existing coastline/topo-mask. ~30 m vs 450 m land, license permits public use, simple `gdalwarp` mosaic.
4. **Seamarks from OSM (OpenSeaMap) through the existing Overpass client**, emitted as placement GeoJSON (buoys, lights, wrecks, harbours). Low cost, same ODbL as the coastline already used, and the biggest "feels like a real sea" feature. Verify the tag list on the Seamarks wiki before coding.
5. **Seascape tiles (openwatersio) as optional fallback/pre-merged source and its source catalogue as the roadmap** (INFOMAR, UK EA/CCO, DDM, Kartverket, Litto3D, etc.). Free CC BY 4.0 and global, but quantized and datum-mixed, so keep it second to direct sources and cache aggressively.

Deferred: Cesium 3D Tiles export (large effort, paid SaaS in the loop), Immersive Ocean (GPL/AGPL, UE 5.3 only), godal (cgo), ETOPO 2022 (duplicates GEBCO), LibreTerrain and gen-map (no such projects found).
