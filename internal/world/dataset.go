package world

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ocean-compiler/ocean/internal/cudem"
	"github.com/ocean-compiler/ocean/internal/emodnet"
	"github.com/ocean-compiler/ocean/internal/etopo"
	"github.com/ocean-compiler/ocean/internal/fetch"
	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/internal/gebco"
	"github.com/ocean-compiler/ocean/internal/gmrt"
)

// SourceSpec is one elevation dataset of a world, as written in the config's "sources" list.
type SourceSpec struct {
	ID   string `json:"id,omitempty"`   // unique name used in the manifest and logs; defaults to the type
	Type string `json:"type"`           // gmrt | geotiff | emodnet
	Path string `json:"path,omitempty"` // geotiff: file or http(s) URL of an elevation raster (metres, negative below the datum)
	Mask string `json:"mask,omitempty"` // geotiff: optional raster on the same grid, finite where the value is survey-backed
	// Part restricts which cells the dataset may supply: "all" (default), "survey" (only cells its mask marks as
	// survey-backed) or "background" (only the others). With gmrt or a masked geotiff this lets one fetched
	// dataset appear twice in the list with another dataset ranked between its survey part and its fill.
	Part    string `json:"part,omitempty"`
	Release string `json:"release,omitempty"` // emodnet: dated release ("2016".."2022"); empty is the service's current coverage
	// Quality classifies the water cells of the dataset in the source layer: "survey" (a measurement at about output
	// resolution: only claim this for data that is), "compiled" (default; a gridded compilation, resampled),
	// "regional" (a coarser regional compilation) or "background" (a global fill).
	Quality     string       `json:"quality,omitempty"`
	Vertical    *VerticalRef `json:"vertical,omitempty"`
	License     string       `json:"license,omitempty"`
	Attribution string       `json:"attribution,omitempty"`
	Year        int          `json:"year,omitempty"`       // vintage of the data, used by precedence "auto"
	AccuracyM   float64      `json:"accuracy_m,omitempty"` // documented vertical accuracy, used by precedence "auto"
	Nodata      *float64     `json:"nodata,omitempty"`     // geotiff: value to treat as missing when the file does not say
	Resampling  string       `json:"resampling,omitempty"` // overrides the world's resampling for this dataset
}

