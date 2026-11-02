// Package osm fetches OpenStreetMap coastline ways via Overpass and converts
// them to GeoJSON LineStrings. Data is ODbL; attribution is carried into the
// world manifest.
package osm

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/ocean-compiler/ocean/internal/fetch"
)

// getMirrors fetches one Overpass query. A response already cached from ANY mirror is used before the network is tried:
// responses are cached per mirror URL, so without this a warm build would first re-ask a mirror that was down last time
// (waiting out its timeout), and could silently take different data from another mirror than the build before it.
func getMirrors(cacheDir, source string, form url.Values, license, attribution string) (*fetch.Provenance, error) {
	for _, m := range Mirrors {
		if p := fetch.Cached(cacheDir, source, m, form, "overpass.json", license, attribution); p != nil {
			return p, nil
		}
	}
	var err error
	for _, m := range Mirrors {
		var p *fetch.Provenance
		if p, err = fetch.GetTimeout(overpassTimeout, cacheDir, source, m, form, "overpass.json", license, attribution); err == nil {
			return p, nil
		}
	}
	return nil, fmt.Errorf("all Overpass mirrors failed (last: %w)", err)
}

// Mirrors are tried in order.
var Mirrors = []string{
	"https://overpass-api.de/api/interpreter",
	"https://overpass.kumi.systems/api/interpreter",
	"https://overpass.private.coffee/api/interpreter",
}

// overpassTimeout bounds one mirror attempt: the query asks the server for 60-90 s, so a mirror silent for
// longer is down or overloaded and the next one should be tried.
const overpassTimeout = 100 * time.Second

type overpass struct {
	Elements []struct {
		Type     string `json:"type"`
		ID       int64  `json:"id"`
		Geometry []struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"geometry"`
	} `json:"elements"`
}

// FetchCoastline downloads natural=coastline ways in the bbox and writes a
// GeoJSON file next to the raw response. Returns provenance of the raw
// response and the GeoJSON path.
func FetchCoastline(cacheDir string, w, s, e, n float64) (*fetch.Provenance, string, error) {
	q := fmt.Sprintf(`[out:json][timeout:60];way["natural"="coastline"](%.6f,%.6f,%.6f,%.6f);out geom;`, s, w, n, e)
	form := url.Values{"data": {q}}
	prov, err := getMirrors(cacheDir, "osm-coastline", form, "ODbL 1.0", "© OpenStreetMap contributors, https://www.openstreetmap.org/copyright")
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(prov.File)
	if err != nil {
		return nil, "", err
	}
	gj, err := ToGeoJSON(raw)
	if err != nil {
		return nil, "", err
	}
	out := filepath.Join(filepath.Dir(prov.File), "coastline.geojson")
	return prov, out, os.WriteFile(out, gj, 0o644)
}

// ToGeoJSON converts an Overpass `out geom` JSON response to a GeoJSON
// FeatureCollection of LineStrings (EPSG:4326, lon/lat order).
func ToGeoJSON(raw []byte) ([]byte, error) {
	var o overpass
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("overpass json: %w", err)
	}
	feats := []map[string]any{}
	for _, el := range o.Elements {
		if el.Type != "way" || len(el.Geometry) < 2 {
			continue
		}
		coords := make([][2]float64, len(el.Geometry))
		for i, g := range el.Geometry {
			coords[i] = [2]float64{g.Lon, g.Lat}
		}
		feats = append(feats, map[string]any{"type": "Feature",
			"properties": map[string]any{"osm_way": el.ID, "natural": "coastline"},
			"geometry":   map[string]any{"type": "LineString", "coordinates": coords}})
	}
	return json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
}
