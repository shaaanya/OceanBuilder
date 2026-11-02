package world_test

import (
	"bytes"
	"encoding/json"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ocean-compiler/ocean/internal/export"
	"github.com/ocean-compiler/ocean/internal/libreterrain"
	"github.com/ocean-compiler/ocean/internal/validate"
	"github.com/ocean-compiler/ocean/internal/world"
	"github.com/ocean-compiler/ocean/pkg/oceanworld"
)

// mkTif writes a Float32 GeoTIFF (EPSG:4326) covering w,s,e,n with n x m cells holding value; holeGeoJSON, when
// set, burns nodata (-9999) into the polygon.
func mkTif(t *testing.T, path string, w, s, e, n float64, cols, rows int, value float64, hole string) {
	t.Helper()
	run := func(name string, args ...string) {
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run("gdal_create", "-of", "GTiff", "-outsize", itoa(cols), itoa(rows), "-bands", "1", "-ot", "Float32", "-burn", ftoa(value),
		"-a_srs", "EPSG:4326", "-a_nodata", "-9999", "-a_ullr", ftoa(w), ftoa(n), ftoa(e), ftoa(s), path)
	if hole != "" {
		gj := filepath.Join(filepath.Dir(path), "hole.geojson")
		if err := os.WriteFile(gj, []byte(hole), 0o644); err != nil {
			t.Fatal(err)
		}
		run("gdal_rasterize", "-burn", "-9999", gj, path)
	}
}

func itoa(i int) string     { return strconv.Itoa(i) }
func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func fmtF(f float64) string { return strconv.FormatFloat(f, 'f', 9, 64) }

var squareHole = `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[[-5.1016,50.1555],[-5.0984,50.1555],[-5.0984,50.1585],[-5.1016,50.1585],[-5.1016,50.1555]]]}}]}`

func twoSourceCfg(t *testing.T, order []string, prec string) (*world.Config, string) {
	dir := t.TempDir()
	// coarse regional grid over the whole box (-50 m), fine grid over a sub-area (-20 m) with a nodata hole
	mkTif(t, filepath.Join(dir, "coarse.tif"), -5.125, 50.14, -5.075, 50.18, 50, 40, -50, "")
	mkTif(t, filepath.Join(dir, "fine.tif"), -5.105, 50.150, -5.095, 50.165, 100, 150, -20, squareHole)
	spec := map[string]world.SourceSpec{
		"fine": {ID: "fine", Type: "geotiff", Path: filepath.Join(dir, "fine.tif"), Quality: "survey", Year: 2020, AccuracyM: 0.5,
			License: "test", Attribution: "synthetic", Vertical: &world.VerticalRef{Datum: "EGM2008"}},
		"coarse": {ID: "coarse", Type: "geotiff", Path: filepath.Join(dir, "coarse.tif"), Quality: "compiled", Year: 2010,
			License: "test", Attribution: "synthetic", Vertical: &world.VerticalRef{Datum: "EGM2008"}},
	}
	cfg := &world.Config{Name: "compose", BBox: [4]float64{-5.115, 50.145, -5.085, 50.175}, CellM: 20, TileCells: 16, Coastline: "none", Precedence: prec}
	for _, o := range order {
		cfg.Sources = append(cfg.Sources, spec[o])
	}
	return cfg, dir
}

func buildIt(t *testing.T, cfg *world.Config) (*world.Manifest, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "w")
	m, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: out})
	if err != nil {
		t.Fatal(err)
	}
	return m, out
}

func use(m *world.Manifest, id string) world.DatasetUse {
	for _, u := range m.Stats.Composition.Datasets {
		if u.ID == id {
			return u
		}
	}
	return world.DatasetUse{}
}

