package world_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ocean-compiler/ocean/internal/gmrt"
	"github.com/ocean-compiler/ocean/internal/validate"
	"github.com/ocean-compiler/ocean/internal/world"
	"github.com/ocean-compiler/ocean/pkg/oceanworld"
)

// fakeGMRT serves GridServer requests from an analytic seabed that is continuous across 180: depth falls 400 m per
// degree eastwards, z = -50 - 400 (lon mod 360 - 180). A request is answered with a raster whose pixel grid starts at the
// requested west edge and extends one pixel past the east edge (the real service does the same, observed 2026-11-02).
// wrap360 labels the pieces west of 180 with longitudes above 180, which is what the real service did for a request in
// -180..-179 on that date; without it they keep their -180..180 labels. It records every requested box.
func fakeGMRT(t *testing.T, wrap360 bool, limit int) (url string, boxes func() [][4]float64) {
	var mu sync.Mutex
	var got [][4]float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := func(k string) float64 { v, _ := strconv.ParseFloat(q.Get(k), 64); return v }
		west, south, east, north := f("minlongitude"), f("minlatitude"), f("maxlongitude"), f("maxlatitude")
		mu.Lock()
		got = append(got, [4]float64{west, south, east, north})
		mu.Unlock()
		px := 0.001
		cols, rows := int(math.Ceil((east-west)/px))+1, int(math.Ceil((north-south)/px))+1
		for limit > 0 && cols*rows > limit { // the real service answers an oversized request at a coarser level, silently
			px *= 2
			cols, rows = int(math.Ceil((east-west)/px))+1, int(math.Ceil((north-south)/px))+1
		}
		label := func(lon float64) float64 {
			if wrap360 && lon < 0 {
				return lon + 360
			}
			return lon
		}
		dir := t.TempDir()
		raw := filepath.Join(dir, "r.bin")
		buf := make([]byte, 4*cols*rows)
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				lon := west + (float64(c)+0.5)*px
				v := float32(-50 - 400*(math.Mod(lon+360, 360)-180))
				if q.Get("layer") == gmrt.LayerMask {
					v = 1
				}
				binary.LittleEndian.PutUint32(buf[4*(r*cols+c):], math.Float32bits(v))
			}
		}
		os.WriteFile(raw, buf, 0o644)
		os.WriteFile(raw+".hdr", []byte(fmt.Sprintf("ENVI\nsamples = %d\nlines = %d\nbands = 1\nheader offset = 0\nfile type = ENVI Standard\ndata type = 4\ninterleave = bsq\nbyte order = 0\n", cols, rows)), 0o644)
		tif := filepath.Join(dir, "o.tif")
		cmd := exec.Command("gdal_translate", "-q", "-of", "GTiff", "-a_srs", "EPSG:4326", "-a_ullr",
			ftoa(label(west)), ftoa(north+px), ftoa(label(west)+float64(cols)*px), ftoa(north+px-float64(rows)*px), raw, tif)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("gdal_translate: %v\n%s", err, out)
			http.Error(w, "fail", 500)
			return
		}
		b, _ := os.ReadFile(tif)
		w.Header().Set("Content-Type", "image/tiff")
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() [][4]float64 { mu.Lock(); defer mu.Unlock(); return append([][4]float64(nil), got...) }
}

