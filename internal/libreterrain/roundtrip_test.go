package libreterrain

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaaanya/OceanBuilder/internal/gdal"
	"github.com/shaaanya/OceanBuilder/internal/validate"
	"github.com/shaaanya/OceanBuilder/internal/world"
)

func fixtureCfg(bbox [4]float64) *world.Config {
	fx := "../../testdata/falmouth/"
	return &world.Config{Name: "fixture", BBox: bbox, CellM: 100, TileCells: 16, Resampling: "average",
		Local: &world.Local{Topo: fx + "topo.tif", Mask: fx + "mask.tif", Coastline: fx + "coast.geojson",
			License: "CC BY 4.0 / ODbL (test fixture)", Attribution: "GMRT; © OpenStreetMap contributors"}}
}

// A wide bbox so the world is not square and export has to pad.
var wideBBox = [4]float64{-5.135, 50.16, -5.06, 50.18}

func buildWorld(t *testing.T, cfg *world.Config) (string, *world.Manifest) {
	t.Helper()
	dir := t.TempDir()
	m, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return dir, m
}

func sameTiles(t *testing.T, a, b string, m *world.Manifest) (differing int) {
	t.Helper()
	for _, tl := range m.TilesAt(0) {
		for _, f := range []string{tl.Height, tl.Depth, tl.Shore} {
			x, _ := os.ReadFile(filepath.Join(a, f))
			y, _ := os.ReadFile(filepath.Join(b, f))
			if len(x) == 0 || !bytes.Equal(x, y) {
				differing++
			}
		}
	}
	return
}

