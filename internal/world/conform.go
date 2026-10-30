package world

import (
	"fmt"
	"math"
)

// Coastline conforming. OSM coastline ways keep land on their left and water on their right.
// Every cell within a short distance of a coastline takes the side of its nearest coastline
// sample (land when it lies left of the direction of travel). DEM cells that disagree are
// flipped to +-conformFlipM, unless the DEM says deep water / high ground, which is evidence
// the label is wrong. No flood fill: coastline fragments clipped at the bbox leave land and sea
// connected, so "regions" are not reliable; the nearest-sample side is local and always defined.

const (
	conformFlipM      = 0.5  // elevation given to a flipped cell, in metres
	conformMaxDeepM   = 10.0 // water deeper than this is never turned into land
	conformMaxHighM   = 30.0 // land higher than this is never turned into water
	conformMinAgree   = 0.5  // at or below this DEM/OSM agreement the way direction is assumed wrong (see conformGrid)
	conformSampleStep = 0.25 // cells between samples along a line
)

// ConformStats are the numbers of one conform pass; they land in the manifest.
type ConformStats struct {
	MaxShiftM    float64     `json:"max_shift_m"`
	Labelled     int         `json:"cells_labelled"`       // valid cells within the shift radius that got a side
	Agreement    float64     `json:"dem_agreement_before"` // share of labelled cells whose DEM sign already matched OSM
	ToLand       int         `json:"cells_to_land"`
	ToWater      int         `json:"cells_to_water"`
	SkippedGuard int         `json:"cells_skipped_deep_or_high"`
	RawOffset    *CoastCheck `json:"offset_before"` // OSM to DEM shoreline before conforming: the independent figure
	AfterOffset  *CoastCheck `json:"offset_after"`  // after conforming this compares OSM with itself: not evidence
	Note         string      `json:"note"`
}

type coastSample struct{ x, y, nx, ny float64 } // grid position and unit normal pointing to the land side

// conformGrid edits data in place and returns the indices of changed cells.
// lines are coastline polylines in grid coordinates (col, row; row grows southwards).
func conformGrid(data []float32, W, H int, cellM, maxShiftM float64, lines [][][2]float64, valid func(int) bool) (ConformStats, []int, error) {
	st := ConformStats{MaxShiftM: maxShiftM}
	rnd := func(v float64) int { return int(math.Floor(v + 0.5)) }
	in := func(x, y int) bool { return x >= 0 && y >= 0 && x < W && y < H }

	owner := make([]int32, W*H) // index+1 of the nearest coastline sample, 0 = none yet
	depth := make([]int32, W*H)
	var samples []coastSample
	var queue []int
	for _, ln := range lines {
		for k := 0; k+1 < len(ln); k++ {
			a, b := ln[k], ln[k+1]
			dx, dy := b[0]-a[0], b[1]-a[1]
			L := math.Hypot(dx, dy)
			if L == 0 {
				continue
			}
			// Left of the direction of travel in a north-up grid (row grows south) is (dy, -dx).
			nx, ny := dy/L, -dx/L
			n := int(math.Ceil(L / conformSampleStep))
			for s := 0; s <= n; s++ {
				t := float64(s) / float64(n)
				x, y := a[0]+dx*t, a[1]+dy*t
				cx, cy := rnd(x), rnd(y)
				if !in(cx, cy) {
					continue
				}
				i := cy*W + cx
				if owner[i] == 0 {
					queue = append(queue, i)
				}
				// The sample closest to the cell centre owns a coastline cell.
				if owner[i] == 0 || math.Hypot(x-float64(cx), y-float64(cy)) < math.Hypot(samples[owner[i]-1].x-float64(cx), samples[owner[i]-1].y-float64(cy)) {
					samples = append(samples, coastSample{x, y, nx, ny})
					owner[i] = int32(len(samples))
				}
			}
		}
	}
	if len(samples) == 0 {
		return st, nil, fmt.Errorf("no coastline segment inside the grid")
	}

	// Spread the owner outwards (8-neighbourhood, breadth first) up to R cells, keeping the closest sample.
	R := int32(math.Ceil(maxShiftM / cellM))
	d2 := func(i int, s coastSample) float64 {
		return math.Pow(float64(i%W)-s.x, 2) + math.Pow(float64(i/W)-s.y, 2)
	}
	barrier := make([]bool, W*H)
	for _, i := range queue {
		barrier[i] = true
	}
	for h := 0; h < len(queue); h++ {
		i := queue[h]
		if depth[i] >= R {
			continue
		}
		x, y := i%W, i/W
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if (dx == 0 && dy == 0) || !in(x+dx, y+dy) {
					continue
				}
				j := (y+dy)*W + x + dx
				if barrier[j] {
					continue
				}
				switch {
				case owner[j] == 0:
					owner[j], depth[j] = owner[i], depth[i]+1
					queue = append(queue, j)
				case depth[j] == depth[i]+1 && d2(j, samples[owner[i]-1]) < d2(j, samples[owner[j]-1]):
					owner[j] = owner[i]
				}
			}
		}
	}

	// side: +1 land, -1 water, 0 on the line itself or not reached.
	side := func(i int) int8 {
		if owner[i] == 0 || barrier[i] {
			return 0
		}
		s := samples[owner[i]-1]
		if v := (float64(i%W)-s.x)*s.nx + (float64(i/W)-s.y)*s.ny; v > 1e-9 {
			return 1
		} else if v < -1e-9 {
			return -1
		}
		return 0
	}

	// Agreement guard. Reversing every way flips every side, so the reversed agreement is exactly 1 - a:
	// requiring a > 0.5 refuses precisely the cases where the reversed reading fits the DEM better.
	// (Real data: Falmouth a = 0.81, Mindelo a = 0.69; a lower threshold would let Mindelo's reversed 0.31 through.)
	agree := 0
	for i := range data {
		if d := side(i); d != 0 && valid(i) {
			st.Labelled++
			if (data[i] >= 0) == (d > 0) {
				agree++
			}
		}
	}
	if st.Labelled == 0 {
		return st, nil, nil
	}
	st.Agreement = float64(agree) / float64(st.Labelled)
	if st.Agreement <= conformMinAgree {
		return st, nil, fmt.Errorf("DEM agrees with the OSM land/water sides on only %.0f%% of %d cells near the coast: way direction or CRS looks wrong, not conforming", 100*st.Agreement, st.Labelled)
	}

	var changed []int
	for i := range data {
		d := side(i)
		if d == 0 || !valid(i) {
			continue
		}
		z := data[i]
		switch {
		case d > 0 && z < 0:
			if z < -conformMaxDeepM {
				st.SkippedGuard++
				continue
			}
			data[i] = conformFlipM
			st.ToLand++
			changed = append(changed, i)
		case d < 0 && z >= 0:
			if z > conformMaxHighM {
				st.SkippedGuard++
				continue
			}
			data[i] = -conformFlipM
			st.ToWater++
			changed = append(changed, i)
		}
	}
	return st, changed, nil
}