// Listed order decides: the fine grid wins inside its footprint, the coarse one fills its nodata hole and the rest,
// every cell is attributed, and reversing the list gives the coarse grid every cell it covers.
func TestPrecedenceOverlapHoleAndAttribution(t *testing.T) {
	if testing.Short() {
		t.Skip("slow (seconds to tens of seconds): runs in the integration tier, i.e. without -short")
	}
	cfg, _ := twoSourceCfg(t, []string{"fine", "coarse"}, "")
	m, out := buildIt(t, cfg)
	f, c := use(m, "fine"), use(m, "coarse")
	if f.Selected == 0 || c.Selected == 0 || f.Overridden != 0 || c.Overridden == 0 {
		t.Fatalf("fine %+v coarse %+v", f, c)
	}
	// the hole is nodata in the fine grid, so coarse cells inside the fine footprint are not "overridden"
	if f.Selected+c.Selected != m.Stats.Cells-cellsNoData(m) {
		t.Fatalf("selected %d+%d != valid cells", f.Selected, c.Selected)
	}
	if m.Stats.CellsByDataset["fine"] != f.Selected || m.Stats.CellsByDataset["coarse"] != c.Selected {
		t.Fatalf("cells_by_dataset %v vs composition %+v %+v", m.Stats.CellsByDataset, f, c)
	}
	// overlap statistic: winner (fine, -20) minus other (coarse, -50) = +30 m exactly
	if len(m.Stats.Composition.Overlaps) != 1 || math.Abs(m.Stats.Composition.Overlaps[0].MedianM-30) > 0.1 {
		t.Fatalf("overlaps %+v", m.Stats.Composition.Overlaps)
	}
	// dataset layer agrees with heights in every tile: -20 only where the layer says fine, -50 where it says coarse
	idx := map[string]int{}
	for _, d := range m.Datasets {
		idx[d.ID] = d.Index
	}
	seenFine, seenCoarse := 0, 0
	for _, tl := range m.TilesAt(0) {
		h, ds := readHeights(t, out, tl.Height), readDatasetLayer(t, out, tl.Dataset)
		for i, v := range h {
			switch ds[i] {
			case uint8(idx["fine"]):
				seenFine++
				if math.Abs(float64(v)+20) > 1e-3 {
					t.Fatalf("fine cell holds %v", v)
				}
			case uint8(idx["coarse"]):
				seenCoarse++
				if math.Abs(float64(v)+50) > 1e-3 {
					t.Fatalf("coarse cell holds %v", v)
				}
			case 0:
				if v != -9999 {
					t.Fatalf("no dataset but height %v", v)
				}
			}
		}
	}
	if seenFine == 0 || seenCoarse == 0 {
		t.Fatalf("layer shows fine %d coarse %d", seenFine, seenCoarse)
	}

	rev, _ := twoSourceCfg(t, []string{"coarse", "fine"}, "")
	mr, _ := buildIt(t, rev)
	if use(mr, "fine").Selected != 0 || use(mr, "coarse").Selected != c.Selected+f.Selected {
		t.Fatalf("reversed: %+v %+v", use(mr, "fine"), use(mr, "coarse"))
	}
	// "auto" ranks the survey-quality grid first whatever the list order, and records why in the manifest
	auto, _ := twoSourceCfg(t, []string{"coarse", "fine"}, "auto")
	ma, _ := buildIt(t, auto)
	if use(ma, "fine").Selected != f.Selected || ma.Stats.Composition.Order[0] != "fine" {
		t.Fatalf("auto: order %v fine %+v", ma.Stats.Composition.Order, use(ma, "fine"))
	}
}

