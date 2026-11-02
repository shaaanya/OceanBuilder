package world

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaaanya/OceanBuilder/internal/gdal"
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

func reverseCoast(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/falmouth/coast.geojson")
	if err != nil {
		t.Fatal(err)
	}
	var fc map[string]any
	if err := json.Unmarshal(b, &fc); err != nil {
		t.Fatal(err)
	}
	rev := func(c []any) {
		for a, z := 0, len(c)-1; a < z; a, z = a+1, z-1 {
			c[a], c[z] = c[z], c[a]
		}
	}
	for _, f := range fc["features"].([]any) {
		g := f.(map[string]any)["geometry"].(map[string]any)
		switch g["type"] {
		case "LineString":
			rev(g["coordinates"].([]any))
		case "MultiLineString":
			for _, ln := range g["coordinates"].([]any) {
				rev(ln.([]any))
			}
		}
	}
	out, _ := json.Marshal(fc)
	p := filepath.Join(t.TempDir(), "reversed.geojson")
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Real OSM coastline, real GMRT: the forward direction conforms, the reversed one is refused and builds nothing.
func TestConformGuardOnRealFixtureBothDirections(t *testing.T) {
	if testing.Short() {
		t.Skip("slow (seconds to tens of seconds): runs in the integration tier, i.e. without -short")
	}
	fx := "../../testdata/falmouth/"
	for _, bb := range [][4]float64{{-5.135, 50.16, -5.06, 50.18}, {-5.12, 50.155, -5.085, 50.185}} {
		mk := func(coast string) *Config {
			return &Config{Name: "fixture", BBox: bb, CellM: 100, TileCells: 16, Resampling: "average", CoastConform: "osm", CoastConformMaxM: 600,
				Local: &Local{Topo: fx + "topo.tif", Mask: fx + "mask.tif", Coastline: coast, License: "t", Attribution: "t"}}
		}
		m, err := Build(mk(fx+"coast.geojson"), Options{CacheDir: t.TempDir(), OutDir: t.TempDir()})
		if err != nil {
			t.Fatalf("forward %v: %v", bb, err)
		}
		cc := m.Stats.CoastConform
		if cc == nil || cc.Agreement <= 0.5 {
			t.Fatalf("forward %v: agreement must clear the guard, got %+v", bb, cc)
		}
		t.Logf("%v forward agreement %.2f over %d cells", bb, cc.Agreement, cc.Labelled)
		_, err = Build(mk(reverseCoast(t)), Options{CacheDir: t.TempDir(), OutDir: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "way direction") {
			t.Fatalf("reversed %v must be refused by the guard, got %v", bb, err)
		}
	}
}

func TestBuildIDTracksAlgorithmVersionsOnlyWhenStepIsActive(t *testing.T) {
	base := &Manifest{Config: Config{Name: "t"}}
	id0 := buildID(base)
	if id0 != buildID(&Manifest{Config: Config{Name: "t"}}) {
		t.Fatal("build id must be deterministic")
	}
	for name, m := range map[string]*Manifest{
		"conform":    {Config: Config{Name: "t", CoastConform: "osm"}},
		"seamarks":   {Config: Config{Name: "t", Seamarks: true}},
		"refinement": {Config: Config{Name: "t"}, Refinement: &RefineStats{}},
	} {
		if buildID(m) == id0 {
			t.Errorf("%s: an active optional step must change the build id", name)
		}
	}
}
