// Package etopo reads windows of NOAA NCEI's ETOPO 2022 15 arc-second global relief model (static GeoTIFF tiles of
// 15 x 15 degrees, no authentication) with GDAL's /vsicurl range reads. Observed 2026-11-01, see
// docs/research/source-probes.md: tiles are 3600 x 3600 Float32, NoData -99999, heights relative to the EGM2008 geoid,
// land positive.
package etopo

import (
	"fmt"
	"math"
	"path/filepath"

	"github.com/shaaanya/OceanBuilder/internal/fetch"
	"github.com/shaaanya/OceanBuilder/internal/gdal"
)

// BaseURL is the directory of the 15 arc-second sets; tests point it at a local server.
var BaseURL = "https://www.ngdc.noaa.gov/mgg/global/relief/ETOPO2022/data/15s"

const (
	NoData      = -99999
	License     = "NOAA NCEI ETOPO 2022: the product page states 'no copyright or security distribution restrictions'; terms not independently reviewed by this tool"
	Attribution = "NOAA National Centers for Environmental Information. 2022: ETOPO 2022 15 Arc-Second Global Relief Model. NOAA National Centers for Environmental Information. DOI: 10.25921/fd45-gt74"
)

// TileName returns the file name of the 15 degree tile whose north-west corner is (west, top), e.g. N60W015.
func TileName(west, top int, set string) string {
	ns, ew := "N", "E"
	if top < 0 {
		ns, top = "S", -top
	}
	if west < 0 {
		ew, west = "W", -west
	}
	return fmt.Sprintf("ETOPO_2022_v1_15s_%s%02d%s%03d_%s.tif", ns, top, ew, west, set)
}

// Tiles lists the (west, top) corners of the tiles that cover the box.
func Tiles(w, s, e, n float64) [][2]int {
	var out [][2]int
	for top := int(math.Ceil(n/15)) * 15; top > int(math.Floor(s/15))*15; top -= 15 {
		for west := int(math.Floor(w/15)) * 15; float64(west) < e; west += 15 {
			out = append(out, [2]int{west, top})
		}
	}
	return out
}

func urls(w, s, e, n float64, dir, set string) []string {
	var out []string
	for _, t := range Tiles(w, s, e, n) {
		out = append(out, "/vsicurl/"+BaseURL+"/"+dir+"/"+TileName(t[0], t[1], set))
	}
	return out
}

// Fetch cuts the surface-elevation window w,s,e,n out of the tiles into a GeoTIFF in the cache (EPSG:4326, nodata
// -99999). The returned provenance names the tile URLs; the sha256 is that of the window.
func Fetch(cacheDir string, w, s, e, n float64) (*fetch.Provenance, error) {
	src := urls(w, s, e, n, "15s_surface_elev_gtif", "surface")
	desc := fmt.Sprintf("%s/15s_surface_elev_gtif/ (%d tile(s)) window %.5f,%.5f,%.5f,%.5f", BaseURL, len(src), w, s, e, n)
	key := fmt.Sprintf("etopo-surface|%v|%.6f,%.6f,%.6f,%.6f", src, w, s, e, n)
	ps, err := fetch.Derived(cacheDir, "etopo2022", key, desc, License, Attribution, []string{"etopo2022.tif"}, func(dir string) error {
		// -s_srs/-t_srs EPSG:4326: the files carry a compound CRS (WGS 84 + EGM2008 height); naming the horizontal CRS on both
		// sides keeps GDAL from converting heights. The datum is declared in the world config instead.
		args := []string{"-q", "-overwrite", "-s_srs", "EPSG:4326", "-t_srs", "EPSG:4326", "-te", f(w), f(s), f(e), f(n), "-r", "near", "-srcnodata", "-99999", "-dstnodata", "-99999",
			"-ot", "Float32", "-of", "GTiff", "-co", "COMPRESS=DEFLATE"}
		args = append(args, src...)
		args = append(args, filepath.Join(dir, "etopo2022.tif"))
		_, err := gdal.Run("gdalwarp", args...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ps[0], nil
}

func f(v float64) string { return fmt.Sprintf("%.9f", v) }