// valid_max stops a lower-ranked dataset from supplying cells above it: land artefacts of a bathymetry source cannot
// fill the nodata hole of the land DEM, and without the rule they do.
func TestValidMaxKeepsHighCellsOutOfFallbackDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: integration tier")
	}
	for _, limited := range []bool{false, true} {
		cfg, _ := twoSourceCfg(t, []string{"fine", "coarse"}, "")
		// coarse becomes a +100 m "land spike" source; fine keeps its nodata hole
		dir := filepath.Dir(cfg.Sources[1].Path)
		mkTif(t, cfg.Sources[1].Path, -5.125, 50.14, -5.075, 50.18, 50, 40, 100, "")
		_ = dir
		if limited {
			zero := 0.0
			cfg.Sources[1].ValidMax = &zero
		}
		m, _ := buildIt(t, cfg)
		if got := use(m, "coarse").Selected; limited && got != 0 || !limited && got == 0 {
			t.Fatalf("valid_max set=%v: coarse supplied %d cells", limited, got)
		}
		if limited && m.Stats.NoDataFraction == 0 {
			t.Fatal("hole must stay nodata, not be filled")
		}
	}
}

func cellsNoData(m *world.Manifest) int {
	return int(math.Round(m.Stats.NoDataFraction * float64(m.Stats.Cells)))
}

// Datasets whose vertical references cannot be shown to agree are refused, with the reason.
func TestVerticalRefusals(t *testing.T) {
	cfg, _ := twoSourceCfg(t, []string{"fine", "coarse"}, "")
	cfg.Sources[1].Vertical = &world.VerticalRef{Datum: "MLLW"}
	cases := map[string]struct {
		mut  func(c *world.Config)
		want string
	}{
		"mixed datums, no target": {func(c *world.Config) {}, "do not share a verified vertical reference"},
		"tidal to geoid":          {func(c *world.Config) { c.Vertical = &world.VerticalTarget{Datum: "EGM2008"} }, "tidal datums vary"},
		"unknown vs unknown":      {func(c *world.Config) { c.Sources[0].Vertical.Datum, c.Sources[1].Vertical.Datum = "unknown", "unknown" }, "do not share a verified"},
		"offset without citation": {func(c *world.Config) {
			o := 0.3
			c.Sources[1].Vertical = &world.VerticalRef{Datum: "MLLW", OffsetM: &o}
			c.Vertical = &world.VerticalTarget{Datum: "EGM2008"}
		}, "offset_source"},
		"no ballpark pretence (MSL)": {func(c *world.Config) {
			c.Sources[1].Vertical.Datum = "MSL"
			c.Vertical = &world.VerticalTarget{Datum: "EGM2008"}
		}, "no verified transformation"},
		"unknown datum name":       {func(c *world.Config) { c.Sources[1].Vertical.Datum = "sea level-ish" }, "unknown"},
		"geotiff without vertical": {func(c *world.Config) { c.Sources[1].Vertical = nil }, "needs a vertical reference"},
	}
	for name, tc := range cases {
		c := *cfg
		c.Sources = append([]world.SourceSpec(nil), cfg.Sources...)
		v0, v1 := *c.Sources[0].Vertical, *c.Sources[1].Vertical
		c.Sources[0].Vertical, c.Sources[1].Vertical = &v0, &v1
		tc.mut(&c)
		_, err := world.Build(&c, world.Options{CacheDir: t.TempDir(), OutDir: filepath.Join(t.TempDir(), "w")})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want error containing %q, got %v", name, tc.want, err)
		}
	}
	// accepted explicitly: builds, records that nothing was converted
	c := *cfg
	c.Vertical = &world.VerticalTarget{Datum: "EGM2008", AllowMixed: true}
	m, _ := buildIt(t, &c)
	if !m.Vertical.Mixed {
		t.Fatalf("mixed build must say so: %+v", m.Vertical)
	}
}