func (c *Config) normalizeSources() error {
	if len(c.Sources) == 0 {
		if c.Precedence != "" && c.Precedence != "listed" || c.Vertical != nil && c.Vertical.Datum == "" {
			return fmt.Errorf("config: precedence and vertical need a sources list")
		}
		return nil
	}
	if c.Local != nil || c.RegionalBathymetry != "" {
		return fmt.Errorf("config: sources replaces local and regional_bathymetry; list those datasets in sources instead")
	}
	switch c.Precedence {
	case "", "listed", "auto":
	default:
		return fmt.Errorf("config: precedence must be listed or auto, got %q", c.Precedence)
	}
	if c.Vertical != nil {
		if _, ok := lookupDatum(c.Vertical.Datum); !ok || c.Vertical.Datum == "" {
			return fmt.Errorf("config: vertical.datum %q unknown (known: %s)", c.Vertical.Datum, strings.Join(VerticalDatumNames(), ", "))
		}
	}
	seen := map[string]bool{}
	for i := range c.Sources {
		s := &c.Sources[i]
		if s.ID == "" {
			s.ID = s.Type
			if seen[s.ID] {
				s.ID = fmt.Sprintf("%s-%d", s.Type, i+1)
			}
		}
		if seen[s.ID] {
			return fmt.Errorf("config: sources[%d]: duplicate id %q", i, s.ID)
		}
		seen[s.ID] = true
		switch s.Type {
		case "gmrt":
			if s.Quality == "" {
				s.Quality = "compiled"
			}
		case "emodnet":
			if s.Release != "" {
				if _, ok := emodnet.CoverageID(s.Release); !ok {
					return fmt.Errorf("config: sources[%d]: release %q not one of %v", i, s.Release, emodnet.Releases)
				}
			}
			if s.Quality == "" {
				s.Quality = "regional"
			}
		case "etopo2022", "gebco", "cudem":
			if s.Quality == "" {
				s.Quality = "compiled"
			}
		case "geotiff":
			if s.Path == "" || s.License == "" || s.Attribution == "" {
				return fmt.Errorf("config: sources[%d] (%s): geotiff needs path, license and attribution", i, s.ID)
			}
			if s.Vertical == nil {
				return fmt.Errorf("config: sources[%d] (%s): geotiff needs a vertical reference; write {\"datum\": \"unknown\"} if the provider does not document one", i, s.ID)
			}
			if s.Quality == "" {
				s.Quality = "compiled"
			}
		default:
			return fmt.Errorf("config: sources[%d]: type %q not one of gmrt|geotiff|emodnet|etopo2022|gebco|cudem", i, s.Type)
		}
		switch s.Quality {
		case "survey", "compiled", "regional", "background":
		default:
			return fmt.Errorf("config: sources[%d] (%s): quality %q not one of survey|compiled|regional|background", i, s.ID, s.Quality)
		}
		switch s.Part {
		case "", "all":
		case "survey", "background":
			if s.Type == "emodnet" || s.Type == "etopo2022" || s.Type == "cudem" || s.Type == "geotiff" && s.Mask == "" {
				return fmt.Errorf("config: sources[%d] (%s): part %q needs a survey mask (gmrt, or a geotiff with mask)", i, s.ID, s.Part)
			}
		default:
			return fmt.Errorf("config: sources[%d] (%s): part must be all|survey|background", i, s.ID)
		}
		if s.Vertical != nil {
			if _, ok := lookupDatum(s.Vertical.Datum); !ok {
				return fmt.Errorf("config: sources[%d] (%s): vertical datum %q unknown (known: %s)", i, s.ID, s.Vertical.Datum, strings.Join(VerticalDatumNames(), ", "))
			}
		}
		switch s.Resampling {
		case "", "near", "bilinear", "cubic", "average":
		default:
			return fmt.Errorf("config: sources[%d] (%s): resampling %q not one of near|bilinear|cubic|average", i, s.ID, s.Resampling)
		}
	}
	return nil
}

// effectiveSources is the dataset list of the build: the config's sources, or the legacy single dataset (a local
// topo/mask pair, else GMRT) when none are given.
func (c *Config) effectiveSources() []SourceSpec {
	switch {
	case len(c.Sources) > 0:
		return c.Sources
	case c.Local != nil:
		return []SourceSpec{{ID: "local", Type: "geotiff", Path: c.Local.Topo, Mask: c.Local.Mask, Quality: "compiled",
			License: c.Local.License, Attribution: c.Local.Attribution, Vertical: &VerticalRef{Datum: "unknown"}}}
	}
	return []SourceSpec{{ID: "gmrt", Type: "gmrt", Quality: "compiled"}}
}

// dataset is a resolved SourceSpec: fetched, with provenance, warped to the world grid.
type dataset struct {
	idx        int // 0-based position in the manifest; the dataset layer stores idx+1
	spec       SourceSpec
	vals       []*fetch.Provenance // value raster(s): more than one only when a request had to be split
	masks      []*fetch.Provenance // survey mask raster(s)
	valSrc     []string            // files fed to gdalwarp (the provenance file, or its vertically shifted copy)
	nativeResM float64             // coarsest ground cell of the received rasters
	delivery   *gmrt.Delivery
	vref       VerticalRef
	vapplied   *VerticalApplied
	offsetM    float64
	srcSRS     string // explicit horizontal source CRS when the file embeds a vertical one
	valPath    string // warped full-grid values (float32, nodata -9999)
	maskPath   string // warped survey mask, nil-equivalent when ""
}

func (d *dataset) datumName() string {
	if d.vref.Datum == "" {
		return "unknown"
	}
	return d.vref.Datum
}