// A GMRT world across 180 is requested as two small boxes (never one spanning the globe), the pieces mosaic without a
// gap or a repeated strip, and the heights on both sides match the analytic seabed. Both labelling conventions of the
// service for the western piece are covered.
func TestAntimeridianGMRTSplit(t *testing.T) {
	for _, wrap := range []bool{true, false} {
		t.Run(fmt.Sprintf("wrap360=%v", wrap), func(t *testing.T) {
			url, boxes := fakeGMRT(t, wrap, 0)
			old := gmrt.Endpoint
			gmrt.Endpoint = url
			t.Cleanup(func() { gmrt.Endpoint = old })
			cfg := &world.Config{Name: "am-gmrt", BBox: [4]float64{179.7, -17, 180.3, -16.6}, CellM: 200, TileCells: 32, Coastline: "none"}
			m, out := buildIt(t, cfg)
			var bx [][4]float64
			for _, b := range boxes() {
				if b[2]-b[0] > 0.021 { // the 0.02 degree resolution probes are not data requests
					bx = append(bx, b)
				}
			}
			if len(bx) != 4 { // topo and mask for each of the two boxes
				t.Fatalf("expected 4 requests (topo+mask x 2 boxes), got %v", bx)
			}
			var span float64
			for _, b := range bx {
				if b[2]-b[0] > 1 || b[0] < -180 || b[2] > 180 {
					t.Errorf("request box %v is not a small box inside -180..180", b)
				}
				span += b[2] - b[0]
			}
			pad := 1.2 * 32 * 200 / (111320 * math.Cos(16.8*math.Pi/180)) // degrees of longitude, one side
			if want := 2 * (0.6 + 2*pad); math.Abs(span-want) > 0.01 {    // padded width, twice (topo and mask)
				t.Errorf("requested widths add up to %.4f degrees, padded box is about %.4f", span, want)
			}
			if m.Stats.NoDataFraction != 0 {
				t.Errorf("nodata fraction %v: a gap at the seam", m.Stats.NoDataFraction)
			}
			rep, err := validate.Run(out)
			if err != nil || rep.Result == "FAILED" {
				t.Fatalf("validate: %v\n%s", err, rep.Format())
			}
			w, err := oceanworld.Open(out)
			if err != nil {
				t.Fatal(err)
			}
			for _, lon := range []float64{179.75, 179.99, 179.9995, 180.0005, -179.99, -179.8} {
				s, err := w.SampleLonLat(lon, -16.8, 0)
				want := -50 - 400*(math.Mod(lon+360, 360)-180)
				if err != nil || !s.Valid || math.Abs(s.ElevationM-want) > 3 {
					t.Errorf("lon %v: got %+v (%v), want %.1f", lon, s, err, want)
				}
			}
		})
	}
}

// A request that fits the service's size limit is one download; a larger one is split so that each piece stays at the
// finest resolution. When the service coarsens anyway the manifest and the validator say so, and source_res_m is the
// coarsest piece actually received, never the requested resolution.
func TestGMRTDeliveryAndSplitting(t *testing.T) {
	old, oldBudget := gmrt.Endpoint, gmrt.NodeBudget
	t.Cleanup(func() { gmrt.Endpoint, gmrt.NodeBudget = old, oldBudget })
	cfg := func() *world.Config {
		return &world.Config{Name: "split", BBox: [4]float64{10, 40, 10.5, 40.3}, CellM: 200, TileCells: 32, Coastline: "none"}
	}
	run := func(budget float64, limit int) (*world.Manifest, string, [][4]float64) {
		url, boxes := fakeGMRT(t, false, limit)
		gmrt.Endpoint, gmrt.NodeBudget = url, budget
		m, out := buildIt(t, cfg())
		return m, out, boxes()
	}
	// 1. budget under the service limit: several pieces, all at the finest cell.
	m, out, bx := run(60_000, 100_000)
	d := m.Datasets[0].Delivery
	if d == nil || d.Pieces < 3 || d.Coarsened != 0 || math.Abs(d.CellMMax-d.CellMMin) > 1 {
		t.Fatalf("split delivery: %+v (%d requests)", d, len(bx))
	}
	if math.Abs(m.Resampling.SourceResM-d.CellMMax) > 1 || m.Resampling.SourceResM > 130 {
		t.Errorf("source_res_m %v does not match the delivered cell %v", m.Resampling.SourceResM, d.CellMMax)
	}
	if rep, err := validate.Run(out); err != nil || rep.Result == "FAILED" || hasCode(rep.Format(), "source-coarsened") {
		t.Fatalf("validate: %v\n%s", err, rep.Format())
	}
	// 2. no splitting but a service limit: the pieces come back coarser; this must be reported.
	m, out, _ = run(1e12, 100_000)
	d = m.Datasets[0].Delivery
	if d == nil || d.Coarsened == 0 || d.Note == "" || d.CellMMax < 1.9*d.ReferenceCellM {
		t.Fatalf("coarsened delivery not detected: %+v", d)
	}
	if m.Resampling.SourceResM < 1.9*d.ReferenceCellM {
		t.Errorf("source_res_m %v must be the coarse cell (reference %v)", m.Resampling.SourceResM, d.ReferenceCellM)
	}
	rep, err := validate.Run(out)
	if err != nil || !hasCode(rep.Format(), "source-coarsened") {
		t.Fatalf("validator must warn: %v\n%s", err, rep.Format())
	}
}

func hasCode(report, code string) bool { return strings.Contains(report, "["+code+"]") }
