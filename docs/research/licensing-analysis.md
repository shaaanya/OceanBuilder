# Licensing analysis of source data and compiled worlds

**Status: research notes, not legal advice.** Written by an AI assistant from the primary texts listed in section 0. Nothing here is a legal opinion. Where terms are ambiguous this file says so and marks the question **UNRESOLVED**. Items where qualified legal review is recommended are collected in section 8.

Retrieved: **2026-11-01** (all pages below were fetched on that date; terms can change without notice). This file complements `docs/research/licensing.md` (what was checked on 2026-10-30 and what the tool does) and goes deeper on the OpenStreetMap/ODbL question that file leaves open.

Facts about the tool used below were read from the repo on the same date: `coastline` defaults to `"osm"` (`internal/world/world.go`), `coast_conform` and `seamarks` default to off, `regional_bathymetry` (EMODnet) is opt-in, and `docs/formats/world-v2.md` assigns source code 5 to "conformed" cells whose height is a synthetic +-0.5 m. The Falmouth fixture `testdata/falmouth/coast.geojson` contains 11 features (counted with a JSON parse).

## 0. Fetch log (what was and was not read)

| Text | URL | Result |
|---|---|---|
| ODbL 1.0 | https://opendatacommons.org/licenses/odbl/1-0/ | read (definitions, 2.x, 4.2 to 4.8) |
| OSMF Licence Community Guidelines (index) | https://osmfoundation.org/wiki/Licence/Community_Guidelines | read |
| Collective Database guideline | requested `.../Collective_Database_-_Guideline` | **HTTP 404**. The real page is `https://osmfoundation.org/wiki/Licence/Community_Guidelines/Collective_Database_Guideline_Guideline` (sic), read; endorsed by the OSMF board 2016-06-17 |
| Regional Cuts guideline | `.../Community_Guidelines/Regional_Cuts_-_Guideline` | **not fetched**; "regional cut" below is used as the Collective Database and Horizontal Layers pages use it, without the Regional Cuts text |
| Produced Work guideline | `.../Community_Guidelines/Produced_Work_-_Guideline` | read; endorsed 2014-06-06 |
| Substantial guideline | `.../Community_Guidelines/Substantial_-_Guideline` | read; endorsed 2014-06-06 |
| Horizontal Map Layers guideline | `.../Community_Guidelines/Horizontal_Map_Layers_-_Guideline` | read; endorsed 2014-06-06 |
| Geocoding guideline | `.../Community_Guidelines/Geocoding_-_Guideline` | read; endorsed 2017-08-24 |
| Trivial Transformations guideline | `.../Community_Guidelines/Trivial_Transformations_-_Guideline` | read; says "This is at the proposal stage in our process - it may change after discussion". Not endorsed, so not relied on here |
| OSMF Attribution Guideline | https://osmfoundation.org/wiki/Licence/Attribution_Guidelines | read; adopted by the OSMF board 2021-06-25 |
| OSM copyright page | https://www.openstreetmap.org/copyright | read |
| GMRT Terms of Use | https://www.gmrt.org/about/terms_of_use.php | read |
| GMRT About | https://www.gmrt.org/about/index.php | read (one relevant sentence) |
| GEBCO gridded data (current, GEBCO_2026) | https://www.gebco.net/data-products/gridded-bathymetry-data | read |
| GEBCO terms of use | https://www.gebco.net/data-products/gridded-bathymetry/terms-of-use | read |
| GEBCO_2024 and GEBCO_2026 grid pages | https://www.gebco.net/data-products-gridded-bathymetry-data/gebco2024-grid and `.../gebco2026-grid` | read (attribution strings) |
| EMODnet Terms of Use | requested `https://emodnet.ec.europa.eu/en/terms-use` | **HTTP 403** (also `/en/data-policy` 404). The page linked from the EMODnet bathymetry page, `https://emodnet.ec.europa.eu/en/terms-use-emodnet-online-services-data-and-data-products`, was read |
| EMODnet Bathymetry page | https://emodnet.ec.europa.eu/en/bathymetry | read (DOIs) |
| EMODnet DTM 2024 metadata | DOI https://doi.org/10.12770/cf51df64-56f9-4a99-b1aa-36b8d7b743a1 redirects to the Ifremer Sextant record (JS page, not parsed). The GeoNetwork XML `https://emodnet.ec.europa.eu/geonetwork/srv/api/records/cf51df64-56f9-4a99-b1aa-36b8d7b743a1/formatters/xml` was read for constraints | read |
| NOAA NCEI Coastal Relief Model page | https://www.ncei.noaa.gov/products/coastal-relief-model | read (FAQ, citations) |
| NOAA CUDEM product page, NCEI copyright and disclaimer pages | `.../products/coastal-elevation-models`, `https://www.ncei.noaa.gov/about/copyright`, `.../about/disclaimers`, the CUDEM ISO landing page | **not verified**: the product page contained no navigation, citation or copyright text that a keyword search could find; the others returned 404/503. `https://www.noaa.gov/disclaimer` returned only the external-links disclaimer, which is not a data policy |
| AWS Open Data Terrain Tiles | https://registry.opendata.aws/terrain-tiles/ | read |
| Tilezen/joerd attribution and data sources | https://github.com/tilezen/joerd/blob/master/docs/attribution.md (read as raw) and `.../docs/data-sources.md` | read |
| OpenSeaMap licence | guesses at `wiki.openseamap.org/wiki/License`, `.../Licence`, `.../OpenSeaMap-dev:License` | **no licence page retrieved**. `https://map.openseamap.org/` only shows an icon titled "This work is licensed under the Creative Commons Attribution-ShareAlike 2.0 License" |

## 1. OpenStreetMap and the ODbL

### 1.1 The relevant ODbL text

URL: https://opendatacommons.org/licenses/odbl/1-0/ . Short quotes:

