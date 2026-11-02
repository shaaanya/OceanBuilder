// Package cudem reads windows of NOAA's Continuously Updated DEM (CUDEM) 1/9 arc-second topobathy tiles from the
// anonymous AWS open-data bucket noaa-nos-coastal-lidar-pds with GDAL's /vsicurl range reads. Observed 2026-11-01
// (docs/research/source-probes.md): Cloud Optimized GeoTIFF, Float32, NoData -9999, heights NAVD88 (EPSG:5703),
// horizontal NAD83, 0.25 x 0.25 degree tiles of 200-350 MB, so whole tiles are never downloaded. US coasts and
// territories only. A tile index (urllist8483.txt) lists every tile; the region folder and the version in the file name
// cannot be derived from coordinates.
package cudem

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/shaaanya/OceanBuilder/internal/fetch"
	"github.com/shaaanya/OceanBuilder/internal/gdal"
)

// IndexURL is the list of tile URLs; tests point it at a local server.
var IndexURL = "https://noaa-nos-coastal-lidar-pds.s3.amazonaws.com/dem/NCEI_ninth_Topobathy_2014_8483/urllist8483.txt"

// MaxDegrees bounds the window per side: a 0.05 degree window took 34 s to read in the probe, so 0.5 degrees would take
// many minutes and several GB.
const MaxDegrees = 0.5

const (
	License     = "NOAA NCEI CUDEM: not subject to copyright protection within the United States; not to be used for navigation (ISO metadata 8483)"
	Attribution = "Cooperative Institute for Research in Environmental Sciences (CIRES) at the University of Colorado, Boulder. 2014: Continuously Updated Digital Elevation Model (CUDEM) - 1/9 Arc-Second Resolution Bathymetric-Topographic Tiles. NOAA National Centers for Environmental Information. https://doi.org/10.25921/ds9v-ky35"
)

// Tile is one entry of the index. A tile covers [West, West+TileDeg] x [North-TileDeg, North] (the file has a further
// 6 pixel buffer).
type Tile struct {
	URL         string
	West, North float64
}

const TileDeg = 0.25

var nameRE = regexp.MustCompile(`_n(\d+)x(\d+)_([we])(\d+)x(\d+)_`)

// ParseIndex reads the tile list; lines that do not name a tile are ignored.
func ParseIndex(text string) []Tile {
	var out []Tile
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		m := nameRE.FindStringSubmatch(line)
		if m == nil || !strings.HasSuffix(line, ".tif") {
			continue
		}
		// n24x75 = 24.75, w082x00 = -82.00
		north, _ := strconv.ParseFloat(m[1]+"."+m[2], 64)
		west, _ := strconv.ParseFloat(m[4]+"."+m[5], 64)
		if m[3] == "w" {
			west = -west
		}
		out = append(out, Tile{URL: line, West: west, North: north})
	}
	return out
}

// Select returns the tiles that intersect the box.
func Select(ts []Tile, w, s, e, n float64) []Tile {
	var out []Tile
	for _, t := range ts {
		if t.West < e && t.West+TileDeg > w && t.North-TileDeg < n && t.North > s {
			out = append(out, t)
		}
	}
	return out
}

// Fetch cuts the window w,s,e,n out of the tiles into a GeoTIFF (EPSG:4269, heights NAVD88 metres, nodata -9999).
func Fetch(cacheDir string, w, s, e, n float64) (*fetch.Provenance, error) {
	if e-w > MaxDegrees || n-s > MaxDegrees {
		return nil, fmt.Errorf("cudem: window %.3f x %.3f degrees is larger than the %.1f degree limit (windows are read over HTTP at roughly 0.3 MB/s); use a smaller world or a local file", e-w, n-s, MaxDegrees)
	}
	idx, err := fetch.Get(cacheDir, "cudem-index", IndexURL, nil, "urllist.txt", License, Attribution)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(idx.File)
	if err != nil {
		return nil, err
	}
	tiles := Select(ParseIndex(string(b)), w, s, e, n)
	if len(tiles) == 0 {
		return nil, fmt.Errorf("cudem: no 1/9 arc-second tile covers %.4f,%.4f,%.4f,%.4f (US coasts and territories only; index sha256 %s)", w, s, e, n, idx.SHA256[:12])
	}
	var urls []string
	for _, t := range tiles {
		urls = append(urls, "/vsicurl/"+t.URL)
	}
	desc := fmt.Sprintf("%s (index sha256 %s): %d tile(s), window %.5f,%.5f,%.5f,%.5f", IndexURL, idx.SHA256[:12], len(tiles), w, s, e, n)
	key := fmt.Sprintf("cudem|%v|%.6f,%.6f,%.6f,%.6f", urls, w, s, e, n)
	ps, err := fetch.Derived(cacheDir, "cudem", key, desc, License, Attribution, []string{"cudem.tif"}, func(dir string) error {
		// The tiles carry a compound CRS (NAD83 + NAVD88 height); naming the horizontal CRS on both sides keeps GDAL from
		// converting heights. NAVD88 is declared in the world config instead.
		args := []string{"-q", "-overwrite", "-s_srs", "EPSG:4269", "-t_srs", "EPSG:4269", "-te", f(w), f(s), f(e), f(n), "-r", "near",
			"-srcnodata", "-9999", "-dstnodata", "-9999", "-ot", "Float32", "-of", "GTiff", "-co", "COMPRESS=DEFLATE"}
		args = append(args, urls...)
		args = append(args, filepath.Join(dir, "cudem.tif"))
		_, err := gdal.Run("gdalwarp", args...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ps[0], nil
}

func f(v float64) string { return fmt.Sprintf("%.9f", v) }
