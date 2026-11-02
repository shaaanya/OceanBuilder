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

	"github.com/shaaanya/OceanBuilder/internal/etopo"
	"github.com/shaaanya/OceanBuilder/internal/gebco"
	"github.com/shaaanya/OceanBuilder/internal/validate"
	"github.com/shaaanya/OceanBuilder/internal/world"
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

// The manifest says what the world must carry: user files are recorded as user-declared, OSM appears only when OSM data
// was used, and a coastline copy can be left out of the output.
func TestLicensingRecord(t *testing.T) {
	cfg, _ := twoSourceCfg(t, []string{"fine", "coarse"}, "")
	m, out := buildIt(t, cfg)
	l := m.Licensing
	if l == nil || l.CheckedAt == "" || len(l.Sources) != 2 {
		t.Fatalf("licensing %+v", l)
	}
	for _, s := range l.Sources {
		if s.SPDX != "LicenseRef-user-declared" || s.AttributionText != "synthetic" {
			t.Errorf("source %+v", s)
		}
	}
	for _, n := range l.RequiredNotices {
		if n == "odbl" {
			t.Errorf("no OSM data was used but odbl is a required notice: %v", l.RequiredNotices)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "coastline.wgs84.geojson")); err == nil {
		t.Error("coastline none must not write a coastline file")
	}
	// a user-supplied coastline is a separable user-declared component; no_coastline_copy leaves it out of the output
	dir := t.TempDir()
	topo, coast := filepath.Join(dir, "t.tif"), filepath.Join(dir, "c.geojson")
	mkTif(t, topo, -5.125, 50.14, -5.075, 50.18, 50, 40, -30, "")
	os.WriteFile(coast, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[-5.115,50.16],[-5.085,50.16]]}}]}`), 0o644)
	for _, omit := range []bool{false, true} {
		c := &world.Config{Name: "lc", BBox: [4]float64{-5.115, 50.145, -5.085, 50.175}, CellM: 20, TileCells: 16, NoCoastlineCopy: omit,
			Local: &world.Local{Topo: topo, Coastline: coast, License: "test", Attribution: "synthetic"}}
		m, out := buildIt(t, c)
		_, err := os.Stat(filepath.Join(out, "coastline.wgs84.geojson"))
		if omit != (err != nil) {
			t.Errorf("no_coastline_copy=%v but file present=%v", omit, err == nil)
		}
		var comp int
		for _, cm := range m.Licensing.Components {
			if cm.Path == "coastline.wgs84.geojson" {
				comp++
				if cm.SPDX != "LicenseRef-user-declared" || !cm.Separable {
					t.Errorf("component %+v", cm)
				}
			}
		}
		if comp != map[bool]int{false: 1, true: 0}[omit] {
			t.Errorf("omit=%v: %d coastline components", omit, comp)
		}
	}
}