- **Derivative Database**: "a database based upon the Database, and includes any translation, adaptation, arrangement, modification, or any other alteration of the Database or of a Substantial part of the Contents. This includes, but is not limited to, Extracting or Re-utilising the whole or a Substantial part of the Contents in a new Database."
- **Collective Database**: "this Database in unmodified form as part of a collection of independent databases in themselves that together are assembled into a collective whole. A work that constitutes a Collective Database will not be considered a Derivative Database."
- **Produced Work**: "a work (such as an image, audiovisual material, text, or sounds) resulting from using the whole or a Substantial part of the Contents (via a search or other query) from this Database, a Derivative Database, or this Database as part of a Collective Database."
- **Substantial**: "substantial in terms of quantity or quality or a combination of both. The repeated and systematic Extraction or Re-utilisation of insubstantial parts of the Contents may amount to the Extraction or Re-utilisation of a Substantial part of the Contents."
- **Publicly**: "to Persons other than You or under Your control by either more than 50% ownership or by the power to direct their activities".
- 4.2 (Publicly Conveying the Database or a Derivative Database): include the licence or its URI and "Keep intact any copyright or Database Right notices"; 4.2.d: if a file cannot hold notices, put them "in a location (such as a relevant directory) where users would be likely to look for it".
- 4.3 (Produced Work): no 4.2 notice needed, "However, if you Publicly Use a Produced Work, You must include a notice ... reasonably calculated to make any Person that uses, views, accesses, interacts with, or is otherwise exposed to the Produced Work aware that Content was obtained from the Database ... and that it is available under this License." Example text: "Contains information from DATABASE NAME, which is made available here under the Open Database License (ODbL)."
- 4.4.a: "Any Derivative Database that You Publicly Use must be only under the terms of" the ODbL, a later similar version, or a compatible licence. 4.4.b: "Extraction or Re-utilisation of the whole or a Substantial part of the Contents into a new database is a Derivative Database". 4.4.c: a Derivative Database "is Publicly Used and so must comply with Section 4.4. if a Produced Work created from the Derivative Database is Publicly Used". 4.4.d: "You must not add Contents to Derivative Databases under Section 4.4 a that are incompatible with the rights granted under this License."
- 4.5: share-alike does not apply (a) to Collective Databases, although "this License still applies to this Database or a Derivative Database as a part of the Collective Database"; (b) "Using this Database, a Derivative Database, or this Database as part of a Collective Database to create a Produced Work does not create a Derivative Database for purposes of Section 4.4"; (c) internal use "within an organisation".
- 4.6: if you Publicly Use a Derivative Database or a Produced Work from one, you "must also offer to recipients ... a copy in a machine readable form of: a. The entire Derivative Database; or b. A file containing all of the alterations made to the Database or the method of making the alterations to the Database (such as an algorithm), including any additional Contents". Free of charge over the internet.
- 4.7.a: no "technological measures on the Database, a Derivative Database, or the whole or a Substantial part of the Contents that alter or restrict the terms of this License"; 4.7.b allows a restricted copy only with a parallel unrestricted copy "at least as accessible to the recipient as a practical matter". 4.8: "You may not impose any further restrictions on the exercise of the rights granted".
- The page's own disclaimer: Open Data Commons "is not a law firm"; seek a qualified professional.

The ODbL licenses copyright, the sui generis database right, and is also "an agreement in contract" (2.1). Whether a given jurisdiction has a database right (the EU does, the US does not) changes what a licence-free use looks like, but the contract limb and the Licensor's position are the same. This jurisdiction interplay is **UNRESOLVED** and is not analysed further.

### 1.2 What the OSMF guidelines say (and their weight)

The guidelines are the Licensor's interpretation, not law. The index page presents them as clarifications, and the Attribution Guideline says its own text is "not a substitute for the legal text itself. If the two texts disagree, the legal text takes precedence." The Collective Database guideline adds that its situations are not the only ways in which data can be combined without invoking share-alike (and, by the same token, it does not say that everything else triggers it).

- **Produced Work guideline.** "The published result of your project is either a Produced Worked or a Derivative Database within the meaning of the ODbL. If the published result of your project is intended for the extraction of the original data, then it is a database and not a Produced Work. Otherwise it is a Produced Work." Usually Produced Works: "PNG, JPG, PDF, SVG images and any raster image; a map in a physically printed work". "Database dumps are usually not Produced Works." It also notes that publishing a Produced Work still triggers 4.6 where the work comes from a Derivative Database.
- **Substantial guideline** (insubstantial if "one-off and not repeated over time for the same or a similar project"): "Less than 100 Features"; more than 100 only if "non-systematic and clearly based on your own qualitative criteria"; or an area of up to 1,000 inhabitants. It adds: "The systematic extraction of all eating places within an area ... would be considered to be systematic" and "we regard repeated small extractions as one big extraction!" A "Feature" is "a street, restaurant, park, cemetery, bus stop, mountain top etc."
- **Horizontal Map Layers guideline** (about 2D maps, Produced Works): "if all data for that Feature Type is from non-OpenStreetMap sources, then the ODbl share-alike conditions do not apply to that Feature Type"; "If you use OpenStreetMap data along with non-OpenStreetMap data for a given Feature Type, then the share-alike condition would apply regardless of whether some data for that Feature Type is in a different layer". Sharing is not needed for unrelated data plotted over an OSM base (its bird-migration example).
- **Collective Database guideline.** OSM and non-OSM datasets "will be considered independent (and thus form a Collective Database rather than a Derivative Database) so long as the data used for a particular data type is either all OSM or all non-OSM within the same regional cut." Not triggering share-alike: datasets that "do not reference each other", or non-OSM data that "completely replaces" a type of geometry/data for a primary feature in the cut, or adds features that include no OSM data. A reference "can be by a database key or any other method of identifying a specific OSM or non-OSM element that may be used with a database join". Its third example (a proprietary list complemented with OSM data, duplicates removed) "would not be covered by this guideline" and the proprietary data may become subject to share-alike. "Two data sets need not be physically separated to qualify as 'independent'."
- **Geocoding guideline** (not about terrain, but the closest statement of the "reverse engineering" worry): systematically recreating "the whole or a substantial part of the OSM database" from results yields a Derivative Database; a collection is "not a substantial extract" only if it has no systematic attempt to aggregate all primary features of a type "within a geographic area city-sized or larger".
- **Trivial Transformations guideline** is only at "the proposal stage"; its logic ("without use of any external sources of data") would not cover `coast_conform`, which does mix an external source. Not relied on.

