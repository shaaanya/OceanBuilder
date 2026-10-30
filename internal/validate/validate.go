// Package validate checks a built world against its manifest and against
// physical/geometric invariants. Every metric is computed from the data.
package validate

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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

// ReadHeights decodes a .r16 tile into metres using the manifest encoding.
func ReadHeights(path string, m *world.Manifest) ([]float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	S := m.Grid.SamplesPerTile
	if len(b) != S*S*2 {
		return nil, fmt.Errorf("%s: %d bytes, want %d", path, len(b), S*S*2)
	}
	out := make([]float64, S*S)
	for i := range out {
		u := binary.LittleEndian.Uint16(b[i*2:])
		out[i] = m.Height.MinM + float64(u)/65535*(m.Height.MaxM-m.Height.MinM)
	}
	return out, nil
}

// Run validates the world in dir.
func Run(dir string) (*Report, error) {
	m, err := world.LoadManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read manifest: %w", err)
	}
	r := &Report{World: m.Name, BuildID: m.BuildID, Tiles: len(m.Tiles), Metrics: map[string]float64{}}
	if m.SchemaVersion != world.SchemaVersion {
		r.add(Error, "schema", "manifest schema_version %d, tool understands %d", m.SchemaVersion, world.SchemaVersion)
	}
	checkBounds(r, m)
	checkProvenance(r, m, dir)
	S := m.Grid.SamplesPerTile
	byPos := map[[2]int][]float64{}
	ids := map[string]bool{}
	if len(m.Tiles) != m.Grid.TilesX*m.Grid.TilesY {
		r.add(Error, "tile-count", "manifest lists %d tiles, grid is %dx%d", len(m.Tiles), m.Grid.TilesX, m.Grid.TilesY)
	}
	var lowRes, partial, steep, steps int
	var nodataCells, cells int
	var water, measured float64
	for _, t := range m.Tiles {
		if ids[t.ID] {
			r.add(Error, "duplicate-tile", "duplicate tile id %s", t.ID)
		}
		ids[t.ID] = true
		if want := fmt.Sprintf("L0_%d_%d", t.X, t.Y); t.ID != want {
			r.add(Error, "tile-id", "tile %s: id does not match position (%s)", t.ID, want)
		}
		path := filepath.Join(dir, t.Height)
		raw, err := os.ReadFile(path)
		if err != nil {
			r.add(Error, "missing-tile", "tile %s: %v", t.ID, err)
			continue
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != t.SHA256 {
			r.add(Error, "tile-hash", "tile %s: height file hash differs from manifest", t.ID)
		}
		h, err := ReadHeights(path, m)
		if err != nil {
			r.add(Error, "tile-size", "tile %s: %v (inconsistent resolution)", t.ID, err)
			continue
		}
		byPos[[2]int{t.Col, t.Row}] = h
		for _, f := range []string{t.Depth, t.Shore} {
			if err := checkPNG(filepath.Join(dir, f), S); err != nil {
				r.add(Error, "tile-mask", "tile %s: %v", t.ID, err)
			}
		}
		nd := t.NoData * float64(S*S)
		nodataCells += int(math.Round(nd))
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
		lim := math.Tan(60*math.Pi/180) * m.Grid.CellM
		for i := 0; i < S; i++ {
			for j := 0; j < S; j++ {
				if j+1 < S {
					steps++
					if math.Abs(h[i*S+j+1]-h[i*S+j]) > lim {
						steep++
					}
				}
				if i+1 < S {
					steps++
					if math.Abs(h[(i+1)*S+j]-h[i*S+j]) > lim {
						steep++
					}
				}
			}
		}
	}
	// Neighbour consistency: shared edges must match exactly (crack-free tiling).
	var cracks int
	for c := 0; c < m.Grid.TilesX; c++ {
		for rw := 0; rw < m.Grid.TilesY; rw++ {
			a, ok := byPos[[2]int{c, rw}]
			if !ok {
				continue
			}
			if e, ok := byPos[[2]int{c + 1, rw}]; ok {
				for i := 0; i < S; i++ {
					if a[i*S+S-1] != e[i*S] {
						cracks++
					}
				}
			}
			if s, ok := byPos[[2]int{c, rw + 1}]; ok {
				for j := 0; j < S; j++ {
					if a[(S-1)*S+j] != s[j] {
						cracks++
					}
				}
			}
		}
	}
	if cracks > 0 {
		r.add(Error, "tile-gap", "%d edge samples differ between neighbouring tiles (cracks)", cracks)
	}
	r.Metrics["tiles"] = float64(len(m.Tiles))
	r.Metrics["edge_mismatches"] = float64(cracks)
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
		r.add(Warn, "low-res-bathymetry", "%d tiles are mostly (>50%%) interpolated/global-grid bathymetry rather than survey-backed", lowRes)
	}
	if m.Resampling.Upsampled {
		r.add(Warn, "upsampled", "output cell %.0f m is finer than the delivered source cell (≈%.0f m): the extra detail is interpolation, not data", m.Resampling.OutputCellM, m.Resampling.SourceResM)
	}
	if water > 0 && m.Config.Local == nil {
		if fill := 1 - measured/water; fill > 0.05 {
			r.Metrics["gebco_fill_water_fraction"] = fill
			if rg := m.Stats.Regional; rg != nil && water > 0 && float64(rg.FillCellsLeft)/water <= 0.05 {
				replaced := float64(rg.CellsReplaced) / float64(m.Stats.Cells)
				r.add(Info, "regional-fill", "%s replaced %d GEBCO-filled water cells (%.1f%% of the grid) with a ~115 m compilation; %d fill cells remain at GEBCO_08 (≈%.0f m). Median offset to GMRT survey cells %.2f m (rms %.2f m, measured on flat cells at a median depth of %.0f m, so it says little about the shallow-water datum) was measured, not corrected. Replaced cells are still not survey-backed", rg.Source, rg.CellsReplaced, 100*replaced, rg.FillCellsLeft, m.Resampling.FillResM, rg.MedianOffsetM, rg.RMSOffsetM, rg.FlatMedianDepthM)
			} else {
				r.add(Warn, "gebco-fill", "%.1f%% of water cells have no survey data and come from the GEBCO_08 grid (≈%.0f m resolution) resampled to %.0f m", 100*fill, m.Resampling.FillResM, m.Resampling.OutputCellM)
			}
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
	r.add(Info, "vertical", "%s", m.Vertical.Note)
	sort.SliceStable(r.Findings, func(i, j int) bool { return rank(r.Findings[i].Level) < rank(r.Findings[j].Level) })
	switch {
	case r.count(Error) > 0:
		r.Result = "FAILED"
	case r.count(Warn) > 0:
		r.Result = "BUILT WITH WARNINGS"
	default:
		r.Result = "OK"
	}
	return r, nil
}

func rank(l Level) int { return map[Level]int{Error: 0, Warn: 1, Info: 2}[l] }

func checkBounds(r *Report, m *world.Manifest) {
	b := m.Grid.BBoxWGS84
	if !(b[0] < b[2] && b[1] < b[3] && b[0] >= -180 && b[2] <= 180 && b[1] >= -90 && b[3] <= 90) {
		r.add(Error, "bounds", "invalid geographic bounds %v", b)
	}
	if m.Grid.CellM <= 0 || m.Grid.TileCells <= 0 || m.Grid.SamplesPerTile != m.Grid.TileCells+1 {
		r.add(Error, "grid", "inconsistent grid parameters")
	}
	for _, t := range m.Tiles {
		u := t.BoundsUTM
		if math.Abs((u[2]-u[0])-m.Grid.TileSpanM) > 1e-6 || math.Abs((u[3]-u[1])-m.Grid.TileSpanM) > 1e-6 {
			r.add(Error, "tile-bounds", "tile %s: bounds size differs from tile span %.1f m", t.ID, m.Grid.TileSpanM)
		}
	}
	if m.Height.MaxM <= m.Height.MinM {
		r.add(Error, "height-range", "height range min %.2f >= max %.2f", m.Height.MinM, m.Height.MaxM)
	}
}

func checkProvenance(r *Report, m *world.Manifest, dir string) {
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

func checkPNG(path string, size int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if cfg.Width != size || cfg.Height != size {
		return fmt.Errorf("%s is %dx%d, want %dx%d", filepath.Base(path), cfg.Width, cfg.Height, size, size)
	}
	return nil
}

// Format renders the human-readable report.
func (r *Report) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "WORLD: %s\n\nTiles: %d\n", r.World, r.Tiles)
	if v, ok := r.Metrics["elevation_coverage"]; ok {
		fmt.Fprintf(&b, "Elevation coverage: %.1f%%\n", 100*v)
	}
	if v, ok := r.Metrics["measured_water_fraction"]; ok {
		fmt.Fprintf(&b, "Water cells backed by survey data (GMRT mask): %.1f%%\n", 100*v)
	}
	if v, ok := r.Metrics["coastline_offset_median_m"]; ok {
		fmt.Fprintf(&b, "OSM coastline to DEM shoreline offset: median %.0f m, p90 %.0f m\n", v, r.Metrics["coastline_offset_p90_m"])
	}
	fmt.Fprintf(&b, "Neighbour edge mismatches: %d\n", int(r.Metrics["edge_mismatches"]))
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
