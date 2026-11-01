package world_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ocean-compiler/ocean/internal/etopo"
	"github.com/ocean-compiler/ocean/internal/gebco"
	"github.com/ocean-compiler/ocean/internal/validate"
	"github.com/ocean-compiler/ocean/internal/world"
)

func gdalMust(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// An ETOPO 2022 source (served here by a local HTTP server standing in for NOAA) builds a world whose manifest declares
// the EGM2008 datum, carries the tile provenance, and validates.
func TestETOPOSourceBuildsWithDeclaredDatum(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "15s_surface_elev_gtif"), 0o755)
	gdalMust(t, "gdal_create", "-of", "GTiff", "-outsize", "300", "300", "-ot", "Float32", "-burn", "-60", "-a_srs", "EPSG:4326", "-a_ullr", "-15", "60", "0", "45", "-a_nodata", "-99999",
		filepath.Join(dir, "15s_surface_elev_gtif", etopo.TileName(-15, 60, "surface")))
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()
	old := etopo.BaseURL
	etopo.BaseURL = srv.URL
	defer func() { etopo.BaseURL = old }()
	cfg := &world.Config{Name: "etopo", BBox: [4]float64{-5.12, 50.14, -5.08, 50.18}, CellM: 100, TileCells: 16, Coastline: "none",
		Sources: []world.SourceSpec{{Type: "etopo2022"}}}
	m, out := buildIt(t, cfg)
	d := m.Datasets[0]
	if d.ID != "etopo2022" || d.DeclaredDtm != "EGM2008" || m.Vertical.Datum != "EGM2008" {
		t.Fatalf("dataset %+v vertical %+v", d, m.Vertical)
	}
	if len(m.Sources) != 1 || m.Sources[0].SHA256 == "" || m.Sources[0].License == "" || m.Sources[0].Attribution == "" || m.Sources[0].URL == "" {
		t.Fatalf("provenance %+v", m.Sources)
	}
	if rep, err := validate.Run(out); err != nil || rep.Result == "FAILED" {
		t.Fatalf("validate: %v\n%s", err, rep.Format())
	}
}

// A GEBCO source (fake download API) gives per-sample provenance from the TID layer: cells whose TID is a direct
// measurement become "survey" (measured), the rest stay "background".
func TestGEBCOSourceMeasuredFromTID(t *testing.T) {
	dir := t.TempDir()
	// 100 x 100 px over -5.2..-4.9 x 50.0..50.3; TID 11 (multibeam) on the western half, 41 (interpolated) on the eastern
	gdalMust(t, "gdal_create", "-of", "GTiff", "-outsize", "100", "100", "-ot", "Int16", "-burn", "-40", "-a_srs", "EPSG:4326", "-a_ullr", "-5.2", "50.3", "-4.9", "50.0", filepath.Join(dir, "e.tif"))
	gdalMust(t, "gdal_create", "-of", "GTiff", "-outsize", "100", "100", "-ot", "Byte", "-burn", "11", "-a_srs", "EPSG:4326", "-a_ullr", "-5.2", "50.3", "-4.9", "50.0", filepath.Join(dir, "t.tif"))
	os.WriteFile(filepath.Join(dir, "p.geojson"), []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[-5.05,50.0],[-4.9,50.0],[-4.9,50.3],[-5.05,50.3],[-5.05,50.0]]]}}]}`), 0o644)
	gdalMust(t, "gdal_rasterize", "-burn", "41", filepath.Join(dir, "p.geojson"), filepath.Join(dir, "t.tif"))
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for name, f := range map[string]string{"gebco_2026_n50.3_s50.0_w-5.2_e-4.9_geotiff.tif": "e.tif", "gebco_2026_tid_n50.3_s50.0_w-5.2_e-4.9_geotiff.tif": "t.tif"} {
		w, _ := zw.Create(name)
		b, _ := os.ReadFile(filepath.Join(dir, f))
		w.Write(b)
	}
	zw.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/queue", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		io.WriteString(w, `{"basketId":"T"}`)
	})
	mux.HandleFunc("/queue/status/T", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"status":"finished"}`) })
	mux.HandleFunc("/queue/download/T", func(w http.ResponseWriter, r *http.Request) { w.Write(zb.Bytes()) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	oldAPI, oldPoll := gebco.APIURL, gebco.Poll
	gebco.APIURL, gebco.Poll = srv.URL, time.Millisecond
	defer func() { gebco.APIURL, gebco.Poll = oldAPI, oldPoll }()
	cfg := &world.Config{Name: "gebco", BBox: [4]float64{-5.15, 50.05, -4.95, 50.25}, CellM: 500, TileCells: 16, Coastline: "none",
		Sources: []world.SourceSpec{{Type: "gebco"}}}
	m, out := buildIt(t, cfg)
	survey, back := m.Stats.BySource["survey"], m.Stats.BySource["background"]
	if survey == 0 || back == 0 {
		t.Fatalf("by source %v: both measured and interpolated cells expected", m.Stats.BySource)
	}
	// the TID boundary is at -5.05, the box spans -5.15..-4.95, so roughly half of the cells are measured
	if f := float64(survey) / float64(survey+back); f < 0.35 || f > 0.65 {
		t.Errorf("measured fraction %.2f, want about 0.5 (%v)", f, m.Stats.BySource)
	}
	if m.Datasets[0].DeclaredDtm != "MSL" {
		t.Errorf("declared datum %q", m.Datasets[0].DeclaredDtm)
	}
	if rep, err := validate.Run(out); err != nil || rep.Result == "FAILED" {
		t.Fatalf("validate: %v\n%s", err, rep.Format())
	}
}
