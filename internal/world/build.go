package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ocean-compiler/ocean/internal/fetch"
	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/internal/gmrt"
	"github.com/ocean-compiler/ocean/internal/osm"
)

const (
	noData      = -9999.0
	ClassNoData = 255
)

// Options controls a build run.
type Options struct {
	CacheDir string
	OutDir   string
	Log      func(format string, a ...any)
}

func (o *Options) logf(f string, a ...any) {
	if o.Log != nil {
		o.Log(f, a...)
	}
}

// Build fetches sources, normalises them onto a metric tile-aligned grid and
// writes tiles, masks, the manifest and attribution to opt.OutDir.
func Build(cfg *Config, opt Options) (*Manifest, error) {
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	span := cfg.TileSpanM()
	N := cfg.TileCells
	w, s, e, n := cfg.BBox[0], cfg.BBox[1], cfg.BBox[2], cfg.BBox[3]
	clon, clat := (w+e)/2, (s+n)/2

	// 1. Fetch sources with a padding of ~1.2 tiles so the tile-aligned grid is covered.
	pad := math.Max(span*1.2, 500)
	dLat := pad / 110574.0
	dLon := pad / (111320.0 * math.Cos(clat*math.Pi/180))
	var err error
	fb := gmrt.BBox{W: math.Max(w-dLon, -180), S: math.Max(s-dLat, -85), E: math.Min(e+dLon, 180), N: math.Min(n+dLat, 85)}
	m := &Manifest{SchemaVersion: SchemaVersion, Name: cfg.Name, Config: *cfg, Files: map[string]string{}}
	var topo, mask *fetch.Provenance
	var coastGeoJSON string
	if cfg.Local != nil {
		var coast *fetch.Provenance
		if topo, mask, coast, err = localSources(cfg.Local); err != nil {
			return nil, err
		}
		if coast != nil {
			m.Sources = append(m.Sources, *coast)
			coastGeoJSON = coast.File
		}
	} else {
		opt.logf("fetching GMRT topo+mask for %.4f,%.4f,%.4f,%.4f", fb.W, fb.S, fb.E, fb.N)
		if topo, err = gmrt.Fetch(opt.CacheDir, fb, gmrt.LayerTopo); err != nil {
			return nil, err
		}
		if mask, err = gmrt.Fetch(opt.CacheDir, fb, gmrt.LayerMask); err != nil {
			return nil, err
		}
		if cfg.Coastline == "osm" {
			opt.logf("fetching OSM coastline")
			p, gj, err := osm.FetchCoastline(opt.CacheDir, fb.W, fb.S, fb.E, fb.N)
			if err != nil {
				m.Stats.CoastlineNote = "OSM coastline unavailable: " + err.Error()
				opt.logf("WARNING: %s", m.Stats.CoastlineNote)
			} else {
				m.Sources = append(m.Sources, *p)
				coastGeoJSON = gj
			}
		}
	}
	m.Sources = append([]fetch.Provenance{*topo}, m.Sources...)
	if mask != nil {
		m.Sources = append(m.Sources[:1], append([]fetch.Provenance{*mask}, m.Sources[1:]...)...)
	}
	nativeRes, err := nativeResolution(topo.File, clat)
	if err != nil {
		return nil, err
	}

	// 2. Tile-aligned metric grid in UTM.
	epsg := gdal.UTMEPSG(clon, clat)
	srs := fmt.Sprintf("EPSG:%d", epsg)
	var edge [][2]float64
	for i := 0; i <= 8; i++ {
		f := float64(i) / 8
		lo, la := w+(e-w)*f, s+(n-s)*f
		edge = append(edge, [2]float64{lo, s}, [2]float64{lo, n}, [2]float64{w, la}, [2]float64{e, la})
	}
	utm, err := gdal.Transform("EPSG:4326", srs, edge)
	if err != nil {
		return nil, err
	}
	minE, maxE, minN, maxN := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, p := range utm {
		minE, maxE = math.Min(minE, p[0]), math.Max(maxE, p[0])
		minN, maxN = math.Min(minN, p[1]), math.Max(maxN, p[1])
	}
	x0, x1 := int(math.Floor(minE/span)), int(math.Floor(maxE/span))
	y0, y1 := int(math.Floor(minN/span)), int(math.Floor(maxN/span))
	tx, ty := x1-x0+1, y1-y0+1
	W, H := tx*N+1, ty*N+1
	originE, originN := float64(x0)*span, float64(y1+1)*span
	g := &m.Grid
	*g = Grid{CellM: cfg.CellM, TileCells: N, SamplesPerTile: N + 1, TileSpanM: span, TilesX: tx, TilesY: ty,
		OriginE: originE, OriginN: originN, FirstTileX: x0, FirstTileY: y0}
	opt.logf("grid %dx%d samples, %dx%d tiles of %d m, %s", W, H, tx, ty, int(span), srs)

	work, err := os.MkdirTemp(opt.CacheDir, "work-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	// Vertex-registered grid: sample k sits at originE + k*cell, so pixel edges are offset by cell/2.
	h := cfg.CellM / 2
	te := []string{fmt.Sprint(originE - h), fmt.Sprint(originN - float64(H-1)*cfg.CellM - h),
		fmt.Sprint(originE + float64(W-1)*cfg.CellM + h), fmt.Sprint(originN + h)}
	warp := func(src, dst, method, ot string) error {
		_, err := gdal.Run("gdalwarp", "-overwrite", "-q", "-t_srs", srs, "-te", te[0], te[1], te[2], te[3],
			"-ts", fmt.Sprint(W), fmt.Sprint(H), "-r", method, "-ot", ot, "-dstnodata", fmt.Sprint(noData),
			"-of", "ENVI", src, dst)
		return err
	}
	demPath, maskPath := filepath.Join(work, "dem.bin"), filepath.Join(work, "mask.bin")
	if err := warp(topo.File, demPath, cfg.Resampling, "Float32"); err != nil {
		return nil, err
	}
	// topo-mask holds the same elevations but NaN wherever GMRT has no ~100 m
	// ocean survey (there topo is filled from GEBCO_08). Nearest-neighbour keeps it binary.
	maskSrc := topo.File // no mask supplied: no cell is claimed to be survey-backed
	if mask != nil {
		maskSrc = mask.File
	}
	if err := warp(maskSrc, maskPath, "near", "Float32"); err != nil {
		return nil, err
	}
	dem, err := gdal.ReadF32(demPath, W, H)
	if err != nil {
		return nil, err
	}
	mraw, err := gdal.ReadF32(maskPath, W, H)
	if err != nil {
		return nil, err
	}
	meas := make([]uint8, W*H)
	if mask != nil {
		for i, v := range mraw.Data {
			if !math.IsNaN(float64(v)) && v > noData+1 {
				meas[i] = 1
			}
		}
	}

	// 3. Derive world-wide grids so tile borders see identical neighbours.
	zmin, zmax := math.Inf(1), math.Inf(-1)
	valid := func(i int) bool { return dem.Data[i] > noData+1 }
	for i, v := range dem.Data {
		if valid(i) {
			zmin, zmax = math.Min(zmin, float64(v)), math.Max(zmax, float64(v))
		}
	}
	if math.IsInf(zmin, 1) {
		return nil, fmt.Errorf("no valid elevation samples inside bbox: source does not cover the area")
	}
	if zmax == zmin {
		zmax = zmin + 1
	}
	class := make([]uint8, W*H)
	shore := make([]uint8, W*H)
	isWater := func(i int) bool { return valid(i) && dem.Data[i] < 0 }
	var st = &m.Stats
	var nWater, nMeasured, nNoData int
	for r := 0; r < H; r++ {
		for c := 0; c < W; c++ {
			i := r*W + c
			switch {
			case !valid(i):
				class[i] = ClassNoData
				nNoData++
			case dem.Data[i] >= 0:
				class[i] = 0
				for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					rr, cc := r+d[0], c+d[1]
					if rr >= 0 && rr < H && cc >= 0 && cc < W && isWater(rr*W+cc) {
						shore[i] = 255
						st.ShoreCells++
						break
					}
				}
			default:
				depth := -float64(dem.Data[i])
				k := 1
				for k <= len(cfg.DepthBands) && depth > cfg.DepthBands[k-1] {
					k++
				}
				class[i] = uint8(k)
				nWater++
				if meas[i] == 1 {
					nMeasured++
				}
			}
		}
	}
	st.Cells = W * H
	st.NoDataFraction = float64(nNoData) / float64(W*H)
	st.WaterFraction = float64(nWater) / float64(W*H)
	if nWater > 0 {
		st.MeasuredWater = float64(nMeasured) / float64(nWater)
	}

	// 4. Write tiles in parallel; results land in fixed slots so output order is deterministic.
	tilesDir := filepath.Join(opt.OutDir, "tiles", "0")
	if err := os.MkdirAll(tilesDir, 0o755); err != nil {
		return nil, err
	}
	m.Tiles = make([]Tile, tx*ty)
	errs := make([]error, tx*ty)
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for r := 0; r < ty; r++ {
		for c := 0; c < tx; c++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(r, c int) {
				defer wg.Done()
				defer func() { <-sem }()
				t := &m.Tiles[r*tx+c]
				errs[r*tx+c] = writeTile(t, r, c, cfg, g, dem, class, shore, meas, zmin, zmax, W, tilesDir, opt.OutDir, srs)
			}(r, c)
		}
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	m.Checkpoints = checkpoints(dem, g, zmin, zmax, W, H)

	// 5. Coastline: copy to output and compare against the DEM.
	if coastGeoJSON != "" {
		b, err := os.ReadFile(coastGeoJSON)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(opt.OutDir, "coastline.wgs84.geojson"), b, 0o644); err != nil {
			return nil, err
		}
		m.Files["coastline"] = "coastline.wgs84.geojson"
		cc, err := coastCheck(coastGeoJSON, srs, work, dem, shore, g, W, H)
		if err != nil {
			return nil, err
		}
		st.CoastlineCheck = cc
	}

	// 6. Manifest.
	m.Tool.Version, m.Tool.GDAL = ToolVersion, gdal.Version()
	m.CRS = CRS{EPSG: epsg, Name: fmt.Sprintf("WGS 84 / UTM zone %d%s", epsg%100, map[bool]string{true: "N", false: "S"}[epsg < 32700])}
	m.Vertical.Unit = "metre"
	m.Vertical.Datum = "unspecified: GMRT mosaic of contributed grids with heterogeneous vertical references"
	m.Vertical.Note = "Zero is treated as the land/water boundary. No vertical datum reconciliation is performed; the OSM coastline (a high-water-line convention that varies by country) and GMRT zero are not guaranteed to coincide."
	m.Height = HeightEncoding{Bits: 16, ByteOrder: "little", RowOrder: "north_to_south", MinM: zmin, MaxM: zmax,
		Note: "uint16 = round((z-min_m)/(max_m-min_m)*65535); nodata cells are written as 0 and flagged class 255 in the depth map"}
	// GEBCO_08 is 30 arc-seconds; GMRT documents it as the fill where no survey data exists.
	fill := (30.0/3600*111320*math.Cos(clat*math.Pi/180) + 30.0/3600*110574) / 2
	m.Resampling = Resampling{Method: cfg.Resampling, SourceResM: nativeRes, FillResM: fill, OutputCellM: cfg.CellM, Upsampled: cfg.CellM < nativeRes}
	m.DepthClasses = depthClasses(cfg.DepthBands)
	bw, bs := []float64{}, []float64{}
	for _, t := range m.Tiles {
		bw, bs = append(bw, t.BoundsWGS[0], t.BoundsWGS[2]), append(bs, t.BoundsWGS[1], t.BoundsWGS[3])
	}
	m.Grid.BBoxWGS84 = [4]float64{minF(bw), minF(bs), maxF(bw), maxF(bs)}
	m.Files["attribution"] = "ATTRIBUTION.txt"
	for i, p := range m.Sources {
		m.Attribution = append(m.Attribution, fmt.Sprintf("%s — %s — license: %s", p.Source, p.Attribution, p.License))
		// Shipped manifests must not leak local paths.
		m.Sources[i].File = ""
		if strings.HasPrefix(p.URL, "file://") {
			m.Sources[i].URL = "file://" + filepath.Base(p.File)
		}
	}
	if cfg.Local == nil {
		m.Attribution = append(m.Attribution, "GEBCO_08 Grid — fills water cells without survey data inside GMRT's topo layer (≈900 m) — public domain, https://www.gebco.net/data-products/gridded-bathymetry/terms-of-use")
	}
	m.Attribution = append(m.Attribution, "NOT FOR NAVIGATION: bathymetry here is a game-development dataset (GMRT terms: data should not be used for navigation purposes)")
	sort.Strings(m.Attribution)
	m.BuildID = buildID(m)
	if err := writeJSON(filepath.Join(opt.OutDir, ManifestFile), m); err != nil {
		return nil, err
	}
	var att bytes.Buffer
	fmt.Fprintf(&att, "World %q built by Ocean Compiler %s (build %s)\n\nThis output derives from the following sources; respect their licenses when redistributing.\n\n", m.Name, ToolVersion, m.BuildID[:12])
	for _, a := range m.Attribution {
		fmt.Fprintln(&att, "- "+a)
	}
	if err := os.WriteFile(filepath.Join(opt.OutDir, "ATTRIBUTION.txt"), att.Bytes(), 0o644); err != nil {
		return nil, err
	}
	return m, nil
}

