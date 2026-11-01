package world

import (
	"math"
	"path/filepath"
	"testing"
)

// testGrid builds a Grid the way Build does, from absolute tile index ranges.
func testGrid(x0, y0, tx, ty, n int, cell float64) *Grid {
	span := cell * float64(n)
	return &Grid{CellM: cell, TileCells: n, SamplesPerTile: n + 1, TileSpanM: span, TilesX: tx, TilesY: ty,
		OriginE: float64(x0) * span, OriginN: float64(y0+ty) * span, FirstTileX: x0, FirstTileY: y0}
}

// Every LOD vertex must sit on a level-0 vertex at the same easting/northing and carry its exact value, whatever
// the world's alignment to the 2^k tile lattice; vertices outside the world are nodata.
func TestDecimateMatchesLevel0AtSameGroundPosition(t *testing.T) {
	for _, g := range []*Grid{testGrid(100, 200, 3, 5, 8, 10), testGrid(101, 201, 4, 2, 4, 25), testGrid(7, 9, 1, 1, 16, 48), testGrid(1023, 3, 2, 3, 8, 12.5)} {
		W, H := g.TilesX*g.TileCells+1, g.TilesY*g.TileCells+1
		base := &raster{W: W, H: H, dem: make([]float32, W*H), src: make([]uint8, W*H)}
		for r := 0; r < H; r++ {
			for c := 0; c < W; c++ {
				base.dem[r*W+c] = float32(c) + 1000*float32(r) + 0.5 // encodes its own position
				base.src[r*W+c] = uint8(1 + (c+r)%6)
			}
		}
		dir := t.TempDir()
		mk := func(name string, w, h int) *layerFiles {
			return &layerFiles{W: w, H: h, demPath: filepath.Join(dir, name+".dem"), kindPath: filepath.Join(dir, name+".kind"), provPath: filepath.Join(dir, name+".prov")}
		}
		prev := mk("l0", W, H)
		if err := writeLayerFromMemory(prev, base.dem, base.src, make([]uint8, W*H)); err != nil {
			t.Fatal(err)
		}
		b := &bctx{cfg: &Config{TileCells: g.TileCells}}
		for k := 1; k <= 4; k++ { // chained: each level is cut from the one below, as the build does
			p := planLevel(g, k)
			cur := mk(string(rune('a'+k)), p.W, p.H)
			if err := b.decimateLayer(prev, planLevel(g, k-1), p, cur); err != nil {
				t.Fatal(err)
			}
			prev = cur
			d, s, _, err := readLayerToMemory(cur)
			if err != nil {
				t.Fatal(err)
			}
			lr := &raster{dem: d, src: s}
			for r := 0; r < p.H; r++ {
				for c := 0; c < p.W; c++ {
					e, n := p.OriginE+float64(c)*p.CellM, p.OriginN-float64(r)*p.CellM
					wc, wr := (e-g.OriginE)/g.CellM, (g.OriginN-n)/g.CellM
					if math.Abs(wc-math.Round(wc)) > 1e-6 || math.Abs(wr-math.Round(wr)) > 1e-6 {
						t.Fatalf("grid %d level %d: vertex (%d,%d) is not on a level-0 vertex (%.3f,%.3f)", g.FirstTileX, k, r, c, wr, wc)
					}
					ic, ir := int(math.Round(wc)), int(math.Round(wr))
					got, gotSrc := lr.dem[r*p.W+c], lr.src[r*p.W+c]
					if ic < 0 || ir < 0 || ic >= W || ir >= H {
						if got != noData || gotSrc != SrcNoData {
							t.Fatalf("level %d vertex (%d,%d) outside the world must be nodata, got %v/%d", k, r, c, got, gotSrc)
						}
						continue
					}
					if got != base.dem[ir*W+ic] || gotSrc != base.src[ir*W+ic] {
						t.Fatalf("level %d vertex (%d,%d): got %v/%d, level 0 (%d,%d) has %v/%d", k, r, c, got, gotSrc, ir, ic, base.dem[ir*W+ic], base.src[ir*W+ic])
					}
				}
			}
		}
	}
}

// A parent tile spans exactly its four children and its id is the children's index >> 1.
func TestLevelPlansNest(t *testing.T) {
	g := testGrid(101, 201, 4, 3, 8, 10)
	for k := 1; k <= 4; k++ {
		parent, child := planLevel(g, k), planLevel(g, k-1)
		if parent.SpanM != 2*child.SpanM || parent.CellM != 2*child.CellM {
			t.Fatalf("level %d: span/cell do not double", k)
		}
		for x := child.X0; x <= child.X1; x++ {
			if px := x >> 1; px < parent.X0 || px > parent.X1 {
				t.Fatalf("level %d: child column %d has no parent column", k, x)
			}
		}
		for y := child.Y0; y <= child.Y1; y++ {
			if py := y >> 1; py < parent.Y0 || py > parent.Y1 {
				t.Fatalf("level %d: child row %d has no parent row", k, y)
			}
		}
		if parent.OriginE > child.OriginE || parent.OriginE+float64(parent.TX)*parent.SpanM < child.OriginE+float64(child.TX)*child.SpanM {
			t.Fatalf("level %d does not cover level %d in easting", k, k-1)
		}
	}
	if n := autoLevels(g); planLevel(g, n).TX != 1 || planLevel(g, n).TY != 1 {
		t.Fatalf("autoLevels = %d does not end in a single tile", n)
	}
}

func TestLinkTiles(t *testing.T) {
	tiles := []Tile{{Level: 0, X: 2, Y: 4}, {Level: 0, X: 3, Y: 4}, {Level: 0, X: 2, Y: 5}, {Level: 1, X: 1, Y: 2}}
	linkTiles(tiles)
	if tiles[0].Parent != "L1_1_2" || tiles[3].Parent != "" {
		t.Fatalf("parents: %q %q", tiles[0].Parent, tiles[3].Parent)
	}
	if tiles[0].Neighbours["e"] != "L0_3_4" || tiles[0].Neighbours["n"] != "L0_2_5" || tiles[1].Neighbours["w"] != "L0_2_4" || len(tiles[3].Neighbours) != 0 {
		t.Fatalf("neighbours: %v / %v / %v", tiles[0].Neighbours, tiles[1].Neighbours, tiles[3].Neighbours)
	}
}

func TestDeriveClassAndShore(t *testing.T) {
	r := &raster{W: 4, H: 1, dem: []float32{10, -3, -30, noData}}
	r.derive([]float64{5, 20})
	if r.class[0] != 0 || r.class[1] != 1 || r.class[2] != 3 || r.class[3] != ClassNoData {
		t.Fatalf("classes %v", r.class)
	}
	if r.shore[0] != 255 || r.shore[1] != 0 || r.shores != 1 {
		t.Fatalf("shore %v (%d)", r.shore, r.shores)
	}
}