// DatasetInfo is the manifest entry of a dataset.
type DatasetInfo struct {
	Index        int              `json:"index"` // value stored in tiles/*/*.dataset.png
	ID           string           `json:"id"`
	Type         string           `json:"type"`
	Role         string           `json:"role"` // elevation | regional-fill | coastline | edit
	Part         string           `json:"part,omitempty"`
	Quality      string           `json:"quality,omitempty"`
	Year         int              `json:"year,omitempty"`
	AccuracyM    float64          `json:"accuracy_m,omitempty"`
	NativeResM   float64          `json:"native_res_m,omitempty"`
	Delivery     *gmrt.Delivery   `json:"delivery,omitempty"` // what the service actually returned (GMRT)
	Vertical     *VerticalApplied `json:"vertical,omitempty"`
	DeclaredDtm  string           `json:"declared_datum,omitempty"`
	Sources      []string         `json:"sources,omitempty"` // provenance source names in sources[]
	SourceSHA256 []string         `json:"source_sha256,omitempty"`
	Rank         int              `json:"precedence_rank,omitempty"` // 1 = tried first
}

// splitFetch lists the types whose fetchers take a plain WGS84 box in -180..180 and can therefore be called once per
// box when a world crosses the antimeridian (each call gets its own box, never a box that spans the globe).
var splitFetch = map[string]bool{"gmrt": true, "etopo2022": true, "gebco": true}

