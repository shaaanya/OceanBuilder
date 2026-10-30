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

func TestParseSeamarks(t *testing.T) {
	raw := `{"elements":[
	 {"type":"way","id":5,"center":{"lat":50.15,"lon":-5.05},"tags":{"seamark:type":"harbour","name":"Falmouth Docks"}},
	 {"type":"node","id":2,"lat":50.07,"lon":-5.06,"tags":{"seamark:type":"wreck","name":"Volnay","wreck:depth":"19m"}},
	 {"type":"node","id":1,"lat":50.10,"lon":-5.00,"tags":{"seamark:type":"buoy_lateral"}},
	 {"type":"way","id":6,"tags":{"seamark:type":"harbour"}},
	 {"type":"node","id":3,"lat":50.1,"lon":-5.0,"tags":{"name":"no seamark tag"}}]}`
	sm, err := ParseSeamarks([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(sm) != 3 {
		t.Fatalf("want 3 (way without centre and untagged node dropped), got %d", len(sm))
	}
	if sm[0].Type != "buoy_lateral" || sm[1].Type != "harbour" || sm[2].Type != "wreck" {
		t.Fatalf("order must be by type: %v %v %v", sm[0].Type, sm[1].Type, sm[2].Type)
	}
	if sm[1].Lon != -5.05 || sm[1].Lat != 50.15 || sm[1].OSMType != "way" {
		t.Fatalf("way must use its centre as lon,lat: %+v", sm[1])
	}
	if sm[2].Tags["wreck:depth"] != "19m" {
		t.Fatal("raw tags must be kept")
	}
}
