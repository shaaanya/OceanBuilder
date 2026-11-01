package world

import (
	"fmt"
	"math"

	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/pkg/oceanworld"
)

// wrapLon maps a longitude to [-180, 180).
func wrapLon(l float64) float64 { return math.Mod(math.Mod(l+180, 360)+360, 360) - 180 }

// utmLon returns lon expressed on the branch nearest the central meridian of zone, so a box that crosses the
// antimeridian (east > 180) measures its distance from the meridian correctly.
func utmLon(lon float64, zone int) float64 {
	cm := float64(zone*6 - 183)
	return cm + wrapLon(lon-cm)
}

// GridPlan is the metric grid a config produces, known before any data is fetched.
type GridPlan struct {
	EPSG               int // 0 for a local transverse Mercator grid
	SRS                string
	TM                 *oceanworld.TM // set for a local grid
	Grid               Grid
	W, H               int // level-0 samples
	ScaleMin, ScaleMax float64
	Note               string // projection distortion warning, if any
}

// CRS is the manifest description of the plan's projection.
func (p *GridPlan) CRS() CRS {
	if p.TM == nil {
		return CRS{EPSG: p.EPSG, Kind: "utm", Name: fmt.Sprintf("WGS 84 / UTM zone %d%s", p.EPSG%100, map[bool]string{true: "N", false: "S"}[p.EPSG < 32700])}
	}
	return CRS{Kind: "tmerc", Name: fmt.Sprintf("WGS 84 / local transverse Mercator, lon_0 %.3f, k_0 %.6f", p.TM.Lon0, p.TM.K0),
		Lon0: p.TM.Lon0, K0: p.TM.K0, FalseEasting: p.TM.FalseEasting, FalseNorthing: p.TM.FalseNorthing, Proj4: p.SRS}
}

func (p *GridPlan) schema() int {
	if p.TM != nil {
		return SchemaVersionTM
	}
	return SchemaVersion
}

func scaleRange(w, s, e, n float64, k func(lon, lat float64) float64) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for i := 0; i <= 4; i++ {
		for j := 0; j <= 4; j++ {
			v := k(w+(e-w)*float64(i)/4, s+(n-s)*float64(j)/4)
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	return
}

// localTM builds a transverse Mercator centred on the bbox with k_0 chosen so the scale error is symmetric about 1
// (k = k_0 (1 + d^2/2), so k_0 = 1 - dmax^2/4 spreads the error evenly instead of putting it all on one side).
func localTM(w, s, e, n float64) oceanworld.TM {
	lon0 := math.Round((w+e)/2*1000) / 1000
	laNear := 0.0
	if s > 0 {
		laNear = s
	} else if n < 0 {
		laNear = n
	}
	dmax := math.Max(math.Abs(w-lon0), math.Abs(e-lon0)) * math.Pi / 180 * math.Cos(laNear*math.Pi/180)
	k0 := math.Round((1-dmax*dmax/4)*1e8) / 1e8
	return oceanworld.TM{Lon0: lon0, K0: k0, FalseEasting: 500000}
}

// PlanGrid chooses the projection, checks distortion over the bbox and lays out the tile-aligned grid. One UTM zone
// is used when the box fits it (the 0.2.0 behaviour, unchanged); a wider box gets a local transverse Mercator (schema
// 3) instead of being refused, unless projection is "utm". A box wider than the local grid can hold within
// MaxUTMScaleError is still refused.
func PlanGrid(cfg *Config) (*GridPlan, error) {
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	span, N := cfg.TileSpanM(), cfg.TileCells
	w, s, e, n := cfg.BBox[0], cfg.BBox[1], cfg.BBox[2], cfg.BBox[3]
	p := &GridPlan{}
	useUTM := cfg.Projection != "tmerc"
	if useUTM {
		epsg := gdal.UTMEPSG(wrapLon((w+e)/2), (s+n)/2)
		if cfg.UTMEPSG != 0 {
			epsg = cfg.UTMEPSG
		}
		zone := epsg % 100
		lo, hi := scaleRange(w, s, e, n, func(lon, lat float64) float64 { return gdal.UTMScale(utmLon(lon, zone), lat, epsg) })
		dev := math.Max(math.Abs(hi-1), math.Abs(lo-1))
		switch {
		case dev <= MaxUTMScaleError:
			p.EPSG, p.SRS, p.ScaleMin, p.ScaleMax = epsg, fmt.Sprintf("EPSG:%d", epsg), lo, hi
			if dev > WarnUTMScaleError {
				p.Note = fmt.Sprintf("UTM zone %d scale factor is %.5f..%.5f over the bbox: 1 m on the grid is %.2f..%.2f m on the ground (distances are %.2f%% off at the far side)", zone, lo, hi, 1/hi, 1/lo, 100*dev)
			}
		case cfg.Projection == "utm" || cfg.UTMEPSG != 0:
			return nil, fmt.Errorf("bbox reaches %.2f%% scale distortion in UTM zone %d (limit %.1f%%): it spans too much of the zone's width. Use projection \"auto\" or \"tmerc\" for a local transverse Mercator grid, or split the region", 100*dev, zone, 100*MaxUTMScaleError)
		default:
			useUTM = false
		}
	}
	if !useUTM {
		tm := localTM(w, s, e, n)
		lo, hi := scaleRange(w, s, e, n, func(lon, lat float64) float64 { return oceanworld.GroundScale(lon, lat, tm) })
		if dev := math.Max(math.Abs(hi-1), math.Abs(lo-1)); dev > MaxUTMScaleError {
			return nil, fmt.Errorf("bbox reaches %.2f%% scale distortion even in a local transverse Mercator (limit %.1f%%): the region is too wide for one metric grid. Split it", 100*dev, 100*MaxUTMScaleError)
		}
		p.TM, p.ScaleMin, p.ScaleMax = &tm, lo, hi
		p.SRS = fmt.Sprintf("+proj=tmerc +lat_0=0 +lon_0=%.3f +k=%.8f +x_0=%.0f +y_0=%.0f +datum=WGS84 +units=m +no_defs", tm.Lon0, tm.K0, tm.FalseEasting, tm.FalseNorthing)
		p.Note = fmt.Sprintf("local transverse Mercator (lon_0 %.3f, k_0 %.6f): 1 m on the grid is %.4f..%.4f m on the ground", tm.Lon0, tm.K0, 1/hi, 1/lo)
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