// A geoid transformation is applied on the source grid before warping and recorded with its PROJ grids. The
// expected shift is computed independently with gdaltransform from the same published models.
func TestVerticalGeoidShiftEGM96ToEGM2008(t *testing.T) {
	cfg, _ := twoSourceCfg(t, []string{"coarse"}, "")
	cfg.Sources[0].Vertical = &world.VerticalRef{Datum: "EGM96"}
	cfg.Vertical = &world.VerticalTarget{Datum: "EGM2008"}
	m, out := buildIt(t, cfg)
	ap := m.Datasets[0].Vertical
	if ap == nil || ap.Kind != "proj-grid" || !ap.Verified || len(ap.Grids) < 2 || ap.Grids[0].SHA256 == "" || !strings.Contains(ap.Pipeline, "vgridshift") {
		t.Fatalf("applied %+v", ap)
	}
	tl := m.TilesAt(0)[0]
	h := readHeights(t, out, tl.Height)
	// centre sample of the tile: position from the manifest bounds, converted back to lon/lat by gdaltransform
	S := m.Grid.SamplesPerTile
	e, n := (tl.BoundsUTM[0]+tl.BoundsUTM[2])/2, (tl.BoundsUTM[1]+tl.BoundsUTM[3])/2
	ll := gdalTransform(t, "EPSG:"+itoa(m.CRS.EPSG), "EPSG:4326", e, n)
	z96 := gdalTransformZ(t, "EPSG:4326+5773", "EPSG:4979", ll[0], ll[1], -50)
	z08 := gdalTransformZ(t, "EPSG:4326+3855", "EPSG:4979", ll[0], ll[1], -50)
	want := -50 + (z96 - z08) // value in EGM2008 = ellipsoidal(from EGM96 value) - N2008 ... expressed as a height shift
	got := float64(h[(S/2)*S+S/2])
	if math.Abs(got-want) > 0.02 {
		t.Fatalf("EGM96 -50 m became %.4f m, expected %.4f from PROJ", got, want)
	}
	if got == -50 {
		t.Fatal("heights were not shifted: a silent no-op")
	}
}

// Tile bytes, manifest and dataset layer do not depend on how the grid is windowed.
func TestWindowSizeDoesNotChangeOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("slow (seconds to tens of seconds): runs in the integration tier, i.e. without -short")
	}
	cfg, _ := twoSourceCfg(t, []string{"fine", "coarse"}, "")
	cfg.LODLevels = 2
	var ref map[string][]byte
	for _, wt := range []int{0, 1, 2, 3} {
		out := filepath.Join(t.TempDir(), "w")
		m, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: out, WindowTiles: wt})
		if err != nil {
			t.Fatal(err)
		}
		files := readAllWithDataset(t, out, m)
		if ref == nil {
			ref = files
			continue
		}
		for k, v := range ref {
			if !bytes.Equal(v, files[k]) {
				t.Fatalf("window %d tiles: %s differs", wt, k)
			}
		}
	}
}

func readAllWithDataset(t *testing.T, dir string, m *world.Manifest) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, tl := range m.Tiles {
		for _, f := range []string{tl.Height, tl.Depth, tl.Shore, tl.Source, tl.Dataset} {
			b, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				t.Fatal(err)
			}
			files[f] = b
		}
	}
	return files
}

func readHeights(t *testing.T, dir, rel string) []float32 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24)
	}
	return out
}

func readDatasetLayer(t *testing.T, dir, rel string) []uint8 {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r := img.Bounds()
	out := make([]uint8, 0, r.Dx()*r.Dy())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			v, _, _, _ := img.At(x, y).RGBA()
			out = append(out, uint8(v>>8))
		}
	}
	return out
}

func gdalTransform(t *testing.T, from, to string, x, y float64) [2]float64 {
	z := gdalTransformFull(t, from, to, x, y, 0)
	return [2]float64{z[0], z[1]}
}

func gdalTransformZ(t *testing.T, from, to string, lon, lat, z float64) float64 {
	return gdalTransformFull(t, from, to, lon, lat, z)[2]
}

