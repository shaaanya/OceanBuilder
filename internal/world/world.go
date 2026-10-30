// Package world holds the engine-independent world model: configuration,
// tile addressing, and the manifest written by `ocean build`.
package world

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ocean-compiler/ocean/internal/fetch"
)

const (
	SchemaVersion = 1
	ToolVersion   = "0.1.0"
	ManifestFile  = "world.manifest.json"
)

// Config is the user-facing world description (world.json).
type Config struct {
	Name       string     `json:"name"`
	BBox       [4]float64 `json:"bbox"`                  // west, south, east, north (WGS84 degrees)
	CellM      float64    `json:"cell_m"`                // output grid spacing in metres
	TileCells  int        `json:"tile_cells"`            // cells per tile edge; tiles hold TileCells+1 samples
	Resampling string     `json:"resampling,omitempty"`  // gdalwarp -r method, default bilinear
	DepthBands []float64  `json:"depth_bands,omitempty"` // upper bounds of depth classes in metres
	Coastline  string     `json:"coastline,omitempty"`   // "osm" (default) or "none"
	Local      *Local     `json:"local,omitempty"`       // use files on disk instead of downloading
	// CoastConform "osm" makes the land/water boundary follow the OSM coastline within CoastConformMaxM
	// of it (see conform.go); empty keeps the DEM's own zero contour.
	CoastConform     string  `json:"coast_conform,omitempty"`
	CoastConformMaxM float64 `json:"coast_conform_max_m,omitempty"` // default 300
	// Seamarks adds OpenSeaMap features (buoys, lights, harbours, wrecks...) from OSM as seamarks.geojson.
	Seamarks bool `json:"seamarks,omitempty"`
	// Refinement is the path of a refinement.json from `ocean libreterrain import`, relative
	// to this config file. Its land-elevation edits are applied on top of the sources.
	Refinement string `json:"refinement,omitempty"`
	dir        string // directory of the config file; resolves Refinement
}

// Local points at user-provided data. Topo is a GeoTIFF of elevation in metres
// (negative below sea level) in any CRS GDAL can read; Mask (optional) is the same
// grid with NaN where the value is not survey-backed; Coastline is optional GeoJSON
// LineStrings in EPSG:4326. License and Attribution are mandatory: they travel into the manifest.
type Local struct {
	Topo        string `json:"topo"`
	Mask        string `json:"mask,omitempty"`
	Coastline   string `json:"coastline,omitempty"`
	License     string `json:"license"`
	Attribution string `json:"attribution"`
}

// LoadConfig reads and validates a world config.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.dir = filepath.Dir(path)
	return &c, c.Normalize()
}

// Normalize applies defaults and rejects invalid values.
func (c *Config) Normalize() error {
	if c.Name == "" {
		return fmt.Errorf("config: name is required")
	}
	w, s, e, n := c.BBox[0], c.BBox[1], c.BBox[2], c.BBox[3]
	if !(w < e && s < n && w >= -180 && e <= 180 && s >= -85 && n <= 85) {
		return fmt.Errorf("config: bbox must be [west,south,east,north] within lon ±180, lat ±85 with west<east, south<north")
	}
	if c.CellM <= 0 {
		return fmt.Errorf("config: cell_m must be > 0")
	}
	if c.TileCells == 0 {
		c.TileCells = 256
	}
	if c.TileCells < 2 || c.TileCells&(c.TileCells-1) != 0 {
		return fmt.Errorf("config: tile_cells must be a power of two (Unity heightmaps need 2^n+1 samples), got %d", c.TileCells)
	}
	if c.Resampling == "" {
		c.Resampling = "bilinear"
	}
	switch c.Resampling {
	case "near", "bilinear", "cubic", "average":
	default:
		return fmt.Errorf("config: resampling %q not one of near|bilinear|cubic|average", c.Resampling)
	}
	if len(c.DepthBands) == 0 {
		c.DepthBands = []float64{5, 20, 50, 200}
	}
	for i := 1; i < len(c.DepthBands); i++ {
		if c.DepthBands[i] <= c.DepthBands[i-1] {
			return fmt.Errorf("config: depth_bands must be strictly increasing")
		}
	}
	if c.Local != nil && (c.Local.Topo == "" || c.Local.License == "" || c.Local.Attribution == "") {
		return fmt.Errorf("config: local needs topo, license and attribution")
	}
	if c.Coastline == "" {
		c.Coastline = "osm"
		if c.Local != nil && c.Local.Coastline == "" {
			c.Coastline = "none"
		}
	}
	if c.Coastline != "osm" && c.Coastline != "none" {
		return fmt.Errorf("config: coastline must be osm or none")
	}
	switch c.CoastConform {
	case "":
	case "osm":
		if c.Coastline == "none" {
			return fmt.Errorf("config: coast_conform needs a coastline (coastline is none)")
		}
		if c.CoastConformMaxM == 0 {
			c.CoastConformMaxM = 300
		}
		if c.CoastConformMaxM < c.CellM || c.CoastConformMaxM > 2000 {
			return fmt.Errorf("config: coast_conform_max_m must be between cell_m and 2000, got %g", c.CoastConformMaxM)
		}
	default:
		return fmt.Errorf("config: coast_conform must be empty or osm")
	}
	return nil
}