func writeTile(t *Tile, r, c int, cfg *Config, g *Grid, dem *gdal.Raster, class, shore, meas []uint8, zmin, zmax float64, W int, dir, out, srs string) error {
	N := cfg.TileCells
	S := N + 1
	x, y := g.FirstTileX+c, g.FirstTileY+(g.TilesY-1-r)
	id := fmt.Sprintf("L0_%d_%d", x, y)
	*t = Tile{ID: id, Level: 0, X: x, Y: y, Col: c, Row: r,
		Height: "tiles/0/" + id + ".height.r16", Depth: "tiles/0/" + id + ".depth.png", Shore: "tiles/0/" + id + ".shore.png"}
	t.BoundsUTM[0] = g.OriginE + float64(c)*g.TileSpanM
	t.BoundsUTM[2] = t.BoundsUTM[0] + g.TileSpanM
	t.BoundsUTM[3] = g.OriginN - float64(r)*g.TileSpanM
	t.BoundsUTM[1] = t.BoundsUTM[3] - g.TileSpanM
	corners, err := gdal.Transform(srs, "EPSG:4326", [][2]float64{{t.BoundsUTM[0], t.BoundsUTM[1]}, {t.BoundsUTM[2], t.BoundsUTM[1]}, {t.BoundsUTM[0], t.BoundsUTM[3]}, {t.BoundsUTM[2], t.BoundsUTM[3]}})
	if err != nil {
		return err
	}
	var lo, la []float64
	for _, p := range corners {
		lo, la = append(lo, p[0]), append(la, p[1])
	}
	t.BoundsWGS = [4]float64{minF(lo), minF(la), maxF(lo), maxF(la)}

	buf := make([]byte, S*S*2)
	cls := image.NewGray(image.Rect(0, 0, S, S))
	shr := image.NewGray(image.Rect(0, 0, S, S))
	t.MinM, t.MaxM = math.Inf(1), math.Inf(-1)
	var nd, water, measured int
	for i := 0; i < S; i++ {
		for j := 0; j < S; j++ {
			gi := (r*N+i)*W + c*N + j
			v := float64(dem.Data[gi])
			var u uint16
			if v > noData+1 {
				u = uint16(math.Round((v - zmin) / (zmax - zmin) * 65535))
				t.MinM, t.MaxM = math.Min(t.MinM, v), math.Max(t.MaxM, v)
			} else {
				nd++
			}
			binary.LittleEndian.PutUint16(buf[(i*S+j)*2:], u)
			cls.Pix[i*S+j], shr.Pix[i*S+j] = class[gi], shore[gi]
			if v <= noData+1 {
				continue
			}
			if v < 0 {
				water++
				if meas[gi] == 1 {
					measured++
				}
			}
		}
	}
	if nd == S*S {
		t.MinM, t.MaxM = 0, 0
	}
	t.NoData = float64(nd) / float64(S*S)
	t.Water = float64(water) / float64(S*S)
	if water > 0 {
		t.Measured = float64(measured) / float64(water)
	}
	sum := sha256.Sum256(buf)
	t.SHA256 = hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(out, t.Height), buf, 0o644); err != nil {
		return err
	}
	if err := writePNG(filepath.Join(out, t.Depth), cls); err != nil {
		return err
	}
	return writePNG(filepath.Join(out, t.Shore), shr)
}