### 1.3 Attribution text

The OSM copyright page (https://www.openstreetmap.org/copyright) says "You are free to copy, distribute, transmit and adapt our data, as long as you credit OpenStreetMap and its contributors. If you alter or build upon our data, you may distribute the result only under the same license." The OSMF Attribution Guideline (2021) says: "Attribution must be to 'OpenStreetMap'" and "must also make it clear that the data is available under the Open Database License" (e.g. a link to openstreetmap.org/copyright). "The historical forms of attribution '© OpenStreetMap contributors' or '© OpenStreetMap' are acceptable." For databases: "You must include attribution to OpenStreetMap and either the text of the ODbL or a link to it as part of the database, derivative database, or database as part of a collective database", in a location "such as a readme file, or within the data or metadata". For an application that shows a map: a corner, splash screen, or an "About / Data licences" menu entry; the attribution may collapse after five seconds provided the licence info stays findable. Static images of fewer than 100 features or under 10,000 m2 need no attribution (not relevant to a data set).

Recommended text for a world that carries OSM data (a conservative combination of the above, not an OSMF-mandated string):

> © OpenStreetMap contributors. Contains information from OpenStreetMap, which is made available here under the Open Database License (ODbL). https://www.openstreetmap.org/copyright (licence text: https://opendatacommons.org/licenses/odbl/1-0/)

### 1.4 OceanBuilder uses, one by one

**(a) Coastline used only to measure an offset, stats in the manifest.** The OSM data is read, compared to the DEM zero contour, and a handful of aggregate numbers (offset, coverage) are stored. No coastline geometry reaches the output. Under the ODbL text this looks like an internal use of the Contents (4.5.c), and what is published (a few statistics) is very unlikely to be a Substantial part of the Contents even when summed over runs. The Substantial definition does warn that "repeated and systematic Extraction ... of insubstantial parts" can add up, and a metric like "fraction of coastline samples outside the area" is not a recreation of the geometry. Assessment: low risk, no share-alike. The attribution the tool already writes is a sensible courtesy. **Mild UNRESOLVED**: nobody has tested whether published per-world statistics derived from an OSM extract can ever be "Substantial in terms of quality".

**(b) `coastline.wgs84.geojson` copied into the world directory.** This is a re-published extract of the OSM coastline for the bbox. For a real-sized world it is almost certainly Substantial under the guideline's logic: it is the systematic extraction of all features of one type in an area, not a hand-picked sample, and the 100-feature safe harbour is for one-off, non-systematic extracts. (The Falmouth fixture is a 6 km square with 11 features, below 100, but a handful of fixtures does not make a general rule, and repeated extractions "count as one big extraction".) So treat the file as an ODbL database that is conveyed Publicly: ship the licence reference and attribution with it (4.2, and the Attribution Guideline's databases paragraph), keep the file separate and labelled, and do not put technical restrictions on it (4.7). It is also a derivative of a database only in a format-conversion sense; whether that format conversion is a "Derivative Database" matters little here because the answer is the same: the file itself is under the ODbL. The more interesting question is its effect on the other files in the directory. Placing it next to tile files in one directory does not by itself make the tiles share-alike: the file is "this Database in unmodified form" and the tiles are independent, which is the Collective Database situation (4.5.a; the guideline states physical separation is not required). **Mild UNRESOLVED** because the Collective guideline's "independent" test depends on whether the tiles "reference" the coastline file, and `stats` in the manifest do reference it.

**(c) `coast_conform`: per-cell land/water decision from OSM within about 300 m, written as source code 5.** This is the hard case. Facts: the DEM supplies the heights, and for cells in the band the OSM coastline decides whether the cell is land or water; the stored height is a synthetic +-0.5 m of the right sign. The tiles then carry (i) a per-cell source code and (ii) heights whose sign in the band encodes the OSM line to within one cell.

Reading the guidelines at face value:

1. *Is it extraction of a Substantial part?* The raster encodes the position of the coastline at cell resolution for the whole shoreline of the world. That is a re-expression of the coastline, "an adaptation, arrangement, modification or other alteration" of the data (the Derivative Database definition). Whether quantity and quality are Substantial is the same argument as in (b): for the whole shoreline of a real region, likely yes; for a tiny world, perhaps no.
2. *Produced Work or database?* The Produced Work guideline's test is whether the published result "is intended for the extraction of the original data". A tile set read by a game engine to render terrain is not obviously "intended for extraction", and heightmaps are raster data, which are "usually" Produced Works in the guideline's examples (images). Against that, the tile set is not an image for viewing; it is structured data consumed by software, and the coastline can be recovered from it (the format is documented in the spec and a reader is published). **UNRESOLVED**, and the OSMF guideline gives no example that settles it.
3. *Does mixing in one feature type trigger share-alike?* The land/water boundary in the band is one data type. The Horizontal Layers and Collective Database guidelines both say the safe case is "all OSM or all non-OSM" per feature type per regional cut. Conformed cells are not that: in the same regional cut, the land/water decision comes from OSM in the band and from the DEM elsewhere, and the cells reference each other (the same grid). Read at face value the guidelines point towards a **Derivative Database**, not a Collective Database, for the layer that mixes them. This is the likely reading but it is an inference from guidelines written for 2D vector maps, so it is **UNRESOLVED**.
4. *What would share-alike require?* If the tile set (or the conformed layer) is a Derivative Database that is Publicly Used, 4.4.a says it must be under the ODbL (or a compatible licence), 4.6 requires offering the whole Derivative Database or a file of the alterations, "or the method of making the alterations ... (such as an algorithm)", and 4.7 forbids restrictive technical measures. ODbL permits commercial use, so the consequence is mainly "the OSM-influenced data must be redistributable by recipients, with notices", not "the game must be open". Whether the open-source tool plus the world config counts as "the method of making the alterations" under 4.6.b is plausible but **UNRESOLVED**.
5. *Can the GMRT/GEBCO/EMODnet heights be inside an ODbL derivative?* 4.4.d says one "must not add Contents ... that are incompatible with the rights granted under this License". GEBCO is described as public domain; GMRT is CC BY 4.0; EMODnet products default to CC BY 4.0 (section 2). CC BY 4.0 permits adaptation under other terms with attribution, so a combination looks feasible, but nobody has checked CC BY 4.0 attribution plus ODbL notices together, and the GMRT/EMODnet upstream terms are not fully known. **UNRESOLVED**.

