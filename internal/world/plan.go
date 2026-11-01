package world

import (
	"fmt"
	"math"

	"github.com/ocean-compiler/ocean/internal/gdal"
)

// GridPlan is the metric grid a config produces, known before any data is fetched.
type GridPlan struct {
	EPSG               int
	SRS                string
	Grid               Grid
	W, H               int // level-0 samples
	ScaleMin, ScaleMax float64
	Note               string // projection distortion warning, if any
}

// PlanGrid chooses the UTM zone, checks projection distortion over the bbox and lays out the tile-aligned grid.
// It refuses a bbox whose far edge is too far from the zone's central meridian (multi-zone worlds are unsupported).
func PlanGrid(cfg *Config) (*GridPlan, error) {
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	span, N := cfg.TileSpanM(), cfg.TileCells
	w, s, e, n := cfg.BBox[0], cfg.BBox[1], cfg.BBox[2], cfg.BBox[3]
	epsg := gdal.UTMEPSG((w+e)/2, (s+n)/2)
	if cfg.UTMEPSG != 0 {
		epsg = cfg.UTMEPSG
	}
	p := &GridPlan{EPSG: epsg, SRS: fmt.Sprintf("EPSG:%d", epsg), ScaleMin: math.Inf(1), ScaleMax: math.Inf(-1)}
	for _, lo := range []float64{w, (w + e) / 2, e} {
		for _, la := range []float64{s, (s + n) / 2, n} {
			k := gdal.UTMScale(lo, la, epsg)
			p.ScaleMin, p.ScaleMax = math.Min(p.ScaleMin, k), math.Max(p.ScaleMax, k)
		}
	}
	if dev := math.Max(math.Abs(p.ScaleMax-1), math.Abs(p.ScaleMin-1)); dev > MaxUTMScaleError {
		return nil, fmt.Errorf("bbox reaches %.2f%% scale distortion in UTM zone %d (limit %.1f%%): it spans too much of the zone's width. Split the region, or set utm_epsg to the zone that fits best; multi-zone worlds are not supported", 100*dev, epsg%100, 100*MaxUTMScaleError)
	} else if dev > WarnUTMScaleError {
		p.Note = fmt.Sprintf("UTM zone %d scale factor is %.5f..%.5f over the bbox: 1 m on the grid is %.2f..%.2f m on the ground (distances are %.2f%% off at the far side)", epsg%100, p.ScaleMin, p.ScaleMax, 1/p.ScaleMax, 1/p.ScaleMin, 100*dev)
	}
	var edge [][2]float64
	for i := 0; i <= 8; i++ {
		f := float64(i) / 8
		lo, la := w+(e-w)*f, s+(n-s)*f
		edge = append(edge, [2]float64{lo, s}, [2]float64{lo, n}, [2]float64{w, la}, [2]float64{e, la})
	}
	utm, err := gdal.Transform("EPSG:4326", p.SRS, edge)
	if err != nil {
		return nil, err
	}
	minE, maxE, minN, maxN := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, q := range utm {
		minE, maxE = math.Min(minE, q[0]), math.Max(maxE, q[0])
		minN, maxN = math.Min(minN, q[1]), math.Max(maxN, q[1])
	}
	x0, x1 := int(math.Floor(minE/span)), int(math.Floor(maxE/span))
	y0, y1 := int(math.Floor(minN/span)), int(math.Floor(maxN/span))
	tx, ty := x1-x0+1, y1-y0+1
	p.W, p.H = tx*N+1, ty*N+1
	p.Grid = Grid{CellM: cfg.CellM, TileCells: N, SamplesPerTile: N + 1, TileSpanM: span, TilesX: tx, TilesY: ty,
		OriginE: float64(x0) * span, OriginN: float64(y1+1) * span, FirstTileX: x0, FirstTileY: y0}
	return p, nil
}
