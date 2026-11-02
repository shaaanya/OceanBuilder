// Package gmrt fetches Global Multi-Resolution Topography grids.
// GMRT merges land DEMs and multibeam bathymetry at up to ~1 m resolution
// and exposes a mask layer marking cells backed by measured (non-interpolated)
// data. See docs/sources.md.
package gmrt

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"

	"github.com/shaaanya/OceanBuilder/internal/fetch"
	"github.com/shaaanya/OceanBuilder/internal/gdal"
)

// Endpoint is the GridServer URL (a variable so tests can point it at a local server).
var Endpoint = "https://www.gmrt.org/services/GridServer"

// BBox is west, south, east, north in WGS84 degrees.
type BBox struct{ W, S, E, N float64 }

// Layers fetched: "topo" is elevation in metres (negative below sea level);
// "topo-mask" is 1 where the cell comes from a high-resolution survey.
const (
	LayerTopo = "topo"
	LayerMask = "topo-mask"
)

// Fetch downloads one GMRT layer as GeoTIFF.
func Fetch(cacheDir string, b BBox, layer string) (*fetch.Provenance, error) {
	q := url.Values{}
	q.Set("minlongitude", fmt.Sprintf("%.6f", b.W))
	q.Set("maxlongitude", fmt.Sprintf("%.6f", b.E))
	q.Set("minlatitude", fmt.Sprintf("%.6f", b.S))
	q.Set("maxlatitude", fmt.Sprintf("%.6f", b.N))
	q.Set("format", "geotiff")
	q.Set("resolution", "max")
	q.Set("layer", layer)
	return fetch.Get(cacheDir, "gmrt", Endpoint+"?"+q.Encode(), nil, layer+".tif",
		"CC BY 4.0 (https://www.gmrt.org/about/terms_of_use.php); data must not be used for navigation",
		"Ryan, W.B.F. et al. (2009), Global Multi-Resolution Topography (GMRT) synthesis data set, Geochem. Geophys. Geosyst., 10, Q03014, doi:10.1029/2008GC002332; data doi:10.1594/IEDA.100001")
}

// NodeBudget is the largest piece, in grid nodes at the service's best resolution, that Plan asks for in one request.
// Measured 2026-11-02 (docs/sources.md): at 61 m a 3 x 3 degree request (29.8 M nodes, 119 MB, 102 s) still came back at
// full resolution, while 0.3.0 saw a 6 x 5 degree request (about 99 M nodes) come back at twice the cell size, with no
// message. The service documents a node limit for GeoTIFF but not its value; this budget stays well under the largest
// size seen to work.
var NodeBudget = 16_000_000.0

// Delivery says what the service returned for one world, so the manifest can state the resolution actually received
// instead of the one requested.
type Delivery struct {
	Request        string  `json:"request"`          // the resolution parameter that was sent
	ReferenceCellM float64 `json:"reference_cell_m"` // finest cell the service offered at the centre of the area (small probe request)
	Pieces         int     `json:"pieces"`           // requests the area was split into to stay under NodeBudget
	CellMMin       float64 `json:"cell_m_min"`       // finest and coarsest cell actually received, ground metres
	CellMMax       float64 `json:"cell_m_max"`
	Coarsened      int     `json:"coarsened_pieces"` // pieces whose cell is more than 25 % coarser than the reference
	Note           string  `json:"note,omitempty"`
}

// CellDeg returns the pixel size in degrees and the raster size of a downloaded GeoTIFF.
func CellDeg(path string) (dx, dy float64, w, h int, err error) {
	out, err := gdal.Run("gdalinfo", "-json", path)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	var info struct {
		Size []int     `json:"size"`
		GT   []float64 `json:"geoTransform"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil || len(info.Size) != 2 || len(info.GT) != 6 {
		return 0, 0, 0, 0, fmt.Errorf("gdalinfo %s: unreadable size or geotransform", path)
	}
	return math.Abs(info.GT[1]), math.Abs(info.GT[5]), info.Size[0], info.Size[1], nil
}

// CellM converts a cell size in degrees to mean ground metres at latitude lat.
func CellM(dx, dy, lat float64) float64 {
	return (dx*111320*math.Cos(lat*math.Pi/180) + dy*110574) / 2
}

// Plan splits b into pieces that each stay under NodeBudget at the service's finest resolution, which it learns from a
// tiny probe request at the centre of b. Interior edges overlap by a few reference cells. It returns one piece for any
// area that fits, so an ordinary request is a single download, unchanged.
func Plan(cacheDir string, b BBox) (pieces []BBox, ref Delivery, err error) {
	cx, cy := (b.W+b.E)/2, (b.S+b.N)/2
	probe, err := Fetch(cacheDir, BBox{W: cx - 0.01, S: cy - 0.01, E: cx + 0.01, N: cy + 0.01}, LayerTopo)
	if err != nil {
		return nil, ref, fmt.Errorf("gmrt resolution probe: %w", err)
	}
	dx, dy, _, _, err := CellDeg(probe.File)
	if err != nil {
		return nil, ref, err
	}
	ref = Delivery{Request: "resolution=max", ReferenceCellM: CellM(dx, dy, cy)}
	nodes := (b.E - b.W) / dx * ((b.N - b.S) / dy)
	if nodes <= NodeBudget {
		return []BBox{b}, ref, nil
	}
	n := int(math.Ceil(nodes / NodeBudget))
	nx := max(1, int(math.Ceil(math.Sqrt(float64(n)*(b.E-b.W)/(b.N-b.S)))))
	ny := max(1, int(math.Ceil(float64(n)/float64(nx))))
	ov := 4 * math.Max(dx, dy)
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			p := BBox{
				W: b.W + (b.E-b.W)*float64(i)/float64(nx), E: b.W + (b.E-b.W)*float64(i+1)/float64(nx),
				S: b.S + (b.N-b.S)*float64(j)/float64(ny), N: b.S + (b.N-b.S)*float64(j+1)/float64(ny),
			}
			if i > 0 {
				p.W -= ov
			}
			if i < nx-1 {
				p.E += ov
			}
			if j > 0 {
				p.S -= ov
			}
			if j < ny-1 {
				p.N += ov
			}
			pieces = append(pieces, p)
		}
	}
	return pieces, ref, nil
}

// Add records a received piece: its cell size (ground metres) is compared with the reference.
func (d *Delivery) Add(cellM float64) {
	d.Pieces++
	if d.Pieces == 1 || cellM < d.CellMMin {
		d.CellMMin = cellM
	}
	if cellM > d.CellMMax {
		d.CellMMax = cellM
	}
	if cellM > 1.25*d.ReferenceCellM {
		d.Coarsened++
	}
}
