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
	regionalMinOverlap   = 200  // flat shared survey cells needed to trust the offset estimate
	regionalMaxSlope     = 0.05 // a cell is flat when no 4-neighbour of either grid differs by more than this (m per m)
	regionalMaxOffsetM   = 5.0  // median |regional - GMRT| above this means datum, unit or alignment trouble
	regionalCoastMarginM = 2.0  // cells shallower than this are skipped everywhere: the shoreline is not bathymetry
)

// RegionalStats are written to the manifest.
type RegionalStats struct {
	Source        string  `json:"source"`
	OverlapCells  int     `json:"overlap_cells"`   // flat GMRT survey-backed water cells also covered by the regional grid
	MedianOffsetM float64 `json:"median_offset_m"` // regional minus GMRT on the overlap (negative: regional is deeper)
	RMSOffsetM    float64 `json:"rms_offset_m"`
	CellsReplaced int     `json:"cells_replaced"`  // GEBCO-fill water cells now taken from the regional grid
	FillCellsLeft int     `json:"fill_cells_left"` // non-survey water cells the regional grid did not cover
	MaxChangeM    float64 `json:"max_abs_change_m"`
	Note          string  `json:"note"`
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
	for i := range dem {
		if meas[i] == 1 && flat(dem, i) && flat(regional, i) && valid(i) && dem[i] < -regionalCoastMarginM && rvalid(regional[i]) && regional[i] < -regionalCoastMarginM {
			d := float64(regional[i]) - float64(dem[i])
			diffs = append(diffs, d)
			sq += d * d
		}
	}
	st.OverlapCells = len(diffs)
	if len(diffs) < regionalMinOverlap {
		return st, fmt.Errorf("only %d flat survey-backed water cells are shared with the regional grid (need %d): cannot validate its vertical reference, not filling", len(diffs), regionalMinOverlap)
	}
	sort.Float64s(diffs)
	st.MedianOffsetM = diffs[len(diffs)/2]
	st.RMSOffsetM = math.Sqrt(sq / float64(len(diffs)))
	if math.Abs(st.MedianOffsetM) > regionalMaxOffsetM {
		return st, fmt.Errorf("regional grid differs from GMRT by a median %.1f m on %d flat shared survey cells (limit %.1f m): vertical datum, units or alignment mismatch, not filling", st.MedianOffsetM, len(diffs), regionalMaxOffsetM)
	}
	for i := range dem {
		if meas[i] == 1 || !valid(i) || dem[i] >= -regionalCoastMarginM {
			continue
		}
		if !rvalid(regional[i]) || regional[i] >= -regionalCoastMarginM {
			st.FillCellsLeft++
			continue
		}
		if c := math.Abs(float64(regional[i]) - float64(dem[i])); c > st.MaxChangeM {
			st.MaxChangeM = c
		}
		dem[i] = regional[i]
		st.CellsReplaced++
	}
	st.Note = "Offset measured on shared survey cells and NOT applied: no vertical datum conversion. Replaced cells are interpolated regional values, not flagged survey-backed."
	return st, nil
}
