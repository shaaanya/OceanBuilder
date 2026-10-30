package world

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ocean-compiler/ocean/internal/fetch"
	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/internal/osm"
)

// floatingSeamark reports marks that sit on the water surface by definition.
func floatingSeamark(t string) bool {
	return strings.HasPrefix(t, "buoy_") || t == "light_float" || t == "light_vessel"
}

// seamarks fetches OSM seamarks for bbox (W,S,E,N: the tile grid's envelope), writes seamarks.geojson (WGS84 points
// with grid coordinates and the DEM elevation under each) and returns summary stats.
func seamarks(bbox [4]float64, srs string, dem *gdal.Raster, g *Grid, W, H int, opt Options) (*fetch.Provenance, *SeamarkStats, error) {
	prov, sm, err := osm.FetchSeamarks(opt.CacheDir, bbox[0], bbox[1], bbox[2], bbox[3])
	if err != nil {
		return nil, nil, err
	}
	pts := make([][2]float64, len(sm))
	for i, s := range sm {
		pts[i] = [2]float64{s.Lon, s.Lat}
	}
	utm := [][2]float64{}
	if len(pts) > 0 {
		if utm, err = gdal.Transform("EPSG:4326", srs, pts); err != nil {
			return nil, nil, err
		}
	}
	st := &SeamarkStats{ByType: map[string]int{}, Note: "OpenSeaMap tagging is volunteer-mapped and not a nautical chart; NOT FOR NAVIGATION."}
	feats := []map[string]any{}
	for i, s := range sm {
		e, n := utm[i][0], utm[i][1]
		col, row := (e-g.OriginE)/g.CellM, (g.OriginN-n)/g.CellM
		if col < 0 || row < 0 || col > float64(W-1) || row > float64(H-1) {
			st.OutsideGrid++
			continue
		}
		props := map[string]any{"osm_type": s.OSMType, "osm_id": s.OSMID, "seamark_type": s.Type,
			"easting": e, "northing": n, "tags": s.Tags}
		if s.Name != "" {
			props["name"] = s.Name
		}
		if z, ok := sampleBilinear(dem, col, row); ok {
			props["dem_elevation_m"] = z
			if floatingSeamark(s.Type) {
				st.Floating++
				if z >= 0 {
					st.FloatingOnLand++
				}
			}
		} else if floatingSeamark(s.Type) {
			st.Floating++
		}
		feats = append(feats, map[string]any{"type": "Feature", "properties": props,
			"geometry": map[string]any{"type": "Point", "coordinates": [2]float64{s.Lon, s.Lat}}})
		st.ByType[s.Type]++
	}
	st.Count = len(feats)
	b, err := json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
	if err != nil {
		return nil, nil, err
	}
	return prov, st, os.WriteFile(filepath.Join(opt.OutDir, "seamarks.geojson"), b, 0o644)
}