// TileSpanM is the ground size of one tile edge in metres.
func (c *Config) TileSpanM() float64 { return c.CellM * float64(c.TileCells) }

// Manifest describes a built world. It is the contract between the Go core
// and engine importers.
type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	BuildID       string `json:"build_id"` // sha256 over config + input hashes + tool version
	Tool          struct {
		Version string `json:"version"`
		GDAL    string `json:"gdal"`
	} `json:"tool"`
	Config   Config `json:"config"`
	CRS      CRS    `json:"crs"`
	Vertical struct {
		Unit  string `json:"unit"`
		Datum string `json:"datum"`
		Note  string `json:"note"`
	} `json:"vertical"`
	Grid         Grid               `json:"grid"`
	Height       HeightEncoding     `json:"height_encoding"`
	DepthClasses []DepthClass       `json:"depth_classes"`
	Sources      []fetch.Provenance `json:"sources"`
	Resampling   Resampling         `json:"resampling"`
	Tiles        []Tile             `json:"tiles"`
	Stats        Stats              `json:"stats"`
	Refinement   *RefineStats       `json:"refinement,omitempty"`
	Checkpoints  []Checkpoint       `json:"checkpoints"`
	Files        map[string]string  `json:"files"`
	Attribution  []string           `json:"attribution"`
}

// Checkpoint is a world vertex with its expected elevation as stored in the
// tiles, so an engine importer can verify orientation and placement independently.
type Checkpoint struct {
	Name       string  `json:"name"`
	Easting    float64 `json:"easting"`
	Northing   float64 `json:"northing"`
	ElevationM float64 `json:"elevation_m"`
}

type CRS struct {
	EPSG int    `json:"epsg"`
	Name string `json:"name"`
}

type Grid struct {
	CellM          float64    `json:"cell_m"`
	TileCells      int        `json:"tile_cells"`
	SamplesPerTile int        `json:"samples_per_tile"`
	TileSpanM      float64    `json:"tile_span_m"`
	TilesX         int        `json:"tiles_x"`
	TilesY         int        `json:"tiles_y"`
	OriginE        float64    `json:"origin_easting"`      // west edge of tile column 0 (vertex position)
	OriginN        float64    `json:"origin_northing_top"` // north edge of tile row 0
	FirstTileX     int        `json:"first_tile_x"`        // absolute index floor(E/span) of column 0
	FirstTileY     int        `json:"first_tile_y"`        // absolute index floor(N/span) of the SOUTHERNMOST row
	BBoxWGS84      [4]float64 `json:"bbox_wgs84"`          // covered area, W,S,E,N
}