The effect is local: only cells within `coast_conform_max_m` (default 300 m) of the coastline can be source 5, and the manifest counts them. That makes the OSM-derived share measurable, but "small share of cells" is not a legal threshold: the Substantial test is about quantity or quality of the Contents extracted (the whole coastline is the quality-relevant part).

**(d) `seamarks.geojson` (OpenSeaMap seamarks via Overpass).** The tool queries the OSM database; the result is OSM Contents, so the ODbL applies to this file with the same attribution and notice duties as (b). Seamarks are point features, so a world with more than 100 of them, or "all seamarks of a type in an area", is systematic and probably Substantial by the Substantial guideline's own examples. OpenSeaMap's own statements: I could not retrieve an OpenSeaMap licence page. The OpenSeaMap web map shows a Creative Commons Attribution-ShareAlike 2.0 badge, which most plausibly refers to OpenSeaMap's rendered chart tiles and documentation, not to the OSM database content that OceanBuilder queries (the earlier session note in `docs/research/ecosystem.md` records that the OpenSeaMap wiki says OSM-hosted data is ODbL and chart tiles are CC-BY-SA; this was not re-read today). OceanBuilder does not use OpenSeaMap's tiles, so the ODbL is the working assumption. Seamark data is volunteer-contributed and is not an official chart. The navigation disclaimer should apply to this file too (the OpenSeaMap statements about navigation were not retrieved, so this is a precaution, not a quotation). **UNRESOLVED**: the exact OpenSeaMap licence statements.

**(e) A game that ships tiles built with (c).** Beyond (c)'s open questions, shipping adds issues that are specific to games:

- *Produced Work vs database.* If the tiles are a Produced Work, 4.5.b says it does not create a Derivative Database, but 4.4.c and 4.6 still reach a Produced Work created from a Derivative Database. The game is better described as a larger work that contains an identifiable ODbL component than as a "Collective Database" in the ODbL's sense, which requires the Database "in unmodified form"; conformed (modified) data does not fit that definition. 4.3 requires a notice "reasonably calculated to make any Person that uses ... the Produced Work aware that Content was obtained from" OSM and available under the ODbL; the Attribution Guideline's safe harbour for applications is a visible credit screen or "Data licences" menu item.
- *Packed or encrypted assets.* 4.7.a/b: if the tiles end up inside an encrypted or proprietary asset bundle and the OSM-derived content is a Derivative Database, the ODbL asks for an unrestricted parallel copy "at least as accessible to the recipient as a practical matter". Console/DRM packaging makes this concrete.
- *EULA.* 4.8: no "further restrictions on the exercise of the rights granted". A blanket EULA clause forbidding extraction of game assets cannot override the ODbL for the OSM part.
- *Whether shipped heightmaps are "a database" at all* in the game context is **UNRESOLVED** and, in my ranking, the second most important question after (c) itself.

### 1.5 Practical recommendation for OSM

1. **Default worlds should avoid OSM-derived heights.** This is already the case: `coast_conform` and `seamarks` are off by default. Keep them off by default and say in the docs that turning them on changes the licence position of the output.
2. **Treat OSM files as separate ODbL components.** `coastline.wgs84.geojson` and `seamarks.geojson` stay as separate, labelled files with ODbL notices; tiles do not embed them. Consider making the coastline copy opt-in: verified in `internal/world/pipeline.go` (step 5) that the file is written whenever a coastline is present, and `coastline: "osm"` is the default, so every default world ships `coastline.wgs84.geojson`.
3. **If conforming is offered, keep OSM-derived cells in a separable layer.** The separable layer should be *purely* OSM-derived (for example a rasterised land/water mask, source id `osm-coastline`), not a height delta against the DEM, because a delta mixes both sources and defeats the "all OSM or all non-OSM" separation that the Collective Database guideline relies on. The base height tiles stay GMRT/GEBCO/EMODnet only. The engine adapter composes them at runtime. This helps but does not guarantee anything: runtime composition of a mask and a DEM that "reference each other" is again the Collective-vs-Derivative question.
4. **Make the method reproducible.** Because 4.6.b allows "the method of making the alterations ... (such as an algorithm)", record in the manifest the tool version, config hash, and OSM extraction time (the manifest already stores retrieval time and sha256) so a recipient can regenerate the layer. Whether that satisfies 4.6.b is a legal question.
5. **What to tell users.** Three tiers: (i) no `coast_conform`, no `seamarks`: attribution file plus the ODbL coastline file notice if the geojson is shipped; (ii) with `seamarks`: ship `seamarks.geojson` as an ODbL file with the notice; (iii) with `coast_conform`: treat the conformed layer as ODbL-covered, ship it separably, offer it (or the method) to recipients, do not wrap it in restrictive packaging, put the OSM credit on the game's credits screen, and get legal review before a commercial release.

## 2. EMODnet Bathymetry

