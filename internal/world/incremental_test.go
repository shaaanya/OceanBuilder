package world_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaaanya/OceanBuilder/internal/gdal"
	"github.com/shaaanya/OceanBuilder/internal/world"
)

func cfgFor(bbox [4]float64) *world.Config {
	fx := "../../testdata/falmouth/"
	return &world.Config{Name: "inc", BBox: bbox, CellM: 100, TileCells: 16, Resampling: "average",
		Local: &world.Local{Topo: fx + "topo.tif", Mask: fx + "mask.tif", Coastline: fx + "coast.geojson",
			License: "test fixture", Attribution: "GMRT; OSM"}}
}

var box = [4]float64{-5.12, 50.155, -5.085, 50.185}

func readAll(t *testing.T, dir string, m *world.Manifest) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	files := []string{world.ManifestFile, "ATTRIBUTION.txt"}
	for _, tl := range m.Tiles {
		files = append(files, tl.Height, tl.Depth, tl.Shore, tl.Source)
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		out[f] = b
	}
	return out
}

func sameFiles(t *testing.T, a, b map[string][]byte) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("%d files vs %d", len(a), len(b))
	}
	for f, x := range a {
		if !bytes.Equal(x, b[f]) {
			t.Fatalf("%s differs", f)
		}
	}
}

// An unchanged rebuild is skipped (output untouched); -force and a clean cache rebuild to the same bytes.
func TestUpToDateSkipAndDeterministicRebuild(t *testing.T) {
	cache, out := t.TempDir(), filepath.Join(t.TempDir(), "w")
	var logs []string
	opt := world.Options{CacheDir: cache, OutDir: out, Log: func(f string, a ...any) { logs = append(logs, f) }}
	m1, err := world.Build(cfgFor(box), opt)
	if err != nil {
		t.Fatal(err)
	}
	first := readAll(t, out, m1)
	marker := filepath.Join(out, "tiles", "0", "marker")
	os.WriteFile(marker, []byte("x"), 0o644) // survives only if the output directory is left untouched

	logs = nil
	m2, err := world.Build(cfgFor(box), opt)
	if err != nil {
		t.Fatal(err)
	}
	if m2.BuildID != m1.BuildID || !strings.Contains(strings.Join(logs, "|"), "up to date") {
		t.Fatalf("second build was not skipped: %v", logs)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("up-to-date build must not touch the output")
	}

	opt.Force = true
	if _, err := world.Build(cfgFor(box), opt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("forced build must replace the output directory")
	}
	sameFiles(t, first, readAll(t, out, m1))

	// Clean cache and fresh output: bytes identical to the first build (reproducibility).
	opt2 := world.Options{CacheDir: t.TempDir(), OutDir: filepath.Join(t.TempDir(), "w")}
	m3, err := world.Build(cfgFor(box), opt2)
	if err != nil {
		t.Fatal(err)
	}
	sameFiles(t, first, readAll(t, opt2.OutDir, m3))
}

// A config change that does not move the grid reuses the cached warps; the result equals a build from a clean cache.
func TestStageCacheReuseGivesSameBytes(t *testing.T) {
	cache := t.TempDir()
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, f) }
	outA := filepath.Join(t.TempDir(), "a")
	ma, err := world.Build(cfgFor(box), world.Options{CacheDir: cache, OutDir: outA, Log: logf})
	if err != nil {
		t.Fatal(err)
	}
	changed := cfgFor(box)
	changed.DepthBands = []float64{3, 10, 40}
	logs = nil
	outB := filepath.Join(t.TempDir(), "b")
	mb, err := world.Build(changed, world.Options{CacheDir: cache, OutDir: outB, Log: logf})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(logs, "|"), "stage cache hit") {
		t.Fatalf("expected a stage cache hit, got %v", logs)
	}
	outC := filepath.Join(t.TempDir(), "c")
	mc, err := world.Build(changed, world.Options{CacheDir: t.TempDir(), OutDir: outC})
	if err != nil {
		t.Fatal(err)
	}
	sameFiles(t, readAll(t, outB, mb), readAll(t, outC, mc))
	if mb.InputKey == "" || mb.InputKey == ma.InputKey {
		t.Fatal("a config change must change the input key")
	}
}

// A cancelled build leaves nothing behind and does not touch an existing good output.
func TestCancelledBuildLeavesOutputUntouched(t *testing.T) {
	cache, out := t.TempDir(), filepath.Join(t.TempDir(), "w")
	if _, err := world.Build(cfgFor(box), world.Options{CacheDir: cache, OutDir: out}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(out, world.ManifestFile))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	old := gdal.Ctx
	gdal.Ctx = ctx
	defer func() { gdal.Ctx = old }()
	other := cfgFor(box)
	other.CellM = 80 // new grid: cannot be served from cache, must run gdalwarp
	if _, err := world.Build(other, world.Options{CacheDir: cache, OutDir: out}); err == nil {
		t.Fatal("cancelled build must fail")
	}
	after, _ := os.ReadFile(filepath.Join(out, world.ManifestFile))
	if !bytes.Equal(before, after) {
		t.Fatal("existing output was modified by a cancelled build")
	}
	ents, _ := os.ReadDir(filepath.Dir(out))
	for _, e := range ents {
		if strings.Contains(e.Name(), ".building-") {
			t.Fatalf("staging directory %s left behind", e.Name())
		}
	}
}

// A bbox spanning several UTM zones is refused with advice instead of being silently distorted when UTM is demanded,
// and when even a local transverse Mercator cannot hold it. (0.2.0 refused the 20 degree box in any case; 0.3.0 plans it
// as a local grid, see ADR 0006.)
func TestWideBBoxRefusedAndNarrowOneReportsScale(t *testing.T) {
	wide := cfgFor([4]float64{-20, 50.15, 0, 50.185}) // 20 degrees wide: the far edge is 11 degrees off the central meridian
	wide.Projection = "utm"
	_, err := world.Build(wide, world.Options{CacheDir: t.TempDir(), OutDir: filepath.Join(t.TempDir(), "w")})
	if err == nil || !strings.Contains(err.Error(), "scale distortion") {
		t.Fatalf("want a distortion refusal, got %v", err)
	}
	tooWide := cfgFor([4]float64{-60, 50.15, 60, 50.185})
	if _, err = world.Build(tooWide, world.Options{CacheDir: t.TempDir(), OutDir: filepath.Join(t.TempDir(), "w")}); err == nil || !strings.Contains(err.Error(), "scale distortion") {
		t.Fatalf("want a distortion refusal for a 120 degree box, got %v", err)
	}
	m, err := world.Build(cfgFor(box), world.Options{CacheDir: t.TempDir(), OutDir: filepath.Join(t.TempDir(), "w")})
	if err != nil {
		t.Fatal(err)
	}
	if k := m.Stats.UTMScaleMin; k < 0.9995 || k > 1.0 || m.Stats.UTMScaleMax < k {
		t.Fatalf("scale factors %v..%v", m.Stats.UTMScaleMin, m.Stats.UTMScaleMax)
	}
}
