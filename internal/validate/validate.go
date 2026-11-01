// Package validate checks a built world against its manifest and against
// physical/geometric invariants. Every metric is computed from the data.
package validate

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ocean-compiler/ocean/internal/world"
)

type Level string

const (
	Error Level = "error"
	Warn  Level = "warning"
	Info  Level = "info"
)

type Finding struct {
	Level   Level  `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Report struct {
	World    string             `json:"world"`
	BuildID  string             `json:"build_id"`
	Tiles    int                `json:"tiles"`
	Metrics  map[string]float64 `json:"metrics"`
	Findings []Finding          `json:"findings"`
	Result   string             `json:"result"` // OK | BUILT WITH WARNINGS | FAILED
}

func (r *Report) add(l Level, code, f string, a ...any) {
	r.Findings = append(r.Findings, Finding{l, code, fmt.Sprintf(f, a...)})
}

func (r *Report) count(l Level) (n int) {
	for _, f := range r.Findings {
		if f.Level == l {
			n++
		}
	}
	return
}

// ReadHeights decodes a .height.f32 tile into metres. Cells without a value come back as the manifest's nodata
// sentinel (m.Height.NoData).
func ReadHeights(path string, m *world.Manifest) ([]float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	S := m.Grid.SamplesPerTile
	if len(b) != S*S*4 {
		return nil, fmt.Errorf("%s: %d bytes, want %d", path, len(b), S*S*4)
	}
	out := make([]float64, S*S)
	for i := range out {
		out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:])))
	}
	return out, nil
}

// loaded is one tile's files read into memory.
type loaded struct {
	t               world.Tile
	h               []float64
	class, shr, src []uint8
	dsl             []uint8 // dataset layer (format revision 1), nil when the world has none
}

func readGray(path string, size int) ([]uint8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	g, ok := img.(*image.Gray)
	if !ok {
		return nil, fmt.Errorf("%s: not an 8-bit grey PNG", filepath.Base(path))
	}
	if b := g.Bounds(); b.Dx() != size || b.Dy() != size {
		return nil, fmt.Errorf("%s is %dx%d, want %dx%d", filepath.Base(path), b.Dx(), b.Dy(), size, size)
	}
	return g.Pix, nil
}

func fileHash(path string) (string, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), b, nil
}

// Run validates the world in dir.
func Run(dir string) (*Report, error) {
	m, err := world.LoadManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read manifest: %w", err)
	}
	r := &Report{World: m.Name, BuildID: m.BuildID, Tiles: len(m.Tiles), Metrics: map[string]float64{}}
	if m.SchemaVersion != world.SchemaVersion && m.SchemaVersion != world.SchemaVersionTM {
		r.add(Error, "schema", "manifest schema_version %d, tool understands %d", m.SchemaVersion, world.SchemaVersion)
		return r.finish(), nil
	}
	checkBounds(r, m)
	checkProvenance(r, m, dir)
	S := m.Grid.SamplesPerTile
	nd := m.Height.NoData
	ids := map[string]bool{}
	byLevel := map[int]map[[2]int]*loaded{}
	var lowRes, partial, steep, steps int
	var nodataCells, cells int
	var water, measured float64
	bySource := map[string]int{}
	byDataset := map[string]int{}
	dsName := map[int]string{}
	for _, d := range m.Datasets {
		dsName[d.Index] = d.ID
	}
	var layerBad int
	lim := math.Tan(60*math.Pi/180) * m.Grid.CellM
	for _, t := range m.Tiles {
		if ids[t.ID] {
			r.add(Error, "duplicate-tile", "duplicate tile id %s", t.ID)
		}
		ids[t.ID] = true
		if want := fmt.Sprintf("L%d_%d_%d", t.Level, t.X, t.Y); t.ID != want {
			r.add(Error, "tile-id", "tile %s: id does not match level and position (%s)", t.ID, want)
		}
		sum, _, err := fileHash(filepath.Join(dir, t.Height))
		if err != nil {
			r.add(Error, "missing-tile", "tile %s: %v", t.ID, err)
			continue
		}
		if sum != t.SHA256.Height {
			r.add(Error, "tile-hash", "tile %s: height file hash differs from manifest", t.ID)
		}
		h, err := ReadHeights(filepath.Join(dir, t.Height), m)
		if err != nil {
			r.add(Error, "tile-size", "tile %s: %v (inconsistent resolution)", t.ID, err)
			continue
		}
		L := &loaded{t: t, h: h}
		ok := true
		for _, f := range []struct {
			name, path, want string
			dst              *[]uint8
		}{{"depth_class", t.Depth, t.SHA256.Depth, &L.class}, {"shore_mask", t.Shore, t.SHA256.Shore, &L.shr}, {"source", t.Source, t.SHA256.Source, &L.src}} {
			if hs, _, err := fileHash(filepath.Join(dir, f.path)); err != nil {
				r.add(Error, "tile-mask", "tile %s: %v", t.ID, err)
				ok = false
				continue
			} else if hs != f.want {
				r.add(Error, "tile-hash", "tile %s: %s file hash differs from manifest", t.ID, f.name)
			}
			if *f.dst, err = readGray(filepath.Join(dir, f.path), S); err != nil {
				r.add(Error, "tile-mask", "tile %s: %v", t.ID, err)
				ok = false
			}
		}
		if !ok {
			continue
		}
		if t.Dataset != "" {
			if hs, _, err := fileHash(filepath.Join(dir, t.Dataset)); err != nil {
				r.add(Error, "tile-mask", "tile %s: %v", t.ID, err)
				continue
			} else if hs != t.SHA256.Dataset {
				r.add(Error, "tile-hash", "tile %s: dataset layer hash differs from manifest", t.ID)
			}
			if L.dsl, err = readGray(filepath.Join(dir, t.Dataset), S); err != nil {
				r.add(Error, "tile-mask", "tile %s: %v", t.ID, err)
				continue
			}
		} else if m.FormatRevision >= 1 {
			r.add(Error, "tile-mask", "tile %s: format revision %d requires a dataset layer", t.ID, m.FormatRevision)
		}
		if byLevel[t.Level] == nil {
			byLevel[t.Level] = map[[2]int]*loaded{}
		}
		byLevel[t.Level][[2]int{t.X, t.Y}] = L
		// Layers must agree with the heights they were derived from, per sample.
		var tw, tm, tnd int
		// Neighbouring tiles share their edge samples; count each world vertex once.
		owns := func(i int) bool {
			return t.Level == 0 && (i/S < S-1 || t.Row == m.Grid.TilesY-1) && (i%S < S-1 || t.Col == m.Grid.TilesX-1)
		}
		for i, v := range h {
			isND := v == nd
			if isND != (L.class[i] == world.ClassNoData) || isND != (L.src[i] == world.SrcNoData) || isND && L.dsl != nil && L.dsl[i] != 0 {
				layerBad++
				continue
			}
			if isND {
				tnd++
				if owns(i) {
					bySource["nodata"]++
				}
				continue
			}
			wantClass := bandOf(v, m.Config.DepthBands)
			if L.class[i] != wantClass {
				layerBad++
			}
			if t.Level == 0 && L.src[i] == world.SrcLODDerived {
				layerBad++ // lod-derived samples exist only above level 0
			}
			if v < 0 {
				tw++
				if L.src[i] == world.SrcSurvey {
					tm++
				}
				if L.src[i] == world.SrcLand || L.src[i] == world.SrcRefined {
					layerBad++
				}
			} else if L.src[i] == world.SrcSurvey || L.src[i] == world.SrcBackground || L.src[i] == world.SrcRegional {
				layerBad++
			}
			if L.dsl != nil {
				if k := int(L.dsl[i]); k == 0 || k > len(m.Datasets) {
					layerBad++ // a valid sample must name a dataset of the manifest
				}
			}
			if owns(i) {
				bySource[sourceName(m, int(L.src[i]))]++
				if L.dsl != nil && L.dsl[i] != 0 {
					byDataset[dsName[int(L.dsl[i])]]++
				}
			}
		}
		for i := 1; i < S-1; i++ { // interior shore only: edge samples depend on the neighbouring tile
			for j := 1; j < S-1; j++ {
				k := i*S + j
				if h[k] == nd {
					continue
				}
				want := uint8(0)
				if h[k] >= 0 {
					for _, d := range [4]int{-S, S, -1, 1} {
						if h[k+d] != nd && h[k+d] < 0 {
							want = 255
						}
					}
				}
				if L.shr[k] != want {
					layerBad++
				}
			}
		}
		if tnd != int(math.Round(t.NoData*float64(S*S))) {
			r.add(Error, "tile-stats", "tile %s: manifest nodata_fraction %.4f, files say %d of %d", t.ID, t.NoData, tnd, S*S)
		}
		if tw > 0 && math.Abs(float64(tm)/float64(tw)-t.Measured) > 1e-9 {
			r.add(Error, "tile-stats", "tile %s: manifest measured_water_fraction %.4f, source layer says %.4f", t.ID, t.Measured, float64(tm)/float64(tw))
		}
		if t.Level != 0 {
			continue
		}
		nodataCells += tnd
		cells += S * S
		if t.NoData > 0 && t.NoData < 1 {
			partial++
		}
		if t.NoData == 1 {
			r.add(Error, "empty-tile", "tile %s has no elevation data", t.ID)
		}
		w := t.Water * float64(S*S)
		water += w
		measured += w * t.Measured
		if t.Water > 0.01 && t.Measured < 0.5 {
			lowRes++
		}
		// Slope discontinuities: steps above 60 degrees at the grid spacing.
		for i := 0; i < S; i++ {
			for j := 0; j < S; j++ {
				if h[i*S+j] == nd {
					continue
				}
				if j+1 < S && h[i*S+j+1] != nd {
					steps++
					if math.Abs(h[i*S+j+1]-h[i*S+j]) > lim {
						steep++
					}
				}
				if i+1 < S && h[(i+1)*S+j] != nd {
					steps++
					if math.Abs(h[(i+1)*S+j]-h[i*S+j]) > lim {
						steep++
					}
				}
			}
		}
	}
	if layerBad > 0 {
		r.add(Error, "layer-mismatch", "%d samples where depth class, shore mask or source layer disagree with the height file", layerBad)
	}
	if n := len(byLevel[0]); n != m.Grid.TilesX*m.Grid.TilesY {
		r.add(Error, "tile-count", "level 0 has %d tiles, grid is %dx%d", n, m.Grid.TilesX, m.Grid.TilesY)
	}
	for name, n := range m.Stats.BySource {
		if bySource[name] != n {
			r.add(Error, "source-stats", "manifest says %d %q cells, tile files say %d", n, name, bySource[name])
		}
	}
	for name, n := range m.Stats.CellsByDataset {
		if byDataset[name] != n {
			r.add(Error, "dataset-stats", "manifest says %d cells from dataset %q, dataset layers say %d", n, name, byDataset[name])
		}
	}
	if m.Vertical.Mixed {
		r.add(Warn, "vertical-mixed", "datasets with different or unverified vertical references were combined on request (see datasets[].vertical): heights are not comparable across datasets better than their datum differences")
	}
	if c := m.Stats.Composition; c != nil {
		for _, o := range c.Overlaps {
			if o.Water >= 100 && (o.MedianM > 1 || o.MedianM < -1) {
				r.add(Warn, "dataset-offset", "datasets %s and %s disagree systematically where they overlap: median %.2f m over %d water cells (rms %.2f m). A vertical datum difference or a registration error is likely", o.Winner, o.Other, o.MedianM, o.Water, o.RMSM)
			}
		}
		for _, sm := range c.Seams {
			if sm.Pairs >= 50 && sm.P90M > 5 {
				r.add(Warn, "dataset-seam", "steps of up to %.1f m (p90 %.1f m) across %d places where the supplying dataset changes between %s and %s", sm.MaxM, sm.P90M, sm.Pairs, sm.A, sm.B)
			}
		}
	}
	// Neighbour consistency per level: shared edges must match exactly (crack-free tiling).
	var cracks int
	for _, tiles := range byLevel {
		for pos, a := range tiles {
			if e, ok := tiles[[2]int{pos[0] + 1, pos[1]}]; ok {
				for i := 0; i < S; i++ {
					if a.h[i*S+S-1] != e.h[i*S] {
						cracks++
					}
				}
			}
			if n, ok := tiles[[2]int{pos[0], pos[1] + 1}]; ok { // y grows north; row 0 of a is the south edge of n
				for j := 0; j < S; j++ {
					if a.h[j] != n.h[(S-1)*S+j] {
						cracks++
					}
				}
			}
		}
	}
	if cracks > 0 {
		r.add(Error, "tile-gap", "%d edge samples differ between neighbouring tiles (cracks)", cracks)
	}
	// LOD consistency: a parent sample is exactly the child sample at the same ground position.
	lodBad := checkLOD(byLevel, m)
	if lodBad > 0 {
		r.add(Error, "lod-mismatch", "%d parent samples differ from the child sample at the same position", lodBad)
	}
	r.Metrics["tiles"] = float64(len(m.Tiles))
	r.Metrics["levels"] = float64(len(m.LOD.Levels))
	r.Metrics["edge_mismatches"] = float64(cracks)
	r.Metrics["lod_mismatches"] = float64(lodBad)
	if cells > 0 {
		r.Metrics["elevation_coverage"] = 1 - float64(nodataCells)/float64(cells)
		if nodataCells > 0 {
			r.add(Warn, "nodata", "%.2f%% of samples have no elevation data (%d tiles partly empty)", 100*float64(nodataCells)/float64(cells), partial)
		}
	}
	if steps > 0 {
		frac := float64(steep) / float64(steps)
		r.Metrics["steep_step_fraction"] = frac
		if frac > 0.01 {
			r.add(Warn, "discontinuity", "%.2f%% of neighbouring samples differ by more than a 60° slope; check source artefacts", 100*frac)
		}
	}
	if water > 0 {
		r.Metrics["measured_water_fraction"] = measured / water
	}
	if lowRes > 0 {
		r.add(Warn, "low-res-bathymetry", "%d tiles are mostly (>50%%) interpolated/background-grid bathymetry rather than survey-backed", lowRes)
	}
	if m.Resampling.Upsampled {
		r.add(Warn, "upsampled", "output cell %.0f m is finer than the delivered source cell (≈%.0f m): the extra detail is interpolation, not data", m.Resampling.OutputCellM, m.Resampling.SourceResM)
	}
	if water > 0 && m.Config.Local == nil {
		if fill := 1 - measured/water; fill > 0.05 {
			r.Metrics["background_water_fraction"] = fill
			if rg := m.Stats.Regional; rg != nil && float64(rg.FillCellsLeft)/water <= 0.05 {
				replaced := float64(rg.CellsReplaced) / float64(m.Stats.Cells)
				r.add(Info, "regional-fill", "%s replaced %d background-grid water cells (%.1f%% of the grid) with a ~115 m compilation; %d fill cells remain at the background grid (≈%.0f m). Median offset to survey cells %.2f m (rms %.2f m, measured on flat cells at a median depth of %.0f m, so it says little about the shallow-water datum) was measured, not corrected. Replaced cells are still not survey-backed", rg.Source, rg.CellsReplaced, 100*replaced, rg.FillCellsLeft, m.Resampling.FillResM, rg.MedianOffsetM, rg.RMSOffsetM, rg.FlatMedianDepthM)
			} else {
				r.add(Warn, "background-fill", "%.1f%% of water cells have no survey data and come from a coarse background grid (≈%.0f m resolution) resampled to %.0f m", 100*fill, m.Resampling.FillResM, m.Resampling.OutputCellM)
			}
		}
	}
	if rg := m.Stats.Regional; rg != nil {
		for _, c := range rg.Caveats {
			r.add(Warn, "regional-fill-caveat", "%s", c)
		}
	}
	if cc := m.Stats.CoastlineCheck; cc != nil && cc.Points-cc.Outside > 0 {
		r.Metrics["coastline_offset_median_m"] = cc.OffsetMedianM
		r.Metrics["coastline_offset_p90_m"] = cc.OffsetP90M
		r.Metrics["coastline_unmatched"] = cc.NoShoreWithin
		r.Metrics["coastline_median_elev_m"] = cc.MedianM
		if cf := m.Stats.CoastConform; cf != nil {
			r.add(Info, "coast-conformed", "land/water follows the OSM coastline within %.0f m: %d cells to land, %d to water (+-0.5 m), %d left alone (deep/high). Offset before: %.0f m median / %.0f m p90; the post-conform figure compares OSM with itself and is not evidence", cf.MaxShiftM, cf.ToLand, cf.ToWater, cf.SkippedGuard, cc.OffsetMedianM, cc.OffsetP90M)
		} else if cc.OffsetMedianM > 2*m.Grid.CellM || cc.NoShoreWithin > 0.25 {
			r.add(Warn, "coast-misaligned", "OSM coastline is %.0f m (median) / %.0f m (p90) from the nearest DEM shoreline; %.0f%% of points have none within %.0f m. DEM elevation at the coastline: median %.1f m (p10 %.1f, p90 %.1f). Horizontal offset or vertical datum differs between sources", cc.OffsetMedianM, cc.OffsetP90M, 100*cc.NoShoreWithin, cc.SearchRadiusM, cc.MedianM, cc.P10M, cc.P90M)
		}
		if cc.Outside > 0 {
			r.Metrics["coastline_coverage"] = 1 - float64(cc.Outside)/float64(cc.Points)
			r.add(Info, "coast-coverage", "%d of %d coastline samples fall outside the built area", cc.Outside, cc.Points)
		}
	} else if m.Stats.CoastlineNote != "" {
		r.add(Warn, "no-coastline", "%s", m.Stats.CoastlineNote)
	} else {
		r.add(Warn, "no-coastline", "no coastline in this world; shore masks come from the DEM zero contour only")
	}
	for _, n := range m.Stats.Notes {
		r.add(Warn, "projection", "%s", n)
	}
	r.add(Info, "vertical", "%s", m.Vertical.Note)
	return r.finish(), nil
}

func (r *Report) finish() *Report {
	sort.SliceStable(r.Findings, func(i, j int) bool { return rank(r.Findings[i].Level) < rank(r.Findings[j].Level) })
	switch {
	case r.count(Error) > 0:
		r.Result = "FAILED"
	case r.count(Warn) > 0:
		r.Result = "BUILT WITH WARNINGS"
	default:
		r.Result = "OK"
	}
	return r
}

func bandOf(v float64, bands []float64) uint8 {
	if v >= 0 {
		return 0
	}
	k := 1
	for k <= len(bands) && -v > bands[k-1] {
		k++
	}
	return uint8(k)
}

func sourceName(m *world.Manifest, code int) string {
	for _, l := range m.SourceLayers {
		if l.ID == code {
			return l.Name
		}
	}
	return fmt.Sprint(code)
}

// checkLOD compares every parent sample with the child sample at the same ground position. Level k+1 tile
// (X,Y) has children (2X+dx, 2Y+dy); parent column j lies at child column 2j - dx*N, parent row i (0 = north)
// at child row 2i - (1-dy)*N.
func checkLOD(byLevel map[int]map[[2]int]*loaded, m *world.Manifest) int {
	N, S := m.Grid.TileCells, m.Grid.SamplesPerTile
	bad := 0
	for lvl, tiles := range byLevel {
		if lvl == 0 {
			continue
		}
		for pos, p := range tiles {
			for dx := 0; dx < 2; dx++ {
				for dy := 0; dy < 2; dy++ {
					c, ok := byLevel[lvl-1][[2]int{2*pos[0] + dx, 2*pos[1] + dy}]
					if !ok {
						continue
					}
					for i := 0; i < S; i++ {
						ci := 2*i - (1-dy)*N
						if ci < 0 || ci >= S {
							continue
						}
						for j := 0; j < S; j++ {
							cj := 2*j - dx*N
							if cj < 0 || cj >= S {
								continue
							}
							ph, ch := p.h[i*S+j], c.h[ci*S+cj]
							if m.LOD.Method == "vertex-decimation" || m.LOD.Method == "" {
								if ph != ch || p.src[i*S+j] != c.src[ci*S+cj] {
									bad++
								}
								continue
							}
							// shoal: same nodata state and side of the waterline as the finer sample it replaces; unchanged samples keep
							// their code, changed ones are marked lod-derived; never deeper than the sample it replaces.
							nd := m.Height.NoData
							ps, cs := p.src[i*S+j], c.src[ci*S+cj]
							switch {
							case (ph == nd) != (ch == nd):
								bad++
							case ph == nd:
							case (ph < 0) != (ch < 0):
								bad++
							case ph == ch && ps != cs, ph != ch && ps != world.SrcLODDerived:
								bad++
							case ch < 0 && ph < ch:
								bad++
							}
						}
					}
				}
			}
		}
	}
	return bad
}

func rank(l Level) int { return map[Level]int{Error: 0, Warn: 1, Info: 2}[l] }

func checkBounds(r *Report, m *world.Manifest) {
	b := m.Grid.BBoxWGS84
	if !(b[0] < b[2] && b[1] < b[3] && b[0] >= -180 && b[2] <= 540 && b[2]-b[0] <= 180 && b[1] >= -90 && b[3] <= 90) {
		r.add(Error, "bounds", "invalid geographic bounds %v", b)
	}
	if m.Grid.CellM <= 0 || m.Grid.TileCells <= 0 || m.Grid.SamplesPerTile != m.Grid.TileCells+1 {
		r.add(Error, "grid", "inconsistent grid parameters")
	}
	lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, t := range m.Tiles {
		if t.Level == 0 {
			lo, hi = [2]float64{math.Min(lo[0], t.BoundsWGS[0]), math.Min(lo[1], t.BoundsWGS[1])}, [2]float64{math.Max(hi[0], t.BoundsWGS[2]), math.Max(hi[1], t.BoundsWGS[3])}
		}
	}
	if len(m.Tiles) > 0 && (math.Abs(lo[0]-b[0]) > 1e-9 || math.Abs(lo[1]-b[1]) > 1e-9 || math.Abs(hi[0]-b[2]) > 1e-9 || math.Abs(hi[1]-b[3]) > 1e-9) {
		lvl := Error
		if strings.HasPrefix(m.Tool.Version, "0.1.") || strings.HasPrefix(m.Tool.Version, "0.2.") || m.Tool.Version == "0.3.0-dev" { // older builds took the union over all LOD levels
			lvl = Warn
		}
		r.add(lvl, "bounds", "grid.bbox_wgs84 %v is not the extent of the level-0 tiles [%.6f %.6f %.6f %.6f]", b, lo[0], lo[1], hi[0], hi[1])
	}
	for _, t := range m.Tiles {
		if t.BoundsWGS[2]-t.BoundsWGS[0] > 90 {
			r.add(Error, "bounds", "tile %s: geographic envelope is %.1f degrees wide (antimeridian wrap)", t.ID, t.BoundsWGS[2]-t.BoundsWGS[0])
		}
		u := t.BoundsUTM
		span := m.Grid.TileSpanM * float64(int(1)<<t.Level)
		if math.Abs((u[2]-u[0])-span) > 1e-6 || math.Abs((u[3]-u[1])-span) > 1e-6 {
			r.add(Error, "tile-bounds", "tile %s: bounds size differs from tile span %.1f m", t.ID, span)
		}
	}
	if m.Height.MaxM < m.Height.MinM || m.Height.Type != "float32" {
		r.add(Error, "height-range", "height encoding %q range %.2f..%.2f is invalid", m.Height.Type, m.Height.MinM, m.Height.MaxM)
	}
}

func checkProvenance(r *Report, m *world.Manifest, dir string) {
	if m.Licensing == nil {
		r.add(Warn, "licensing-missing", "manifest has no licensing record (built by a tool older than 0.3.0): attribution and notices must be assembled by hand from sources[]")
	} else if len(m.Licensing.Sources) == 0 || len(m.Licensing.RequiredNotices) == 0 {
		r.add(Error, "licensing", "licensing record is empty")
	}
	if len(m.Sources) == 0 {
		r.add(Error, "provenance", "manifest has no sources")
	}
	for _, s := range m.Sources {
		var missing []string
		if s.URL == "" {
			missing = append(missing, "url")
		}
		if s.SHA256 == "" {
			missing = append(missing, "sha256")
		}
		if s.RetrievedAt == "" {
			missing = append(missing, "retrieved_at")
		}
		if s.License == "" {
			missing = append(missing, "license")
		}
		if s.Attribution == "" {
			missing = append(missing, "attribution")
		}
		if len(missing) > 0 {
			r.add(Error, "provenance", "source %s lacks %s", s.Source, strings.Join(missing, ", "))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ATTRIBUTION.txt")); err != nil {
		r.add(Error, "attribution", "ATTRIBUTION.txt missing")
	}
	if m.BuildID == "" {
		r.add(Error, "build-id", "manifest has no build_id")
	}
}

// Format renders the human-readable report.
func (r *Report) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "WORLD: %s\n\nTiles: %d in %d levels\n", r.World, r.Tiles, int(r.Metrics["levels"]))
	if v, ok := r.Metrics["elevation_coverage"]; ok {
		fmt.Fprintf(&b, "Elevation coverage: %.1f%%\n", 100*v)
	}
	if v, ok := r.Metrics["measured_water_fraction"]; ok {
		fmt.Fprintf(&b, "Water cells backed by survey data: %.1f%%\n", 100*v)
	}
	if v, ok := r.Metrics["coastline_offset_median_m"]; ok {
		fmt.Fprintf(&b, "OSM coastline to DEM shoreline offset: median %.0f m, p90 %.0f m\n", v, r.Metrics["coastline_offset_p90_m"])
	}
	fmt.Fprintf(&b, "Neighbour edge mismatches: %d, level mismatches: %d\n", int(r.Metrics["edge_mismatches"]), int(r.Metrics["lod_mismatches"]))
	for _, l := range []Level{Error, Warn, Info} {
		var fs []Finding
		for _, f := range r.Findings {
			if f.Level == l {
				fs = append(fs, f)
			}
		}
		if len(fs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s:\n", strings.ToUpper(string(l))+"S")
		for _, f := range fs {
			fmt.Fprintf(&b, "  [%s] %s\n", f.Code, f.Message)
		}
	}
	fmt.Fprintf(&b, "\nRESULT: %s\n", r.Result)
	return b.String()
}