- **Terms page** (read): https://emodnet.ec.europa.eu/en/terms-use-emodnet-online-services-data-and-data-products . Key clauses: "EMODnet operates on the principle of open data and strives to ensure that data and data products are provided free of restrictions of use and free of costs." "EMODnet is a data assembler and broker. This means that EMODnet does not take ownership of the data but assembles datasets from a variety of sources and providers". "Unless indicated otherwise, data products created by EMODnet are owned by the EU and therefore licensed under Creative Commons CC-BY 4.0." Users should "verify any metadata accompanying the downloaded datasets or data products ... to properly cite the data originator and consult any use restrictions or licences of individual data originators". "The EMODnet partners and the EC are not liable for users not respecting the specific data policy of the data originators". "With any download and use of EMODnet online services, data and data products, you agree to be bound by the following Terms of Use", and the terms "will be periodically updated and without prior notice".
- **Acknowledgement wording** (exact, from the same page). For a data product created by EMODnet: "This data product was created by EMODnet (https://emodnet.ec.europa.eu/en/), and is owned by the EU and licensed under the Creative Commons Attribution 4.0 International (CC BY 4.0) license." For specific data sets from originators: "This data was downloaded from the EMODnet Portal (https://emodnet.ec.europa.eu/en/). The data originator(s) is/are [name of the data originator + affiliation ...]." For information used in a derived work: "Information used in this [type of derived work ...] was made available by EMODnet (https://emodnet.ec.europa.eu/en/), founded by the European Commission Directorate-General for Maritime Affairs and Fisheries (EC DG MARE) and funded by the European Maritime Fisheries and Aquaculture Fund (EMFAF)." For a layer: "it may not be possible to acknowledge all data originators as there may be multiple diverse data originators aggregated for one map layer. Users should verify the associated metadata".
- **DOI to cite.** The EMODnet bathymetry page lists "EMODnet Digital Bathymetry (DTM 2024). EMODnet Bathymetry Consortium https://doi.org/10.12770/cf51df64-56f9-4a99-b1aa-36b8d7b743a1" and, for the previous release, DTM 2022: https://doi.org/10.12770/ff3aff8a-cff1-44a3-a2c8-1910bf109f85 .
- **Metadata record.** The GeoNetwork record for the DTM 2024 UUID carries a resource constraint with the single text "DO NOT USE FOR NAVIGATION". I found no licence statement or CC reference in the record's constraints (the access/use limitation fields are missing or "otherRestrictions" without text). So the metadata does not itself state CC BY 4.0.
- **Commercial redistribution of derived products.** The EMODnet-created data products are CC BY 4.0 "unless indicated otherwise", and CC BY 4.0 permits commercial use and adaptation with attribution. But (i) the DTM record does not state the licence, (ii) the DTM is compiled from originator surveys ("EMODnet does not take ownership of the data") whose own policies may differ, and (iii) the terms are binding on download and change without notice. I therefore rate commercial redistribution as "probably yes under CC BY 4.0, unclear until the originator terms are checked". **UNRESOLVED**.
- **Repo fact that affects citation.** `internal/emodnet/emodnet.go` requests `emodnet__mean` (current, unversioned) unless a dated release is pinned; the unversioned coverage "changes when EMODnet publishes", so the DOI to cite is the one for the release actually served. The provenance should record the pinned release, or the retrieval date plus the metadata version, and the manifest should say which DOI applies. I did not verify which DTM the unversioned coverage corresponds to today.

## 3. GMRT and GEBCO

### 3.1 GMRT

URL: https://www.gmrt.org/about/terms_of_use.php (retrieved 2026-11-01).

- "This web site and its downloadable products are designed for both public and academic use, and are licensed under a Creative Commons Attribution 4.0 International (CC BY 4.0) License. Appropriate acknowledgment with a byline/credit/link must be given to the Global Multi-Resolution Topography Synthesis (GMRT). You may browse freely, but you may not circulate or publish materials you obtained from this site if you do not accept the terms of providing adequate citation."
- "Data should not be used for navigation purposes."
- Accuracy: "Because much of the data are legacy in nature, we make no claims to accuracy of the data identification, acquisition parameters, processing methods, or navigation."
- Citation: Ryan, W. B. F. et al. (2009), Global Multi-Resolution Topography (GMRT) synthesis data set, Geochem. Geophys. Geosyst., 10, Q03014, doi:10.1029/2008GC002332. Data doi: the text prints `10.1594/IEDA.100001` while the link target is `https://doi.org/10.1594/IEDA/100001`; checked 2026-11-01 with `curl -I`: `https://doi.org/10.1594/IEDA.100001` returns 404, while `https://doi.org/10.1594/IEDA/100001` answers 302 to `http://doi.iedadata.org/100001` (the final host did not respond within the timeout), so use the slash form `10.1594/IEDA/100001`.
- **Third-party data.** The GMRT About page says "Curated multibeam data are blended and merged with other elevation sources to create a global multi-resolution DEM". The Terms of Use page puts the whole service under CC BY 4.0 and does not list upstream sources or their terms. Whether the CC BY 4.0 statement is complete for every contributed grid cannot be told from these pages. `layer=topo` is filled with GEBCO_08 where no high-resolution data exist (from the GMRT GridServer documentation, per `docs/research/data-sources.md`; not re-read today). **UNRESOLVED**.
- Implication for a game: attribution with link; the navigation sentence is phrased as "should not", as a request rather than as an explicit CC BY 4.0 condition. Whether it binds a redistributor is unclear, but the safe course is to carry it forward in the world's documentation and in the game's about/credits text.

### 3.2 GEBCO

URL: https://www.gebco.net/data-products/gridded-bathymetry-data (current GEBCO_2026) and https://www.gebco.net/data-products/gridded-bathymetry/terms-of-use .

