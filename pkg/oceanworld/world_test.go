package oceanworld_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaaanya/OceanBuilder/internal/validate"
	"github.com/shaaanya/OceanBuilder/internal/world"
	"github.com/shaaanya/OceanBuilder/pkg/oceanworld"
)

func buildFixture(t testing.TB) (string, *world.Manifest) {
	t.Helper()
	fx := "../../testdata/falmouth/"
	cfg := &world.Config{Name: "fixture", BBox: [4]float64{-5.12, 50.155, -5.085, 50.185}, CellM: 100, TileCells: 16, Resampling: "average",
		Local: &world.Local{Topo: fx + "topo.tif", Mask: fx + "mask.tif", Coastline: fx + "coast.geojson",
			License: "test fixture", Attribution: "GMRT; OSM"}}
	dir := t.TempDir()
	m, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return dir, m
}

// The reader works from the manifest and tile files only: at every level-0 vertex it must return exactly the
// stored height, and a vertex on the 2^k lattice must have the same height at level k.
func TestReaderMatchesTilesAtVertices(t *testing.T) {
	dir, m := buildFixture(t)
	w, err := oceanworld.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if w.Levels() != len(m.LOD.Levels) || w.Levels() < 2 {
		t.Fatalf("levels = %d", w.Levels())
	}
	S := m.Grid.SamplesPerTile
	checked := 0
	for _, tl := range m.TilesAt(0) {
		h, err := validate.ReadHeights(filepath.Join(dir, tl.Height), m)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < S-1; i += 3 { // interior: an edge vertex also belongs to the neighbour
			for j := 0; j < S-1; j += 3 {
				e := tl.BoundsUTM[0] + float64(j)*m.Grid.CellM
				n := tl.BoundsUTM[3] - float64(i)*m.Grid.CellM
				s, err := w.SampleUTM(e, n, 0)
				if err != nil {
					t.Fatal(err)
				}
				if !s.Valid { // a nodata neighbour vertex makes the bilinear answer invalid
					continue
				}
				if math.Abs(s.ElevationM-h[i*S+j]) > 1e-4 {
					t.Fatalf("%s (%d,%d): reader %.5f, file %.5f", tl.ID, i, j, s.ElevationM, h[i*S+j])
				}
				checked++
				for k := 1; k < w.Levels(); k++ {
					f := float64(int(1)<<k) * m.Grid.CellM
					if math.Mod(e, f) == 0 && math.Mod(n, f) == 0 {
						sk, err := w.SampleUTM(e, n, k)
						if err != nil {
							t.Fatal(err)
						}
						if sk.Valid && math.Abs(sk.ElevationM-s.ElevationM) > 1e-4 {
							t.Fatalf("level %d at (%.1f,%.1f): %.4f vs level 0 %.4f", k, e, n, sk.ElevationM, s.ElevationM)
						}
					}
				}
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d vertices compared", checked)
	}
}

func TestReaderLonLatDepthAndOutside(t *testing.T) {
	dir, m := buildFixture(t)
	w, err := oceanworld.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	b := m.Grid.BBoxWGS84
	lon, lat := (b[0]+b[2])/2, (b[1]+b[3])/2
	e, n := w.ToUTM(lon, lat)
	lon2, lat2 := w.ToLonLat(e, n)
	if math.Abs(lon-lon2) > 1e-9 || math.Abs(lat-lat2) > 1e-9 {
		t.Fatal("lon/lat round trip")
	}
	s, err := w.SampleLonLat(lon, lat, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Tile == "" || s.Source == "" {
		t.Fatalf("centre of the world has no tile/source: %+v", s)
	}
	if s.Valid && (s.ElevationM < 0) != (s.DepthM > 0) {
		t.Fatalf("depth %.2f and elevation %.2f disagree", s.DepthM, s.ElevationM)
	}
	if out, _ := w.SampleLonLat(lon+20, lat, 0); out.Valid || out.Tile != "" {
		t.Fatal("a point 20 degrees away must be outside the world")
	}
	if _, err := w.SampleUTM(e, n, 99); err == nil {
		t.Fatal("bad level must be an error")
	}
	prof, err := w.Profile(e-500, n, e+500, n, 10, 0)
	if err != nil || len(prof) != 11 {
		t.Fatalf("profile: %v %d", err, len(prof))
	}
}

func TestOpenRefusesOtherSchema(t *testing.T) {
	dir, _ := buildFixture(t)
	p := filepath.Join(dir, "world.manifest.json")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(`{"schema_version": 9}`), 0o644)
	if _, err := oceanworld.Open(dir); err == nil {
		t.Fatal("schema 9 must be refused")
	}
	os.WriteFile(p, b, 0o644)
	if _, err := oceanworld.Open(dir); err != nil {
		t.Fatal(err)
	}
}

// Vertices on the outer boundary (north and east edges have no tile beyond them) belong to the last tile.
func TestReaderAnswersOnWorldBoundary(t *testing.T) {
	dir, m := buildFixture(t)
	w, err := oceanworld.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := m.Grid
	e1, n0 := g.OriginE+float64(g.TilesX)*g.TileSpanM, g.OriginN-float64(g.TilesY)*g.TileSpanM
	for _, p := range [][2]float64{{g.OriginE, g.OriginN}, {e1, g.OriginN}, {g.OriginE, n0}, {e1, n0}} {
		s, err := w.SampleUTM(p[0], p[1], 0)
		if err != nil || s.Tile == "" {
			t.Fatalf("corner (%.0f,%.0f) reported outside the world: %+v %v", p[0], p[1], s, err)
		}
	}
}