func gdalTransformFull(t *testing.T, from, to string, x, y, z float64) [3]float64 {
	t.Helper()
	cmd := exec.Command("gdaltransform", "-s_srs", from, "-t_srs", to)
	cmd.Env = append(os.Environ(), "OSR_AXIS_MAPPING_STRATEGY=TRADITIONAL_GIS_ORDER", "PROJ_NETWORK=OFF")
	cmd.Stdin = strings.NewReader(fmtF(x) + " " + fmtF(y) + " " + fmtF(z) + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var r [3]float64
	f := strings.Fields(string(out))
	if len(f) < 3 {
		t.Fatalf("gdaltransform output %q", out)
	}
	for i := range r {
		r[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return r
}

// EGM96 undulation, checked against references that do not come from the installed grid file (investigation of
// 2026-11-02, docs/research/egm96-discrepancy.md):
//   - NGA's own EGM96 test file (five points, as reproduced in Orekit's GeoidTest): the 15' grid is bilinear-interpolated
//     by PROJ, so it agrees to a few centimetres (largest observed 0.056 m); the tolerance is 0.10 m.
//   - Spherical-harmonic synthesis from the EGM96 coefficients (GeographicLib `Gravity -n egm96 -H`) at two grid nodes,
//     where interpolation plays no role: 36.0213 m at 12.5E 55.5N and 21.1533 m at 180E 0N; tolerance 2 mm.
//
// PROJ's own gie file lists 36.394 and 20.757 for the same two points; the installed grid, GeographicLib's grid and the
// coefficient synthesis all disagree with those by 0.37 and 0.40 m, so they are not EGM96 undulations of these points
// and are deliberately not asserted.
func TestEGM96IndependentReferences(t *testing.T) {
	for _, p := range []struct{ lon, lat, n, tol float64 }{
		{12.5, 55.5, 36.0213, 0.002}, {180, 0, 21.1533, 0.002}, // coefficient synthesis, grid nodes
		{269.7791550 - 360, 38.6281550, -31.628, 0.10}, {305.0211140 - 360, -14.6212170, -2.969, 0.10}, // NGA test file
		{102.4487290, 46.8743190, -43.575, 0.10}, {133.8747120, -23.6174460, 15.871, 0.10}, {359.9995000 - 360, 38.6254730, 50.066, 0.10},
	} {
		got := gdalTransformZ(t, "EPSG:4326+5773", "EPSG:4979", p.lon, p.lat, 0)
		if math.Abs(got-p.n) > p.tol {
			t.Errorf("EGM96 N at %g,%g = %.4f, reference %.4f (tolerance %.3f)", p.lon, p.lat, got, p.n, p.tol)
		}
	}
}

// The validator accepts a multi-source world, recomputes the per-dataset counts from the dataset layers, and
// catches a tampered or inconsistent layer.
func TestValidatorChecksDatasetLayer(t *testing.T) {
	cfg, _ := twoSourceCfg(t, []string{"fine", "coarse"}, "")
	m, out := buildIt(t, cfg)
	rep, err := validate.Run(out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result == "FAILED" {
		t.Fatalf("clean world has errors:\n%s", rep.Format())
	}
	tl := m.TilesAt(0)[0]
	p := filepath.Join(out, tl.Dataset)
	b, _ := os.ReadFile(p)
	b[len(b)/2] ^= 0xff
	os.WriteFile(p, b, 0o644)
	rep, err = validate.Run(out)
	if err != nil || rep.Result != "FAILED" {
		t.Fatalf("tampered dataset layer must be an error: %v %+v", err, rep)
	}
}

// A world across the antimeridian builds from a local source with the default projection (a 0.2 degree box fits UTM
// zone 1, schema 2) and with a forced local transverse Mercator (schema 3); both validate, no tile envelope spans 360
// degrees, and the reader answers on both sides of 180.
func TestAntimeridianBuild(t *testing.T) {
	for _, proj := range []string{"", "tmerc"} {
		t.Run("projection="+proj, func(t *testing.T) {
			dir := t.TempDir()
			mkTif(t, filepath.Join(dir, "a.tif"), 179.88, -16.12, 180.12, -15.88, 120, 120, -37, "")
			cfg := &world.Config{Name: "am", BBox: [4]float64{179.9, -16.1, 180.1, -15.9}, CellM: 100, TileCells: 32, Coastline: "none", Projection: proj,
				Sources: []world.SourceSpec{{ID: "a", Type: "geotiff", Path: filepath.Join(dir, "a.tif"), Quality: "survey", License: "test", Attribution: "synthetic", Vertical: &world.VerticalRef{Datum: "EGM2008"}}}}
			m, out := buildIt(t, cfg)
			if proj == "tmerc" && (m.SchemaVersion != world.SchemaVersionTM || m.CRS.Kind != "tmerc" || m.CRS.EPSG != 0 || m.CRS.Lon0 != 180 || m.CRS.Proj4 == "") {
				t.Fatalf("manifest crs %+v schema %d", m.CRS, m.SchemaVersion)
			}
			if proj == "" && (m.SchemaVersion != world.SchemaVersion || m.CRS.EPSG != 32701 && m.CRS.EPSG != 32760) {
				t.Fatalf("default projection: crs %+v schema %d", m.CRS, m.SchemaVersion)
			}
			for _, tl := range m.Tiles {
				if tl.BoundsWGS[2]-tl.BoundsWGS[0] > 1 || tl.BoundsWGS[0] < 179 || tl.BoundsWGS[2] > 181 {
					t.Fatalf("tile %s envelope %v", tl.ID, tl.BoundsWGS)
				}
			}
			if g := m.Grid.BBoxWGS84; g[2]-g[0] > 1 || g[0] < 179 || g[2] > 181 {
				t.Fatalf("world bbox %v", g)
			}
			rep, err := validate.Run(out)
			if err != nil || rep.Result == "FAILED" {
				t.Fatalf("validate: %v\n%s", err, rep.Format())
			}
			w, err := oceanworld.Open(out)
			if err != nil {
				t.Fatal(err)
			}
			for _, lon := range []float64{179.95, 180.05, -179.95} {
				s, err := w.SampleLonLat(lon, -16, 0)
				if err != nil || !s.Valid || math.Abs(s.ElevationM+37) > 0.05 || s.Dataset != "a" {
					t.Errorf("lon %v: %+v %v", lon, s, err)
				}
			}
			if s, _ := w.SampleLonLat(179.5, -16, 0); s.Valid {
				t.Errorf("outside the world must be invalid: %+v", s)
			}
		})
	}
}

// Configs across 180 that need a request the tool cannot split (OSM coastline, CUDEM, EMODnet, remote geotiff) are
// refused before anything is fetched, with no cache directory touched. GMRT, ETOPO and GEBCO are split in two boxes.
func TestAntimeridianNetworkConfigsRefusedEarly(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	for name, c := range map[string]*world.Config{
		"default gmrt": {Name: "x", BBox: [4]float64{179.5, -17, 180.5, -16}, CellM: 500},
		"cudem":        {Name: "x", BBox: [4]float64{179.5, -17, 180.5, -16}, CellM: 500, Coastline: "none", Sources: []world.SourceSpec{{ID: "c", Type: "cudem"}}},
		"seamarks":     {Name: "x", BBox: [4]float64{179.5, -17, 180.5, -16}, CellM: 500, Coastline: "none", Seamarks: true},
		"remote geotiff": {Name: "x", BBox: [4]float64{179.5, -17, 180.5, -16}, CellM: 500, Coastline: "none",
			Sources: []world.SourceSpec{{ID: "r", Type: "geotiff", Path: "https://example.invalid/a.tif", License: "x", Attribution: "x", Vertical: &world.VerticalRef{Datum: "EGM2008"}}}},
	} {
		_, err := world.Build(c, world.Options{CacheDir: cache, OutDir: filepath.Join(t.TempDir(), "w")})
		if err == nil || !strings.Contains(err.Error(), "antimeridian") {
			t.Errorf("%s: want antimeridian refusal, got %v", name, err)
		}
	}
	if _, err := os.Stat(cache); err == nil {
		t.Error("refusal must happen before the cache is created")
	}
}

// Adapters that were never checked with schema 3 refuse it with an explanation.
func TestSchema3RefusedByUncheckedAdapters(t *testing.T) {
	dir := t.TempDir()
	mkTif(t, filepath.Join(dir, "a.tif"), 179.88, -16.12, 180.12, -15.88, 60, 60, -37, "")
	cfg := &world.Config{Name: "tm", BBox: [4]float64{179.9, -16.1, 180.1, -15.9}, CellM: 200, TileCells: 32, Coastline: "none", Projection: "tmerc",
		Sources: []world.SourceSpec{{ID: "a", Type: "geotiff", Path: filepath.Join(dir, "a.tif"), Quality: "survey", License: "test", Attribution: "synthetic", Vertical: &world.VerticalRef{Datum: "EGM2008"}}}}
	_, out := buildIt(t, cfg)
	proj := filepath.Join(t.TempDir(), "proj")
	os.MkdirAll(filepath.Join(proj, "Assets"), 0o755)
	if _, err := export.Unity(out, proj, 0); err == nil || !strings.Contains(err.Error(), "local transverse Mercator") {
		t.Errorf("unity: %v", err)
	}
	if _, err := libreterrain.Export(out, filepath.Join(t.TempDir(), "x"), libreterrain.ExportOptions{}); err == nil || !strings.Contains(err.Error(), "local transverse Mercator") {
		t.Errorf("libreterrain: %v", err)
	}
	// the GeoTIFF export does work: pixel centres sit on the grid and the SRS is the PROJ string
	files, err := export.GeoTIFF(out, filepath.Join(t.TempDir(), "tif"), 0)
	if err != nil || len(files) == 0 {
		t.Fatalf("geotiff: %v", err)
	}
	info, _ := exec.Command("gdalinfo", files[0]).CombinedOutput()
	if !strings.Contains(string(info), "Transverse_Mercator") && !strings.Contains(string(info), "Transverse Mercator") {
		t.Errorf("exported CRS is not transverse Mercator:\n%s", info)
	}
}

// grid.bbox_wgs84 is the level-0 extent (coarse LOD tiles overhang the grid), and the validator rejects a manifest
// that says otherwise.
func TestBBoxWGS84IsLevelZeroExtent(t *testing.T) {
	cfg, _ := twoSourceCfg(t, []string{"fine", "coarse"}, "")
	m, out := buildIt(t, cfg)
	var lo, hi [2]float64
	lo, hi = [2]float64{1e9, 1e9}, [2]float64{-1e9, -1e9}
	for _, tl := range m.TilesAt(0) {
		lo = [2]float64{math.Min(lo[0], tl.BoundsWGS[0]), math.Min(lo[1], tl.BoundsWGS[1])}
		hi = [2]float64{math.Max(hi[0], tl.BoundsWGS[2]), math.Max(hi[1], tl.BoundsWGS[3])}
	}
	if b := m.Grid.BBoxWGS84; b != [4]float64{lo[0], lo[1], hi[0], hi[1]} {
		t.Fatalf("bbox_wgs84 %v, level-0 extent %v %v", b, lo, hi)
	}
	mp := filepath.Join(out, "world.manifest.json")
	raw, _ := os.ReadFile(mp)
	var j map[string]any
	if err := json.Unmarshal(raw, &j); err != nil {
		t.Fatal(err)
	}
	g := j["grid"].(map[string]any)
	b := g["bbox_wgs84"].([]any)
	b[0] = b[0].(float64) - 0.5
	raw, _ = json.MarshalIndent(j, "", "  ")
	os.WriteFile(mp, raw, 0o644)
	if rep, err := validate.Run(out); err != nil || rep.Result != "FAILED" {
		t.Fatalf("wrong bbox_wgs84 must fail validation: %v", err)
	}
}