// resolveDatasets fetches every dataset of the config. boxes are the padded WGS84 request boxes: one, or two when the
// world crosses 180 (see fetchBoxes). The pieces of a source are kept as separate value rasters and mosaicked by the
// warp onto the world grid.
func resolveDatasets(cfg *Config, boxes []gmrt.BBox, opt Options) ([]*dataset, error) {
	var out []*dataset
	cache := map[string][2][]*fetch.Provenance{} // one fetch serves every entry of the same gmrt / masked source
	var gmrtDelivery *gmrt.Delivery
	for i, sp := range cfg.effectiveSources() {
		d := &dataset{idx: i, spec: sp}
		if len(boxes) > 1 && sp.Type != "geotiff" && !splitFetch[sp.Type] {
			return nil, fmt.Errorf("source %q (%s): the world crosses the antimeridian and this source cannot be requested across 180 (supported: gmrt, etopo2022, gebco, local geotiff)", sp.ID, sp.Type)
		}
		fb := boxes[0]
		switch sp.Type {
		case "gmrt":
			v, ok := cache["gmrt"]
			if !ok {
				dl := &gmrt.Delivery{}
				for bi, bx := range boxes {
					pieces, ref, err := gmrt.Plan(opt.CacheDir, bx)
					if err != nil {
						return nil, err
					}
					if bi == 0 || ref.ReferenceCellM < dl.ReferenceCellM {
						dl.Request, dl.ReferenceCellM = ref.Request, ref.ReferenceCellM
					}
					for _, pc := range pieces {
						opt.logf("fetching GMRT topo+mask for %.4f,%.4f,%.4f,%.4f", pc.W, pc.S, pc.E, pc.N)
						topo, err := gmrt.Fetch(opt.CacheDir, pc, gmrt.LayerTopo)
						if err != nil {
							return nil, err
						}
						mask, err := gmrt.Fetch(opt.CacheDir, pc, gmrt.LayerMask)
						if err != nil {
							return nil, err
						}
						dx, dy, _, _, err := gmrt.CellDeg(topo.File)
						if err != nil {
							return nil, err
						}
						dl.Add(gmrt.CellM(dx, dy, (pc.S+pc.N)/2))
						v[0], v[1] = append(v[0], topo), append(v[1], mask)
					}
				}
				if dl.Coarsened > 0 {
					dl.Note = fmt.Sprintf("%d of %d GMRT pieces came back more than 25%% coarser (%.0f m) than the finest cell the service offered at the centre of the area (%.0f m): the service reduced the resolution of those requests. source_res_m reports the coarsest piece.", dl.Coarsened, dl.Pieces, dl.CellMMax, dl.ReferenceCellM)
					opt.logf("WARNING: %s", dl.Note)
				}
				gmrtDelivery = dl
				cache["gmrt"] = v
			}
			d.delivery = gmrtDelivery
			d.vals, d.masks = v[0], v[1]
			d.vref = VerticalRef{Datum: "GMRT-mosaic"}
		case "emodnet":
			opt.logf("fetching EMODnet bathymetry")
			p, err := emodnet.Fetch(opt.CacheDir, sp.Release, fb.W, fb.S, fb.E, fb.N)
			if err != nil {
				return nil, fmt.Errorf("source %s: %w", sp.ID, err)
			}
			d.vals = []*fetch.Provenance{p}
			d.vref = VerticalRef{Datum: "unknown"}
		case "etopo2022":
			for _, bx := range boxes {
				opt.logf("reading ETOPO 2022 window %.4f,%.4f,%.4f,%.4f", bx.W, bx.S, bx.E, bx.N)
				p, err := etopo.Fetch(opt.CacheDir, bx.W, bx.S, bx.E, bx.N)
				if err != nil {
					return nil, fmt.Errorf("source %s: %w", sp.ID, err)
				}
				d.vals = append(d.vals, p)
			}
			d.vref = VerticalRef{Datum: "EGM2008"} // User Guide: heights relative to the EGM2008 geoid
		case "gebco":
			for _, bx := range boxes {
				opt.logf("requesting GEBCO subset %.4f,%.4f,%.4f,%.4f (download.gebco.net)", bx.W, bx.S, bx.E, bx.N)
				elev, mask, err := gebco.Fetch(opt.CacheDir, bx.W, bx.S, bx.E, bx.N)
				if err != nil {
					return nil, fmt.Errorf("source %s: %w", sp.ID, err)
				}
				d.vals, d.masks = append(d.vals, elev), append(d.masks, mask)
			}
			d.vref = VerticalRef{Datum: "MSL"} // GEBCO: "assumed to be referred to Mean Sea Level", shallow-water sources may differ
		case "cudem":
			opt.logf("reading CUDEM 1/9 arc-second window")
			p, err := cudem.Fetch(opt.CacheDir, fb.W, fb.S, fb.E, fb.N)
			if err != nil {
				return nil, fmt.Errorf("source %s: %w", sp.ID, err)
			}
			d.vals = []*fetch.Provenance{p}
			d.vref = VerticalRef{Datum: "NAVD88"}
		case "geotiff":
			key := "geotiff|" + sp.Path + "|" + sp.Mask
			v, ok := cache[key]
			if !ok {
				tn, mn := sp.ID+"-topo", sp.ID+"-mask"
				if cfg.Local != nil { // legacy form keeps its historical provenance names
					tn, mn = "topo", "mask"
				}
				topo, err := wrapLocal(tn, sp.Path, "topo.tif", opt.CacheDir, sp.License, sp.Attribution)
				if err != nil {
					return nil, fmt.Errorf("source %s: %w", sp.ID, err)
				}
				v = [2][]*fetch.Provenance{{topo}, nil}
				if sp.Mask != "" {
					mask, err := wrapLocal(mn, sp.Mask, "mask.tif", opt.CacheDir, sp.License, sp.Attribution)
					if err != nil {
						return nil, fmt.Errorf("source %s: %w", sp.ID, err)
					}
					v[1] = []*fetch.Provenance{mask}
				}
				cache[key] = v
			}
			d.vals, d.masks = v[0], v[1]
			d.vref = *sp.Vertical
			if cfg.Local != nil {
				d.vref = VerticalRef{Datum: "unknown"}
			}
		}
		if sp.Vertical != nil {
			d.vref = *sp.Vertical
		}
		for _, p := range d.vals {
			d.valSrc = append(d.valSrc, p.File)
		}
		out = append(out, d)
	}
	return out, nil
}

