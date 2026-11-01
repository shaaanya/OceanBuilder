// Package oceanworld reads worlds built by OceanBuilder (docs/formats/world-v2.md) and answers elevation, depth
// and source queries. It depends only on the standard library and the on-disk format, not on OceanBuilder's
// internals or GDAL, so it doubles as a reference implementation of the format for other languages.
package oceanworld

import (
	"container/list"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// SchemaVersion is the highest manifest schema this package reads. Schema 2 worlds are in a UTM zone; schema 3 adds
// worlds in a local transverse Mercator grid (crs.kind "tmerc"). Otherwise the two are identical.
const SchemaVersion = 3

// Manifest is the subset of world.manifest.json the reader needs.
type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	BuildID       string `json:"build_id"`
	CRS           struct {
		EPSG          int     `json:"epsg"` // 0 for a local transverse Mercator grid
		Name          string  `json:"name"`
		Kind          string  `json:"kind"` // "utm" (or absent) | "tmerc"
		Lon0          float64 `json:"lon_0"`
		K0            float64 `json:"k_0"`
		FalseEasting  float64 `json:"false_easting"`
		FalseNorthing float64 `json:"false_northing"`
		Proj4         string  `json:"proj4"`
	} `json:"crs"`
	Grid struct {
		CellM          float64    `json:"cell_m"`
		TileCells      int        `json:"tile_cells"`
		SamplesPerTile int        `json:"samples_per_tile"`
		TileSpanM      float64    `json:"tile_span_m"`
		TilesX         int        `json:"tiles_x"`
		TilesY         int        `json:"tiles_y"`
		BBoxWGS84      [4]float64 `json:"bbox_wgs84"`
	} `json:"grid"`
	Height struct {
		Type      string  `json:"type"`
		ByteOrder string  `json:"byte_order"`
		RowOrder  string  `json:"row_order"`
		NoData    float64 `json:"nodata"`
		MinM      float64 `json:"min_m"`
		MaxM      float64 `json:"max_m"`
	} `json:"height_encoding"`
	DepthClasses []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"depth_classes"`
	SourceLayers []struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Measured bool   `json:"measured"`
	} `json:"source_layers"`
	LOD struct {
		Levels []struct {
			Level     int     `json:"level"`
			CellM     float64 `json:"cell_m"`
			TileSpanM float64 `json:"tile_span_m"`
			Tiles     int     `json:"tiles"`
		} `json:"levels"`
	} `json:"lod"`
	// Datasets names what the per-sample dataset layer indexes (format revision 1); empty in older worlds.
	Datasets []struct {
		Index int    `json:"index"`
		ID    string `json:"id"`
		Type  string `json:"type"`
		Role  string `json:"role"`
	} `json:"datasets"`
	Tiles       []Tile   `json:"tiles"`
	Attribution []string `json:"attribution"`
}

// Tile is one index entry. File paths are relative to the world directory.
type Tile struct {
	ID         string            `json:"id"`
	Level      int               `json:"level"`
	X          int               `json:"x"`
	Y          int               `json:"y"`
	BoundsUTM  [4]float64        `json:"bounds_utm"` // minE, minN, maxE, maxN
	MinM       float64           `json:"min_m"`
	MaxM       float64           `json:"max_m"`
	Height     string            `json:"height"`
	Depth      string            `json:"depth_class"`
	Shore      string            `json:"shore_mask"`
	Source     string            `json:"source"`
	Dataset    string            `json:"dataset"` // optional layer: which dataset supplied each sample
	Parent     string            `json:"parent"`
	Neighbours map[string]string `json:"neighbours"`
}

// Data is one tile's decoded layers: S×S samples, row 0 = north.
type Data struct {
	Height []float32
	Class  []uint8
	Shore  []uint8
	Source []uint8
	// Dataset holds, per sample, the index into Manifest.Datasets (0: none). Nil for worlds without the layer.
	Dataset []uint8
}

// World is an opened world. It is safe for concurrent use.
type World struct {
	M      Manifest
	Dir    string
	proj   TM
	byID   map[string]*Tile
	byPos  map[pos]*Tile
	mu     sync.Mutex
	cache  map[string]*list.Element
	lru    *list.List
	maxTil int
}

type pos struct{ level, x, y int }

type cached struct {
	id string
	d  *Data
}

// Open reads the manifest in dir and indexes its tiles. Tile data is loaded lazily and kept in a small LRU cache.
func Open(dir string) (*World, error) {
	b, err := os.ReadFile(filepath.Join(dir, "world.manifest.json"))
	if err != nil {
		return nil, err
	}
	w := &World{Dir: dir, byID: map[string]*Tile{}, byPos: map[pos]*Tile{}, cache: map[string]*list.Element{}, lru: list.New(), maxTil: 64}
	if err := json.Unmarshal(b, &w.M); err != nil {
		return nil, fmt.Errorf("world.manifest.json: %w", err)
	}
	m := &w.M
	if m.SchemaVersion < 2 || m.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("manifest schema_version %d, this reader understands 2..%d", m.SchemaVersion, SchemaVersion)
	}
	if m.Height.Type != "float32" || m.Height.ByteOrder != "little" || m.Height.RowOrder != "north_to_south" {
		return nil, fmt.Errorf("unsupported height encoding %+v", m.Height)
	}
	if m.CRS.Kind == "tmerc" {
		if m.CRS.K0 <= 0 {
			return nil, fmt.Errorf("crs kind tmerc without k_0")
		}
		w.proj = TM{Lon0: m.CRS.Lon0, K0: m.CRS.K0, FalseEasting: m.CRS.FalseEasting, FalseNorthing: m.CRS.FalseNorthing}
	} else if zone, north, ok := ZoneOf(m.CRS.EPSG); ok {
		w.proj = UTMParams(zone, north)
	} else {
		return nil, fmt.Errorf("CRS EPSG:%d is not a WGS 84 / UTM zone and crs.kind is %q; use the grid coordinates directly", m.CRS.EPSG, m.CRS.Kind)
	}
	for i := range m.Tiles {
		t := &m.Tiles[i]
		w.byID[t.ID], w.byPos[pos{t.Level, t.X, t.Y}] = t, t
	}
	return w, nil
}

// Levels is the number of LOD levels (level 0 is the finest).
func (w *World) Levels() int { return len(w.M.LOD.Levels) }

// Tile returns the index entry for an id, or nil.
func (w *World) Tile(id string) *Tile { return w.byID[id] }

// TileAt returns the tile of a level containing a UTM point (the one to the east/north on an exact edge).
func (w *World) TileAt(e, n float64, level int) *Tile {
	span := w.M.Grid.TileSpanM * float64(int(1)<<level)
	return w.byPos[pos{level, int(math.Floor(e / span)), int(math.Floor(n / span))}]
}

// Load decodes a tile's four layers.
func (w *World) Load(t *Tile) (*Data, error) {
	w.mu.Lock()
	if el, ok := w.cache[t.ID]; ok {
		w.lru.MoveToFront(el)
		d := el.Value.(*cached).d
		w.mu.Unlock()
		return d, nil
	}
	w.mu.Unlock()
	S := w.M.Grid.SamplesPerTile
	raw, err := os.ReadFile(filepath.Join(w.Dir, t.Height))
	if err != nil {
		return nil, err
	}
	if len(raw) != S*S*4 {
		return nil, fmt.Errorf("%s: %d bytes, want %d", t.Height, len(raw), S*S*4)
	}
	d := &Data{Height: make([]float32, S*S)}
	for i := range d.Height {
		d.Height[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	for _, l := range []struct {
		path string
		dst  *[]uint8
	}{{t.Depth, &d.Class}, {t.Shore, &d.Shore}, {t.Source, &d.Source}} {
		if *l.dst, err = readGray(filepath.Join(w.Dir, l.path), S); err != nil {
			return nil, err
		}
	}
	if t.Dataset != "" {
		if d.Dataset, err = readGray(filepath.Join(w.Dir, t.Dataset), S); err != nil {
			return nil, err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cache[t.ID] = w.lru.PushFront(&cached{t.ID, d})
	for w.lru.Len() > w.maxTil {
		el := w.lru.Back()
		w.lru.Remove(el)
		delete(w.cache, el.Value.(*cached).id)
	}
	return d, nil
}

func readGray(path string, size int) ([]uint8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	g, ok := img.(*image.Gray)
	if !ok || g.Bounds().Dx() != size || g.Bounds().Dy() != size {
		return nil, fmt.Errorf("%s: want %dx%d 8-bit grey PNG", path, size, size)
	}
	return g.Pix, nil
}

// Sample is the answer at one point.
type Sample struct {
	Valid      bool    // false: outside the world or on a cell without a value
	ElevationM float64 // bilinear between the four surrounding vertices; positive up
	DepthM     float64 // -ElevationM when below zero, else 0
	Class      uint8   // depth class of the nearest vertex (0 land, 1..n bands, 255 nodata)
	Shore      bool    // nearest vertex is a shore cell
	Source     string  // source layer name of the nearest vertex ("survey", "background", ...)
	Dataset    string  // id of the dataset that supplied the nearest vertex ("" when the world has no dataset layer)
	Measured   bool    // nearest vertex is a survey measurement
	Level      int
	Tile       string
}

// SampleUTM samples the world at easting/northing (the world's CRS) from the given LOD level. Elevation is
// interpolated bilinearly only when all four surrounding vertices have values; otherwise Valid is false.
func (w *World) SampleUTM(e, n float64, level int) (Sample, error) {
	if level < 0 || level >= w.Levels() {
		return Sample{}, fmt.Errorf("level %d not in 0..%d", level, w.Levels()-1)
	}
	t := w.TileAt(e, n, level)
	// A point exactly on the world's north or east boundary has no tile beyond it, but the vertex exists as the
	// last row/column of the tile before it.
	for _, d := range [][2]float64{{-1e-6, 0}, {0, -1e-6}, {-1e-6, -1e-6}} {
		if t == nil {
			t = w.TileAt(e+d[0], n+d[1], level)
		}
	}
	if t == nil {
		return Sample{Level: level}, nil
	}
	d, err := w.Load(t)
	if err != nil {
		return Sample{}, err
	}
	N := w.M.Grid.TileCells
	S := N + 1
	cell := w.M.Grid.CellM * float64(int(1)<<level)
	u, v := (e-t.BoundsUTM[0])/cell, (t.BoundsUTM[3]-n)/cell // columns east, rows south
	j0, i0 := min(int(math.Floor(u)), N-1), min(int(math.Floor(v)), N-1)
	fu, fv := u-float64(j0), v-float64(i0)
	nd := float32(w.M.Height.NoData)
	h := [4]float32{d.Height[i0*S+j0], d.Height[i0*S+j0+1], d.Height[(i0+1)*S+j0], d.Height[(i0+1)*S+j0+1]}
	s := Sample{Level: level, Tile: t.ID}
	jn, in := j0+int(math.Round(fu)), i0+int(math.Round(fv))
	s.Class, s.Shore = d.Class[in*S+jn], d.Shore[in*S+jn] != 0
	code := int(d.Source[in*S+jn])
	for _, l := range w.M.SourceLayers {
		if l.ID == code {
			s.Source, s.Measured = l.Name, l.Measured
		}
	}
	if d.Dataset != nil {
		for _, ds := range w.M.Datasets {
			if ds.Index == int(d.Dataset[in*S+jn]) {
				s.Dataset = ds.ID
			}
		}
	}
	for _, x := range h {
		if x <= nd+1 {
			return s, nil
		}
	}
	s.Valid = true
	s.ElevationM = float64(h[0])*(1-fu)*(1-fv) + float64(h[1])*fu*(1-fv) + float64(h[2])*(1-fu)*fv + float64(h[3])*fu*fv
	s.DepthM = math.Max(0, -s.ElevationM)
	return s, nil
}

// ToUTM converts WGS 84 longitude/latitude to the world's grid.
func (w *World) ToUTM(lon, lat float64) (e, n float64) { return LonLatToTM(lon, lat, w.proj) }

// ToLonLat converts the world's grid to WGS 84 longitude/latitude.
func (w *World) ToLonLat(e, n float64) (lon, lat float64) { return TMToLonLat(e, n, w.proj) }

// GroundScale is the point scale factor of the world's grid at lon/lat (see the package function).
func (w *World) GroundScale(lon, lat float64) float64 { return GroundScale(lon, lat, w.proj) }

// SampleLonLat samples at a WGS 84 position.
func (w *World) SampleLonLat(lon, lat float64, level int) (Sample, error) {
	e, n := w.ToUTM(lon, lat)
	return w.SampleUTM(e, n, level)
}

// Profile samples n+1 evenly spaced points from a to b (UTM metres), e.g. a keel path or a sounding line.
func (w *World) Profile(ae, an, be, bn float64, n, level int) ([]Sample, error) {
	if n < 1 {
		return nil, fmt.Errorf("profile needs at least 1 step")
	}
	out := make([]Sample, 0, n+1)
	for i := 0; i <= n; i++ {
		f := float64(i) / float64(n)
		s, err := w.SampleUTM(ae+(be-ae)*f, an+(bn-an)*f, level)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
