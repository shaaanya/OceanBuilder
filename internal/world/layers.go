package world

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Per-cell source codes written to the source layer (tiles/<level>/<id>.source.png). The numbers are part of
// the format (docs/formats/world-v2.md); never renumber, only append.
const (
	SrcNoData     = 0 // no value
	SrcSurvey     = 1 // water cell backed by a survey grid (the primary source's own measurement mask)
	SrcBackground = 2 // water cell from a coarse or unattributed grid; resampled, not measured at output resolution
	SrcLand       = 3 // land elevation from the primary source's DEM
	SrcRegional   = 4 // water cell replaced or blended from a regional bathymetry grid
	SrcConformed  = 5 // land/water decision taken from the coastline (synthetic +-0.5 m), not from elevation data
	SrcRefined    = 6 // land elevation changed by an external edit layer
	SrcLODDerived = 7 // coarse-level sample whose height was computed from neighbouring finer samples (lod_method shoal)
)

// MaxLOD is the highest LOD level the builder will produce.
const MaxLOD = 8

// SourceLayer documents one source code in the manifest.
type SourceLayer struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Measured    bool   `json:"measured"` // true only for values that are a measurement at (or near) output resolution
	Description string `json:"description"`
}

func sourceLayers() []SourceLayer {
	return []SourceLayer{
		{SrcNoData, "nodata", false, "no value; height is the nodata sentinel and depth class 255"},
		{SrcSurvey, "survey", true, "water cell backed by a survey grid"},
		{SrcBackground, "background", false, "water cell from a coarse or unattributed grid, resampled to the output grid"},
		{SrcLand, "land", false, "land elevation from the primary elevation source"},
		{SrcRegional, "regional", false, "water cell replaced or blended from a regional bathymetry grid"},
		{SrcConformed, "conformed", false, "land/water decision follows the coastline; height is a synthetic +-0.5 m"},
		{SrcRefined, "refined", false, "land elevation edited by an external refinement layer"},
		{SrcLODDerived, "lod-derived", false, "LOD level above 0 only: height computed from the 3x3 neighbourhood of the finer level (lod_method smooth or shoal); never on level 0"},
	}
}

func srcName(code int) string {
	for _, l := range sourceLayers() {
		if l.ID == code {
			return l.Name
		}
	}
	return fmt.Sprint(code)
}

// initialSource assigns the first source code per cell from validity, sign and the survey mask.
func initialSource(dem []float32, meas []uint8) []uint8 {
	src := make([]uint8, len(dem))
	for i, v := range dem {
		switch {
		case v <= noData+1:
			src[i] = SrcNoData
		case v >= 0:
			src[i] = SrcLand
		case meas[i] == 1:
			src[i] = SrcSurvey
		default:
			src[i] = SrcBackground
		}
	}
	return src
}

// markChanged sets code on every cell whose value differs from before.
func markChanged(src []uint8, before, after []float32, code uint8) {
	for i := range after {
		if after[i] != before[i] {
			src[i] = code
		}
	}
}

// raster is one LOD level's sample grid: vertices at cell spacing, row 0 = north, whole tiles (N+1 samples per
// tile edge, edges shared). Samples outside the world are nodata.
type raster struct {
	W, H   int
	dem    []float32
	src    []uint8
	prov   []uint8 // dataset index (1-based) per cell
	class  []uint8
	shore  []uint8
	shores int // shore cell count
}

// derive fills class and shore from dem, with the same rules at every level: class 0 land, 1..n depth bands,
// 255 nodata; shore = land cell with a 4-neighbour water cell.
func (r *raster) derive(bands []float64) {
	W, H := r.W, r.H
	r.class, r.shore, r.shores = make([]uint8, W*H), make([]uint8, W*H), 0
	water := func(i int) bool { return r.dem[i] > noData+1 && r.dem[i] < 0 }
	for row := 0; row < H; row++ {
		for c := 0; c < W; c++ {
			i := row*W + c
			v := r.dem[i]
			switch {
			case v <= noData+1:
				r.class[i] = ClassNoData
			case v >= 0:
				for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					rr, cc := row+d[0], c+d[1]
					if rr >= 0 && rr < H && cc >= 0 && cc < W && water(rr*W+cc) {
						r.shore[i] = 255
						r.shores++
						break
					}
				}
			default:
				k := 1
				for k <= len(bands) && -float64(v) > bands[k-1] {
					k++
				}
				r.class[i] = uint8(k)
			}
		}
	}
}

