package main

import (
	"fmt"
	"strings"
)

// sourceDetail is what `ocean sources explain` adds to the `sources list` entry. Every statement here is also in
// docs/sources.md; "Checked live" names the last date a real service was called by this repository's tests or runs,
// not a promise that the service still behaves that way.
type sourceDetail struct {
	Datum, Resolution, Measured, Antimeridian, CheckedLive, Limits string
}

var sourceDetails = map[string]sourceDetail{
	"gmrt": {
		Datum:        "GMRT-mosaic: the service states no vertical datum; a mosaic of contributed grids. Cannot be converted; combine with other datums only via vertical.allow_mixed.",
		Resolution:   "about 61 m at the equator on the finest level; delivered cell size is measured per request and recorded in datasets[].delivery. Water without survey data is a GEBCO_08 fill (30 arc-second).",
		Measured:     "survey mask layer: cells under multibeam are 'survey', everything else 'background'.",
		Antimeridian: "supported (two boxes, mosaicked); verified live 2026-11-02.",
		CheckedLive:  "2026-11-02: Galapagos zone edge, Fiji across 180, Hawaii 4 x 3 degrees (6 pieces).",
		Limits:       "large requests are coarsened silently by the service; the tool splits above 16 M nodes and warns (source-coarsened) when pieces still arrive coarse.",
	},
	"gebco": {
		Datum:        "MSL assumed by GEBCO (not a geoid model; shallow sources may differ).",
		Resolution:   "15 arc-second (about 460 m).",
		Measured:     "TID layer per cell: TID 10-17 direct measurement becomes the survey mask, everything else background.",
		Antimeridian: "supported (two subset requests); verified live 2026-11-02.",
		CheckedLive:  "2026-11-02: Falmouth smoke test, 0.6 x 0.4 degree world across 180 (66 s).",
		Limits:       "uses an undocumented API behind download.gebco.net that can change without notice (docs/decisions/0010).",
	},
	"etopo2022": {
		Datum:        "EGM2008 geoid heights (NOAA: an adequate approximation of mean sea level); the EGM2008-to-EGM96 conversion is applied only when PROJ resolves it with installed grids.",
		Resolution:   "15 arc-second (about 460 m).",
		Measured:     "none: the SID tile exists but its codes are not decoded, so every water cell is 'background'.",
		Antimeridian: "supported (tiles either side of 180); verified live 2026-11-02.",
		CheckedLive:  "2026-11-02: Falmouth smoke test, world across 180.",
		Limits:       "tiles are 15 degrees square and selected by name arithmetic; only the Falmouth and Fiji tiles were read live.",
	},
	"cudem": {
		Datum:        "NAVD88. No verified transformation exists in this tool: use alone, or with vertical.allow_mixed.",
		Resolution:   "1/9 arc-second (about 3 m).",
		Measured:     "none flagged.",
		Antimeridian: "refused before any download.",
		CheckedLive:  "2026-11-02: Key West smoke test (22 s).",
		Limits:       "US coasts and territories only; windows are capped at 0.5 degree per side (about 0.3 MB/s measured). The NOAA DEM mosaic ImageServer is deliberately not used (docs/decisions/0009).",
	},
	"emodnet": {
		Datum:        "unknown: the published DTM is LAT but the WCS declares none. Declare it yourself via vertical.",
		Resolution:   "about 115 m (1/16 arc-minute).",
		Measured:     "none.",
		Antimeridian: "refused before any download.",
		CheckedLive:  "0.2.0 (not re-run in 0.4.0).",
		Limits:       "European seas only; used as a regional fill, never counted as survey.",
	},
	"osm-coastline": {
		Datum:        "horizontal only (WGS 84 lon/lat).",
		Resolution:   "vector; map accuracy varies.",
		Measured:     "n/a. A coast_conform step changes land/water cells only within a guarded distance and marks them 'conformed'.",
		Antimeridian: "refused before any download.",
		CheckedLive:  "0.3.0 builds; mirrors can be slow or down, the build then continues without a coastline and says so.",
		Limits:       "ODbL: a world built with it contains OSM-derived cells; attribution is written, share-alike is not legally analysed.",
	},
	"osm-seamarks": {
		Datum:        "horizontal only.",
		Resolution:   "points.",
		Measured:     "n/a; cross-checked against the DEM, not used to edit it.",
		Antimeridian: "refused before any download.",
		CheckedLive:  "0.3.0 (Kona).",
		Limits:       "uneven coverage; the query is limited to the level-0 extent.",
	},
	"local": {
		Datum:        "whatever you declare (required for geotiff sources).",
		Resolution:   "whatever your file has; recorded from the file.",
		Measured:     "optional mask file; without it nothing is flagged as measured.",
		Antimeridian: "supported for local GeoTIFF.",
		CheckedLive:  "n/a (offline tests).",
		Limits:       "licence and attribution are required fields; a source without them fails validation.",
	},
}

func explainSource(id string) error {
	for _, s := range sources {
		if s.ID != id {
			continue
		}
		d, ok := sourceDetails[id]
		if !ok {
			return fmt.Errorf("source %q has no detail record (bug: add it to sourceDetails)", id)
		}
		fmt.Printf("%s: %s\n\n", s.ID, s.Kind)
		for _, row := range [][2]string{{"coverage", s.Coverage}, {"access", s.Access}, {"licence", s.License},
			{"vertical datum", d.Datum}, {"resolution", d.Resolution}, {"measured vs interpolated", d.Measured},
			{"antimeridian", d.Antimeridian}, {"checked live", d.CheckedLive}, {"limits", d.Limits}} {
			fmt.Printf("%-26s %s\n", row[0]+":", row[1])
		}
		return nil
	}
	var ids []string
	for _, s := range sources {
		ids = append(ids, s.ID)
	}
	return fmt.Errorf("unknown source %q (have: %s)", id, strings.Join(ids, ", "))
}