func writePNG(path string, img image.Image) error {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
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
	for _, s := range m.Sources {
		h.Write([]byte(s.SHA256))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// nativeResolution returns the mean ground size in metres of one source cell.
func nativeResolution(tif string, lat float64) (float64, error) {
	out, err := gdal.Run("gdalinfo", "-json", tif)
	if err != nil {
		return 0, err
	}
	var info struct {
		GT []float64 `json:"geoTransform"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil || len(info.GT) < 6 {
		return 0, fmt.Errorf("gdalinfo %s: no geotransform", tif)
	}
	dx := math.Abs(info.GT[1]) * 111320 * math.Cos(lat*math.Pi/180)
	dy := math.Abs(info.GT[5]) * 110574
	return (dx + dy) / 2, nil
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

// localSources wraps user-provided files with provenance (sha256, mtime, declared license).
func localSources(l *Local) (topo, mask, coast *fetch.Provenance, err error) {
	wrap := func(name, path string) (*fetch.Provenance, error) {
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
			SHA256: sum, Bytes: fi.Size(), License: l.License, Attribution: l.Attribution, File: abs}, nil
	}
	if topo, err = wrap("topo", l.Topo); err != nil {
		return
	}
	if l.Mask != "" {
		if mask, err = wrap("mask", l.Mask); err != nil {
			return
		}
	}
	if l.Coastline != "" {
		if coast, err = wrap("coastline", l.Coastline); err != nil {
			return
		}
	}
	return
}

// checkpoints picks the highest and lowest vertices and the four world corners.
// Elevations are the values as quantised into the tiles.
func checkpoints(dem *gdal.Raster, g *Grid, zmin, zmax float64, W, H int) []Checkpoint {
	q := func(v float32) float64 {
		return zmin + math.Round((float64(v)-zmin)/(zmax-zmin)*65535)/65535*(zmax-zmin)
	}
	at := func(name string, c, r int) *Checkpoint {
		v := dem.Data[r*W+c]
		if v <= noData+1 {
			return nil
		}
		return &Checkpoint{Name: name, Easting: g.OriginE + float64(c)*g.CellM, Northing: g.OriginN - float64(r)*g.CellM, ElevationM: q(v)}
	}
	hi, lo := -1, -1
	for i, v := range dem.Data {
		if v <= noData+1 {
			continue
		}
		if hi < 0 || v > dem.Data[hi] {
			hi = i
		}
		if lo < 0 || v < dem.Data[lo] {
			lo = i
		}
	}
	var out []Checkpoint
	for _, c := range []*Checkpoint{
		at("highest", hi%W, hi/W), at("lowest", lo%W, lo/W),
		at("corner_nw", 0, 0), at("corner_ne", W-1, 0), at("corner_sw", 0, H-1), at("corner_se", W-1, H-1),
	} {
		if c != nil {
			out = append(out, *c)
		}
	}
	return out
}