// fakeLTOutput writes what LibreTerrain would write: build.json (same field names as the real
// file) plus an EXR of the given raster.
func fakeLTOutput(t *testing.T, x *Exchange, data []float32, mut func(*LTBuild)) string {
	t.Helper()
	dir := t.TempDir()
	n := x.Grid.Side
	if err := writeEXR(filepath.Join(dir, "out.exr"), data, n); err != nil {
		t.Fatal(err)
	}
	b := LTBuild{Generator: "LibreTerrain", AppVersion: "test", WorldSizeM: x.LT.WorldSizeM, HeightRangeM: x.LT.HeightRangeM,
		Resolution: [2]int{n, n}, CellSizeM: [2]float64{x.Grid.CellM, x.Grid.CellM}, Orientation: LTOrientation,
		Files: []LTFile{{File: "out.exr", Node: "n_0001", Port: "out", Data: "heightfield", Format: "exr32", Encoding: "metres"}}}
	if mut != nil {
		mut(&b)
	}
	jb, _ := json.Marshal(b)
	if err := os.WriteFile(filepath.Join(dir, "build.json"), jb, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exportFixture(t *testing.T) (string, *world.Manifest, string, *Exchange, []float32) {
	return exportFixtureCfg(t, fixtureCfg(wideBBox))
}

// requireEXR skips the test when GDAL was built without OpenEXR (the conda-forge packages are).
func requireEXR(t *testing.T) {
	t.Helper()
	out, err := gdal.Run("gdalinfo", "--formats")
	if err != nil || !strings.Contains(out, "EXR") {
		t.Skip("GDAL has no EXR driver")
	}
}

func exportFixtureCfg(t *testing.T, cfg *world.Config) (string, *world.Manifest, string, *Exchange, []float32) {
	requireEXR(t)
	wdir, m := buildWorld(t, cfg)
	xdir := filepath.Join(t.TempDir(), "x")
	x, err := Export(wdir, xdir, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	base, w, h, err := readRaster(filepath.Join(xdir, HeightmapEXR))
	if err != nil || w != x.Grid.Side || h != x.Grid.Side {
		t.Fatalf("baseline read: %v %dx%d", err, w, h)
	}
	return wdir, m, xdir, x, base
}

func TestExportPadsToSquareAndMatchesTiles(t *testing.T) {
	wdir, m, _, x, base := exportFixture(t)
	if x.Grid.Width == x.Grid.Height || x.Grid.PadCols+x.Grid.PadRows == 0 {
		t.Fatalf("fixture should be non-square, got %dx%d", x.Grid.Width, x.Grid.Height)
	}
	n := x.Grid.Side
	// Every real sample equals the tile sample (to float32) and padding is the minimum.
	for _, tl := range m.TilesAt(0) {
		hs, err := validate.ReadHeights(filepath.Join(wdir, tl.Height), m)
		if err != nil {
			t.Fatal(err)
		}
		S := m.Grid.SamplesPerTile
		for i := 0; i < S; i++ {
			for j := 0; j < S; j++ {
				r, c := tl.Row*m.Grid.TileCells+i, tl.Col*m.Grid.TileCells+j
				if d := math.Abs(float64(base[r*n+c]) - hs[i*S+j]); d > 1e-4 && tl.NoData == 0 {
					t.Fatalf("tile %s (%d,%d): raster %.4f vs tile %.4f", tl.ID, i, j, base[r*n+c], hs[i*S+j])
				}
			}
		}
	}
	// Padding repeats the edge: the corner pad cell equals the last real cell, not the world minimum.
	if got, want := base[(n-1)*n+n-1], base[(x.Grid.Height-1)*n+x.Grid.Width-1]; got != want {
		t.Fatalf("padding corner = %v, want last real sample %v", got, want)
	}
	if x.Grid.PadCols > 0 && base[5*n+n-1] != base[5*n+x.Grid.Width-1] {
		t.Fatal("padded columns must repeat the last real column")
	}
	if len(x.Checkpoints) < 4 {
		t.Fatal("checkpoints missing from exchange")
	}
}

func TestUnmodifiedRoundTripIsByteIdentical(t *testing.T) {
	wdir, m, xdir, x, base := exportFixture(t)
	out := filepath.Join(t.TempDir(), "ref")
	rep, err := Import(xdir, fakeLTOutput(t, x, base, nil), out, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ChangedCells != 0 || rep.MaxAbsM != 0 {
		t.Fatalf("unmodified round trip changed cells: %+v", rep)
	}
	cfg, err := world.LoadConfig(filepath.Join(out, "fixture-refined.world.json"))
	if err != nil {
		t.Fatal(err)
	}
	rdir, rm := buildWorld(t, cfg)
	if n := sameTiles(t, wdir, rdir, m); n != 0 {
		t.Fatalf("%d tile files differ after an unmodified round trip", n)
	}
	if rm.Refinement == nil || rm.Refinement.CellsChanged != 0 || rm.Refinement.BaseBuildID != m.BuildID {
		t.Fatalf("refinement stats wrong: %+v", rm.Refinement)
	}
	if rm.BuildID == m.BuildID {
		t.Fatal("build id must change when an edit layer is present")
	}
	if len(rm.Sources) != len(m.Sources)+1 || !strings.HasPrefix(rm.Sources[len(rm.Sources)-1].Source, "refinement:") {
		t.Fatal("edit layer missing from provenance")
	}
	r, err := validate.Run(rdir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if f.Level == validate.Error {
			t.Fatalf("validator error on refined world: %s %s", f.Code, f.Message)
		}
	}
}

func TestEditsChangeLandOnlyAndKeepBathymetry(t *testing.T) {
	wdir, m, xdir, x, base := exportFixture(t)
	edited := append([]float32(nil), base...)
	for i := range edited {
		edited[i] += 25 // raise everything, including the sea floor
	}
	out := filepath.Join(t.TempDir(), "ref")
	rep, err := Import(xdir, fakeLTOutput(t, x, edited, nil), out, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rep.MaxAbsM-25) > 1e-3 || rep.ChangedCells != x.Grid.Width*x.Grid.Height {
		t.Fatalf("report %+v", rep)
	}
	cfg, _ := world.LoadConfig(filepath.Join(out, "fixture-refined.world.json"))
	rdir, rm := buildWorld(t, cfg)
	if rm.Refinement.CellsChanged == 0 || rm.Refinement.IgnoredWaterCells == 0 {
		t.Fatalf("want land edited and water edits ignored: %+v", rm.Refinement)
	}
	// Sea floor is unchanged in metres (the tile encoding range may differ, so compare decoded metres).
	for _, tl := range m.TilesAt(0) {
		a, _ := validate.ReadHeights(filepath.Join(wdir, tl.Height), m)
		cls, _ := readGrayPNG(filepath.Join(wdir, tl.Depth), m.Grid.SamplesPerTile)
		b, _ := validate.ReadHeights(filepath.Join(rdir, tl.Height), rm)
		for i := range a {
			if cls[i] != 0 && cls[i] != world.ClassNoData && math.Abs(a[i]-b[i]) > 0.02 {
				t.Fatalf("tile %s sample %d: sea floor %.3f -> %.3f", tl.ID, i, a[i], b[i])
			}
		}
	}
	if rm.Stats.ShoreCells != m.Stats.ShoreCells || rm.Stats.WaterFraction != m.Stats.WaterFraction {
		t.Fatal("coastline (water/shore cells) moved by a land edit")
	}
	if rm.Refinement.MaxAbsChangeM > 25.001 {
		t.Fatalf("land moved by %.3f m, more than the 25 m edit", rm.Refinement.MaxAbsChangeM)
	}
}

func TestImportRefusesBadInput(t *testing.T) {
	if testing.Short() {
		t.Skip("slow (seconds to tens of seconds): runs in the integration tier, i.e. without -short")
	}
	_, _, xdir, x, base := exportFixture(t)
	n := x.Grid.Side
	flip := func(f func(r, c int) (int, int)) []float32 {
		o := make([]float32, len(base))
		for r := 0; r < n; r++ {
			for c := 0; c < n; c++ {
				rr, cc := f(r, c)
				o[r*n+c] = base[rr*n+cc]
			}
		}
		return o
	}
	cases := map[string]struct {
		data []float32
		mut  func(*LTBuild)
		want string
	}{
		"north-south flip": {flip(func(r, c int) (int, int) { return n - 1 - r, c }), nil, "mirrored north-south"},
		"east-west flip":   {flip(func(r, c int) (int, int) { return r, n - 1 - c }), nil, "mirrored east-west"},
		"transposed":       {flip(func(r, c int) (int, int) { return c, r }), nil, "transposed"},
		"resampled":        {base, func(b *LTBuild) { b.Resolution = [2]int{n - 1, n - 1} }, "resampled"},
		"world size":       {base, func(b *LTBuild) { b.WorldSizeM[0] *= 2 }, "world size"},
		"anisotropic":      {base, func(b *LTBuild) { b.CellSizeM[1] = 24 }, "cell size"},
		"height range":     {base, func(b *LTBuild) { b.HeightRangeM[1] += 100 }, "height range"},
		"orientation text": {base, func(b *LTBuild) { b.Orientation = "row 0 = world Y max" }, "orientation statement"},
		"not LibreTerrain": {base, func(b *LTBuild) { b.Generator = "x" }, "generator"},
		"ambiguous files": {base, func(b *LTBuild) {
			b.Files = append(b.Files, b.Files[0])
			b.Files[1].File = "other.exr"
		}, "-file"},
	}
	for name, c := range cases {
		out := filepath.Join(t.TempDir(), "ref")
		_, err := Import(xdir, fakeLTOutput(t, x, c.data, c.mut), out, ImportOptions{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, c.want)
		}
		if _, e := os.Stat(out); e == nil {
			t.Errorf("%s: output folder created despite refusal", name)
		}
	}
	// Tampered baseline.
	b, _ := os.ReadFile(filepath.Join(xdir, HeightmapEXR))
	b[len(b)/2] ^= 0xff
	os.WriteFile(filepath.Join(xdir, HeightmapEXR), b, 0o644)
	if _, err := Import(xdir, fakeLTOutput(t, x, base, nil), filepath.Join(t.TempDir(), "r"), ImportOptions{}); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("tampered baseline: %v", err)
	}
}

func TestImportNeverWritesInsideInputs(t *testing.T) {
	_, _, xdir, x, base := exportFixture(t)
	lt := fakeLTOutput(t, x, base, nil)
	for _, out := range []string{filepath.Join(xdir, "sub"), filepath.Join(lt, "sub"), xdir} {
		if _, err := Import(xdir, lt, out, ImportOptions{}); err == nil {
			t.Errorf("import into %s should be refused", out)
		}
	}
}

func TestPNG16Quantisation(t *testing.T) {
	lo, hi := -100.0, 300.0
	step := (hi - lo) / 65535
	dir := t.TempDir()
	data := []float32{-100, 0, 100, 300}
	if err := writePNG16(filepath.Join(dir, "a.png"), data, 2, lo, hi); err != nil {
		t.Fatal(err)
	}
	got, w, h, err := readPNG16(filepath.Join(dir, "a.png"), lo, hi)
	if err != nil || w != 2 || h != 2 {
		t.Fatal(err)
	}
	for i := range data {
		if math.Abs(float64(got[i]-data[i])) > step/2+1e-6 {
			t.Fatalf("png16 quantisation error %.5f > half step %.5f", got[i]-data[i], step/2)
		}
	}
}

// TestRealLibreTerrain drives the real application headlessly. It is skipped when libreterrain-mcp is absent.
func TestRealLibreTerrain(t *testing.T) {
	bridge := FindBridge("")
	if bridge == "" {
		t.Skip("libreterrain-mcp not found (set LIBRETERRAIN_MCP)")
	}
	_, m := buildWorld(t, fixtureCfg(wideBBox))
	wdir := t.TempDir()
	if _, err := world.Build(fixtureCfg(wideBBox), world.Options{CacheDir: t.TempDir(), OutDir: wdir}); err != nil {
		t.Fatal(err)
	}
	xdir := filepath.Join(t.TempDir(), "x")
	x, err := Export(wdir, xdir, ExportOptions{Bridge: bridge})
	if err != nil {
		t.Fatal(err)
	}
	if x.Files.Project == "" {
		t.Fatal("no project written")
	}
	c, err := Open(bridge, filepath.Join(xdir, x.Files.Project))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Build(x.LT.BuildResolution, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	c.Close()
	ltdir := filepath.Join(xdir, "lt-output")
	// 1. Unmodified: exact.
	rep, err := Import(xdir, ltdir, filepath.Join(t.TempDir(), "r0"), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.MaxAbsM != 0 {
		t.Fatalf("real LibreTerrain EXR32 round trip is not exact: max |diff| %g m", rep.MaxAbsM)
	}
	t.Logf("unmodified EXR32 round trip (non-square world padded to %d²): cells=%d max=%g mean=%g, checkpoints exact", x.Grid.Side, rep.Cells, rep.MaxAbsM, rep.MeanAbsM)

	// 2. Edited in LibreTerrain: blur the terrain (a real edit) and export only the blurred output.
	c, err = Open(bridge, filepath.Join(xdir, x.Files.Project))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	g, err := c.Tool("get_graph", nil)
	if err != nil {
		t.Fatal(err)
	}
	src := g["nodes"].([]any)[0].(map[string]any)["id"].(string)
	refs, err := c.Edit(map[string]any{"op": "add_node", "type": "adjust.blur", "ref": "b", "settings": map[string]any{"radius_m": 400.0}},
		map[string]any{"op": "connect", "from": src, "to": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.MarkExport(refs["b"], "exr32"); err != nil {
		t.Fatal(err)
	}
	rev, err := c.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tool("mark_export", map[string]any{"revision": rev, "node": src, "on": false}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(ltdir); err != nil { // clear the previous build's files so only the blurred output exists
		t.Fatal(err)
	}
	if _, err := c.Build(x.LT.BuildResolution, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "r1")
	rep, err = Import(xdir, ltdir, out, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ChangedCells == 0 {
		t.Fatalf("blur changed nothing: %+v", rep)
	}
	t.Logf("blurred: changed=%d/%d max=%.2f m mean=%.3f m", rep.ChangedCells, rep.Cells, rep.MaxAbsM, rep.MeanAbsM)
	cfg, _ := world.LoadConfig(filepath.Join(out, "fixture-refined.world.json"))
	_, rm := buildWorld(t, cfg)
	if rm.Stats.ShoreCells != m.Stats.ShoreCells {
		t.Fatalf("shore cells %d -> %d after blur", m.Stats.ShoreCells, rm.Stats.ShoreCells)
	}
	t.Logf("refinement applied: %+v", *rm.Refinement)
}

// With coast_conform on, the exported baseline is the conformed DEM and the config travels with the
// exchange, so an unedited round trip must still rebuild byte-identical tiles (conforming is deterministic
// and the refinement sees the final land/water split).
func TestUnmodifiedRoundTripWithCoastConformIsByteIdentical(t *testing.T) {
	cfg := fixtureCfg(wideBBox)
	cfg.CoastConform = "osm"
	wdir, m, xdir, x, base := exportFixtureCfg(t, cfg)
	if m.Stats.CoastConform == nil || m.Stats.CoastConform.ToLand+m.Stats.CoastConform.ToWater == 0 {
		t.Fatalf("fixture must actually conform some cells, else this test proves nothing: %+v", m.Stats.CoastConform)
	}
	out := filepath.Join(t.TempDir(), "ref")
	if _, err := Import(xdir, fakeLTOutput(t, x, base, nil), out, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	rcfg, err := world.LoadConfig(filepath.Join(out, "fixture-refined.world.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rcfg.CoastConform != "osm" {
		t.Fatal("coast_conform must travel into the refined config")
	}
	rdir, rm := buildWorld(t, rcfg)
	if n := sameTiles(t, wdir, rdir, m); n != 0 {
		t.Fatalf("%d tile files differ after an unmodified round trip with coast_conform", n)
	}
	if rm.Stats.CoastConform == nil || rm.Stats.CoastConform.ToLand != m.Stats.CoastConform.ToLand || rm.Stats.CoastConform.ToWater != m.Stats.CoastConform.ToWater {
		t.Fatal("conform must make the same decisions on the rebuild")
	}
}

// The delta belongs to one baseline. A refinement whose base_build_id does not match what the world builds as
// today (here: the id is altered, as if sources or tool version had changed) must be refused, not applied.
func TestRefinementRefusedWhenBaselineChanged(t *testing.T) {
	_, _, xdir, x, base := exportFixture(t)
	out := filepath.Join(t.TempDir(), "ref")
	if _, err := Import(xdir, fakeLTOutput(t, x, base, nil), out, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	rp := filepath.Join(out, "refinement.json")
	b, err := os.ReadFile(rp)
	if err != nil {
		t.Fatal(err)
	}
	var ref map[string]any
	if err := json.Unmarshal(b, &ref); err != nil {
		t.Fatal(err)
	}
	ref["base_build_id"] = strings.Repeat("0", 64)
	b, _ = json.Marshal(ref)
	if err := os.WriteFile(rp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := world.LoadConfig(filepath.Join(out, "fixture-refined.world.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "export again") {
		t.Fatalf("a refinement over a changed baseline must be refused, got %v", err)
	}
}
