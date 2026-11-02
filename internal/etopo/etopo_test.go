package etopo

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

func TestTileNames(t *testing.T) {
	if got := TileName(-15, 60, "surface"); got != "ETOPO_2022_v1_15s_N60W015_surface.tif" {
		t.Fatal(got) // the example file name the live directory listing shows
	}
	got := Tiles(-5.2, 50.05, -4.95, 50.22)
	if len(got) != 1 || got[0] != [2]int{-15, 60} {
		t.Fatalf("Falmouth tiles %v", got)
	}
	// a box over the equator and the prime meridian needs four tiles
	if got := Tiles(-1, -1, 1, 1); len(got) != 4 {
		t.Fatalf("%v", got)
	}
	if TileName(0, 0, "surface") != "ETOPO_2022_v1_15s_N00E000_surface.tif" || TileName(-30, -15, "x") != "ETOPO_2022_v1_15s_S15W030_x.tif" {
		t.Fatal("hemisphere prefixes")
	}
}

// Fetch reads a window through /vsicurl range requests from a local server standing in for NOAA.
func TestFetchWindowFromServer(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "15s_surface_elev_gtif"), 0o755)
	tile := filepath.Join(dir, "15s_surface_elev_gtif", TileName(-15, 60, "surface"))
	// 150 x 150 pixels over -15..0, 45..60 (0.1 degree), value = -100 - column index so a wrong window shows
	if out, err := exec.Command("gdal_create", "-of", "GTiff", "-outsize", "150", "150", "-ot", "Float32", "-burn", "-100", "-a_srs", "EPSG:4326", "-a_ullr", "-15", "60", "0", "45", "-a_nodata", "-99999", tile).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()
	old := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = old }()
	p, err := Fetch(t.TempDir(), -5.2, 50.05, -4.95, 50.22)
	if err != nil {
		t.Fatal(err)
	}
	if p.SHA256 == "" || p.License == "" || p.Attribution == "" || p.URL == "" {
		t.Fatalf("provenance %+v", p)
	}
	info, err := gdal.Run("gdalinfo", "-stats", p.File)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"STATISTICS_MAXIMUM=-100", "STATISTICS_MINIMUM=-100"} {
		if !strings.Contains(info, want) {
			t.Errorf("window lacks %s:\n%s", want, info)
		}
	}
}

// Smoke test against the real service: set OCEAN_SMOKE=1. Falmouth is on tile N60W015.
func TestSmokeFalmouth(t *testing.T) {
	if os.Getenv("OCEAN_SMOKE") == "" {
		t.Skip("set OCEAN_SMOKE=1 to call the real ETOPO 2022 service")
	}
	p, err := Fetch(t.TempDir(), -5.2, 50.05, -5.0, 50.2)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := gdal.Run("gdalinfo", "-stats", p.File)
	t.Logf("%s\n%s", p.URL, info)
	if !strings.Contains(info, "Size is 48, 36") {
		t.Errorf("unexpected window size:\n%s", info)
	}
}
