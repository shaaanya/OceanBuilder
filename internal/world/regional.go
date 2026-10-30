package world

import (
	"fmt"
	"math"
	"sort"
)

// Regional bathymetry fill. A higher-resolution regional grid (EMODnet, about 115 m) replaces the
// GEBCO-filled water cells of GMRT (about 758 m), and only those: GMRT keeps every cell backed by
// its own survey data, and land is never touched. The regional grid declares no vertical datum, so the
// offset to GMRT is measured on the cells both sources cover with survey data, reported, and the fill
// is refused when that offset is implausible or cannot be measured.

const (
	regionalMinOverlap    = 200   // flat shared survey cells needed to trust the offset estimate
	regionalMaxSlope      = 0.05  // a cell is flat when no 4-neighbour of either grid differs by more than this (m per m)
	regionalMaxShiftGain  = 0.75  // refuse when a one-cell shift cuts the rms below this share of the aligned rms
	regionalMaxRelOffset  = 0.05  // median |regional - GMRT| / depth above this: units or sign problem, whatever the slope
	regionalMaxOffsetM    = 5.0   // median |regional - GMRT| above this means datum, unit or alignment trouble
	regionalBlendCells    = 8     // replacement ramps in over this many cells from the nearest kept (GMRT or uncovered) water cell
	regionalShallowCheckM = 100.0 // offsets measured on flat cells deeper than this say nothing about the shallow-water datum
	regionalBigChangeM    = 100.0 // a replaced cell moved by more than this deserves a look
	regionalBigShare      = 0.5   // replacing more than this share of the water means the result is mostly regional data
	regionalCoastMarginM  = 2.0   // cells shallower than this are skipped everywhere: the shoreline is not bathymetry
)

// RegionalStats are written to the manifest.
type RegionalStats struct {
	Source           string   `json:"source"`
	OverlapCells     int      `json:"overlap_cells"`   // flat GMRT survey-backed water cells also covered by the regional grid
	MedianOffsetM    float64  `json:"median_offset_m"` // regional minus GMRT on the overlap (negative: regional is deeper)
	RMSOffsetM       float64  `json:"rms_offset_m"`
	CellsReplaced    int      `json:"cells_replaced"`  // GEBCO-fill water cells now taken from the regional grid
	FillCellsLeft    int      `json:"fill_cells_left"` // non-survey water cells the regional grid did not cover
	MaxChangeM       float64  `json:"max_abs_change_m"`
	BlendCells       int      `json:"blend_cells"`
	FlatMedianDepthM float64  `json:"flat_cells_median_depth_m"`    // how deep the cells are that the offset was measured on
	RelOffset        float64  `json:"median_relative_offset"`       // median of (regional - GMRT) / depth over all shared survey cells
	AlignRMS0        float64  `json:"align_rms_at_zero_shift_m"`    // rms over all shared survey cells, aligned
	AlignRMSShift    float64  `json:"align_rms_best_1cell_shift_m"` // lowest rms with the regional grid moved one cell in any direction
	Status           string   `json:"status"`                       // "accepted" or "accepted_with_caveats"; a refused fill is a build error
	Caveats          []string `json:"caveats,omitempty"`            // why the accepted fill is not fully justified by the evidence
	Note             string   `json:"note"`
}

// caveats lists what the evidence does not support, so acceptance is never read as validation.
func (st *RegionalStats) setStatus(waterCells int) {
	if st.FlatMedianDepthM > regionalShallowCheckM {
		st.Caveats = append(st.Caveats, fmt.Sprintf("vertical offset measured on flat cells at median depth %.0f m (> %.0f m): shallow-water datum not verified", st.FlatMedianDepthM, regionalShallowCheckM))
	}
	if waterCells > 0 && float64(st.CellsReplaced)/float64(waterCells) > regionalBigShare {
		st.Caveats = append(st.Caveats, fmt.Sprintf("%.0f%% of water cells replaced: output is mostly regional data", 100*float64(st.CellsReplaced)/float64(waterCells)))
	}
	if st.MaxChangeM > regionalBigChangeM {
		st.Caveats = append(st.Caveats, fmt.Sprintf("a cell changed by %.0f m (> %.0f m): GEBCO fill and regional grid disagree strongly there", st.MaxChangeM, regionalBigChangeM))
	}
	st.Status = "accepted"
	if len(st.Caveats) > 0 {
		st.Status = "accepted_with_caveats"
	}
}

