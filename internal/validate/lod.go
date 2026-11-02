package validate

import (
	"fmt"
	"math"
	"sort"

	"github.com/ocean-compiler/ocean/internal/world"
	"github.com/ocean-compiler/ocean/pkg/oceanworld"
)

// LODLevelError is how far one LOD level is from level 0, measured at level-0 vertices: the level's surface (bilinear,
// as a consumer would sample it) minus the level-0 height. All figures come from the tile files.
type LODLevelError struct {
	Level        int     `json:"level"`
	CellM        float64 `json:"cell_m"`
	Compared     int     `json:"compared"`       // level-0 vertices where both surfaces have a value
	RMSM         float64 `json:"rms_m"`          // root mean square of (level k - level 0)
	P95AbsM      float64 `json:"p95_abs_m"`      // 95th percentile of |difference|
	MaxAbsM      float64 `json:"max_abs_m"`      // largest |difference|
	TooDeepMaxM  float64 `json:"too_deep_max_m"` // largest amount by which level k is deeper than level 0 in water (the unsafe direction)
	TooDeepFrac  float64 `json:"too_deep_fraction"`
	Flips        int     `json:"land_water_flips"` // vertices whose side of the waterline differs
	FlipFraction float64 `json:"flip_fraction"`
	Uncovered    int     `json:"uncovered"` // level-0 vertices with a value where level k has none (bilinear needs four valid corners)

	// Where a game cares most. All are measured at level-0 vertices against level 0, like the figures above.
	LandLost       int     `json:"land_lost"`                     // level 0 is land, level k is water (a shrinking island, a closed-over rock)
	WaterLost      int     `json:"water_lost"`                    // level 0 is water, level k is land (a closed channel or inlet)
	ShallowCells   int     `json:"shallow_compared"`              // level-0 water vertices shallower than ShallowM
	ShallowRMSM    float64 `json:"shallow_rms_m"`                 // rms of (level k - level 0) over those
	ShallowMaxDeep float64 `json:"shallow_too_deep_max_m"`        // largest amount level k is deeper than level 0 there
	ClassDisagree  float64 `json:"depth_class_disagree_fraction"` // vertices whose depth class (manifest bands) differs from level 0's
	SlopeRMS       float64 `json:"slope_rms"`                     // rms of the east-west gradient error (rise over run, dimensionless) between neighbouring level-0 vertices
}

// ShallowM is the depth below which water counts as shallow in the report (m). It is a reporting convention, not a
// navigation limit.
const ShallowM = 20.0

// LODThresholds are limits for LODCheck; a zero field is not checked.
type LODThresholds struct {
	MaxRMSM          float64 // rms error of any level
	MaxTooDeepM      float64 // largest amount a level may be deeper than level 0 in water
	MaxShallowRMSM   float64
	MaxFlipFraction  float64 // land/water side changes, as a fraction of compared vertices
	MaxClassDisagree float64
	MaxLevel         int // levels above this are not checked (0 = all)
}

// LODCheck lists every limit a level breaks, one line each; empty means the report is within the thresholds.
func LODCheck(r []LODLevelError, t LODThresholds) []string {
	var out []string
	chk := func(l LODLevelError, name string, got, max float64) {
		if max > 0 && got > max {
			out = append(out, fmt.Sprintf("level %d: %s %.4g exceeds %.4g", l.Level, name, got, max))
		}
	}
	for _, l := range r {
		if t.MaxLevel > 0 && l.Level > t.MaxLevel {
			continue
		}
		chk(l, "rms (m)", l.RMSM, t.MaxRMSM)
		chk(l, "too-deep max (m)", l.TooDeepMaxM, t.MaxTooDeepM)
		chk(l, "shallow-water rms (m)", l.ShallowRMSM, t.MaxShallowRMSM)
		chk(l, "land/water flip fraction", l.FlipFraction, t.MaxFlipFraction)
		chk(l, "depth-class disagreement", l.ClassDisagree, t.MaxClassDisagree)
	}
	return out
}

