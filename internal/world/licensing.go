package world

import (
	"fmt"
	"sort"
)

// LicensingDate is when the terms recorded below were last read (docs/research/licensing-analysis.md, sources.md). Terms
// can change without notice. This is a record of what was read, not legal advice and not a licence grant.
const LicensingDate = "2026-11-01"

// Licensing tells a consumer of the world what it must carry and which parts are separable. It is generated from the
// sources that actually contributed to this build.
type Licensing struct {
	CheckedAt       string             `json:"checked_at"`
	Disclaimer      string             `json:"disclaimer"`
	Sources         []LicenseSource    `json:"sources"`
	Components      []LicenseComponent `json:"components"`
	RequiredNotices []string           `json:"required_notices"` // attribution | navigation | odbl | no-endorsement
	Unresolved      []string           `json:"unresolved,omitempty"`
}

// LicenseSource is the terms record of one source.
type LicenseSource struct {
	ID                    string `json:"id"`
	SPDX                  string `json:"spdx"` // SPDX id, or LicenseRef-... when no SPDX id exists
	TermsURL              string `json:"terms_url,omitempty"`
	AttributionText       string `json:"attribution_text"`
	ShareAlike            bool   `json:"share_alike"`
	NavigationDisclaimer  bool   `json:"navigation_disclaimer"`
	EndorsementProhibited bool   `json:"endorsement_prohibited,omitempty"`
	UpstreamUnverified    bool   `json:"upstream_terms_unverified,omitempty"`
	Note                  string `json:"note,omitempty"`
}

// LicenseComponent maps a part of the output to the sources whose data it contains.
type LicenseComponent struct {
	Path      string   `json:"path"`
	Contains  []string `json:"contains_sources"`
	SPDX      string   `json:"spdx,omitempty"`
	Separable bool     `json:"separable"`
	Note      string   `json:"note,omitempty"`
}

const osmText = "© OpenStreetMap contributors. Contains information from OpenStreetMap, which is made available under the Open Database License (ODbL). https://www.openstreetmap.org/copyright"

var licenseTable = map[string]LicenseSource{
	"gmrt": {SPDX: "CC-BY-4.0", TermsURL: "https://www.gmrt.org/about/terms_of_use.php", NavigationDisclaimer: true, UpstreamUnverified: true,
		AttributionText: "Global Multi-Resolution Topography (GMRT) Synthesis, https://www.gmrt.org. Ryan et al. (2009), doi:10.1029/2008GC002332; data doi:10.1594/IEDA/100001. Contributed grids carry their own credits.",
		Note:            "Contributed grids inside GMRT may carry their own terms; the service states no vertical datum."},
	"emodnet": {SPDX: "CC-BY-4.0", TermsURL: "https://emodnet.ec.europa.eu/en/terms-use-emodnet-online-services-data-and-data-products", NavigationDisclaimer: true, UpstreamUnverified: true,
		AttributionText: "This data product was created by EMODnet (https://emodnet.ec.europa.eu/en/), and is owned by the EU and licensed under the Creative Commons Attribution 4.0 International (CC BY 4.0) license.",
		Note:            "CC BY 4.0 is the default for EMODnet-created products; the DTM metadata record itself states no licence."},
	"gebco": {SPDX: "LicenseRef-GEBCO-public-domain-with-conditions", TermsURL: "https://www.gebco.net/data-products/gridded-bathymetry/terms-of-use", NavigationDisclaimer: true, EndorsementProhibited: true,
		AttributionText: "GEBCO Bathymetric Compilation Group 2026 (2026). The GEBCO_2026 Grid - a continuous terrain model of the global oceans and land. NERC EDS British Oceanographic Data Centre (NOC). doi:10.5285/4f68d5c7-45eb-f999-e063-7086abc0ea6f",
		Note:            "Public domain per its terms, with conditions: acknowledge the source, do not imply endorsement, not for navigation."},
	"etopo2022": {SPDX: "LicenseRef-NOAA-NCEI-no-copyright-statement", TermsURL: "https://www.ncei.noaa.gov/products/etopo-global-relief-model", NavigationDisclaimer: true, UpstreamUnverified: true,
		AttributionText: "NOAA National Centers for Environmental Information. 2022: ETOPO 2022 15 Arc-Second Global Relief Model. NOAA National Centers for Environmental Information. DOI: 10.25921/fd45-gt74",
		Note:            "The product page states 'no copyright or security distribution restrictions'; the terms were not reviewed beyond that."},
	"cudem": {SPDX: "LicenseRef-US-government-no-copyright", TermsURL: "https://doi.org/10.25921/ds9v-ky35", NavigationDisclaimer: true,
		AttributionText: "Cooperative Institute for Research in Environmental Sciences (CIRES) at the University of Colorado, Boulder. 2014: Continuously Updated Digital Elevation Model (CUDEM) - 1/9 Arc-Second Resolution Bathymetric-Topographic Tiles. NOAA National Centers for Environmental Information. https://doi.org/10.25921/ds9v-ky35",
		Note:            "Not subject to copyright in the United States (ISO metadata); not to be used for navigation."},
	"osm-coastline": {SPDX: "ODbL-1.0", TermsURL: "https://opendatacommons.org/licenses/odbl/1-0/", ShareAlike: true, AttributionText: osmText},
	"osm-seamarks": {SPDX: "ODbL-1.0", TermsURL: "https://opendatacommons.org/licenses/odbl/1-0/", ShareAlike: true, NavigationDisclaimer: true, AttributionText: osmText,
		Note: "Navigation disclaimer is precautionary; OpenSeaMap's own statements were not retrieved."},
}

