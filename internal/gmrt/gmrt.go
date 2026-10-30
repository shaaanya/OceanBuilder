// Package gmrt fetches Global Multi-Resolution Topography grids.
// GMRT merges land DEMs and multibeam bathymetry at up to ~1 m resolution
// and exposes a mask layer marking cells backed by measured (non-interpolated)
// data. See docs/research/data-sources.md.
package gmrt

import (
	"fmt"
	"net/url"

	"github.com/ocean-compiler/ocean/internal/fetch"
)

const endpoint = "https://www.gmrt.org/services/GridServer"

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
	return fetch.Get(cacheDir, "gmrt", endpoint+"?"+q.Encode(), nil, layer+".tif",
		"CC BY 4.0 (https://www.gmrt.org/about/terms_of_use.php); data must not be used for navigation",
		"Ryan, W.B.F. et al. (2009), Global Multi-Resolution Topography (GMRT) synthesis data set, Geochem. Geophys. Geosyst., 10, Q03014, doi:10.1029/2008GC002332; data doi:10.1594/IEDA.100001")
}