// LODReport compares every LOD level with level 0. stride > 1 samples every stride-th vertex in each direction.
func LODReport(dir string, stride int) ([]LODLevelError, error) {
	if stride < 1 {
		stride = 1
	}
	w, err := oceanworld.Open(dir)
	if err != nil {
		return nil, err
	}
	wm, err := world.LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	// depth class of a height: 0 land, band index + 1 for water by the manifest's bands
	var upper []float64
	for _, c := range wm.DepthClasses {
		if c.MaxDepth != nil {
			upper = append(upper, *c.MaxDepth)
		}
	}
	class := func(z float64) int {
		if z >= 0 {
			return 0
		}
		for i, u := range upper {
			if -z <= u {
				return i + 1
			}
		}
		return len(upper) + 1
	}
	S := w.M.Grid.SamplesPerTile
	nd := float32(w.M.Height.NoData)
	cell := w.M.Grid.CellM
	var out []LODLevelError
	for _, lv := range w.M.LOD.Levels {
		if lv.Level == 0 {
			continue
		}
		le := LODLevelError{Level: lv.Level, CellM: lv.CellM}
		var sq, shSq, slSq float64
		var abs []float64
		var deep, water, classBad, slopeN int
		for ti := range w.M.Tiles {
			t := &w.M.Tiles[ti]
			if t.Level != 0 {
				continue
			}
			d, err := w.Load(t)
			if err != nil {
				return nil, err
			}
			for i := 0; i < S; i += stride {
				for j := 0; j < S; j += stride {
					truth := d.Height[i*S+j]
					if truth <= nd+1 {
						continue
					}
					e, n := t.BoundsUTM[0]+float64(j)*cell, t.BoundsUTM[3]-float64(i)*cell
					s, err := w.SampleUTM(e, n, lv.Level)
					if err != nil {
						return nil, err
					}
					if !s.Valid {
						le.Uncovered++
						continue
					}
					le.Compared++
					diff := s.ElevationM - float64(truth)
					sq += diff * diff
					abs = append(abs, math.Abs(diff))
					if (s.ElevationM < 0) != (truth < 0) {
						le.Flips++
						if truth >= 0 {
							le.LandLost++
						} else {
							le.WaterLost++
						}
					}
					if class(s.ElevationM) != class(float64(truth)) {
						classBad++
					}
					if truth < 0 && truth > -ShallowM {
						le.ShallowCells++
						shSq += diff * diff
						le.ShallowMaxDeep = math.Max(le.ShallowMaxDeep, -diff)
					}
					if j+1 < S { // gradient error to the east neighbour
						if tn := d.Height[i*S+j+1]; tn > nd+1 {
							if sn, err := w.SampleUTM(e+cell, n, lv.Level); err == nil && sn.Valid {
								g := (sn.ElevationM - s.ElevationM - (float64(tn) - float64(truth))) / cell
								slSq += g * g
								slopeN++
							}
						}
					}
					if truth < 0 {
						water++
						if diff < -1e-6 { // coarse lower (deeper) than the truth
							deep++
							le.TooDeepMaxM = math.Max(le.TooDeepMaxM, -diff)
						}
					}
				}
			}
		}
		if le.Compared > 0 {
			le.RMSM = math.Sqrt(sq / float64(le.Compared))
			sort.Float64s(abs)
			le.P95AbsM, le.MaxAbsM = abs[int(0.95*float64(len(abs)-1))], abs[len(abs)-1]
			le.FlipFraction = float64(le.Flips) / float64(le.Compared)
			le.ClassDisagree = float64(classBad) / float64(le.Compared)
		}
		if le.ShallowCells > 0 {
			le.ShallowRMSM = math.Sqrt(shSq / float64(le.ShallowCells))
		}
		if slopeN > 0 {
			le.SlopeRMS = math.Sqrt(slSq / float64(slopeN))
		}
		if water > 0 {
			le.TooDeepFrac = float64(deep) / float64(water)
		}
		out = append(out, le)
	}
	return out, nil
}

// FormatLOD renders a LODReport as a table.
func FormatLOD(method string, r []LODLevelError) string {
	s := fmt.Sprintf("LOD method %s; error of each level against level 0 at level-0 vertices (bilinear)\n", method)
	s += fmt.Sprintf("%-5s %-7s %-9s %-8s %-8s %-8s %-9s | %-9s %-9s %-9s | %-9s %-7s %-7s %-8s\n", "level", "cell m", "compared", "rms m", "p95 m", "max m", "too deep", "land lost", "water lost", "uncovered", "shallow", "class", "slope", "")
	for _, l := range r {
		s += fmt.Sprintf("%-5d %-7.0f %-9d %-8.3f %-8.3f %-8.3f %-9.3f | %-9d %-9d %-9d | %-9.3f %-7.4f %-7.4f\n", l.Level, l.CellM, l.Compared, l.RMSM, l.P95AbsM, l.MaxAbsM, l.TooDeepMaxM, l.LandLost, l.WaterLost, l.Uncovered, l.ShallowRMSM, l.ClassDisagree, l.SlopeRMS)
	}
	s += fmt.Sprintf("columns: rms/p95/max = |level k - level 0| (m); too deep = largest amount level k is deeper than level 0 in water (m); land lost / water lost = vertices that change side of the waterline; shallow = rms (m) where level 0 water is shallower than %g m; class = fraction of vertices whose depth class differs; slope = rms east-west gradient error (rise/run).\nThe waterline counts above are vertex tests, not a connectivity analysis: a vanished island shows as land lost, a closed channel as water lost, but the report does not count islands or channels.\n", ShallowM)
	return s
}
