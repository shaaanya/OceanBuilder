package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shaaanya/OceanBuilder/internal/fetch"
	"github.com/shaaanya/OceanBuilder/internal/gdal"
)

const (
	noData      = -9999.0
	ClassNoData = 255
)

// Options controls a build run.
type Options struct {
	CacheDir string
	OutDir   string
	// LockPath, when set, pins sources: an existing lock must match what was resolved (error otherwise);
	// UpdateLock rewrites it from this build.
	LockPath   string
	UpdateLock bool
	// Force rebuilds even when the existing output was made from identical inputs (same input_key).
	Force bool
	// MemoryMB bounds the memory of windowed processing and of the steps that hold the whole grid (default 1024).
	// It never changes the output; WindowTiles (tests) forces the window edge in tiles, to prove that.
	MemoryMB    int
	WindowTiles int
	Log         func(format string, a ...any)
	finalDir    string // where the finished output will live; checked for an up-to-date build
	// SkipDiskCheck disables the free-space preflight (disk.go), for example on a filesystem that misreports free space.
	SkipDiskCheck bool
}

func (o *Options) logf(f string, a ...any) {
	if o.Log != nil {
		o.Log(f, a...)
	}
}

// Build fetches sources, normalises them onto a metric tile-aligned grid and writes tiles, masks, the manifest
// and attribution to opt.OutDir. The output is assembled in a sibling directory and swapped in only when complete,
// so a failed or cancelled build never leaves a half-written world, and a build whose inputs are identical to the
// existing output (same input_key) returns that output untouched unless opt.Force is set.
func Build(cfg *Config, opt Options) (*Manifest, error) {
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	if err := gdal.CheckTools(); err != nil {
		return nil, err
	}
	final := filepath.Clean(opt.OutDir)
	parent := filepath.Dir(final)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	if !opt.SkipDiskCheck && os.Getenv("OCEAN_SKIP_DISK_CHECK") == "" {
		plan, err := PlanGrid(cfg)
		if err != nil {
			return nil, err
		}
		need := WorkDiskEstimate(plan.W * plan.H)
		for _, dir := range []string{parent, opt.CacheDir} { // both may hold the work; the same disk is checked twice, which is conservative
			if dir == "" {
				continue
			}
			if err := CheckDisk(dir, need, fmt.Sprintf("building %s (%d x %d level-0 cells, without downloads)", cfg.Name, plan.W, plan.H)); err != nil {
				return nil, err
			}
		}
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(final)+".building-")
	if err != nil {
		return nil, err
	}
	os.Chmod(stage, 0o755)
	opt.finalDir, opt.OutDir = final, stage
	m, skipped, err := build(cfg, opt)
	if err != nil || skipped {
		os.RemoveAll(stage)
		return m, err
	}
	old := ""
	if _, err := os.Stat(final); err == nil {
		old = stage + ".old"
		if err := os.Rename(final, old); err != nil {
			os.RemoveAll(stage)
			return nil, err
		}
	}
	if err := os.Rename(stage, final); err != nil {
		if old != "" {
			os.Rename(old, final)
		}
		os.RemoveAll(stage)
		return nil, err
	}
	if old != "" {
		os.RemoveAll(old)
	}
	return m, nil
}

func writePNG(path string, img image.Image) (string, error) {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return "", err
	}
	return hashHex(b.Bytes()), os.WriteFile(path, b.Bytes(), 0o644)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func depthClasses(bands []float64) []DepthClass {
	f := func(v float64) *float64 { return &v }
	cs := []DepthClass{{ID: 0, Name: "land"}}
	prev := 0.0
	for i, b := range bands {
		cs = append(cs, DepthClass{ID: i + 1, Name: fmt.Sprintf("%g–%g m", prev, b), MinDepth: f(prev), MaxDepth: f(b)})
		prev = b
	}
	cs = append(cs, DepthClass{ID: len(bands) + 1, Name: fmt.Sprintf(">%g m", prev), MinDepth: f(prev)})
	return append(cs, DepthClass{ID: ClassNoData, Name: "nodata"})
}

