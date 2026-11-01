package validate

import (
	"fmt"
	"math"
	"sort"

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
	S := w.M.Grid.SamplesPerTile
	nd := float32(w.M.Height.NoData)
	cell := w.M.Grid.CellM
	var out []LODLevelError
	for _, lv := range w.M.LOD.Levels {
		if lv.Level == 0 {
			continue
		}
		le := LODLevelError{Level: lv.Level, CellM: lv.CellM}
		var sq float64
		var abs []float64
		var deep, water int
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
	s += fmt.Sprintf("%-6s %-8s %-10s %-9s %-9s %-9s %-13s %-9s %-9s\n", "level", "cell m", "compared", "rms m", "p95 m", "max m", "too deep max", "flips", "uncovered")
	for _, l := range r {
		s += fmt.Sprintf("%-6d %-8.0f %-10d %-9.3f %-9.3f %-9.3f %-13.3f %-9d %-9d\n", l.Level, l.CellM, l.Compared, l.RMSM, l.P95AbsM, l.MaxAbsM, l.TooDeepMaxM, l.Flips, l.Uncovered)
	}
	return s
}
