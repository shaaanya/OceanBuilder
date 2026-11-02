package gebco

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func tif(t *testing.T, path, typ string, burn string, patch bool) []byte {
	t.Helper()
	if out, err := exec.Command("gdal_create", "-of", "GTiff", "-outsize", "20", "10", "-ot", typ, "-burn", burn, "-a_srs", "EPSG:4326", "-a_ullr", "-5.2", "50.2", "-5.0", "50.1", path).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if patch { // right half: TID 41 (interpolated), which must not count as measured
		gj := filepath.Join(filepath.Dir(path), "p.geojson")
		os.WriteFile(gj, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[-5.1,50.1],[-5.0,50.1],[-5.0,50.2],[-5.1,50.2],[-5.1,50.1]]]}}]}`), 0o644)
		if out, err := exec.Command("gdal_rasterize", "-burn", "41", gj, path).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	b, _ := os.ReadFile(path)
	return b
}

func fakeAPI(t *testing.T, statuses []string) (*httptest.Server, *int) {
	dir := t.TempDir()
	elev := tif(t, filepath.Join(dir, "e.tif"), "Int16", "-40", false)
	tid := tif(t, filepath.Join(dir, "t.tif"), "Byte", "11", true)
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for name, b := range map[string][]byte{"gebco_2026_n50.2_s50.1_w-5.2_e-5.0_geotiff.tif": elev, "gebco_2026_tid_n50.2_s50.1_w-5.2_e-5.0_geotiff.tif": tid, "GEBCO_Grid_documentation.pdf": []byte("pdf")} {
		f, _ := zw.Create(name)
		f.Write(b)
	}
	zw.Close()
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/queue", func(w http.ResponseWriter, r *http.Request) {
		var b basket
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || len(b.Items) != 1 || b.Items[0].GridID != 1 || len(b.Items[0].Sources) != 2 || b.Items[0].Left != -5.2 || b.Items[0].Top != 50.2 {
			http.Error(w, "bad basket", http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(201)
		io.WriteString(w, `{"message":"Basket added to queue","basketId":"GEBCO_TEST"}`)
	})
	mux.HandleFunc("/queue/status/GEBCO_TEST", func(w http.ResponseWriter, r *http.Request) {
		st := statuses[min(polls, len(statuses)-1)]
		polls++
		io.WriteString(w, `{"status":"`+st+`"}`)
	})
	mux.HandleFunc("/queue/download/GEBCO_TEST", func(w http.ResponseWriter, r *http.Request) { w.Write(zb.Bytes()) })
	srv := httptest.NewServer(mux)
	APIURL = srv.URL
	Poll = time.Millisecond
	return srv, &polls
}

func TestFetchFlowAndMeasuredMask(t *testing.T) {
	srv, polls := fakeAPI(t, []string{"queued", "processing", "finished"})
	defer srv.Close()
	elev, mask, err := Fetch(t.TempDir(), -5.2, 50.1, -5.0, 50.2)
	if err != nil {
		t.Fatal(err)
	}
	if *polls != 3 {
		t.Errorf("polled %d times", *polls)
	}
	for _, p := range []string{elev.SHA256, elev.License, elev.Attribution, mask.SHA256} {
		if p == "" {
			t.Fatalf("incomplete provenance %+v %+v", elev, mask)
		}
	}
	if !strings.Contains(mask.URL, "TID") {
		t.Errorf("mask provenance does not say it is derived from TID: %s", mask.URL)
	}
	// the mask has a value only on the left half (TID 11); the right half (TID 41) is nodata
	if v := validPercent(t, mask.File); v != "50" {
		t.Errorf("mask valid percent %q, want 50 (TID 11 half only)", v)
	}
}

func mustFloat(t *testing.T, v string) float64 {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func validPercent(t *testing.T, file string) string {
	t.Helper()
	out, err := exec.Command("gdalinfo", "-stats", file).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, l := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok && k == "STATISTICS_VALID_PERCENT" {
			return strconv.FormatFloat(mustFloat(t, v), 'f', -1, 64)
		}
	}
	t.Fatalf("no STATISTICS_VALID_PERCENT:\n%s", out)
	return ""
}

func TestFetchReportsFailedBasketAndBadReply(t *testing.T) {
	srv, _ := fakeAPI(t, []string{"failed"})
	defer srv.Close()
	if _, _, err := Fetch(t.TempDir(), -5.2, 50.1, -5.0, 50.2); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Errorf("failed basket: %v", err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"schema"}`, http.StatusUnprocessableEntity)
	}))
	defer bad.Close()
	APIURL = bad.URL
	if _, _, err := Fetch(t.TempDir(), -5.2, 50.1, -5.0, 50.2); err == nil || !strings.Contains(err.Error(), "undocumented") {
		t.Errorf("422: %v", err)
	}
}

// Smoke test against the real API: set OCEAN_SMOKE=1.
func TestSmokeFalmouth(t *testing.T) {
	if os.Getenv("OCEAN_SMOKE") == "" {
		t.Skip("set OCEAN_SMOKE=1 to call download.gebco.net")
	}
	APIURL, Poll = "https://download.gebco.net/api", 2*time.Second
	elev, mask, err := Fetch(t.TempDir(), -5.2, 50.05, -5.0, 50.2)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := exec.Command("gdalinfo", "-stats", elev.File).CombinedOutput()
	t.Logf("%s\n%s\nmeasured (TID 10-17) valid percent of mask: %s", elev.URL, info, validPercent(t, mask.File))
	if !strings.Contains(string(info), "Size is 48, 36") {
		t.Errorf("unexpected size:\n%s", info)
	}
}
