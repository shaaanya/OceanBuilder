// Package emodnet fetches the EMODnet Bathymetry DTM ("mean depth", about 115 m) for a bbox
// through its public WCS 2.0.1 endpoint, as a GeoTIFF in EPSG:4326 holding elevation in metres
// (negative below sea level). The service declares Fees and AccessConstraints NONE; it does not
// declare a vertical datum, so callers must measure the offset against another source.
package emodnet

import (
	"fmt"
	"net/url"

	"github.com/ocean-compiler/ocean/internal/fetch"
)

const (
	Endpoint = "https://ows.emodnet-bathymetry.eu/wcs"
	Coverage = "emodnet__mean"
	// Service-declared coverage extent (lon W,S,E,N), from GetCapabilities on 2026-10-30.
	CoverW, CoverS, CoverE, CoverN = -70.5, 11.0, 43.0, 90.0
)

// Covers reports whether the whole bbox lies inside the declared coverage extent.
func Covers(w, s, e, n float64) bool {
	return w >= CoverW && s >= CoverS && e <= CoverE && n <= CoverN
}

// Fetch downloads the coverage for the bbox (cached by request).
func Fetch(cacheDir string, w, s, e, n float64) (*fetch.Provenance, error) {
	if !Covers(w, s, e, n) {
		return nil, fmt.Errorf("bbox %.3f,%.3f,%.3f,%.3f is outside the EMODnet coverage (lon %.1f..%.1f, lat %.1f..%.1f)", w, s, e, n, CoverW, CoverE, CoverS, CoverN)
	}
	q := url.Values{
		"service": {"WCS"}, "version": {"2.0.1"}, "request": {"GetCoverage"}, "coverageId": {Coverage}, "format": {"image/tiff"},
	}
	// WCS wants repeated subset parameters; fetch.Get takes a ready URL, so build it by hand.
	u := fmt.Sprintf("%s?%s&subset=Lat(%.6f,%.6f)&subset=Long(%.6f,%.6f)", Endpoint, q.Encode(), s, n, w, e)
	return fetch.Get(cacheDir, "emodnet", u, nil, "emodnet-mean.tif", "EMODnet open data (service declares Fees and AccessConstraints: NONE)",
		"EMODnet Bathymetry Consortium, EMODnet Digital Bathymetry (DTM), https://emodnet.ec.europa.eu/en/bathymetry")
}
