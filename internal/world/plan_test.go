package world

import (
	"math"
	"testing"

	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/pkg/oceanworld"
)

func planFor(t *testing.T, bbox [4]float64, mut func(*Config)) *GridPlan {
	t.Helper()
	cfg := &Config{Name: "p", BBox: bbox, CellM: 500, TileCells: 64, Coastline: "none"}
	if mut != nil {
		mut(cfg)
	}
	p, err := PlanGrid(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A box inside one zone keeps the 0.2.0 behaviour: UTM, schema 2.
func TestPlanSingleZoneStaysUTM(t *testing.T) {
	p := planFor(t, [4]float64{-5.2, 50.05, -4.95, 50.22}, nil)
	if p.EPSG != 32630 || p.TM != nil || p.schema() != SchemaVersion {
		t.Fatalf("%+v", p)
	}
	// southern hemisphere: Cape Town area, zone 34S
	if p := planFor(t, [4]float64{18.3, -34.2, 18.7, -33.8}, nil); p.EPSG != 32734 || p.Grid.OriginN <= 0 {
		t.Fatalf("southern %+v", p.Grid)
	}
}

// Across the zone 30/31 boundary (0 degrees) a narrow box still fits one zone, and a local grid is used when forced;
// both give the same ground size to within the scale error, and the grid contains the whole box.
func TestPlanZoneBoundary(t *testing.T) {
	bbox := [4]float64{-0.4, 51.2, 0.4, 51.7}
	u := planFor(t, bbox, nil)
	if u.TM != nil {
		t.Fatal("0.8 degree box straddling a zone edge must fit one zone")
	}
	l := planFor(t, bbox, func(c *Config) { c.Projection = "tmerc" })
	if l.TM == nil || l.schema() != SchemaVersionTM || l.EPSG != 0 || l.SRS == "" {
		t.Fatalf("%+v", l)
	}
	if du := math.Abs(float64(u.Grid.TilesX-l.Grid.TilesX)) + math.Abs(float64(u.Grid.TilesY-l.Grid.TilesY)); du > 1 {
		t.Fatalf("tile counts differ: utm %dx%d tmerc %dx%d", u.Grid.TilesX, u.Grid.TilesY, l.Grid.TilesX, l.Grid.TilesY)
	}
	// k_0 is balanced: scale range is symmetric about 1 to within 1e-6 + a few percent of the spread
	if math.Abs((l.ScaleMin-1)+(l.ScaleMax-1)) > 1e-5 {
		t.Fatalf("scale %v..%v not balanced", l.ScaleMin, l.ScaleMax)
	}
}

// A box too wide for UTM is refused with projection "utm" and planned as a local grid by default.
func TestPlanWideBoxUsesLocalTM(t *testing.T) {
	bbox := [4]float64{-12, 40, 4, 44} // 16 degrees wide, spans three zones
	if _, err := PlanGrid(&Config{Name: "p", BBox: bbox, CellM: 2000, TileCells: 64, Projection: "utm"}); err == nil {
		t.Fatal("projection utm must refuse a 16 degree box")
	}
	p := planFor(t, bbox, func(c *Config) { c.CellM = 2000 })
	if p.TM == nil || p.TM.Lon0 != -4 {
		t.Fatalf("%+v", p.TM)
	}
	if dev := math.Max(p.ScaleMax-1, 1-p.ScaleMin); dev > MaxUTMScaleError {
		t.Fatalf("scale %v..%v", p.ScaleMin, p.ScaleMax)
	}
	// still refused when even a local grid cannot hold it
	if _, err := PlanGrid(&Config{Name: "p", BBox: [4]float64{-60, 0, 60, 20}, CellM: 5000, TileCells: 64}); err == nil {
		t.Fatal("a 120 degree box cannot be one metric grid")
	}
}

// Across the equator northings go negative in a local grid; tiles are indexed with floor so nothing is lost, and the
// grid origin is the north edge of the top tile.
func TestPlanEquatorAndAntimeridian(t *testing.T) {
	eq := planFor(t, [4]float64{10, -2, 12, 2}, func(c *Config) { c.Projection = "tmerc" })
	if eq.Grid.FirstTileY >= 0 || eq.Grid.OriginN <= 0 {
		t.Fatalf("equator grid %+v", eq.Grid)
	}
	am := planFor(t, [4]float64{179, -17, 181, -15}, nil) // Fiji, east > 180
	if am.EPSG != 32701 && am.EPSG != 32760 {             // centre 180 is 3 degrees from the central meridians of zones 1 and 60
		t.Fatalf("unexpected plan %+v", am)
	}
	want := 2.0 * 111320 * math.Cos(16*math.Pi/180) // about 214 km wide
	got := float64(am.Grid.TilesX) * am.Grid.TileSpanM
	if got < want || got > want+2*am.Grid.TileSpanM {
		t.Fatalf("antimeridian box is %.0f m wide in the grid, ground width about %.0f m", got, want)
	}
	// the two longitudes 179.5 and -179.5 are 1 degree apart on the grid, not 359
	tm := oceanworld.TM{Lon0: 180, K0: 1, FalseEasting: 500000}
	e1, _ := oceanworld.LonLatToTM(179.5, 0, tm)
	e2, _ := oceanworld.LonLatToTM(-179.5, 0, tm)
	if math.Abs((e2-e1)-111319) > 100 {
		t.Fatalf("antimeridian separation %.0f m", e2-e1)
	}
}

// The Go reader's transverse Mercator agrees with PROJ (via gdaltransform) for UTM and for a local grid, on both sides
// of the equator and the antimeridian.
func TestReaderTMMatchesPROJ(t *testing.T) {
	cases := []struct {
		srs string
		tm  oceanworld.TM
		pts [][2]float64
	}{
		{"EPSG:32630", oceanworld.UTMParams(30, true), [][2]float64{{-5, 50}, {-2.2, 0.5}, {0.4, 60}}},
		{"EPSG:32734", oceanworld.UTMParams(34, false), [][2]float64{{18.5, -34}, {21, -5}}},
		{"+proj=tmerc +lat_0=0 +lon_0=180.000 +k=0.99950000 +x_0=500000 +y_0=0 +datum=WGS84 +units=m +no_defs",
			oceanworld.TM{Lon0: 180, K0: 0.9995, FalseEasting: 500000}, [][2]float64{{179.2, -16}, {-179.4, -15.5}, {179.9, 1}, {-179.9, -1}}},
	}
	for _, c := range cases {
		out, err := gdal.Transform("EPSG:4326", c.srs, c.pts)
		if err != nil {
			t.Fatal(err)
		}
		for i, pt := range c.pts {
			e, n := oceanworld.LonLatToTM(pt[0], pt[1], c.tm)
			if math.Abs(e-out[i][0]) > 0.01 || math.Abs(n-out[i][1]) > 0.01 {
				t.Errorf("%s %v: reader %.3f,%.3f PROJ %.3f,%.3f", c.srs[:min(12, len(c.srs))], pt, e, n, out[i][0], out[i][1])
			}
			lon, lat := oceanworld.TMToLonLat(e, n, c.tm)
			if math.Abs(lat-pt[1]) > 1e-8 || math.Abs(wrapLon(lon-pt[0])) > 1e-8 {
				t.Errorf("inverse %v -> %.9f,%.9f", pt, lon, lat)
			}
		}
	}
}
