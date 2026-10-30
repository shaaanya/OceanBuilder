package validate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ocean-compiler/ocean/internal/world"
)

func fixtureCfg(bbox [4]float64) *world.Config {
	fx := "../../testdata/falmouth/"
	return &world.Config{Name: "fixture", BBox: bbox, CellM: 100, TileCells: 16, Resampling: "average",
		Local: &world.Local{Topo: fx + "topo.tif", Mask: fx + "mask.tif", Coastline: fx + "coast.geojson",
			License: "CC BY 4.0 / ODbL (test fixture)", Attribution: "GMRT; © OpenStreetMap contributors"}}
}

// inside the fixture crop (lon -5.1395..-5.0515, lat 50.1425..50.1989), with tile snapping margin
var insideBBox = [4]float64{-5.12, 50.155, -5.085, 50.185}

func build(t testing.TB, bbox [4]float64) (string, *world.Manifest) {
	t.Helper()
	dir := t.TempDir()
	m, err := world.Build(fixtureCfg(bbox), world.Options{CacheDir: t.TempDir(), OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return dir, m
}

func codes(r *Report, l Level) map[string]bool {
	out := map[string]bool{}
	for _, f := range r.Findings {
		if f.Level == l {
			out[f.Code] = true
		}
	}
	return out
}

func TestBuildAndValidate(t *testing.T) {
	dir, m := build(t, insideBBox)
	if len(m.Tiles) != m.Grid.TilesX*m.Grid.TilesY || len(m.Tiles) < 4 {
		t.Fatalf("want a multi-tile world, got %d tiles (%dx%d)", len(m.Tiles), m.Grid.TilesX, m.Grid.TilesY)
	}
	if m.Grid.SamplesPerTile != 17 {
		t.Fatalf("samples per tile = %d, want 2^n+1 = 17", m.Grid.SamplesPerTile)
	}
	r, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e := codes(r, Error); len(e) > 0 {
		t.Fatalf("unexpected errors: %v\n%s", e, r.Format())
	}
	if r.Metrics["edge_mismatches"] != 0 {
		t.Fatalf("neighbouring tiles disagree on shared edges: %v", r.Metrics["edge_mismatches"])
	}
	if m.Stats.CoastlineCheck == nil || m.Stats.CoastlineCheck.Points == 0 {
		t.Fatal("coastline check missing")
	}
	if m.Stats.WaterFraction <= 0 || m.Stats.WaterFraction >= 1 {
		t.Fatalf("fixture should contain both land and water, water fraction %v", m.Stats.WaterFraction)
	}
	if m.Stats.ShoreCells == 0 {
		t.Fatal("no shoreline cells detected")
	}
	// Tile ids are absolute grid indices: stable independent of the requested bbox.
	for _, tl := range m.Tiles {
		if tl.BoundsUTM[0] != float64(tl.X)*m.Grid.TileSpanM || tl.BoundsUTM[1] != float64(tl.Y)*m.Grid.TileSpanM {
			t.Fatalf("tile %s bounds %v do not sit on the absolute grid", tl.ID, tl.BoundsUTM)
		}
	}
}

func TestDeterministicBuild(t *testing.T) {
	d1, m1 := build(t, insideBBox)
	d2, m2 := build(t, insideBBox)
	if m1.BuildID != m2.BuildID {
		t.Fatalf("build ids differ: %s vs %s", m1.BuildID, m2.BuildID)
	}
	for _, f := range append([]string{world.ManifestFile}, func() (fs []string) {
		for _, tl := range m1.Tiles {
			fs = append(fs, tl.Height, tl.Depth, tl.Shore)
		}
		return
	}()...) {
		a, _ := os.ReadFile(filepath.Join(d1, f))
		b, _ := os.ReadFile(filepath.Join(d2, f))
		if len(a) == 0 || !bytes.Equal(a, b) {
			t.Fatalf("%s differs between identical builds", f)
		}
	}
}

func TestDetectsMissingAndCorruptTiles(t *testing.T) {
	dir, m := build(t, insideBBox)
	os.Remove(filepath.Join(dir, m.Tiles[0].Height))
	b, _ := os.ReadFile(filepath.Join(dir, m.Tiles[1].Height))
	b[10] ^= 0xff
	os.WriteFile(filepath.Join(dir, m.Tiles[1].Height), b, 0o644)
	r, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := codes(r, Error)
	if !e["missing-tile"] || !e["tile-hash"] || r.Result != "FAILED" {
		t.Fatalf("expected missing-tile and tile-hash errors, got %v (%s)", e, r.Result)
	}
}

func TestDetectsCracks(t *testing.T) {
	dir, m := build(t, insideBBox)
	// Alter the east edge of tile (col 0,row 0) and keep the manifest hash consistent,
	// so only the neighbour comparison can catch it.
	var left *world.Tile
	for i := range m.Tiles {
		if m.Tiles[i].Col == 0 && m.Tiles[i].Row == 0 {
			left = &m.Tiles[i]
		}
	}
	S := m.Grid.SamplesPerTile
	p := filepath.Join(dir, left.Height)
	b, _ := os.ReadFile(p)
	b[(3*S+S-1)*2] ^= 0x40
	os.WriteFile(p, b, 0o644)
	sum := sha256.Sum256(b)
	left.SHA256 = hex.EncodeToString(sum[:])
	mb, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(filepath.Join(dir, world.ManifestFile), mb, 0o644)
	r, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !codes(r, Error)["tile-gap"] {
		t.Fatalf("crack not detected:\n%s", r.Format())
	}
}

func TestNoDataIsReportedNotHidden(t *testing.T) {
	// West part of the bbox lies outside the fixture: those samples must be flagged nodata.
	dir, m := build(t, [4]float64{-5.16, 50.155, -5.10, 50.185})
	if m.Stats.NoDataFraction <= 0 {
		t.Skip("fixture fully covers bbox")
	}
	r, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !codes(r, Warn)["nodata"] && !codes(r, Error)["empty-tile"] {
		t.Fatalf("nodata not reported:\n%s", r.Format())
	}
	if r.Metrics["elevation_coverage"] >= 1 {
		t.Fatalf("coverage = %v with nodata present", r.Metrics["elevation_coverage"])
	}
}

func BenchmarkBuild(b *testing.B) {
	cfg := fixtureCfg(insideBBox)
	cache := b.TempDir()
	for i := 0; i < b.N; i++ {
		if _, err := world.Build(cfg, world.Options{CacheDir: cache, OutDir: b.TempDir()}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate(b *testing.B) {
	dir, _ := build(b, insideBBox)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Run(dir); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCheckpointsMatchTiles(t *testing.T) {
	dir, m := build(t, insideBBox)
	if len(m.Checkpoints) < 3 {
		t.Fatalf("want checkpoints, got %d", len(m.Checkpoints))
	}
	N, S := m.Grid.TileCells, m.Grid.SamplesPerTile
	cache := map[string][]float64{}
	var hi, lo float64 = -1e9, 1e9
	for _, c := range m.Checkpoints {
		col := int((c.Easting - m.Grid.OriginE) / m.Grid.CellM)
		row := int((m.Grid.OriginN - c.Northing) / m.Grid.CellM)
		tc, tr := min(col/N, m.Grid.TilesX-1), min(row/N, m.Grid.TilesY-1)
		var tile *world.Tile
		for i := range m.Tiles {
			if m.Tiles[i].Col == tc && m.Tiles[i].Row == tr {
				tile = &m.Tiles[i]
			}
		}
		h, ok := cache[tile.ID]
		if !ok {
			var err error
			if h, err = ReadHeights(filepath.Join(dir, tile.Height), m); err != nil {
				t.Fatal(err)
			}
			cache[tile.ID] = h
		}
		got := h[(row-tr*N)*S+(col-tc*N)]
		if d := got - c.ElevationM; d > 1e-6 || d < -1e-6 {
			t.Errorf("%s: tile value %.4f, checkpoint %.4f", c.Name, got, c.ElevationM)
		}
		if c.Name == "highest" {
			hi = c.ElevationM
		}
		if c.Name == "lowest" {
			lo = c.ElevationM
		}
	}
	if hi <= lo || hi < m.Height.MaxM-0.01 || lo > m.Height.MinM+0.01 {
		t.Errorf("highest %.2f / lowest %.2f do not match height range %.2f..%.2f", hi, lo, m.Height.MaxM, m.Height.MinM)
	}
}

func TestManifestLeaksNoLocalPaths(t *testing.T) {
	dir, _ := build(t, insideBBox)
	b, _ := os.ReadFile(filepath.Join(dir, world.ManifestFile))
	abs, _ := filepath.Abs("../../testdata")
	if bytes.Contains(b, []byte(abs)) || bytes.Contains(b, []byte("/Users/")) || bytes.Contains(b, []byte(os.TempDir())) {
		t.Fatal("manifest contains absolute local paths")
	}
}

func TestSourceLock(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "w.lock.json")
	opt := func(update bool) world.Options {
		return world.Options{CacheDir: t.TempDir(), OutDir: t.TempDir(), LockPath: lock, UpdateLock: update}
	}
	if _, err := world.Build(fixtureCfg(insideBBox), opt(true)); err != nil {
		t.Fatal(err)
	}
	b0, _ := os.ReadFile(lock)
	var l struct{ Sources map[string]json.RawMessage }
	json.Unmarshal(b0, &l)
	m0, _ := world.Build(fixtureCfg(insideBBox), opt(false))
	if m0 == nil || len(l.Sources) != len(m0.Sources) {
		t.Fatalf("lock must cover every manifest source: %d locked, manifest has %d", len(l.Sources), len(m0.Sources))
	}
	if _, err := world.Build(fixtureCfg(insideBBox), opt(false)); err != nil {
		t.Fatalf("same sources must match their lock: %v", err)
	}
	b, _ := os.ReadFile(lock)
	os.WriteFile(lock, bytes.Replace(b, []byte(`"sha256": "`), []byte(`"sha256": "00`), 1), 0o644)
	if _, err := world.Build(fixtureCfg(insideBBox), opt(false)); err == nil || !strings.Contains(err.Error(), "content changed") {
		t.Fatalf("tampered lock must fail the build, got %v", err)
	}
}
