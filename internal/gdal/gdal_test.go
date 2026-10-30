package gdal

import (
	"math"
	"os/exec"
	"testing"
)

func TestUTMEPSG(t *testing.T) {
	cases := []struct {
		lon, lat float64
		want     int
	}{
		{-5.07, 50.15, 32630}, // Falmouth
		{-25.0, 16.9, 32626},  // Mindelo
		{151.2, -33.9, 32756}, // Sydney, southern hemisphere
		{-179.9, 10, 32601},   // western edge
		{179.9, 10, 32660},    // eastern edge
	}
	for _, c := range cases {
		if got := UTMEPSG(c.lon, c.lat); got != c.want {
			t.Errorf("UTMEPSG(%v,%v) = %d, want %d", c.lon, c.lat, got, c.want)
		}
	}
}

func TestTransformRoundTripAndScale(t *testing.T) {
	if _, err := exec.LookPath("gdaltransform"); err != nil {
		t.Skip("gdaltransform not installed")
	}
	// Two points 0.01° of longitude apart: ground distance must shrink with cos(lat),
	// i.e. degrees are not metres.
	utm, err := Transform("EPSG:4326", "EPSG:32630", [][2]float64{{-5.07, 50.15}, {-5.06, 50.15}})
	if err != nil {
		t.Fatal(err)
	}
	dx := utm[1][0] - utm[0][0]
	want := 0.01 * 111320 * math.Cos(50.15*math.Pi/180)
	if math.Abs(dx-want) > want*0.01 {
		t.Fatalf("0.01° lon at 50°N = %.1f m, want ≈%.1f m", dx, want)
	}
	back, err := Transform("EPSG:32630", "EPSG:4326", utm)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(back[0][0]+5.07) > 1e-7 || math.Abs(back[0][1]-50.15) > 1e-7 {
		t.Fatalf("round trip drifted: %v", back[0])
	}
}