// wrapLocal gives a user file (or an http(s) URL, downloaded once into the cache) a provenance record.
func wrapLocal(name, path, file, cacheDir, license, attribution string) (*fetch.Provenance, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return fetch.Get(cacheDir, "remote-"+name, path, nil, file, license, attribution)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	sum, err := fetch.SHA256File(abs)
	if err != nil {
		return nil, err
	}
	return &fetch.Provenance{Source: "local:" + name, URL: "file://" + abs, RetrievedAt: fi.ModTime().UTC().Format(time.RFC3339),
		SHA256: sum, Bytes: fi.Size(), License: license, Attribution: attribution, File: abs}, nil
}

// qualityRank orders quality classes for precedence "auto": better measurements first.
func qualityRank(q string) int {
	switch q {
	case "survey":
		return 0
	case "compiled":
		return 1
	case "regional":
		return 2
	}
	return 3
}

// orderDatasets applies the precedence rule and returns the datasets best-first. "listed" keeps the config order.
// "auto" sorts by: quality class, documented accuracy (declared beats undeclared), native resolution (finer first),
// vintage (newer first), then list order. It does not look at nominal resolution alone, and every key is a dataset
// property reported in the manifest, so the result can be explained and reproduced.
func orderDatasets(ds []*dataset, mode string) []*dataset {
	out := append([]*dataset(nil), ds...)
	if mode == "auto" {
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i], out[j]
			if ra, rb := qualityRank(a.spec.Quality), qualityRank(b.spec.Quality); ra != rb {
				return ra < rb
			}
			if (a.spec.AccuracyM > 0) != (b.spec.AccuracyM > 0) {
				return a.spec.AccuracyM > 0
			}
			if a.spec.AccuracyM != b.spec.AccuracyM {
				return a.spec.AccuracyM < b.spec.AccuracyM
			}
			if a.nativeResM != b.nativeResM {
				return a.nativeResM < b.nativeResM
			}
			if a.spec.Year != b.spec.Year {
				return a.spec.Year > b.spec.Year
			}
			return a.idx < b.idx
		})
	}
	return out
}

// groundSize returns the mean ground size in metres of one cell of a raster, for any CRS.
func groundSize(path string, lat float64) (float64, error) {
	out, err := gdal.Run("gdalinfo", "-json", path)
	if err != nil {
		return 0, err
	}
	var info struct {
		Size []float64 `json:"size"`
		GT   []float64 `json:"geoTransform"`
		CS   struct {
			WKT string `json:"wkt"`
		} `json:"coordinateSystem"`
		Ext struct {
			Coordinates [][][]float64 `json:"coordinates"`
		} `json:"wgs84Extent"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil || len(info.GT) < 6 || len(info.Size) < 2 {
		return 0, fmt.Errorf("gdalinfo %s: no geotransform", path)
	}
	if w := strings.TrimSpace(info.CS.WKT); strings.HasPrefix(w, "GEOGCRS") || strings.HasPrefix(w, "GEOGCS") || strings.HasPrefix(w, "GEODCRS") {
		dx := math.Abs(info.GT[1]) * 111320 * math.Cos(lat*math.Pi/180)
		dy := math.Abs(info.GT[5]) * 110574
		return (dx + dy) / 2, nil
	}
	if len(info.Ext.Coordinates) == 0 || len(info.Ext.Coordinates[0]) < 4 {
		return 0, fmt.Errorf("gdalinfo %s: projected raster without a WGS84 extent", path)
	}
	c := info.Ext.Coordinates[0] // UL, LL, LR, UR, UL
	return (haversineM(c[0], c[3])/info.Size[0] + haversineM(c[0], c[1])/info.Size[1]) / 2, nil
}

func haversineM(a, b []float64) float64 {
	const R = 6371008.8
	la1, la2 := a[1]*math.Pi/180, b[1]*math.Pi/180
	dla, dlo := la2-la1, (b[0]-a[0])*math.Pi/180
	h := math.Sin(dla/2)*math.Sin(dla/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dlo/2)*math.Sin(dlo/2)
	return 2 * R * math.Asin(math.Min(1, math.Sqrt(h)))
}