- "The GEBCO Grid is placed in the public domain and may be used free of charge. Use of the GEBCO Grid indicates that the user accepts the conditions of use and disclaimer information given below."
- Users are free to "Copy, publish, distribute and transmit", "Adapt", and "Commercially exploit The GEBCO Grid, by, for example, combining it with other information, or by including it in their own product or application".
- Users must: "Acknowledge the source of The GEBCO Grid", "Not use The GEBCO Grid in a way that suggests any official status or that GEBCO, or the IHO or IOC, endorses any particular application", and "Not mislead others or misrepresent The GEBCO Grid or its source."
- Disclaimer: "The GEBCO Grid should NOT be used for navigation or for any other purpose involving safety at sea." It is "made available 'as is'"; it is "based on bathymetric data from many different sources of varying quality and coverage"; because it is "an information product created by interpolation of measured data, the resolution ... may be significantly different to that of the resolution of the underlying measured data."
- Scope sentence: the terms "apply to The GEBCO Grid and other GEBCO-derived information products". GEBCO_08 (the grid under OceanBuilder's GMRT fill) is not named on the current pages; applying the current terms to it is an **assumption**.
- Attribution strings as given today: GEBCO_2026 "GEBCO Bathymetric Compilation Group 2026 (2026). The GEBCO_2026 Grid - a continuous terrain model for oceans and land at 15 arc-second intervals. NERC EDS British Oceanographic Data Centre NOC. doi:10.5285/4f68d5c7-45eb-f999-e063-7086abc036fa". GEBCO_2024: "GEBCO Compilation Group (2024) GEBCO 2024 Grid (doi:10.5285/1c44ce99-0a0d-5f4f-e063-7086abc0ea0f)". These are the *ask* "if the data sets are used in a presentation or publication"; the terms also say a suitable form is "in the documentation that accompanies The GEBCO Grid". Do not cite a GEBCO_2026 DOI for GEBCO_08 data; cite the edition actually used. For the GEBCO_08 fill the correct citation form was not retrieved.
- The "public domain" statement is GEBCO's own and is not a formal dedication like CC0; its legal effect in each jurisdiction, and whether the conditions in the terms bind as a contract, are **UNRESOLVED**. In practice GEBCO says the grid may be commercially exploited, which is the key point for a game.

### 3.3 "Not for navigation" and a game

Both GMRT ("should not be used for navigation purposes") and GEBCO ("NOT be used for navigation or for any other purpose involving safety at sea") put a use restriction on the *data*. A game does not navigate a ship, but a world compiled from real bathymetry could be used for real passage planning by someone who finds it, particularly if the game presents itself as accurate. Recommended handling: a navigation disclaimer in `ATTRIBUTION.txt`, in the manifest, and in the game's credits or legal screen ("Not for navigation; depths are interpolated and may be wrong"); do not market the data as a chart; do not imply endorsement by GEBCO/IHO/IOC. Whether a disclaimer is enough, and whether the restriction can be passed to end users, is for review.

## 4. NOAA (NCEI CRM, CUDEM, ETOPO1)

Verified today (https://www.ncei.noaa.gov/products/coastal-relief-model, FAQ): "Are there any restrictions on using this model? Just two: The U.S. CRM cannot be used for navigation. Products derived primarily from NCEI data are not subject to copyright protection in the United States (see copyright). Use the citations tab to acknowledge NCEI as a source." The CRM merges "NCEI's suite of Continuously Updated DEMs (CUDEMs), as well as source data from other international, U.S. federal and state agencies". The citation shown for volumes 1-5, 9, 10: "NOAA National Centers for Environmental Information. (2023). Coastal Relief Models (CRMs) [Data set]. NOAA National Centers for Environmental Information. doi: 10.25921/5ZN5-KN44". The page also lists older per-volume citations with other DOIs.

Not verified today: any CUDEM-specific page, the NCEI "copyright" page that the CRM FAQ points to, and a NOAA data-use policy text. CUDEM tiles are in an anonymous public bucket (see `docs/research/ecosystem.md`), but I did not read a licence statement for them. Do not rely on a remembered CUDEM DOI.

Secondary only (quoted inside Tilezen/joerd's attribution page, not read at NOAA): ETOPO1's metadata says "Not to be used for navigation. Although these data are of high quality and useful for planning and modeling purposes, they are not suitable for navigation", "Not subject to copyright protection within the United States", plus the 17 U.S.C. 403 notice requirement for third parties' predominantly-US-government works, and a formal credit "DOC/NOAA/NESDIS/NCEI > National Centers for Environmental Information, NESDIS, NOAA, U.S. Department of Commerce". Treat these as unverified until read at NOAA.

Implications: US-government works are free of copyright "in the United States" only; the status elsewhere is **UNRESOLVED** (the SRTM text quoted by joerd shows that "Copyright protection is asserted ... outside of the United States" can appear on US-government products). The CRM combines non-NOAA sources whose terms are not repeated on the page. Navigation language must be carried forward. Requested attribution is to NCEI via the citation tab. NOAA is not used by the build today.

## 5. AWS Open Data / Mapzen Terrain Tiles ("Terrarium")

- Registry entry (https://registry.opendata.aws/terrain-tiles/): "A global dataset providing bare-earth terrain heights, tiled for easy usage and provided on S3." Managed by "Mapzen, a Linux Foundation project". The registry's License field is only a pointer to https://github.com/tilezen/joerd/blob/master/docs/attribution.md . "How to Cite": "Terrain Tiles was accessed on DATE from https://registry.opendata.aws/terrain-tiles."
- Attribution page: "Attribution is required for many terrain tile data providers. Example language is provided below, but you are responsible for researching each project to follow their license terms." Required block (abridged): ArcticDEM "DEM(s) were created from DigitalGlobe, Inc., imagery and funded under National Science Foundation awards 1043681, 1559691, and 1542736"; "Australia terrain data © Commonwealth of Australia (Geoscience Australia) 2017"; "Austria terrain data © offene Daten Österreichs - Digitales Geländemodell (DGM) Österreich"; "Canada terrain data contains information licensed under the Open Government Licence - Canada"; "Europe terrain data produced using Copernicus data and information funded by the European Union - EU-DEM layers"; "Global ETOPO1 terrain data U.S. National Oceanic and Atmospheric Administration"; "Mexico terrain data source: INEGI, Continental relief, 2016"; "New Zealand terrain data Copyright 2011 Crown copyright (c) Land Information New Zealand and the New Zealand Government (All rights reserved)"; "Norway terrain data © Kartverket"; "United Kingdom terrain data © Environment Agency copyright and/or database right 2015. All rights reserved"; "United States 3DEP (formerly NED) and global GMTED2010 and SRTM terrain data courtesy of the U.S. Geological Survey". The hosted Mapzen service additionally requires "Mapzen".
- **Differences from the brief.** GEBCO is *not* in this list. For the ocean the data-sources page says "ETOPO1 for ocean bathymetry, 1 arc-minute resolution globally", and "ETOPO1 is used to show ocean bathymetry at all zooms". An ocean tool should note that this is about 2.5 km nominal bathymetry, so Terrain Tiles are not a bathymetry source for OceanBuilder anyway.
- **Internal inconsistency.** The "Required attribution" block says "All rights reserved" for the UK and New Zealand, while the fine print says UK data is under the UK Open Government Licence v3 and NZ data is under CC BY 3.0 NZ. Which governs is **UNRESOLVED**.
- **Share-alike.** None of the listed regional licences found in the fine print is share-alike (3DEP/SRTM/GMTED public domain with a credit request, ArcticDEM "unlicensed", OGL-Canada, OGL-UK, CC BY, Mexican free-use). So the burden is attribution, per region used, plus per-provider disclaimers; the page itself says it is the user's job to research each project.
- **Redistribution.** A compiled world built from Terrain Tiles would need the attribution lines for each region it contains. Whether attributing only the regions actually present (by tile footprint) is enough, versus the whole block, is **UNRESOLVED**; the block is written for the whole service. Not used by OceanBuilder today.

## 6. Not needed

Copernicus DEM and other land sources: out of scope by request.

## 7. The compiled world database

### 7.1 Three layers of "data"

1. **Source data**: files as obtained (GMRT GeoTIFF, EMODnet GeoTIFF, Overpass JSON). Each carries its own terms. The cache stores these with provenance sidecars.
2. **Derived data**: resampled, warped and composited rasters in the UTM grid; per-cell `source` layer; masks; statistics. Each derived value is an adaptation of one or more sources. For CC BY 4.0 and public-domain sources this is plain adaptation. For the ODbL (OSM) it is the Derivative Database / Produced Work question in section 1.
3. **Compiled world** (manifest + tiles + attribution + optional geojson files): a collection. For sources with attribution-only terms the collection can carry all required notices at once. For the ODbL, the safest model is "each OSM-derived component is identifiable and separable, so the rest of the collection can be treated as a Collective Database whose other parts are independent"; this is the model that should be reflected in the manifest.

### 7.2 What a distributed world must carry

Given sections 1 to 5, a world that is distributed (alone or inside a game) must at minimum carry:

- all source acknowledgements, in the exact wording above where a source gives one (EMODnet, GEBCO, GMRT including the Ryan et al. citation, OSM, NCEI if used);
- a "not for navigation" notice for GMRT, GEBCO, EMODnet and NOAA data, plus the GEBCO ban on implying official status or endorsement;
- for any OSM-derived component: the ODbL notice (4.3 text or the Attribution Guideline form), the ODbL licence URL, a copy of or link to the licence in the same place as the data, and preservation of notices (4.2.c), plus the per-component licence label;
- the retrieval dates and versions so that the DOI to cite is unambiguous (EMODnet release, GEBCO edition);
- a warning that licence terms were checked on a date and that upstream terms (GMRT, EMODnet) could not be fully verified.

### 7.3 Proposed machine-readable shape

Add to `world.manifest.json` (a proposal for the format spec, not implemented). Two parts: a per-source record and a per-component licence map. The second part is needed because a per-source record cannot express "this geojson file is ODbL while the tiles are CC BY".

```json
"licensing": {
  "checked_at": "2026-11-01",
  "sources": [
    {
      "id": "gmrt",
      "spdx": "CC-BY-4.0",
      "terms_url": "https://www.gmrt.org/about/terms_of_use.php",
      "terms_checked_at": "2026-11-01",
      "attribution_text": "Global Multi-Resolution Topography (GMRT) Synthesis, https://www.gmrt.org . Ryan et al. (2009), doi:10.1029/2008GC002332; data doi:10.1594/IEDA/100001",
      "citation": "Ryan et al. (2009) Geochem. Geophys. Geosyst. 10, Q03014",
      "doi": "10.1029/2008GC002332",
      "share_alike": false,
      "commercial_use": "yes",
      "navigation_disclaimer": true,
      "requires_notice": true,
      "endorsement_prohibited": false,
      "upstream_terms_unverified": true
    },
    {
      "id": "gebco",
      "spdx": "LicenseRef-GEBCO-public-domain-with-conditions",
      "terms_url": "https://www.gebco.net/data-products/gridded-bathymetry/terms-of-use",
      "attribution_text": "GEBCO Bathymetric Compilation Group <year> (<year>). The GEBCO_<year> Grid ... doi:...",
      "share_alike": false, "commercial_use": "yes",
      "navigation_disclaimer": true, "requires_notice": true,
      "endorsement_prohibited": true
    },
    {
      "id": "emodnet-dtm",
      "spdx": "CC-BY-4.0",
      "spdx_note": "default for EMODnet-created products per Terms of Use; the DTM metadata record states no licence",
      "terms_url": "https://emodnet.ec.europa.eu/en/terms-use-emodnet-online-services-data-and-data-products",
      "doi": "10.12770/cf51df64-56f9-4a99-b1aa-36b8d7b743a1",
      "attribution_text": "This data product was created by EMODnet (https://emodnet.ec.europa.eu/en/), and is owned by the EU and licensed under the Creative Commons Attribution 4.0 International (CC BY 4.0) license.",
      "share_alike": false, "commercial_use": "unclear",
      "navigation_disclaimer": true, "requires_notice": true,
      "upstream_terms_unverified": true
    },
    {
      "id": "osm-coastline",
      "spdx": "ODbL-1.0",
      "terms_url": "https://opendatacommons.org/licenses/odbl/1-0/",
      "attribution_text": "© OpenStreetMap contributors. Contains information from OpenStreetMap, which is made available here under the Open Database License (ODbL). https://www.openstreetmap.org/copyright",
      "share_alike": true, "commercial_use": "yes",
      "navigation_disclaimer": false, "requires_notice": true,
      "requires_license_copy": true
    },
    {
      "id": "osm-seamarks",
      "spdx": "ODbL-1.0",
      "terms_url": "https://opendatacommons.org/licenses/odbl/1-0/",
      "attribution_text": "(same as osm-coastline)",
      "share_alike": true, "commercial_use": "yes",
      "navigation_disclaimer": true,
      "navigation_disclaimer_note": "precautionary; OpenSeaMap statements not retrieved",
      "requires_notice": true, "requires_license_copy": true
    }
  ],
  "components": [
    {"path": "tiles/**", "contains_sources": ["gmrt", "gebco"], "license_summary": "attribution", "osm_derived_cells": 0},
    {"path": "coastline.wgs84.geojson", "contains_sources": ["osm-coastline"], "spdx": "ODbL-1.0", "separable": true},
    {"path": "seamarks.geojson", "contains_sources": ["osm-seamarks"], "spdx": "ODbL-1.0", "separable": true},
    {"path": "layers/osm-coast-mask/**", "contains_sources": ["osm-coastline"], "spdx": "ODbL-1.0", "separable": true}
  ],
  "required_notices": ["attribution", "navigation", "odbl"],
  "unresolved": ["odbl-derivative-database-scope", "upstream-terms-gmrt-emodnet"]
}
```

Notes on the shape: SPDX ids exist for `CC-BY-4.0` and `ODbL-1.0`; for GEBCO, NOAA and others use `LicenseRef-...` ids rather than inventing a standard SPDX id. `commercial_use` is a tri-state because "unclear" is a real state (EMODnet DTM). `share_alike` is a property of the *source* terms, while the consequences for the *world* are expressed in `components[]`; do not compute a world licence from the sources automatically. `upstream_terms_unverified` is a flag for sources (GMRT, EMODnet) that aggregate third-party data. The existing `cells_by_source` map should map source codes to these ids so that code 5 resolves to `osm-coastline`.

### 7.4 User responsibilities when distributing a generated world in a game

OceanBuilder cannot discharge these for the user. The user should: (1) ship `ATTRIBUTION.txt` and show the attributions in the game's credits screen or an "About / Data licences" menu entry; (2) show the navigation disclaimer; (3) not state or imply that GEBCO/IHO/IOC/EMODnet/NOAA/OSM endorse the game; (4) preserve source notices and DOIs; (5) if any OSM-derived component is in the shipped build (conformed cells, geojson, seamarks), follow the ODbL path for that component, which includes no restrictive packaging of it (4.7), an offer of the data or the alteration method (4.6), and a notice (4.3); (6) not present interpolated or filled data as measured, which the sources themselves ask (GEBCO "Not mislead others or misrepresent"); (7) re-check upstream terms before each release, since EMODnet's terms change "without prior notice"; (8) obtain legal review before a commercial release that includes OSM-derived cells.

## 8. Unresolved questions and recommended legal review (ranked)

1. **Is a tile set that contains `coast_conform` (source 5) cells a Derivative Database, or a Produced Work, and what does share-alike then cover (whole tile set or the conformed layer only)?** Face-value reading of the Horizontal Layers and Collective Database guidelines points to a Derivative Database for the mixed land/water feature type, but those guidelines were written for vector maps. *Legal review recommended before any commercial release that enables `coast_conform`.*
2. **Games: ODbL 4.4.c / 4.6 / 4.7 / 4.8 against encrypted or packed asset bundles and EULA no-extraction clauses**, and whether shipped heightmaps are "a database" or a Produced Work. *Legal review recommended.*
3. **"Substantial" for the coastline copy and the seamarks file** at world scale (systematic extraction of all features of a type), and whether the ~300 m conformed raster band lets the coastline be re-derived. The Falmouth fixture (11 features) is under the 100-feature mark but real worlds are not.
4. **Compatibility of CC BY 4.0 content (GMRT, EMODnet) and GEBCO's conditions inside an ODbL Derivative Database** (4.4.d), if question 1 is answered "derivative".
5. **GMRT and EMODnet aggregate third-party data** whose own terms are not listed on the pages read (GMRT's blanket CC BY 4.0; EMODnet's "consult any use restrictions or licences of individual data originators"). The EMODnet DTM record states no licence and "DO NOT USE FOR NAVIGATION".
6. **Whether 4.6.b "method of making the alterations (such as an algorithm)" is satisfied by the open-source tool, config and manifest provenance.**
7. **GEBCO "public domain" statement** (not a formal dedication; conditions bind by "use indicates acceptance"), whether the current terms cover GEBCO_08, and how the "should NOT be used for navigation" restrictions apply to a game and to downstream players.
8. **US-government data (NOAA/NCEI)**: "not subject to copyright protection in the United States" only; non-US status; CUDEM-specific terms not verified; the ETOPO1 text above is secondary.
9. **ODbL jurisdictional effect**: whether, absent a database right (for example in the US), geometry such as a coastline is protected only by contract and whether that changes what share-alike can require.
10. **Terrain Tiles attribution** (not used today): the UK/NZ "All rights reserved" lines against OGL/CC BY in the fine print, and whether attributing only regions present is adequate.
11. **OpenSeaMap's own licence statements**: not retrieved; the working assumption is that seamarks queried through Overpass are OSM Contents under the ODbL.
12. **Provenance of the unversioned EMODnet coverage** (`emodnet__mean`) and which DOI applies.
