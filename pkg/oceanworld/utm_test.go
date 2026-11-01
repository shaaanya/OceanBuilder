package oceanworld

import (
	"fmt"
	"math"
	"testing"

	"github.com/ocean-compiler/ocean/internal/gdal"
)

// The pure-Go projection must agree with GDAL/PROJ to a millimetre, in both hemispheres, near a zone edge
// and at high latitude, and must round-trip.
func TestUTMMatchesGDAL(t *testing.T) {
	cases := []struct {
		lon, lat float64
		epsg     int
	}{
		{-5.1, 50.15, 32630},   // Falmouth
		{-24.99, 16.89, 32626}, // Mindelo
		{151.2, -33.86, 32756}, // Sydney, southern hemisphere
		{-70.2, -53.9, 32719},  // Tierra del Fuego
		{172.9, -43.5, 32759},  // Christchurch
		{-3.0, 0.0, 32630},     // equator, on the zone edge
		{-8.9999, 0.5, 32630},  // zone 30 western edge
		{18.0, 69.7, 32634},    // Tromsø, high latitude
		{-135.0, 58.3, 32608},  // south-east Alaska
	}
	for _, c := range cases {
		want, err := gdal.Transform("EPSG:4326", fmt.Sprintf("EPSG:%d", c.epsg), [][2]float64{{c.lon, c.lat}})
		if err != nil {
			t.Skipf("gdaltransform unavailable: %v", err)
		}
		zone, north, _ := ZoneOf(c.epsg)
		e, n := LonLatToUTM(c.lon, c.lat, zone, north)
		if math.Abs(e-want[0][0]) > 1e-3 || math.Abs(n-want[0][1]) > 1e-3 {
			t.Errorf("EPSG:%d (%.4f,%.4f): got %.4f,%.4f, GDAL %.4f,%.4f", c.epsg, c.lon, c.lat, e, n, want[0][0], want[0][1])
		}
		lon, lat := UTMToLonLat(e, n, zone, north)
		if math.Abs(lon-c.lon) > 1e-9 || math.Abs(lat-c.lat) > 1e-9 {
			t.Errorf("EPSG:%d round trip (%.6f,%.6f) -> (%.9f,%.9f)", c.epsg, c.lon, c.lat, lon, lat)
		}
	}
}

func TestZoneOf(t *testing.T) {
	if z, n, ok := ZoneOf(32630); !ok || z != 30 || !n {
		t.Fatal("32630")
	}
	if z, n, ok := ZoneOf(32756); !ok || z != 56 || n {
		t.Fatal("32756")
	}
	if _, _, ok := ZoneOf(4326); ok {
		t.Fatal("4326 is not UTM")
	}
}
