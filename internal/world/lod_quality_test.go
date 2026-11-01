package world_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ocean-compiler/ocean/internal/validate"
	"github.com/ocean-compiler/ocean/internal/world"
)

// lodScene writes a 10 m GeoTIFF with the features LOD must not destroy: a land mass with a ~30 m wide channel cut
// through it, a ~25 x 35 m islet and a ~30 x 45 m shoal in 30 m deep water, and a cliff coast (land +10 m next to -30 m).
func lodScene(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "scene.tif")
	mkTif(t, path, -5.116, 50.144, -5.084, 50.176, 356, 356, -30, "")
	box := func(w, s, e, n, v float64) {
		gj := fmt.Sprintf(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[%v,%v],[%v,%v],[%v,%v],[%v,%v],[%v,%v]]]}}]}`, w, s, e, s, e, n, w, n, w, s)
		f := filepath.Join(dir, "f.geojson")
		os.WriteFile(f, []byte(gj), 0o644)
		if out, err := exec.Command("gdal_rasterize", "-burn", ftoa(v), f, path).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	box(-5.116, 50.144, -5.100, 50.176, 10)     // land
	box(-5.116, 50.1600, -5.100, 50.1603, -8)   // channel through the land
	box(-5.0950, 50.1550, -5.0947, 50.1553, 4)  // islet
	box(-5.0900, 50.1650, -5.0896, 50.1654, -3) // shoal
	return path
}

func lodCfg(path, method string) *world.Config {
	return &world.Config{Name: "lod", BBox: [4]float64{-5.112, 50.148, -5.088, 50.172}, CellM: 20, TileCells: 16, Coastline: "none", LODLevels: 2, LODMethod: method,
		Sources: []world.SourceSpec{{ID: "s", Type: "geotiff", Path: path, Quality: "survey", License: "test", Attribution: "synthetic", Vertical: &world.VerticalRef{Datum: "EGM2008"}}}}
}

// Every LOD method builds a world that validates (shared edges, parent/child rules, source codes), keeps level 0
// identical, and reports measurable error. The numbers are logged; the assertions are the guarantees each method claims.
func TestLODMethodsOnHardFeatures(t *testing.T) {
	scene := lodScene(t)
	res := map[string][]validate.LODLevelError{}
	var l0 map[string][]byte
	for _, method := range []string{"decimate", "shoal"} {
		m, out := buildIt(t, lodCfg(scene, method))
		rep, err := validate.Run(out)
		if err != nil || rep.Result == "FAILED" {
			t.Fatalf("%s: validate: %v\n%s", method, err, rep.Format())
		}
		r, err := validate.LODReport(out, 1)
		if err != nil {
			t.Fatal(err)
		}
		res[method] = r
		t.Logf("\n%s", validate.FormatLOD(m.LOD.Method, r))
		// level 0 does not depend on the LOD method
		files := map[string][]byte{}
		for _, tl := range m.TilesAt(0) {
			b, _ := os.ReadFile(filepath.Join(out, tl.Height))
			files[tl.ID] = b
		}
		if l0 == nil {
			l0 = files
		} else {
			for id, b := range files {
				if string(b) != string(l0[id]) {
					t.Fatalf("%s: level 0 tile %s differs from decimate build", method, id)
				}
			}
		}
	}
	// shoal's claim, measured at every level-0 vertex: the level surface is never deeper than level 0 in water
	for i, h := range res["shoal"] {
		if h.TooDeepMaxM != 0 {
			t.Errorf("shoal level %d is deeper than level 0 by up to %.3f m", h.Level, h.TooDeepMaxM)
		}
		// and decimation does not have the property on this scene (otherwise the test proves nothing)
		if d := res["decimate"][i]; d.TooDeepMaxM < 1 {
			t.Errorf("decimation level %d too_deep_max %.3f m: scene does not exercise the shoal", d.Level, d.TooDeepMaxM)
		}
	}
}