// fillRegional edits dem in place. meas marks GMRT survey-backed cells (1) and is not changed:
// the regional grid is a compilation, not a claim of direct survey for each cell.
//
// The vertical offset is estimated on flat cells only (slope below regionalMaxSlope in both grids): on a steep
// slope a 115 m and a 39 m grid legitimately differ by tens of metres from sampling alone, which would swamp a
// metre-scale datum offset (Mindelo: scatter of -80..+30 m at 200-1500 m depth).
func fillRegional(dem, regional []float32, meas []uint8, W, H int, cellM float64, valid func(int) bool, rvalid func(float32) bool) (RegionalStats, error) {
	var st RegionalStats
	var diffs []float64
	var sq float64
	flat := func(g []float32, i int) bool {
		x, y := i%W, i/W
		lim := float32(regionalMaxSlope * cellM)
		for _, o := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			if x+o[0] < 0 || y+o[1] < 0 || x+o[0] >= W || y+o[1] >= H {
				continue
			}
			if d := g[(y+o[1])*W+x+o[0]] - g[i]; d > lim || d < -lim {
				return false
			}
		}
		return true
	}
	var flatDepths []float64
	for i := range dem {
		if meas[i] == 1 && flat(dem, i) && flat(regional, i) && valid(i) && dem[i] < -regionalCoastMarginM && rvalid(regional[i]) && regional[i] < -regionalCoastMarginM {
			d := float64(regional[i]) - float64(dem[i])
			diffs = append(diffs, d)
			flatDepths = append(flatDepths, -float64(dem[i]))
			sq += d * d
		}
	}
	// Checks that do not depend on slope. The relative offset catches feet/metres and sign errors; a one-cell shift
	// test on every shared survey cell catches horizontal misregistration, which flat cells cannot see.
	shared := func(i int) bool {
		return meas[i] == 1 && valid(i) && dem[i] < -regionalCoastMarginM && rvalid(regional[i]) && regional[i] < -regionalCoastMarginM
	}
	var rel []float64
	for i := range dem {
		if shared(i) {
			rel = append(rel, (float64(regional[i])-float64(dem[i]))/-float64(dem[i]))
		}
	}
	sort.Float64s(rel)
	st.RelOffset = rel[len(rel)/2]
	rms := func(dx, dy int) float64 {
		var sum float64
		var n int
		for i := range dem {
			if meas[i] != 1 || !valid(i) || dem[i] >= -regionalCoastMarginM {
				continue
			}
			x, y := i%W+dx, i/W+dy
			if x < 0 || y < 0 || x >= W || y >= H {
				continue
			}
			j := y*W + x
			if !rvalid(regional[j]) || regional[j] >= -regionalCoastMarginM {
				continue
			}
			d := float64(regional[j]) - float64(dem[i])
			sum += d * d
			n++
		}
		if n == 0 {
			return math.Inf(1)
		}
		return math.Sqrt(sum / float64(n))
	}
	st.AlignRMS0 = rms(0, 0)
	st.AlignRMSShift = math.Min(math.Min(rms(1, 0), rms(-1, 0)), math.Min(rms(0, 1), rms(0, -1)))
	if math.Abs(st.RelOffset) > regionalMaxRelOffset {
		return st, fmt.Errorf("regional grid differs from GMRT by a median %.1f%% of depth on shared survey cells (limit %.0f%%): units or sign mismatch, not filling", 100*st.RelOffset, 100*regionalMaxRelOffset)
	}
	// Real Falmouth data is nearly flat across +-70 m (rms 1.75 -> 1.58 m): resolution mismatch, not misregistration. A
	// genuine offset of a cell or two roughly halves the rms at a one-cell shift, so only a large gain refuses.
	if st.AlignRMSShift < regionalMaxShiftGain*st.AlignRMS0 {
		return st, fmt.Errorf("regional grid fits GMRT much better when moved one cell (rms %.1f m vs %.1f m aligned): horizontal misregistration of a cell or more, not filling", st.AlignRMSShift, st.AlignRMS0)
	}
	st.OverlapCells = len(diffs)
	if len(diffs) < regionalMinOverlap {
		return st, fmt.Errorf("only %d flat survey-backed water cells are shared with the regional grid (need %d): cannot validate its vertical reference, not filling", len(diffs), regionalMinOverlap)
	}
	sort.Float64s(diffs)
	sort.Float64s(flatDepths)
	st.FlatMedianDepthM = flatDepths[len(flatDepths)/2]
	st.MedianOffsetM = diffs[len(diffs)/2]
	st.RMSOffsetM = math.Sqrt(sq / float64(len(diffs)))
	if math.Abs(st.MedianOffsetM) > regionalMaxOffsetM {
		return st, fmt.Errorf("regional grid differs from GMRT by a median %.1f m on %d flat shared survey cells (limit %.1f m): vertical datum mismatch, not filling", st.MedianOffsetM, len(diffs), regionalMaxOffsetM)
	}
	// Cells to replace, and the kept water cells they must blend into: a hard swap leaves steps along the seam
	// (Falmouth: median seam step 0.46 -> 1.43 m without blending).
	replace := make([]bool, len(dem))
	kept := make([]bool, len(dem))
	for i := range dem {
		if !valid(i) || dem[i] >= -regionalCoastMarginM {
			continue
		}
		if meas[i] == 1 {
			kept[i] = true
			continue
		}
		if !rvalid(regional[i]) || regional[i] >= -regionalCoastMarginM {
			st.FillCellsLeft++
			kept[i] = true
			continue
		}
		replace[i] = true
	}
	dist := make([]int32, len(dem)) // L1 chamfer distance (cells) to the nearest kept water cell
	const far = int32(1 << 29)
	for i := range dist {
		if kept[i] {
			dist[i] = 0
		} else {
			dist[i] = far
		}
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			i := y*W + x
			if x > 0 && dist[i-1]+1 < dist[i] {
				dist[i] = dist[i-1] + 1
			}
			if y > 0 && dist[i-W]+1 < dist[i] {
				dist[i] = dist[i-W] + 1
			}
		}
	}
	for y := H - 1; y >= 0; y-- {
		for x := W - 1; x >= 0; x-- {
			i := y*W + x
			if x < W-1 && dist[i+1]+1 < dist[i] {
				dist[i] = dist[i+1] + 1
			}
			if y < H-1 && dist[i+W]+1 < dist[i] {
				dist[i] = dist[i+W] + 1
			}
		}
	}
	st.BlendCells = regionalBlendCells
	water := 0
	for i := range dem {
		if valid(i) && dem[i] < -regionalCoastMarginM {
			water++
		}
	}
	for i := range dem {
		if !replace[i] {
			continue
		}
		w := 1.0
		if dist[i] < far && int(dist[i]) < regionalBlendCells {
			w = float64(dist[i]) / regionalBlendCells
		}
		nv := float32(float64(dem[i]) + w*(float64(regional[i])-float64(dem[i])))
		if c := math.Abs(float64(nv) - float64(dem[i])); c > st.MaxChangeM {
			st.MaxChangeM = c
		}
		dem[i] = nv
		st.CellsReplaced++
	}
	st.setStatus(water)
	st.Note = "Offset measured on flat shared survey cells and NOT applied: no vertical datum conversion. Replaced cells are interpolated regional values (ramped in over blend_cells from kept cells), not flagged survey-backed."
	return st, nil
}