type HeightEncoding struct {
	Bits      int     `json:"bits"`
	ByteOrder string  `json:"byte_order"`
	RowOrder  string  `json:"row_order"`
	MinM      float64 `json:"min_m"`
	MaxM      float64 `json:"max_m"`
	Note      string  `json:"note"`
}

type DepthClass struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	MinDepth *float64 `json:"min_depth_m,omitempty"`
	MaxDepth *float64 `json:"max_depth_m,omitempty"`
}

type Resampling struct {
	Method      string  `json:"method"`
	SourceResM  float64 `json:"source_native_res_m"` // mean ground size of one delivered GMRT cell
	FillResM    float64 `json:"gebco_fill_res_m"`    // true resolution of water cells without survey data (GEBCO_08, 30")
	OutputCellM float64 `json:"output_cell_m"`
	Upsampled   bool    `json:"upsampled"` // output finer than source: values are interpolated, not measured
}

// Tile is one entry in the tile index. ID is deterministic from position.
type Tile struct {
	ID        string     `json:"id"`
	Level     int        `json:"level"`
	X         int        `json:"x"` // absolute index floor(E/span)
	Y         int        `json:"y"` // absolute index floor(N/span)
	Col       int        `json:"col"`
	Row       int        `json:"row"`          // 0 = north
	BoundsUTM [4]float64 `json:"bounds_utm"`   // minE,minN,maxE,maxN
	BoundsWGS [4]float64 `json:"bounds_wgs84"` // W,S,E,N of tile corners (envelope)
	MinM      float64    `json:"min_m"`
	MaxM      float64    `json:"max_m"`
	NoData    float64    `json:"nodata_fraction"`
	Water     float64    `json:"water_fraction"`
	Measured  float64    `json:"measured_water_fraction"` // share of water cells backed by survey data (GMRT mask)
	Height    string     `json:"height"`
	Depth     string     `json:"depth_class"`
	Shore     string     `json:"shore_mask"`
	SHA256    string     `json:"height_sha256"`
}

// Stats are world-level numbers computed from the data.
type Stats struct {
	Cells          int           `json:"cells"`
	NoDataFraction float64       `json:"nodata_fraction"`
	WaterFraction  float64       `json:"water_fraction"`
	MeasuredWater  float64       `json:"measured_water_fraction"`
	ShoreCells     int           `json:"shore_cells"`
	CoastlineCheck *CoastCheck   `json:"coastline_check,omitempty"`
	CoastlineNote  string        `json:"coastline_note,omitempty"`
	Seamarks       *SeamarkStats `json:"seamarks,omitempty"`
	CoastConform   *ConformStats `json:"coast_conform,omitempty"`
}

// SeamarkStats summarises seamarks.geojson. Floating marks (buoys, light vessels) that the DEM
// puts on land point to a coastline/DEM offset or a mis-mapped feature; it is a cross-check, not a correction.
type SeamarkStats struct {
	Count          int            `json:"count"`
	OutsideGrid    int            `json:"dropped_outside_grid"`
	ByType         map[string]int `json:"by_type"`
	Floating       int            `json:"floating_marks"`
	FloatingOnLand int            `json:"floating_marks_on_dem_land"`
	Note           string         `json:"note,omitempty"`
}

// CoastCheck compares DEM elevation sampled along the OSM coastline with 0 m.
type CoastCheck struct {
	Points        int     `json:"points_sampled"`
	Outside       int     `json:"points_outside_grid"`
	MedianM       float64 `json:"median_dem_elevation_m"`
	P10M          float64 `json:"p10_dem_elevation_m"`
	P90M          float64 `json:"p90_dem_elevation_m"`
	SearchRadiusM float64 `json:"search_radius_m"`
	NoShoreWithin float64 `json:"fraction_without_dem_shoreline_in_radius"`
	OffsetMedianM float64 `json:"horizontal_offset_median_m"` // OSM coastline to nearest DEM land/water boundary cell
	OffsetP90M    float64 `json:"horizontal_offset_p90_m"`
}

// LoadManifest reads a built world's manifest from its output directory.
func LoadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	return &m, nil
}
