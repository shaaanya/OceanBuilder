package osm

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"

	"github.com/ocean-compiler/ocean/internal/fetch"
)

// Seamark is one OpenSeaMap feature reduced to a point: a node's own position,
// or the centre of a way or relation.
type Seamark struct {
	OSMType  string            `json:"osm_type"`
	OSMID    int64             `json:"osm_id"`
	Type     string            `json:"seamark_type"` // value of the seamark:type tag
	Name     string            `json:"name,omitempty"`
	Lon, Lat float64           `json:"-"`
	Tags     map[string]string `json:"tags"`
}

// FetchSeamarks downloads every element tagged seamark:type in the bbox.
// The raw response is cached; the parsed features are returned in a stable order.
func FetchSeamarks(cacheDir string, w, s, e, n float64) (*fetch.Provenance, []Seamark, error) {
	q := fmt.Sprintf(`[out:json][timeout:90];nwr["seamark:type"](%.6f,%.6f,%.6f,%.6f);out center tags;`, s, w, n, e)
	form := url.Values{"data": {q}}
	var prov *fetch.Provenance
	var err error
	for _, m := range Mirrors {
		prov, err = fetch.Get(cacheDir, "osm-seamarks", m, form, "overpass.json",
			"ODbL 1.0", "© OpenStreetMap contributors (OpenSeaMap tagging), https://www.openstreetmap.org/copyright")
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("all Overpass mirrors failed (last: %w)", err)
	}
	raw, err := os.ReadFile(prov.File)
	if err != nil {
		return nil, nil, err
	}
	sm, err := ParseSeamarks(raw)
	return prov, sm, err
}

// ParseSeamarks reads an Overpass `out center tags` response. Elements without a
// position or without a seamark:type are dropped. Order: type, then OSM id.
func ParseSeamarks(raw []byte) ([]Seamark, error) {
	var o struct {
		Elements []struct {
			Type   string  `json:"type"`
			ID     int64   `json:"id"`
			Lat    float64 `json:"lat"`
			Lon    float64 `json:"lon"`
			Center *struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
			Tags map[string]string `json:"tags"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("overpass json: %w", err)
	}
	out := []Seamark{}
	for _, el := range o.Elements {
		t := el.Tags["seamark:type"]
		if t == "" {
			continue
		}
		lon, lat := el.Lon, el.Lat
		if el.Type != "node" {
			if el.Center == nil {
				continue
			}
			lon, lat = el.Center.Lon, el.Center.Lat
		}
		out = append(out, Seamark{OSMType: el.Type, OSMID: el.ID, Type: t, Name: el.Tags["name"], Lon: lon, Lat: lat, Tags: el.Tags})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].OSMID < out[j].OSMID
	})
	return out, nil
}