// buildLicensing derives the licensing record from what this build used.
func buildLicensing(cfg *Config, m *Manifest, dss []*dataset, regional bool) *Licensing {
	l := &Licensing{CheckedAt: LicensingDate,
		Disclaimer: "Records the terms of each source as read on the date above; terms can change. Not legal advice and not a licence for the compiled world. Not for navigation.",
		Unresolved: []string{"upstream-terms-gmrt-emodnet-not-fully-verified"}}
	seen := map[string]bool{}
	add := func(id string, ls LicenseSource) {
		if seen[id] {
			return
		}
		seen[id] = true
		ls.ID = id
		l.Sources = append(l.Sources, ls)
	}
	var tileSources []string
	for _, d := range dss {
		t := d.spec.Type
		if ls, ok := licenseTable[t]; ok {
			add(d.spec.ID, ls)
		} else { // user file: the licence is the user's declaration
			add(d.spec.ID, LicenseSource{SPDX: "LicenseRef-user-declared", AttributionText: d.spec.Attribution, Note: "licence as declared by the user: " + d.spec.License})
		}
		tileSources = append(tileSources, d.spec.ID)
	}
	if regional {
		add("emodnet-regional", licenseTable["emodnet"])
		tileSources = append(tileSources, "emodnet-regional")
	}
	odbl := false
	if m.Files["coastline"] != "" || cfg.Coastline != "none" || cfg.CoastConform == "osm" {
		if cfg.Local == nil || cfg.Local.Coastline == "" {
			add("osm-coastline", licenseTable["osm-coastline"])
			odbl = true
		}
	}
	if cfg.Seamarks {
		add("osm-seamarks", licenseTable["osm-seamarks"])
		odbl = true
	}
	tiles := LicenseComponent{Path: "tiles/**", Contains: tileSources, Separable: false}
	if cfg.CoastConform == "osm" {
		tiles.Contains = append(tiles.Contains, "osm-coastline")
		tiles.SPDX = "ODbL-1.0"
		tiles.Note = "land/water decisions of cells with source code 5 (conformed) come from the OSM coastline: whether the tile set is then a derivative database or a produced work is unresolved"
		l.Unresolved = append(l.Unresolved, "odbl-derivative-database-scope-for-coast-conform")
	}
	l.Components = append(l.Components, tiles)
	if m.Files["coastline"] != "" {
		if cfg.Local != nil && cfg.Local.Coastline != "" {
			l.Components = append(l.Components, LicenseComponent{Path: m.Files["coastline"], Contains: []string{"local-coastline"}, SPDX: "LicenseRef-user-declared", Separable: true,
				Note: "licence as declared by the user: " + cfg.Local.License})
		} else {
			l.Components = append(l.Components, LicenseComponent{Path: m.Files["coastline"], Contains: []string{"osm-coastline"}, SPDX: "ODbL-1.0", Separable: true,
				Note: "copy of the OSM coastline used for checks; set no_coastline_copy to leave it out of the output"})
		}
	}
	if m.Files["seamarks"] != "" {
		l.Components = append(l.Components, LicenseComponent{Path: m.Files["seamarks"], Contains: []string{"osm-seamarks"}, SPDX: "ODbL-1.0", Separable: true})
	}
	notices := map[string]bool{"attribution": true, "navigation": true}
	for _, s := range l.Sources {
		if s.EndorsementProhibited {
			notices["no-endorsement"] = true
		}
	}
	if odbl {
		notices["odbl"] = true
	}
	for k := range notices {
		l.RequiredNotices = append(l.RequiredNotices, k)
	}
	sort.Strings(l.RequiredNotices)
	return l
}

// attributionLines are the extra lines ATTRIBUTION.txt carries for the licensing record.
func (l *Licensing) attributionLines() []string {
	var out []string
	for _, s := range l.Sources {
		out = append(out, fmt.Sprintf("%s — %s — %s (terms: %s)", s.ID, s.AttributionText, s.SPDX, s.TermsURL))
		if s.EndorsementProhibited {
			out = append(out, s.ID+": do not state or imply that the data provider endorses this product")
		}
	}
	return out
}
