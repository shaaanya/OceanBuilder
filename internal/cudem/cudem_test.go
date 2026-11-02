package cudem

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaaanya/OceanBuilder/internal/gdal"
)

const sample = `https://noaa-nos-coastal-lidar-pds.s3.amazonaws.com/dem/NCEI_ninth_Topobathy_2014_8483/FL/ncei19_n24x75_w082x00_2022v1.tif
https://noaa-nos-coastal-lidar-pds.s3.amazonaws.com/dem/NCEI_ninth_Topobathy_2014_8483/FL/ncei19_n24x75_w081x75_2022v1.tif
https://noaa-nos-coastal-lidar-pds.s3.amazonaws.com/dem/NCEI_ninth_Topobathy_2014_8483/HI/ncei19_n21x50_w158x00_2014v1.tif
not a tile
https://example.invalid/readme.txt
`

func TestIndexParsingAndSelection(t *testing.T) {
	ts := ParseIndex(sample)
	if len(ts) != 3 || ts[0].West != -82 || ts[0].North != 24.75 || ts[1].West != -81.75 {
		t.Fatalf("%+v", ts)
	}
	// the window lies inside the tile with west -82.00 (covers -82.00..-81.75, 24.50..24.75)
	got := Select(ts, -81.95, 24.55, -81.90, 24.60)
	if len(got) != 1 || got[0].West != -82 {
		t.Fatalf("Key West window: %+v", got)
	}
	if got := Select(ts, -81.74, 24.55, -81.70, 24.60); len(got) != 1 || got[0].West != -81.75 {
		t.Fatalf("east window: %+v", got)
	}
	if got := Select(ts, -5.2, 50.05, -5.0, 50.2); len(got) != 0 {
		t.Fatalf("Falmouth is not covered: %+v", got)
	}
}

func TestFetchRefusesOutsideCoverageAndHugeWindows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sample)) }))
	defer srv.Close()
	old := IndexURL
	IndexURL = srv.URL + "/urllist.txt"
	defer func() { IndexURL = old }()
	if _, err := Fetch(t.TempDir(), -5.2, 50.05, -5.0, 50.2); err == nil || !strings.Contains(err.Error(), "no 1/9 arc-second tile") {
		t.Errorf("outside coverage: %v", err)
	}
	if _, err := Fetch(t.TempDir(), -83, 24, -81, 25); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("huge window: %v", err)
	}
}

// A window through /vsicurl keeps the horizontal CRS only and does not touch the heights.
func TestFetchWindowKeepsHeights(t *testing.T) {
	dir := t.TempDir()
	tile := filepath.Join(dir, "FL", "ncei19_n24x75_w082x00_2022v1.tif")
	os.MkdirAll(filepath.Dir(tile), 0o755)
	if out, err := exec.Command("gdal_create", "-of", "GTiff", "-outsize", "100", "100", "-ot", "Float32", "-burn", "-3.5", "-a_srs", "EPSG:4269+5703", "-a_ullr", "-82", "24.75", "-81.75", "24.5", "-a_nodata", "-9999", tile).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	idx := "http://" + "PLACEHOLDER" + "/FL/ncei19_n24x75_w082x00_2022v1.tif\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/urllist.txt" {
			w.Write([]byte(strings.Replace(idx, "PLACEHOLDER", r.Host, 1)))
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, r.URL.Path))
	}))
	defer srv.Close()
	old := IndexURL
	IndexURL = srv.URL + "/urllist.txt"
	defer func() { IndexURL = old }()
	p, err := Fetch(t.TempDir(), -81.95, 24.55, -81.90, 24.60)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := gdal.Run("gdalinfo", "-stats", p.File)
	if !strings.Contains(info, "STATISTICS_MAXIMUM=-3.5") || !strings.Contains(info, "STATISTICS_MINIMUM=-3.5") {
		t.Errorf("heights changed by the window read:\n%s", info)
	}
	if strings.Contains(info, "VERT_CS") || strings.Contains(info, "VERTCRS") || strings.Contains(info, "NAVD88") {
		t.Errorf("window still carries a vertical CRS:\n%s", info)
	}
}

// Smoke test against the real bucket: set OCEAN_SMOKE=1. A small Key West window (about 0.02 degrees).
func TestSmokeKeyWest(t *testing.T) {
	if os.Getenv("OCEAN_SMOKE") == "" {
		t.Skip("set OCEAN_SMOKE=1 to call the real CUDEM bucket")
	}
	p, err := Fetch(t.TempDir(), -81.81, 24.55, -81.79, 24.57)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := gdal.Run("gdalinfo", "-stats", p.File)
	t.Logf("%s\n%s", p.URL, info)
}
