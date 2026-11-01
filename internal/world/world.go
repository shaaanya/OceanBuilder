// Package world holds the engine-independent world model: configuration,
// tile addressing, and the manifest written by `ocean build`.
package world

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ocean-compiler/ocean/internal/emodnet"
	"github.com/ocean-compiler/ocean/internal/fetch"
)

const (
	SchemaVersion = 2
	ToolVersion   = "0.3.0-dev"
	ManifestFile  = "world.manifest.json"
)

// Algorithm versions of the optional steps that change output. They are hashed into the build id only when
// the step is active (so worlds without them keep their ids). Bump one whenever its logic changes the output.
const (
	ConformAlgo    = 2 // 1 was the discarded flood-fill labelling; 2 is nearest-coastline-sample sides
	SeamarksAlgo   = 1
	RefinementAlgo = 1
	RegionalAlgo   = 2 // 2: blend ramp from kept cells
	FormatAlgo     = 2 // tile/layer encoding and LOD method; bump when the written bytes change for the same inputs
)

// Projection distortion limits: the point scale factor of the UTM grid may differ from 1 by this much over the bbox.
const (
	WarnUTMScaleError = 0.0015 // about 3.7 degrees from the central meridian at the equator
	MaxUTMScaleError  = 0.005  // about 6.8 degrees; beyond this the grid is not a sailing-scale metric grid
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
	// RegionalBathymetry "emodnet" replaces GEBCO-filled water cells with the ~115 m EMODnet DTM (see regional.go).
	// Not available with local sources.
	RegionalBathymetry string `json:"regional_bathymetry,omitempty"`
	// RegionalRelease pins a dated EMODnet coverage ("2016".."2022"); empty uses the service's current,
	// unversioned coverage, which changes when EMODnet publishes.
	RegionalRelease string `json:"regional_release,omitempty"`
	// CoastConform "osm" makes the land/water boundary follow the OSM coastline within CoastConformMaxM
	// of it (see conform.go); empty keeps the DEM's own zero contour.
	CoastConform     string  `json:"coast_conform,omitempty"`
	CoastConformMaxM float64 `json:"coast_conform_max_m,omitempty"` // default 300
	// Seamarks adds OpenSeaMap features (buoys, lights, harbours, wrecks...) from OSM as seamarks.geojson.
	Seamarks bool `json:"seamarks,omitempty"`
	// LODLevels is the number of coarser levels above level 0: 0 = as many as needed until one tile covers the
	// world, -1 = level 0 only, n>0 = exactly n (at most MaxLOD). Levels are vertex decimations of level 0.
	LODLevels int `json:"lod_levels,omitempty"`
	// UTMEPSG forces the projected CRS (32601..32660 north, 32701..32760 south). Empty/0 picks the zone of the
	// bbox centre. A bbox that reaches far from the zone's central meridian is distorted: see Stats.UTMScale.
	UTMEPSG int `json:"utm_epsg,omitempty"`
	// Refinement is the path of a refinement.json from `ocean libreterrain import`, relative
	// to this config file. Its land-elevation edits are applied on top of the sources.
	Refinement string `json:"refinement,omitempty"`
	// Sources lists the elevation datasets of the world. With several, each cell takes its value from the first
	// dataset (in Precedence order) that has a valid value there, and the manifest records which one (see compose.go).
	// Empty means the legacy form: GMRT, or Local when set.
	Sources []SourceSpec `json:"sources,omitempty"`
	// Precedence is "listed" (default: the order of Sources) or "auto" (ranked by quality class, declared accuracy,
	// native resolution, vintage and finally list order; `ocean build` logs the resulting order).
	Precedence string `json:"precedence,omitempty"`
	// Vertical states the vertical reference of the output; see vertical.go. Empty: the datasets must agree.
	Vertical *VerticalTarget `json:"vertical,omitempty"`
	dir      string          // directory of the config file; resolves Refinement
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
	if c.UTMEPSG != 0 && !(c.UTMEPSG >= 32601 && c.UTMEPSG <= 32660 || c.UTMEPSG >= 32701 && c.UTMEPSG <= 32760) {
		return fmt.Errorf("config: utm_epsg %d is not a WGS 84 / UTM code (326zz or 327zz)", c.UTMEPSG)
	}
	if c.LODLevels < -1 || c.LODLevels > MaxLOD {
		return fmt.Errorf("config: lod_levels must be -1 (none), 0 (auto) or 1..%d, got %d", MaxLOD, c.LODLevels)
	}
	if err := c.normalizeSources(); err != nil {
		return err
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
	switch c.RegionalBathymetry {
	case "":
	case "emodnet":
		if c.Local != nil {
			return fmt.Errorf("config: regional_bathymetry is not available with local sources")
		}
	default:
		return fmt.Errorf("config: regional_bathymetry must be empty or emodnet")
	}
	if c.RegionalRelease != "" {
		if c.RegionalBathymetry != "emodnet" {
			return fmt.Errorf("config: regional_release needs regional_bathymetry emodnet")
		}
		if _, ok := emodnet.CoverageID(c.RegionalRelease); !ok {
			return fmt.Errorf("config: regional_release %q not one of %v", c.RegionalRelease, emodnet.Releases)
		}
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
	InputKey     string             `json:"input_key"` // hash of config, tool/algorithm versions and every resolved input: equal keys mean equal outputs
	Config       Config             `json:"config"`
	CRS          CRS                `json:"crs"`
	Vertical     VerticalInfo       `json:"vertical"`
	Grid         Grid               `json:"grid"`
	Height       HeightEncoding     `json:"height_encoding"`
	DepthClasses []DepthClass       `json:"depth_classes"`
	SourceLayers []SourceLayer      `json:"source_layers"`
	LOD          LODInfo            `json:"lod"`
	Sources      []fetch.Provenance `json:"sources"`
	Datasets     []DatasetInfo      `json:"datasets,omitempty"` // elevation datasets and processing steps that supplied samples; see tiles/*/*.dataset.png
	// FormatRevision counts additive changes within schema_version 2: 1 adds the datasets list and the per-tile
	// dataset layer. Readers that do not know a revision ignore what it adds.
	FormatRevision int               `json:"format_revision,omitempty"`
	Resampling     Resampling        `json:"resampling"`
	Tiles          []Tile            `json:"tiles"`
	Stats          Stats             `json:"stats"`
	Refinement     *RefineStats      `json:"refinement,omitempty"`
	Checkpoints    []Checkpoint      `json:"checkpoints"`
	Files          map[string]string `json:"files"`
	Attribution    []string          `json:"attribution"`
}

// Checkpoint is a world vertex with its expected elevation as stored in the
// tiles, so an engine importer can verify orientation and placement independently.
type Checkpoint struct {
	Name       string  `json:"name"`
	Easting    float64 `json:"easting"`
	Northing   float64 `json:"northing"`
	ElevationM float64 `json:"elevation_m"`
}

// VerticalInfo states the vertical reference of the heights. Per-dataset operations are in datasets[].vertical.
type VerticalInfo struct {
	Unit  string `json:"unit"`
	Datum string `json:"datum"` // one datum from the vocabulary, "mixed" (datasets differ, accepted explicitly) or "unknown"
	Note  string `json:"note"`
	Mixed bool   `json:"mixed,omitempty"`
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

// HeightEncoding describes the tile height files: raw float32 metres, no quantisation. MinM/MaxM are the range of
// valid samples at level 0 (informational: engines that need integer heights choose their own scaling from it).
type HeightEncoding struct {
	Type      string  `json:"type"` // "float32"
	ByteOrder string  `json:"byte_order"`
	RowOrder  string  `json:"row_order"`
	Unit      string  `json:"unit"`
	NoData    float64 `json:"nodata"`
	MinM      float64 `json:"min_m"`
	MaxM      float64 `json:"max_m"`
	Note      string  `json:"note"`
}

// LODInfo describes the level pyramid.
type LODInfo struct {
	Method string     `json:"method"` // "vertex-decimation"
	Levels []LODLevel `json:"levels"`
	Note   string     `json:"note"`
}

// LODLevel is the layout of one level. Cell size and tile span double per level; every tile has the same sample count.
type LODLevel struct {
	Level     int     `json:"level"`
	CellM     float64 `json:"cell_m"`
	TileSpanM float64 `json:"tile_span_m"`
	FirstX    int     `json:"first_tile_x"`
	LastX     int     `json:"last_tile_x"`
	FirstY    int     `json:"first_tile_y"`
	LastY     int     `json:"last_tile_y"`
	Tiles     int     `json:"tiles"`
}

type DepthClass struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	MinDepth *float64 `json:"min_depth_m,omitempty"`
	MaxDepth *float64 `json:"max_depth_m,omitempty"`
}

type Resampling struct {
	Method      string  `json:"method"`
	SourceResM  float64 `json:"source_native_res_m"`        // mean ground size of one delivered GMRT cell
	FillResM    float64 `json:"unmeasured_res_m,omitempty"` // true resolution of water cells without survey data, when the source documents it (GMRT: GEBCO 30 arc-seconds)
	OutputCellM float64 `json:"output_cell_m"`
	Upsampled   bool    `json:"upsampled"` // output finer than source: values are interpolated, not measured
}

// Tile is one entry in the tile index. ID is deterministic from level and position.
type Tile struct {
	ID         string            `json:"id"` // L<level>_<x>_<y>
	Level      int               `json:"level"`
	X          int               `json:"x"` // absolute index floor(E / tile span of this level)
	Y          int               `json:"y"` // absolute index floor(N / tile span of this level)
	Col        int               `json:"col"`
	Row        int               `json:"row"`          // 0 = north, within this level's tile grid
	BoundsUTM  [4]float64        `json:"bounds_utm"`   // minE,minN,maxE,maxN
	BoundsWGS  [4]float64        `json:"bounds_wgs84"` // W,S,E,N of tile corners (envelope)
	MinM       float64           `json:"min_m"`
	MaxM       float64           `json:"max_m"`
	NoData     float64           `json:"nodata_fraction"`
	Water      float64           `json:"water_fraction"`
	Measured   float64           `json:"measured_water_fraction"` // share of water cells coded as survey in the source layer
	Height     string            `json:"height"`
	Depth      string            `json:"depth_class"`
	Shore      string            `json:"shore_mask"`
	Source     string            `json:"source"`
	Dataset    string            `json:"dataset,omitempty"` // dataset-index layer (revision 1)
	SHA256     TileHashes        `json:"sha256"`
	Parent     string            `json:"parent,omitempty"`     // id of the tile one level up that contains this one
	Neighbours map[string]string `json:"neighbours,omitempty"` // n, s, e, w tile ids at the same level, when they exist
}

// Stats are world-level numbers computed from the data.
type Stats struct {
	Cells          int            `json:"cells"`
	NoDataFraction float64        `json:"nodata_fraction"`
	WaterFraction  float64        `json:"water_fraction"`
	MeasuredWater  float64        `json:"measured_water_fraction"`
	ShoreCells     int            `json:"shore_cells"`
	BySource       map[string]int `json:"cells_by_source"` // level-0 cell counts per source layer name
	UTMScaleMin    float64        `json:"utm_scale_min"`   // point scale factor of the grid over the bbox: 1 m on the grid is 1/k m on the ground
	UTMScaleMax    float64        `json:"utm_scale_max"`
	Notes          []string       `json:"notes,omitempty"` // build-time findings the validator repeats (e.g. projection distortion)
	CoastlineCheck *CoastCheck    `json:"coastline_check,omitempty"`
	CoastlineNote  string         `json:"coastline_note,omitempty"`
	Seamarks       *SeamarkStats  `json:"seamarks,omitempty"`
	CoastConform   *ConformStats  `json:"coast_conform,omitempty"`
	Regional       *RegionalStats `json:"regional_bathymetry,omitempty"`
	// Composition explains which datasets supplied the cells and how they agree where they overlap.
	Composition    *CompositionStats `json:"composition,omitempty"`
	CellsByDataset map[string]int    `json:"cells_by_dataset,omitempty"` // level-0 cells per dataset id in the final output
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

// TilesAt returns the tiles of one LOD level, in manifest order.
func (m *Manifest) TilesAt(level int) []Tile {
	var out []Tile
	for _, t := range m.Tiles {
		if t.Level == level {
			out = append(out, t)
		}
	}
	return out
}
