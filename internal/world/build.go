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
	"time"

	"github.com/ocean-compiler/ocean/internal/emodnet"
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
	// LockPath, when set, pins sources: an existing lock must match what was resolved (error otherwise);
	// UpdateLock rewrites it from this build.
	LockPath   string
	UpdateLock bool
	// Force rebuilds even when the existing output was made from identical inputs (same input_key).
	Force    bool
	Log      func(format string, a ...any)
	finalDir string // where the finished output will live; checked for an up-to-date build
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
	final := filepath.Clean(opt.OutDir)
	parent := filepath.Dir(final)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
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

func build(cfg *Config, opt Options) (*Manifest, bool, error) {
	span := cfg.TileSpanM()
	w, s, e, n := cfg.BBox[0], cfg.BBox[1], cfg.BBox[2], cfg.BBox[3]
	clat := (s + n) / 2

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
		if topo, mask, coast, err = localSources(cfg.Local, opt.CacheDir); err != nil {
			return nil, false, err
		}
		if coast != nil {
			m.Sources = append(m.Sources, *coast)
			coastGeoJSON = coast.File
		}
	} else {
		opt.logf("fetching GMRT topo+mask for %.4f,%.4f,%.4f,%.4f", fb.W, fb.S, fb.E, fb.N)
		if topo, err = gmrt.Fetch(opt.CacheDir, fb, gmrt.LayerTopo); err != nil {
			return nil, false, err
		}
		if mask, err = gmrt.Fetch(opt.CacheDir, fb, gmrt.LayerMask); err != nil {
			return nil, false, err
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
	var regionalSrc *fetch.Provenance
	if cfg.RegionalBathymetry == "emodnet" {
		opt.logf("fetching EMODnet bathymetry")
		if regionalSrc, err = emodnet.Fetch(opt.CacheDir, cfg.RegionalRelease, fb.W, fb.S, fb.E, fb.N); err != nil {
			return nil, false, fmt.Errorf("regional_bathymetry emodnet: %w", err)
		}
	}
	m.Sources = append([]fetch.Provenance{*topo}, m.Sources...)
	if mask != nil {
		m.Sources = append(m.Sources[:1], append([]fetch.Provenance{*mask}, m.Sources[1:]...)...)
	}
	nativeRes, err := nativeResolution(topo.File, clat)
	if err != nil {
		return nil, false, err
	}
	m.InputKey = inputKey(cfg, m.Sources, regionalSrc)
	if !opt.Force && opt.finalDir != "" {
		if old := reusable(opt.finalDir, m.InputKey); old != nil {
			opt.logf("up to date: %s already holds this build (input %s); use -force to rebuild", opt.finalDir, short(m.InputKey))
			return old, true, nil
		}
	}

	// 2. Tile-aligned metric grid in UTM.
	plan, err := PlanGrid(cfg)
	if err != nil {
		return nil, false, err
	}
	epsg, srs := plan.EPSG, plan.SRS
	W, H, tx, ty := plan.W, plan.H, plan.Grid.TilesX, plan.Grid.TilesY
	originE, originN := plan.Grid.OriginE, plan.Grid.OriginN
	m.Stats.UTMScaleMin, m.Stats.UTMScaleMax = plan.ScaleMin, plan.ScaleMax
	if plan.Note != "" {
		m.Stats.Notes = append(m.Stats.Notes, plan.Note)
		opt.logf("WARNING: %s", plan.Note)
	}
	g := &m.Grid
	*g = plan.Grid
	opt.logf("grid %dx%d samples, %dx%d tiles of %d m, %s", W, H, tx, ty, int(span), srs)

	work, err := os.MkdirTemp(opt.CacheDir, "work-")
	if err != nil {
		return nil, false, err
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
	// Warped rasters are cached by (source hash, kind, method, grid, GDAL version): a rebuild after a change that
	// does not move the grid or the sources (depth bands, conforming, seamarks, LOD...) skips gdalwarp entirely.
	gver := gdal.Version()
	warpCached := func(src *fetch.Provenance, kind, method string) (string, error) {
		if err := gdal.Ctx.Err(); err != nil {
			return "", err
		}
		k := sha256.Sum256([]byte(fmt.Sprint(src.SHA256, "|", kind, "|", method, "|", te, "|", W, "x", H, "|", srs, "|", gver)))
		path := filepath.Join(opt.CacheDir, "stages", hex.EncodeToString(k[:12])+"-"+kind+".bin")
		if fi, err := os.Stat(path); err == nil && fi.Size() == int64(W)*int64(H)*4 {
			opt.logf("stage cache hit: %s", kind)
			return path, nil
		}
		tmp := filepath.Join(work, kind+".bin")
		if err := warp(src.File, tmp, method, "Float32"); err != nil {
			return "", err
		}
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil && os.Rename(tmp, path) == nil {
			return path, nil
		}
		return tmp, nil
	}
	demPath, err := warpCached(topo, "dem", cfg.Resampling)
	if err != nil {
		return nil, false, err
	}
	// topo-mask holds the same elevations but NaN wherever GMRT has no ~100 m
	// ocean survey (there topo is filled from GEBCO_08). Nearest-neighbour keeps it binary.
	maskSrc := topo // no mask supplied: no cell is claimed to be survey-backed
	if mask != nil {
		maskSrc = mask
	}
	maskPath, err := warpCached(maskSrc, "mask", "near")
	if err != nil {
		return nil, false, err
	}
	dem, err := gdal.ReadF32(demPath, W, H)
	if err != nil {
		return nil, false, err
	}
	mraw, err := gdal.ReadF32(maskPath, W, H)
	if err != nil {
		return nil, false, err
	}
	meas := make([]uint8, W*H)
	if mask != nil {
		for i, v := range mraw.Data {
			if !math.IsNaN(float64(v)) && v > noData+1 {
				meas[i] = 1
			}
		}
	}

	src := initialSource(dem.Data, meas)

	// 2a0. Optional regional bathymetry: replaces GEBCO-filled water cells; runs first so the conform step sees final depths.
	if regionalSrc != nil {
		before := append([]float32(nil), dem.Data...)
		regPath, err := warpCached(regionalSrc, "regional", "bilinear")
		if err != nil {
			return nil, false, err
		}
		reg, err := gdal.ReadF32(regPath, W, H)
		if err != nil {
			return nil, false, err
		}
		rs, err := fillRegional(dem.Data, reg.Data, meas, W, H, cfg.CellM,
			func(i int) bool { return dem.Data[i] > noData+1 },
			func(v float32) bool { return v > noData+1 && !math.IsNaN(float64(v)) })
		if err != nil {
			return nil, false, fmt.Errorf("regional_bathymetry emodnet: %w", err)
		}
		markChanged(src, before, dem.Data, SrcRegional)
		rs.Source, _ = emodnet.CoverageID(cfg.RegionalRelease)
		m.Stats.Regional = &rs
		m.Sources = append(m.Sources, *regionalSrc)
		opt.logf("regional bathymetry: %d fill cells replaced (max change %.1f m), %d left; offset vs GMRT survey cells median %.2f m, rms %.2f m over %d cells",
			rs.CellsReplaced, rs.MaxChangeM, rs.FillCellsLeft, rs.MedianOffsetM, rs.RMSOffsetM, rs.OverlapCells)
	}

	// 2a. Optional coastline conforming: runs before the refinement so edits see the final land/water split.
	var conform *ConformStats
	if cfg.CoastConform == "osm" {
		if coastGeoJSON == "" {
			return nil, false, fmt.Errorf("coast_conform is set but no OSM coastline is available: %s", m.Stats.CoastlineNote)
		}
		lines, err := projectedLines(coastGeoJSON, srs, work, g)
		if err != nil {
			return nil, false, err
		}
		validNow := func(i int) bool { return dem.Data[i] > noData+1 }
		raw, err := coastCheck(coastGeoJSON, srs, work, dem, shoreMask(dem.Data, W, H, validNow), g, W, H)
		if err != nil {
			return nil, false, err
		}
		cs, changed, err := conformGrid(dem.Data, W, H, cfg.CellM, cfg.CoastConformMaxM, lines, validNow)
		if err != nil {
			return nil, false, err
		}
		for _, i := range changed {
			meas[i] = 0 // synthetic cells are not survey-backed
		}
		cs.RawOffset = raw
		cs.Note = "Flipped cells get +-0.5 m. offset_before is the independent figure; offset_after compares the OSM line with a DEM built from it."
		conform = &cs
		opt.logf("coast conform: %d cells to land, %d to water, %d skipped (deep/high); DEM agreed on %.0f%% of %d labelled cells before",
			cs.ToLand, cs.ToWater, cs.SkippedGuard, 100*cs.Agreement, cs.Labelled)
	}

	var refBase, refName string
	// 2b. Optional land-elevation edit layer (e.g. from LibreTerrain). Applied on the whole
	// world grid, before masks and tiles, so edits cannot open cracks between tiles.
	if cfg.Refinement != "" {
		ref, delta, err := LoadRefinement(filepath.Join(cfg.dir, cfg.Refinement))
		if err != nil {
			return nil, false, err
		}
		if err := ref.Check(g, epsg, W, H); err != nil {
			return nil, false, err
		}
		st := applyRefinement(dem.Data, W, H, delta, ref.BlendCells)
		st.BaseBuildID = ref.BaseBuildID
		m.Refinement = &st
		refBase, refName = ref.BaseBuildID, ref.BaseWorld
		m.Sources = append(m.Sources, ref.Provenance())
		opt.logf("refinement %s: %d land cells changed (max %.2f m), %d clamped at sea level, %d edits on water ignored",
			ref.Editor, st.CellsChanged, st.MaxAbsChangeM, st.ClampedLandCells, st.IgnoredWaterCells)
	}

	// 3. Level-0 raster: the one grid every tile and every LOD level is cut from, so borders always agree.
	zmin, zmax := math.Inf(1), math.Inf(-1)
	for _, v := range dem.Data {
		if v > noData+1 {
			zmin, zmax = math.Min(zmin, float64(v)), math.Max(zmax, float64(v))
		}
	}
	if math.IsInf(zmin, 1) {
		return nil, false, fmt.Errorf("no valid elevation samples inside bbox: source does not cover the area")
	}
	base := &raster{W: W, H: H, dem: dem.Data, src: src}
	base.derive(cfg.DepthBands)
	st := &m.Stats
	var nWater, nMeasured, nNoData int
	st.BySource = map[string]int{}
	for i, c := range base.class {
		st.BySource[srcName(int(src[i]))]++
		switch {
		case c == ClassNoData:
			nNoData++
		case c > 0:
			nWater++
			if src[i] == SrcSurvey {
				nMeasured++
			}
		}
	}
	st.ShoreCells = base.shores
	st.Cells = W * H
	st.NoDataFraction = float64(nNoData) / float64(W*H)
	st.WaterFraction = float64(nWater) / float64(W*H)
	if nWater > 0 {
		st.MeasuredWater = float64(nMeasured) / float64(nWater)
	}

	// 4. Tiles for level 0 and every LOD level. One gdaltransform call covers every tile corner (subprocess
	// spawns dominated build time).
	nLOD := cfg.LODLevels
	switch {
	case nLOD == 0:
		nLOD = autoLevels(g)
	case nLOD < 0:
		nLOD = 0
	}
	var plans []levelPlan
	var slots [][]tileSlot
	var all [][2]float64
	for k := 0; k <= nLOD; k++ {
		p := planLevel(g, k)
		plans, slots = append(plans, p), append(slots, slotsOf(p))
		for _, sl := range slots[k] {
			for _, c := range sl.utmCorners() {
				all = append(all, c)
			}
		}
	}
	corners, err := gdal.Transform(srs, "EPSG:4326", all)
	if err != nil {
		return nil, false, err
	}
	off := 0
	for k := range slots {
		for i := range slots[k] {
			slots[k][i].corners = corners[off : off+4]
			off += 4
		}
	}
	for k := 0; k <= nLOD; k++ {
		lr := base
		if k > 0 {
			lr = decimate(base, g, plans[k])
			lr.derive(cfg.DepthBands)
		}
		tl, err := writeLevelTiles(opt.OutDir, cfg, g, plans[k], lr, slots[k])
		if err != nil {
			return nil, false, err
		}
		m.Tiles = append(m.Tiles, tl...)
		count := len(tl)
		m.LOD.Levels = append(m.LOD.Levels, LODLevel{Level: k, CellM: plans[k].CellM, TileSpanM: plans[k].SpanM,
			FirstX: plans[k].X0, LastX: plans[k].X1, FirstY: plans[k].Y0, LastY: plans[k].Y1, Tiles: count})
		opt.logf("level %d: %d tiles, cell %.0f m", k, count, plans[k].CellM)
	}
	linkTiles(m.Tiles)
	m.LOD.Method = "vertex-decimation"
	m.LOD.Note = "Level k keeps every 2^k-th vertex of level 0: values are real level-0 samples, never averaged, so tile edges and parent/child corners agree exactly. Thin features narrower than 2^k cells can vanish at coarse levels; depth classes and shore are recomputed per level from the decimated heights."
	m.Checkpoints = checkpoints(dem, g, W, H)

	// 5. Coastline: copy to output and compare against the DEM.
	if coastGeoJSON != "" {
		b, err := os.ReadFile(coastGeoJSON)
		if err != nil {
			return nil, false, err
		}
		if err := os.WriteFile(filepath.Join(opt.OutDir, "coastline.wgs84.geojson"), b, 0o644); err != nil {
			return nil, false, err
		}
		m.Files["coastline"] = "coastline.wgs84.geojson"
		cc, err := coastCheck(coastGeoJSON, srs, work, dem, base.shore, g, W, H)
		if err != nil {
			return nil, false, err
		}
		st.CoastlineCheck = cc
		if conform != nil {
			conform.AfterOffset = cc
			st.CoastlineCheck = conform.RawOffset // headline stays the pre-conform measurement
		}
	}

	st.CoastConform = conform

	// 5b. Seamarks (optional): OpenSeaMap points, projected to the grid and cross-checked against the DEM.
	if cfg.Seamarks {
		opt.logf("fetching OSM seamarks")
		// The rendered grid reaches up to a tile span beyond the configured bbox: cover all of it.
		bb := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, t := range m.Tiles {
			bb = [4]float64{math.Min(bb[0], t.BoundsWGS[0]), math.Min(bb[1], t.BoundsWGS[1]), math.Max(bb[2], t.BoundsWGS[2]), math.Max(bb[3], t.BoundsWGS[3])}
		}
		prov, ss, err := seamarks(bb, srs, dem, g, W, H, opt)
		if err != nil {
			st.Seamarks = &SeamarkStats{Note: "OSM seamarks unavailable: " + err.Error()}
			opt.logf("WARNING: %s", st.Seamarks.Note)
		} else {
			m.Sources = append(m.Sources, *prov)
			m.Files["seamarks"] = "seamarks.geojson"
			st.Seamarks = ss
		}
	}

	// 6. Manifest.
	m.Tool.Version, m.Tool.GDAL = ToolVersion, gdal.Version()
	m.CRS = CRS{EPSG: epsg, Name: fmt.Sprintf("WGS 84 / UTM zone %d%s", epsg%100, map[bool]string{true: "N", false: "S"}[epsg < 32700])}
	m.Vertical.Unit = "metre"
	m.Vertical.Datum = "unspecified: GMRT mosaic of contributed grids with heterogeneous vertical references"
	m.Vertical.Note = "Zero is treated as the land/water boundary. No vertical datum reconciliation is performed; the OSM coastline (a high-water-line convention that varies by country) and GMRT zero are not guaranteed to coincide."
	m.Height = HeightEncoding{Type: "float32", ByteOrder: "little", RowOrder: "north_to_south", Unit: "metre", NoData: noData, MinM: zmin, MaxM: zmax,
		Note: "IEEE-754 float32 metres, positive up, zero = land/water boundary. Cells without a value are exactly nodata and have depth class 255 and source 0"}
	m.SourceLayers = sourceLayers()
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
		if strings.HasPrefix(p.URL, "file://") && p.File != "" {
			m.Sources[i].URL = "file://" + filepath.Base(p.File)
		}
	}
	if cfg.Local == nil {
		m.Attribution = append(m.Attribution, "GEBCO_08 Grid — fills water cells without survey data inside GMRT's topo layer (≈900 m) — public domain, https://www.gebco.net/data-products/gridded-bathymetry/terms-of-use")
	}
	m.Attribution = append(m.Attribution, "NOT FOR NAVIGATION: bathymetry here is a game-development dataset (GMRT terms: data should not be used for navigation purposes)")
	sort.Strings(m.Attribution)
	// Last, so m.Sources is complete (regional grid, refinement) and a failed build never writes a lock.
	if err := applyLock(&opt, m.Sources); err != nil {
		return nil, false, err
	}
	m.BuildID = buildID(m)
	if cfg.Refinement != "" {
		// The edit layer is a delta against one specific baseline: refuse to apply it over a world whose inputs,
		// configuration or algorithms changed since the export.
		if base := baseBuildID(m, refName); base != refBase {
			return nil, false, fmt.Errorf("refinement was exported from build %s but the base of this world is build %s: sources, config or tool version changed since the export; export again", short(refBase), short(base))
		}
	}
	if err := writeJSON(filepath.Join(opt.OutDir, ManifestFile), m); err != nil {
		return nil, false, err
	}
	var att bytes.Buffer
	fmt.Fprintf(&att, "World %q built by Ocean Compiler %s (build %s)\n\nThis output derives from the following sources; respect their licenses when redistributing.\n\n", m.Name, ToolVersion, m.BuildID[:12])
	for _, a := range m.Attribution {
		fmt.Fprintln(&att, "- "+a)
	}
	if err := os.WriteFile(filepath.Join(opt.OutDir, "ATTRIBUTION.txt"), att.Bytes(), 0o644); err != nil {
		return nil, false, err
	}
	return m, false, nil
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
func inputKey(cfg *Config, srcs []fetch.Provenance, regional *fetch.Provenance) string {
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
	if err != nil || m.SchemaVersion != SchemaVersion || m.InputKey != key {
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

// localSources wraps user-provided files with provenance (sha256, mtime, declared license). A path starting
// with http:// or https:// is downloaded once into the cache (fetch.Get: sha256, retrieval time) and used as a file.
func localSources(l *Local, cacheDir string) (topo, mask, coast *fetch.Provenance, err error) {
	wrap := func(name, path, file string) (*fetch.Provenance, error) {
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			return fetch.Get(cacheDir, "remote-"+name, path, nil, file, l.License, l.Attribution)
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
			SHA256: sum, Bytes: fi.Size(), License: l.License, Attribution: l.Attribution, File: abs}, nil
	}
	if topo, err = wrap("topo", l.Topo, "topo.tif"); err != nil {
		return
	}
	if l.Mask != "" {
		if mask, err = wrap("mask", l.Mask, "mask.tif"); err != nil {
			return
		}
	}
	if l.Coastline != "" {
		if coast, err = wrap("coastline", l.Coastline, "coastline.geojson"); err != nil {
			return
		}
	}
	return
}

// checkpoints picks the highest and lowest vertices and the four world corners.
// Elevations are the float32 values stored in the tiles.
func checkpoints(dem *gdal.Raster, g *Grid, W, H int) []Checkpoint {
	at := func(name string, c, r int) *Checkpoint {
		v := dem.Data[r*W+c]
		if v <= noData+1 {
			return nil
		}
		return &Checkpoint{Name: name, Easting: g.OriginE + float64(c)*g.CellM, Northing: g.OriginN - float64(r)*g.CellM, ElevationM: float64(v)}
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
