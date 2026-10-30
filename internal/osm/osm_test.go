package osm

import (
	"encoding/json"
	"testing"
)

func TestToGeoJSON(t *testing.T) {
	raw := `{"elements":[
	 {"type":"way","id":7,"geometry":[{"lat":50.1,"lon":-5.1},{"lat":50.2,"lon":-5.0}]},
	 {"type":"way","id":8,"geometry":[{"lat":50.1,"lon":-5.1}]},
	 {"type":"node","id":9}]}`
	out, err := ToGeoJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	var fc struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string       `json:"type"`
				Coordinates [][2]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(out, &fc); err != nil {
		t.Fatal(err)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("want 1 feature (degenerate way and node dropped), got %d", len(fc.Features))
	}
	g := fc.Features[0].Geometry
	if g.Type != "LineString" || g.Coordinates[0] != [2]float64{-5.1, 50.1} {
		t.Fatalf("coordinates must be lon,lat: %+v", g)
	}
}

func TestToGeoJSONRejectsGarbage(t *testing.T) {
	if _, err := ToGeoJSON([]byte("<html>rate limited</html>")); err == nil {
		t.Fatal("expected error for non-JSON Overpass response")
	}
}
