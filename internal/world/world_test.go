package world

import (
	"math"
	"strings"
	"testing"

	"github.com/ocean-compiler/ocean/internal/gdal"
)

func validCfg() Config {
	return Config{Name: "t", BBox: [4]float64{-5.1, 50.1, -5.0, 50.2}, CellM: 50}
}

func TestConfigNormalize(t *testing.T) {
	c := validCfg()
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if c.TileCells != 256 || c.Resampling != "bilinear" || c.Coastline != "osm" || len(c.DepthBands) == 0 {
		t.Fatalf("defaults not applied: %+v", c)
	}
	bad := map[string]func(*Config){
		"name":        func(c *Config) { c.Name = "" },
		"bbox order":  func(c *Config) { c.BBox = [4]float64{-5, 50, -5.1, 50.2} },
		"bbox pole":   func(c *Config) { c.BBox[3] = 89 },
		"cell":        func(c *Config) { c.CellM = 0 },
		"tile cells":  func(c *Config) { c.TileCells = 100 },
		"resampling":  func(c *Config) { c.Resampling = "lanczos7" },
		"bands":       func(c *Config) { c.DepthBands = []float64{10, 5} },
		"local lic":   func(c *Config) { c.Local = &Local{Topo: "x.tif"} },
		"coast value": func(c *Config) { c.Coastline = "magic" },
	}
	for name, mut := range bad {
		c := validCfg()
		mut(&c)
		if err := c.Normalize(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSampleBilinear(t *testing.T) {
	r := &gdal.Raster{W: 2, H: 2, Data: []float32{0, 10, 20, 30}}
	if v, ok := sampleBilinear(r, 0.5, 0.5); !ok || math.Abs(v-15) > 1e-9 {
		t.Fatalf("centre = %v %v, want 15", v, ok)
	}
	if _, ok := sampleBilinear(r, 1.5, 0); ok {
		t.Fatal("outside grid must report !ok")
	}
	r.Data[3] = noData
	if _, ok := sampleBilinear(r, 0.5, 0.5); ok {
		t.Fatal("sample touching nodata must report !ok, not blend -9999 into the result")
	}
}

func TestNearestShore(t *testing.T) {
	W, H := 11, 11
	shore := make([]uint8, W*H)
	shore[5*W+8] = 255
	d, ok := nearestShore(shore, W, H, 5, 5, 8)
	if !ok || math.Abs(d-3) > 1e-9 {
		t.Fatalf("got %v %v, want 3", d, ok)
	}
	if _, ok := nearestShore(shore, W, H, 5, 5, 2); ok {
		t.Fatal("shoreline beyond radius must not match")
	}
}

func TestDepthClasses(t *testing.T) {
	cs := depthClasses([]float64{5, 20})
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, "|"); got != "land|0–5 m|5–20 m|>20 m|nodata" {
		t.Fatalf("classes = %s", got)
	}
	if cs[len(cs)-1].ID != ClassNoData {
		t.Fatal("last class must be the nodata id")
	}
}