// levelPlan is the tile layout of one LOD level. Tile index x at level k is floor(E / (span * 2^k)), so a
// parent's index is the child's index >> 1 and the levels nest exactly.
type levelPlan struct {
	Level            int
	Factor           int // 2^level
	CellM, SpanM     float64
	X0, X1, Y0, Y1   int // absolute tile index range at this level
	TX, TY           int
	W, H             int // raster samples
	OriginE, OriginN float64
}

func planLevel(g *Grid, k int) levelPlan {
	f := 1 << k
	x0, x1 := g.FirstTileX>>k, (g.FirstTileX+g.TilesX-1)>>k
	y0, y1 := g.FirstTileY>>k, (g.FirstTileY+g.TilesY-1)>>k
	p := levelPlan{Level: k, Factor: f, CellM: g.CellM * float64(f), SpanM: g.TileSpanM * float64(f),
		X0: x0, X1: x1, Y0: y0, Y1: y1, TX: x1 - x0 + 1, TY: y1 - y0 + 1}
	p.W, p.H = p.TX*g.TileCells+1, p.TY*g.TileCells+1
	p.OriginE, p.OriginN = float64(x0)*p.SpanM, float64(y1+1)*p.SpanM
	return p
}

// autoLevels is the highest LOD level needed until one tile covers the world (capped at MaxLOD).
func autoLevels(g *Grid) int {
	for k := 0; k < MaxLOD; k++ {
		if p := planLevel(g, k); p.TX == 1 && p.TY == 1 {
			return k
		}
	}
	return MaxLOD
}

// tileSlot is one planned tile of a level, with its corner coordinates once transformed.
type tileSlot struct {
	plan     levelPlan
	row, col int
	x, y     int
	corners  [][2]float64
}

func slotsOf(p levelPlan) []tileSlot {
	var s []tileSlot
	for r := 0; r < p.TY; r++ {
		for c := 0; c < p.TX; c++ {
			s = append(s, tileSlot{plan: p, row: r, col: c, x: p.X0 + c, y: p.Y1 - r})
		}
	}
	return s
}

func (t *tileSlot) utmCorners() [4][2]float64 {
	e0, n1 := t.plan.OriginE+float64(t.col)*t.plan.SpanM, t.plan.OriginN-float64(t.row)*t.plan.SpanM
	return [4][2]float64{{e0, n1 - t.plan.SpanM}, {e0 + t.plan.SpanM, n1 - t.plan.SpanM}, {e0, n1}, {e0 + t.plan.SpanM, n1}}
}

func tileID(level, x, y int) string { return fmt.Sprintf("L%d_%d_%d", level, x, y) }

// TileHashes are the SHA-256 of each tile file.
type TileHashes struct {
	Height string `json:"height"`
	Depth  string `json:"depth_class"`
	Shore  string `json:"shore_mask"`
	Source string `json:"source"`
	// Dataset is the hash of the optional per-sample dataset layer (absent in worlds built before 0.3).
	Dataset string `json:"dataset,omitempty"`
}

func hashHex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// linkTiles fills parent and edge-neighbour ids from tile indices. The rule is also documented in the format
// spec, so a reader can derive the same links without them.
func linkTiles(tiles []Tile) {
	idx := map[string]bool{}
	for _, t := range tiles {
		idx[tileID(t.Level, t.X, t.Y)] = true
	}
	dirs := []struct {
		name string
		d    [2]int
	}{{"n", [2]int{0, 1}}, {"s", [2]int{0, -1}}, {"e", [2]int{1, 0}}, {"w", [2]int{-1, 0}}}
	for i := range tiles {
		t := &tiles[i]
		if pid := tileID(t.Level+1, t.X>>1, t.Y>>1); idx[pid] {
			t.Parent = pid
		}
		for _, d := range dirs {
			if nid := tileID(t.Level, t.X+d.d[0], t.Y+d.d[1]); idx[nid] {
				if t.Neighbours == nil {
					t.Neighbours = map[string]string{}
				}
				t.Neighbours[d.name] = nid
			}
		}
	}
}
