package world

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ocean-compiler/ocean/internal/emodnet"
	"github.com/ocean-compiler/ocean/internal/fetch"
	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/internal/gmrt"
	"github.com/ocean-compiler/ocean/internal/osm"
)

// FormatRevision is the additive revision of schema 2 this tool writes (see Manifest.FormatRevision).
const FormatRevision = 1

// bctx is the state of one build.
type bctx struct {
	cfg  *Config
	opt  Options
	srs  string
	te   []string
	W, H int
	work string
	gver string
	g    *Grid
}

// memBytes is the memory budget for windows and in-memory steps.
func (b *bctx) memBytes() int64 {
	mb := b.opt.MemoryMB
	if mb <= 0 {
		mb = 1024
	}
	return int64(mb) << 20
}

// inMemory reports whether the whole grid may be held in memory for a step that needs it (about 24 bytes per cell:
// heights, kind, dataset, a before-copy and a second raster).
func (b *bctx) inMemory() bool { return int64(b.W)*int64(b.H)*24 <= b.memBytes() }

func build(cfg *Config, opt Options) (*Manifest, bool, error) {
	if err := cfg.checkAntimeridian(); err != nil { // before any download
		return nil, false, err
	}
	span := cfg.TileSpanM()
	w, s, e, n := cfg.BBox[0], cfg.BBox[1], cfg.BBox[2], cfg.BBox[3]
	clat := (s + n) / 2

	// 1. Fetch sources with a padding of ~1.2 tiles so the tile-aligned grid is covered.
	pad := math.Max(span*1.2, 500)
	dLat := pad / 110574.0
	dLon := pad / (111320.0 * math.Cos(clat*math.Pi/180))
	boxes := fetchBoxes(w, s, e, n, dLon, dLat)
	fb := boxes[0]
	// GDAL subprocesses get a share of the memory budget (the process-wide setting is the same for every build).
	gdal.CacheMB = int(min(max((&bctx{opt: opt}).memBytes()>>20/8, 64), 512))
	m := &Manifest{SchemaVersion: SchemaVersion, Name: cfg.Name, Config: *cfg, Files: map[string]string{}, FormatRevision: FormatRevision}
	dss, err := resolveDatasets(cfg, boxes, opt)
	if err != nil {
		return nil, false, err
	}
	var coastProv *fetch.Provenance
	var coastGeoJSON string
	if cfg.Local != nil {
		if cfg.Local.Coastline != "" {
			if coastProv, err = wrapLocal("coastline", cfg.Local.Coastline, "coastline.geojson", opt.CacheDir, cfg.Local.License, cfg.Local.Attribution); err != nil {
				return nil, false, err
			}
			coastGeoJSON = coastProv.File
		}
	} else if cfg.Coastline == "osm" {
		opt.logf("fetching OSM coastline")
		p, gj, err := osm.FetchCoastline(opt.CacheDir, fb.W, fb.S, fb.E, fb.N)
		if err != nil {
			m.Stats.CoastlineNote = "OSM coastline unavailable: " + err.Error()
			opt.logf("WARNING: %s", m.Stats.CoastlineNote)
		} else {
			coastProv, coastGeoJSON = p, gj
		}
	}
	var regionalSrc *fetch.Provenance
	if cfg.RegionalBathymetry == "emodnet" {
		opt.logf("fetching EMODnet bathymetry")
		if regionalSrc, err = emodnet.Fetch(opt.CacheDir, cfg.RegionalRelease, fb.W, fb.S, fb.E, fb.N); err != nil {
			return nil, false, fmt.Errorf("regional_bathymetry emodnet: %w", err)
		}
	}
	var plists [][]*fetch.Provenance
	for _, d := range dss {
		plists = append(plists, d.vals, d.masks)
	}
	m.Sources = uniqueProvenance(plists...)
	if coastProv != nil {
		m.Sources = append(m.Sources, *coastProv)
	}
	for _, d := range dss {
		for _, v := range d.vals { // the coarsest piece decides: never label the world with a finer source than it received
			r, err := groundSize(v.File, clat)
			if err != nil {
				return nil, false, err
			}
			d.nativeResM = math.Max(d.nativeResM, r)
		}
	}
	vinfo, err := resolveVerticalSet(cfg, dss, opt)
	if err != nil {
		return nil, false, err
	}
	ordered := orderDatasets(dss, cfg.Precedence)
	m.InputKey = inputKey(cfg, m.Sources, regionalSrc, dss)
	if !opt.Force && opt.finalDir != "" {
		if old := reusable(opt.finalDir, m.InputKey); old != nil {
			opt.logf("up to date: %s already holds this build (input %s); use -force to rebuild", opt.finalDir, short(m.InputKey))
			return old, true, nil
		}
	}

	// 2. Tile-aligned metric grid in UTM; every dataset is warped onto it once.
	plan, err := PlanGrid(cfg)
	if err != nil {
		return nil, false, err
	}
	epsg, srs := plan.EPSG, plan.SRS
	m.SchemaVersion = plan.schema()
	W, H, tx, ty := plan.W, plan.H, plan.Grid.TilesX, plan.Grid.TilesY
	originE, originN := plan.Grid.OriginE, plan.Grid.OriginN
	m.Stats.UTMScaleMin, m.Stats.UTMScaleMax = plan.ScaleMin, plan.ScaleMax
	if plan.Note != "" {
		m.Stats.Notes = append(m.Stats.Notes, plan.Note)
		opt.logf("WARNING: %s", plan.Note)
	}
	m.Grid = plan.Grid
	g := &m.Grid
	opt.logf("grid %dx%d samples, %dx%d tiles of %d m, %s", W, H, tx, ty, int(span), srs)
	if err := os.MkdirAll(opt.CacheDir, 0o755); err != nil {
		return nil, false, err
	}
	work, err := os.MkdirTemp(opt.CacheDir, "work-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(work)
	// Vertex-registered grid: sample k sits at originE + k*cell, so pixel edges are offset by cell/2.
	h := cfg.CellM / 2
	te := []string{fmt.Sprint(originE - h), fmt.Sprint(originN - float64(H-1)*cfg.CellM - h),
		fmt.Sprint(originE + float64(W-1)*cfg.CellM + h), fmt.Sprint(originN + h)}
	b := &bctx{cfg: cfg, opt: opt, srs: srs, te: te, W: W, H: H, work: work, gver: gdal.Version(), g: g}
	for _, d := range dss {
		if err := b.warpDataset(d); err != nil {
			return nil, false, err
		}
	}

	// 3. Composition into the level-0 layer files.
	lay0 := &layerFiles{W: W, H: H, demPath: filepath.Join(work, "l0.dem"), kindPath: filepath.Join(work, "l0.kind"), provPath: filepath.Join(work, "l0.prov")}
	comp, err := b.composeLevel0(ordered, lay0)
	if err != nil {
		return nil, false, err
	}
	m.Stats.Composition = comp
	for i, d := range ordered {
		opt.logf("dataset %d %s: %d cells supplied, %d overridden by a better-ranked dataset", i+1, d.spec.ID, comp.Datasets[i].Selected, comp.Datasets[i].Overridden)
	}
	infos := make([]DatasetInfo, 0, len(dss)+3)
	for _, d := range dss {
		di := DatasetInfo{Index: d.idx + 1, ID: d.spec.ID, Type: d.spec.Type, Role: "elevation", Part: d.spec.Part, Quality: d.spec.Quality,
			Year: d.spec.Year, AccuracyM: d.spec.AccuracyM, NativeResM: round2(d.nativeResM), Vertical: d.vapplied, DeclaredDtm: d.datumName(), Delivery: d.delivery}
		for _, p := range append(append([]*fetch.Provenance(nil), d.vals...), d.masks...) {
			di.Sources, di.SourceSHA256 = append(di.Sources, p.Source), append(di.SourceSHA256, p.SHA256)
		}
		for r, o := range ordered {
			if o == d {
				di.Rank = r + 1
			}
		}
		infos = append(infos, di)
	}
	extra := func(id, typ, role string, p *fetch.Provenance) uint8 {
		di := DatasetInfo{Index: len(infos) + 1, ID: id, Type: typ, Role: role}
		if p != nil {
			di.Sources, di.SourceSHA256 = []string{p.Source}, []string{p.SHA256}
		}
		infos = append(infos, di)
		return uint8(di.Index)
	}

	// 3a. Steps that need the whole grid (the regional fill estimates one global offset; conforming guards on a global
	// agreement; the edit layer is a whole-grid delta). They run in memory and refuse worlds above the budget.
	var conform *ConformStats
	var refBase, refName string
	if cfg.RegionalBathymetry != "" || cfg.CoastConform != "" || cfg.Refinement != "" {
		if !b.inMemory() {
			return nil, false, fmt.Errorf("regional_bathymetry, coast_conform and refinement work on the whole %dx%d grid in memory (about %d MB); this world needs more than the %d MB budget: raise the budget (-mem) or build without those steps", W, H, int64(W)*int64(H)*24>>20, b.memBytes()>>20)
		}
		demv, kind, prov, err := readLayerToMemory(lay0)
		if err != nil {
			return nil, false, err
		}
		dem := &gdal.Raster{W: W, H: H, Data: demv}
		if regionalSrc != nil {
			before := append([]float32(nil), dem.Data...)
			regPath, err := b.warpPlain(regionalSrc, "regional", "bilinear")
			if err != nil {
				return nil, false, err
			}
			reg, err := gdal.ReadF32(regPath, W, H)
			if err != nil {
				return nil, false, err
			}
			meas := make([]uint8, W*H)
			for i, k := range kind {
				if k == SrcSurvey {
					meas[i] = 1
				}
			}
			rs, err := fillRegional(dem.Data, reg.Data, meas, W, H, cfg.CellM,
				func(i int) bool { return dem.Data[i] > noData+1 },
				func(v float32) bool { return v > noData+1 && !math.IsNaN(float64(v)) })
			if err != nil {
				return nil, false, fmt.Errorf("regional_bathymetry emodnet: %w", err)
			}
			id := extra("emodnet", "emodnet", "regional-fill", regionalSrc)
			markChangedProv(kind, prov, before, dem.Data, SrcRegional, id)
			rs.Source, _ = emodnet.CoverageID(cfg.RegionalRelease)
			m.Stats.Regional = &rs
			m.Sources = append(m.Sources, *regionalSrc)
			opt.logf("regional bathymetry: %d fill cells replaced (max change %.1f m), %d left; offset vs GMRT survey cells median %.2f m, rms %.2f m over %d cells",
				rs.CellsReplaced, rs.MaxChangeM, rs.FillCellsLeft, rs.MedianOffsetM, rs.RMSOffsetM, rs.OverlapCells)
		}
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
			before := append([]float32(nil), dem.Data...)
			cs, _, err := conformGrid(dem.Data, W, H, cfg.CellM, cfg.CoastConformMaxM, lines, validNow)
			if err != nil {
				return nil, false, err
			}
			id := extra("osm-coastline", "osm", "coastline", coastProv)
			markChangedProv(kind, prov, before, dem.Data, SrcConformed, id)
			cs.RawOffset = raw
			cs.Note = "Flipped cells get +-0.5 m. offset_before is the independent figure; offset_after compares the OSM line with a DEM built from it."
			conform = &cs
			opt.logf("coast conform: %d cells to land, %d to water, %d skipped (deep/high); DEM agreed on %.0f%% of %d labelled cells before",
				cs.ToLand, cs.ToWater, cs.SkippedGuard, 100*cs.Agreement, cs.Labelled)
		}
		if cfg.Refinement != "" {
			ref, delta, err := LoadRefinement(filepath.Join(cfg.dir, cfg.Refinement))
			if err != nil {
				return nil, false, err
			}
			if plan.TM != nil {
				return nil, false, fmt.Errorf("refinement: not supported on a local transverse Mercator world (the exchange records an EPSG code only)")
			}
			if err := ref.Check(g, epsg, W, H); err != nil {
				return nil, false, err
			}
			before := append([]float32(nil), dem.Data...)
			st := applyRefinement(dem.Data, W, H, delta, ref.BlendCells)
			st.BaseBuildID = ref.BaseBuildID
			m.Refinement = &st
			refBase, refName = ref.BaseBuildID, ref.BaseWorld
			rp := ref.Provenance()
			m.Sources = append(m.Sources, rp)
			id := extra("refinement", "refinement", "edit", &rp)
			markChangedProv(kind, prov, before, dem.Data, SrcRefined, id)
			opt.logf("refinement %s: %d land cells changed (max %.2f m), %d clamped at sea level, %d edits on water ignored",
				ref.Editor, st.CellsChanged, st.MaxAbsChangeM, st.ClampedLandCells, st.IgnoredWaterCells)
		}
		if err := writeLayerFromMemory(lay0, dem.Data, kind, prov); err != nil {
			return nil, false, err
		}
	}

	// 4. World statistics from the final level-0 layer (streamed), then tiles for level 0 and every LOD level.
	ws, err := b.scanLayer(lay0, infos)
	if err != nil {
		return nil, false, err
	}
	if !ws.any {
		return nil, false, fmt.Errorf("no valid elevation samples inside bbox: source does not cover the area")
	}
	st := &m.Stats
	st.BySource, st.CellsByDataset = ws.bySource, ws.byDataset
	st.Cells = W * H
	st.NoDataFraction = float64(ws.nodata) / float64(W*H)
	st.WaterFraction = float64(ws.water) / float64(W*H)
	if ws.water > 0 {
		st.MeasuredWater = float64(ws.measured) / float64(ws.water)
	}
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
	// Longitudes come back in -180..180; they are expressed on the branch nearest the bbox centre, so a world across
	// the antimeridian has east above 180 and no tile envelope (coarse tiles reach far past the bbox) spans 360 degrees.
	for i := range corners {
		corners[i][0] = (w+e)/2 + wrapLon(corners[i][0]-(w+e)/2)
	}
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
	b.opt.OutDir = opt.OutDir
	prev := lay0
	for k := 0; k <= nLOD; k++ {
		lay := lay0
		if k > 0 {
			lay = &layerFiles{W: plans[k].W, H: plans[k].H, demPath: filepath.Join(work, fmt.Sprintf("l%d.dem", k)),
				kindPath: filepath.Join(work, fmt.Sprintf("l%d.kind", k)), provPath: filepath.Join(work, fmt.Sprintf("l%d.prov", k))}
			if err := b.decimateLayer(prev, plans[k-1], plans[k], lay); err != nil {
				return nil, false, err
			}
		}
		tl, shores, err := b.writeLevel(lay, plans[k], slots[k])
		if err != nil {
			return nil, false, err
		}
		if k == 0 {
			st.ShoreCells = shores
		}
		m.Tiles = append(m.Tiles, tl...)
		m.LOD.Levels = append(m.LOD.Levels, LODLevel{Level: k, CellM: plans[k].CellM, TileSpanM: plans[k].SpanM,
			FirstX: plans[k].X0, LastX: plans[k].X1, FirstY: plans[k].Y0, LastY: plans[k].Y1, Tiles: len(tl)})
		opt.logf("level %d: %d tiles, cell %.0f m", k, len(tl), plans[k].CellM)
		prev = lay
	}
	linkTiles(m.Tiles)
	m.LOD.Method = "vertex-decimation"
	m.LOD.Note = "Level k keeps every 2^k-th vertex of level 0: values are real level-0 samples, never averaged, so tile edges and parent/child corners agree exactly. Thin features narrower than 2^k cells can vanish at coarse levels; depth classes and shore are recomputed per level from the decimated heights."
	switch cfg.LODMethod {
	case "shoal":
		m.LOD.Method = "shoal-biased"
		m.LOD.Note = "Level k is built from level k-1: a water sample takes the shallowest water height of its 3x3 neighbourhood, so a coarse level is never deeper than any finer water sample it stands for (one step per level); land samples are unchanged. Changed samples carry source code 7 (lod-derived, not measured). Tiles of one level share edge samples exactly. `ocean lod-report` measures the vertical error."
	}
	m.Checkpoints = ws.checkpoints(g)

	// 5. Coastline: copy to output and compare against the DEM (needs the grid in memory; skipped above the budget).
	if coastGeoJSON != "" {
		bb, err := os.ReadFile(coastGeoJSON)
		if err != nil {
			return nil, false, err
		}
		if !cfg.NoCoastlineCopy {
			if err := os.WriteFile(filepath.Join(opt.OutDir, "coastline.wgs84.geojson"), bb, 0o644); err != nil {
				return nil, false, err
			}
			m.Files["coastline"] = "coastline.wgs84.geojson"
		}
		if b.inMemory() {
			dem, err := loadDem(lay0)
			if err != nil {
				return nil, false, err
			}
			cc, err := coastCheck(coastGeoJSON, srs, work, dem, shoreMask(dem.Data, W, H, func(i int) bool { return dem.Data[i] > noData+1 }), g, W, H)
			if err != nil {
				return nil, false, err
			}
			st.CoastlineCheck = cc
			if conform != nil {
				conform.AfterOffset = cc
				st.CoastlineCheck = conform.RawOffset // headline stays the pre-conform measurement
			}
		} else {
			st.CoastlineNote = "coastline-to-DEM check skipped: the grid is larger than the in-memory budget"
		}
	}
	st.CoastConform = conform

	// 5b. Seamarks (optional): OpenSeaMap points, projected to the grid and cross-checked against the DEM.
	if cfg.Seamarks {
		opt.logf("fetching OSM seamarks")
		bb := m.level0Extent()
		if !b.inMemory() {
			st.Seamarks = &SeamarkStats{Note: "OSM seamarks skipped: the grid is larger than the in-memory budget"}
		} else if dem, err := loadDem(lay0); err != nil {
			return nil, false, err
		} else if prov, ss, err := seamarks(bb, srs, dem, g, W, H, opt); err != nil {
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
	m.CRS = plan.CRS()
	m.Vertical = vinfo
	m.Height = HeightEncoding{Type: "float32", ByteOrder: "little", RowOrder: "north_to_south", Unit: "metre", NoData: noData, MinM: ws.zmin, MaxM: ws.zmax,
		Note: "IEEE-754 float32 metres, positive up, zero = land/water boundary. Cells without a value are exactly nodata and have depth class 255 and source 0"}
	m.SourceLayers = sourceLayers()
	m.Datasets = infos
	m.Resampling = Resampling{Method: cfg.Resampling, SourceResM: round2(ordered[0].nativeResM), OutputCellM: cfg.CellM, Upsampled: cfg.CellM < ordered[0].nativeResM}
	for _, d := range dss {
		if d.spec.Type == "gmrt" { // GEBCO_08 is 30 arc-seconds; GMRT documents it as the fill where no survey data exists.
			m.Resampling.FillResM = (30.0/3600*111320*math.Cos(clat*math.Pi/180) + 30.0/3600*110574) / 2
		}
	}
	m.DepthClasses = depthClasses(cfg.DepthBands)
	m.Grid.BBoxWGS84 = m.level0Extent()
	m.Files["attribution"] = "ATTRIBUTION.txt"
	for i, p := range m.Sources {
		m.Attribution = append(m.Attribution, fmt.Sprintf("%s — %s — license: %s", p.Source, p.Attribution, p.License))
		// Shipped manifests must not leak local paths.
		m.Sources[i].File = ""
		if strings.HasPrefix(p.URL, "file://") && p.File != "" {
			m.Sources[i].URL = "file://" + filepath.Base(p.File)
		}
	}
	for _, d := range dss {
		if d.spec.Type == "gmrt" {
			m.Attribution = append(m.Attribution, "GEBCO_08 Grid — fills water cells without survey data inside GMRT's topo layer (≈900 m) — public domain, https://www.gebco.net/data-products/gridded-bathymetry/terms-of-use")
			break
		}
	}
	m.Attribution = append(m.Attribution, "NOT FOR NAVIGATION: bathymetry here is a game-development dataset (GMRT terms: data should not be used for navigation purposes)")
	m.Licensing = buildLicensing(cfg, m, dss, regionalSrc != nil)
	m.Attribution = append(m.Attribution, m.Licensing.attributionLines()...)
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

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// markChangedProv labels every cell whose height differs from before: kind code and the dataset index of the step.
func markChangedProv(kind, prov []uint8, before, after []float32, code, id uint8) {
	for i := range after {
		if after[i] != before[i] {
			kind[i], prov[i] = code, id
		}
	}
}

// loadDem reads the heights of a layer into memory.
func loadDem(l *layerFiles) (*gdal.Raster, error) {
	if err := l.open(); err != nil {
		return nil, err
	}
	defer l.close()
	d, err := l.dem.rect(0, 0, l.H, l.W)
	return &gdal.Raster{W: l.W, H: l.H, Data: d}, err
}

// warpPlain warps a single extra raster (the legacy regional grid) onto the world grid with the stage cache.
func (b *bctx) warpPlain(src *fetch.Provenance, kind, method string) (string, error) {
	d := &dataset{spec: SourceSpec{ID: kind}, vals: []*fetch.Provenance{src}, valSrc: []string{src.File}}
	sav := b.cfg.Resampling
	b.cfg.Resampling = method
	defer func() { b.cfg.Resampling = sav }()
	if err := b.warpDataset(d); err != nil {
		return "", err
	}
	return d.valPath, nil
}

// worldScan is the result of one streamed pass over the final level-0 layer.
type worldScan struct {
	any                     bool
	nodata, water, measured int
	zmin, zmax              float64
	bySource, byDataset     map[string]int
	hi, lo                  [3]float64 // value, row, col of the highest / lowest sample (first in row-major order wins ties)
	corners                 map[string]*Checkpoint
}

func (b *bctx) scanLayer(l *layerFiles, infos []DatasetInfo) (*worldScan, error) {
	if err := l.open(); err != nil {
		return nil, err
	}
	defer l.close()
	ws := &worldScan{zmin: math.Inf(1), zmax: math.Inf(-1), bySource: map[string]int{}, byDataset: map[string]int{}}
	names := map[int]string{}
	for _, di := range infos {
		names[di.Index] = di.ID
	}
	W, H := l.W, l.H
	rows := max(1, min(512, int(b.memBytes()/int64(W)/32)))
	seenHi, seenLo := false, false
	for r0 := 0; r0 < H; r0 += rows {
		nr := min(rows, H-r0)
		dem, err := l.dem.rect(r0, 0, nr, W)
		if err != nil {
			return nil, err
		}
		kind, err := l.kind.rect(r0, 0, nr, W, 0)
		if err != nil {
			return nil, err
		}
		prov, err := l.prov.rect(r0, 0, nr, W, 0)
		if err != nil {
			return nil, err
		}
		for i, v := range dem {
			ws.bySource[srcName(int(kind[i]))]++
			if prov[i] != 0 {
				ws.byDataset[names[int(prov[i])]]++
			}
			if v <= noData+1 {
				ws.nodata++
				continue
			}
			ws.any = true
			f := float64(v)
			ws.zmin, ws.zmax = math.Min(ws.zmin, f), math.Max(ws.zmax, f)
			row, col := float64(r0+i/W), float64(i%W)
			if !seenHi || f > ws.hi[0] {
				ws.hi, seenHi = [3]float64{f, row, col}, true
			}
			if !seenLo || f < ws.lo[0] {
				ws.lo, seenLo = [3]float64{f, row, col}, true
			}
			if v < 0 {
				ws.water++
				if kind[i] == SrcSurvey {
					ws.measured++
				}
			}
		}
	}
	ws.corners = map[string]*Checkpoint{}
	for name, rc := range map[string][2]int{"corner_nw": {0, 0}, "corner_ne": {0, W - 1}, "corner_sw": {H - 1, 0}, "corner_se": {H - 1, W - 1}} {
		v, err := l.dem.rect(rc[0], rc[1], 1, 1)
		if err != nil {
			return nil, err
		}
		if v[0] > noData+1 {
			ws.corners[name] = &Checkpoint{Name: name, ElevationM: float64(v[0])}
			ws.corners[name].Easting, ws.corners[name].Northing = float64(rc[1]), float64(rc[0])
		}
	}
	return ws, nil
}

// checkpoints lists the highest and lowest vertices and the four world corners (elevations as stored in the tiles).
func (ws *worldScan) checkpoints(g *Grid) []Checkpoint {
	at := func(name string, row, col, v float64) Checkpoint {
		return Checkpoint{Name: name, Easting: g.OriginE + col*g.CellM, Northing: g.OriginN - row*g.CellM, ElevationM: v}
	}
	out := []Checkpoint{at("highest", ws.hi[1], ws.hi[2], ws.hi[0]), at("lowest", ws.lo[1], ws.lo[2], ws.lo[0])}
	for _, name := range []string{"corner_nw", "corner_ne", "corner_sw", "corner_se"} {
		if c := ws.corners[name]; c != nil {
			out = append(out, at(name, c.Northing, c.Easting, c.ElevationM))
		}
	}
	return out
}

// level0Extent is the W,S,E,N envelope of the level-0 tiles: the area the world covers. Coarse LOD tiles overhang the
// grid and must not be included (a seamark query or bbox_wgs84 over them was up to 100 km too wide).
func (m *Manifest) level0Extent() [4]float64 {
	bb := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, t := range m.Tiles {
		if t.Level == 0 {
			bb = [4]float64{math.Min(bb[0], t.BoundsWGS[0]), math.Min(bb[1], t.BoundsWGS[1]), math.Max(bb[2], t.BoundsWGS[2]), math.Max(bb[3], t.BoundsWGS[3])}
		}
	}
	return bb
}

// fetchBoxes returns the padded WGS84 boxes to request from the sources. A world inside -180..180 needs one. A world
// that crosses the antimeridian (east > 180) needs two, each a normal small box on one side of 180: [west-pad, 180] and
// [-180, east+pad-360]. Never one box spanning the globe, and never a clamp that silently drops the far side.
func fetchBoxes(w, s, e, n, dLon, dLat float64) []gmrt.BBox {
	S, N := math.Max(s-dLat, -85), math.Min(n+dLat, 85)
	if e <= 180 {
		return []gmrt.BBox{{W: math.Max(w-dLon, -180), S: S, E: math.Min(e+dLon, 180), N: N}}
	}
	return []gmrt.BBox{
		{W: math.Max(w-dLon, -180), S: S, E: 180, N: N},
		{W: -180, S: S, E: math.Min(e+dLon-360, 180), N: N},
	}
}