func buildID(m *Manifest) string {
	h := sha256.New()
	cfg, _ := json.Marshal(m.Config)
	h.Write(cfg)
	h.Write([]byte(ToolVersion))
	algoSig(h, &m.Config)
	if m.Refinement != nil {
		fmt.Fprintf(h, "refinement-algo=%d", RefinementAlgo)
	}
	for _, s := range m.Sources {
		h.Write([]byte(s.SHA256))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// algoSig writes the versions of every output-affecting algorithm that the config activates.
func algoSig(h io.Writer, cfg *Config) {
	fmt.Fprintf(h, "format-algo=%d", FormatAlgo)
	if cfg.CoastConform != "" {
		fmt.Fprintf(h, "conform-algo=%d", ConformAlgo)
	}
	if cfg.RegionalBathymetry != "" {
		fmt.Fprintf(h, "regional-algo=%d", RegionalAlgo)
	}
	if cfg.Seamarks {
		fmt.Fprintf(h, "seamarks-algo=%d", SeamarksAlgo)
	}
}

// inputKey identifies everything a build's bytes depend on: the config, tool and algorithm versions, GDAL, and
// the hash of every resolved source. Equal keys mean equal outputs (tested), so the build can be skipped.
func inputKey(cfg *Config, srcs []fetch.Provenance, regional *fetch.Provenance, dss []*dataset) string {
	h := sha256.New()
	c, _ := json.Marshal(cfg)
	h.Write(c)
	fmt.Fprintf(h, "|%s|%s|", ToolVersion, gdal.Version())
	algoSig(h, cfg)
	for _, s := range srcs {
		fmt.Fprintf(h, "|%s=%s", s.Source, s.SHA256)
	}
	if regional != nil {
		fmt.Fprintf(h, "|%s=%s", regional.Source, regional.SHA256)
	}
	for _, d := range dss {
		if d.vapplied != nil && (d.vapplied.Kind == "proj-grid" || d.vapplied.Kind == "user-offset") {
			fmt.Fprintf(h, "|vertical:%s=%s", d.spec.ID, d.vapplied.key())
		}
	}
	if cfg.Refinement != "" {
		fmt.Fprintf(h, "|refinement-algo=%d", RefinementAlgo)
		b, _ := os.ReadFile(filepath.Join(cfg.dir, cfg.Refinement))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// reusable returns the manifest in dir when it was built from the same inputs and every tile file is intact.
func reusable(dir, key string) *Manifest {
	m, err := LoadManifest(dir)
	if err != nil || (m.SchemaVersion != SchemaVersion && m.SchemaVersion != SchemaVersionTM) || m.InputKey != key {
		return nil
	}
	for _, t := range m.Tiles {
		b, err := os.ReadFile(filepath.Join(dir, t.Height))
		if err != nil || hashHex(b) != t.SHA256.Height {
			return nil
		}
		for _, f := range []string{t.Depth, t.Shore, t.Source} {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				return nil
			}
		}
	}
	return m
}

// coastCheck samples the DEM along the OSM coastline (reprojected to the grid CRS).
func coastCheck(geojson, srs, work string, dem *gdal.Raster, shore []uint8, g *Grid, W, H int) (*CoastCheck, error) {
	proj := filepath.Join(work, "coast.utm.geojson")
	if _, err := gdal.Run("ogr2ogr", "-f", "GeoJSON", "-t_srs", srs, proj, geojson); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(proj)
	if err != nil {
		return nil, err
	}
	var fc struct {
		Features []struct {
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(b, &fc); err != nil {
		return nil, err
	}
	cc := &CoastCheck{}
	var vals, offs []float64
	unmatched := 0
	for _, f := range fc.Features {
		var line [][]float64
		if f.Geometry.Type != "LineString" || json.Unmarshal(f.Geometry.Coordinates, &line) != nil {
			continue
		}
		for i := 1; i < len(line); i++ {
			a, c := line[i-1], line[i]
			steps := int(math.Ceil(math.Hypot(c[0]-a[0], c[1]-a[1]) / g.TileSpanM * float64(g.TileCells)))
			if steps < 1 {
				steps = 1
			}
			for k := 0; k < steps; k++ {
				fr := float64(k) / float64(steps)
				e, n := a[0]+(c[0]-a[0])*fr, a[1]+(c[1]-a[1])*fr
				cc.Points++
				v, ok := sampleBilinear(dem, (e-g.OriginE)/g.CellM, (g.OriginN-n)/g.CellM)
				if !ok {
					cc.Outside++
					continue
				}
				vals = append(vals, v)
				if d, found := nearestShore(shore, W, H, (e-g.OriginE)/g.CellM, (g.OriginN-n)/g.CellM, searchCells); found {
					offs = append(offs, d*g.CellM)
				} else {
					unmatched++
				}
			}
		}
	}
	if len(vals) == 0 {
		return cc, nil
	}
	sort.Float64s(vals)
	q := func(p float64) float64 { return vals[int(p*float64(len(vals)-1))] }
	cc.MedianM, cc.P10M, cc.P90M = q(0.5), q(0.1), q(0.9)
	cc.SearchRadiusM = searchCells * g.CellM
	cc.NoShoreWithin = float64(unmatched) / float64(len(vals))
	if len(offs) > 0 {
		sort.Float64s(offs)
		cc.OffsetMedianM = offs[len(offs)/2]
		cc.OffsetP90M = offs[int(0.9*float64(len(offs)-1))]
	}
	return cc, nil
}

const searchCells = 8

// nearestShore returns the distance in cells from (col,row) to the closest
// shoreline cell within radius cells.
func nearestShore(shore []uint8, W, H int, col, row, radius float64) (float64, bool) {
	best, found := radius*radius+1, false
	r0, c0 := int(math.Round(row)), int(math.Round(col))
	R := int(radius)
	for dr := -R; dr <= R; dr++ {
		for dc := -R; dc <= R; dc++ {
			r, c := r0+dr, c0+dc
			if r < 0 || r >= H || c < 0 || c >= W || shore[r*W+c] == 0 {
				continue
			}
			d := (float64(r)-row)*(float64(r)-row) + (float64(c)-col)*(float64(c)-col)
			if d < best {
				best, found = d, true
			}
		}
	}
	return math.Sqrt(best), found && best <= radius*radius
}

// sampleBilinear samples at fractional (col,row); false when outside or touching nodata.
func sampleBilinear(r *gdal.Raster, col, row float64) (float64, bool) {
	if col < 0 || row < 0 || col > float64(r.W-1) || row > float64(r.H-1) {
		return 0, false
	}
	c0, r0 := int(math.Floor(col)), int(math.Floor(row))
	c1, r1 := min(c0+1, r.W-1), min(r0+1, r.H-1)
	fc, fr := col-float64(c0), row-float64(r0)
	v := [4]float64{float64(r.Data[r0*r.W+c0]), float64(r.Data[r0*r.W+c1]), float64(r.Data[r1*r.W+c0]), float64(r.Data[r1*r.W+c1])}
	for _, x := range v {
		if x <= noData+1 {
			return 0, false
		}
	}
	return v[0]*(1-fc)*(1-fr) + v[1]*fc*(1-fr) + v[2]*(1-fc)*fr + v[3]*fc*fr, true
}

func minF(a []float64) float64 {
	m := math.Inf(1)
	for _, v := range a {
		m = math.Min(m, v)
	}
	return m
}
func maxF(a []float64) float64 {
	m := math.Inf(-1)
	for _, v := range a {
		m = math.Max(m, v)
	}
	return m
}

// shoreMask marks land cells that touch water (4-neighbourhood), as the world build does.
func shoreMask(data []float32, W, H int, valid func(int) bool) []uint8 {
	shore := make([]uint8, W*H)
	water := func(i int) bool { return valid(i) && data[i] < 0 }
	for r := 0; r < H; r++ {
		for c := 0; c < W; c++ {
			i := r*W + c
			if !valid(i) || data[i] < 0 {
				continue
			}
			for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				rr, cc := r+d[0], c+d[1]
				if rr >= 0 && rr < H && cc >= 0 && cc < W && water(rr*W+cc) {
					shore[i] = 255
					break
				}
			}
		}
	}
	return shore
}

// projectedLines reads GeoJSON LineStrings (EPSG:4326) and returns them in grid coordinates (col, row).
func projectedLines(geojson, srs, work string, g *Grid) ([][][2]float64, error) {
	proj := filepath.Join(work, "coast.conform.utm.geojson")
	if _, err := gdal.Run("ogr2ogr", "-f", "GeoJSON", "-t_srs", srs, proj, geojson); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(proj)
	if err != nil {
		return nil, err
	}
	var fc struct {
		Features []struct {
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(b, &fc); err != nil {
		return nil, fmt.Errorf("coastline geojson: %w", err)
	}
	var out [][][2]float64
	add := func(pts [][]float64) {
		if len(pts) < 2 {
			return
		}
		ln := make([][2]float64, 0, len(pts))
		for _, p := range pts {
			if len(p) >= 2 {
				ln = append(ln, [2]float64{(p[0] - g.OriginE) / g.CellM, (g.OriginN - p[1]) / g.CellM})
			}
		}
		out = append(out, ln)
	}
	for _, f := range fc.Features {
		switch f.Geometry.Type {
		case "LineString":
			var pts [][]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &pts); err != nil {
				return nil, fmt.Errorf("coastline LineString: %w", err)
			}
			add(pts)
		case "MultiLineString":
			var parts [][][]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &parts); err != nil {
				return nil, fmt.Errorf("coastline MultiLineString: %w", err)
			}
			for _, pts := range parts {
				add(pts)
			}
		}
	}
	return out, nil
}

// baseBuildID is the build id this world would have without its refinement: the id a LibreTerrain export recorded.
// baseName is the name of the world the export was made from (the refined config is named differently).
func baseBuildID(m *Manifest, baseName string) string {
	b := *m
	if baseName != "" {
		b.Config.Name = baseName
	}
	b.Config.Refinement = ""
	b.Refinement = nil
	b.Sources = nil
	for _, s := range m.Sources {
		if !strings.HasPrefix(s.Source, "refinement:") {
			b.Sources = append(b.Sources, s)
		}
	}
	return buildID(&b)
}

// applyLock enforces or writes the source lock (see fetch.Lock).
func applyLock(opt *Options, srcs []fetch.Provenance) error {
	if opt.LockPath == "" {
		return nil
	}
	l, err := fetch.ReadLock(opt.LockPath)
	if err != nil {
		return err
	}
	if l != nil && !opt.UpdateLock {
		if bad := l.Check(srcs); len(bad) > 0 {
			return fmt.Errorf("sources differ from %s (rebuild with -update-lock to accept):\n  %s", opt.LockPath, strings.Join(bad, "\n  "))
		}
		opt.logf("sources match %s", opt.LockPath)
		return nil
	}
	if l == nil && !opt.UpdateLock {
		opt.logf("no source lock at %s; run with -update-lock to pin these sources", opt.LockPath)
		return nil
	}
	opt.logf("writing %s", opt.LockPath)
	return fetch.NewLock(srcs).Write(opt.LockPath)
}
