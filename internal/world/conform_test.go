package world

import "testing"

// 40x40 grid: DEM land (+5) for col < 20, sea (-5) from col 20. OSM puts the coast at col 22.
func conformFixture() ([]float32, func(int) bool) {
	d := make([]float32, 40*40)
	for r := 0; r < 40; r++ {
		for c := 0; c < 40; c++ {
			d[r*40+c] = -5
			if c < 20 {
				d[r*40+c] = 5
			}
		}
	}
	return d, func(int) bool { return true }
}

func TestConformMovesShorelineToOSMLine(t *testing.T) {
	d, valid := conformFixture()
	// Land on the left of the direction of travel: going north (row decreasing) keeps land to the west.
	north := [][][2]float64{{{22, 39}, {22, 0}}}
	st, ch, err := conformGrid(d, 40, 40, 48, 300, north, valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []int{20, 21} {
		if d[20*40+c] != conformFlipM {
			t.Errorf("col %d should become land, got %v", c, d[20*40+c])
		}
	}
	if d[20*40+23] != -5 || d[20*40+5] != 5 {
		t.Error("cells that already agree must not change")
	}
	if st.ToWater != 0 || st.ToLand != len(ch) || st.ToLand < 40 {
		t.Errorf("unexpected counts: %+v changed=%d", st, len(ch))
	}
}

func TestConformRefusesReversedWayDirection(t *testing.T) {
	d, valid := conformFixture()
	south := [][][2]float64{{{22, 0}, {22, 39}}}
	if _, _, err := conformGrid(d, 40, 40, 48, 300, south, valid); err == nil {
		t.Fatal("a line with land on its east side contradicts the DEM and must be refused")
	}
	for i, v := range d {
		if (i%40 < 20) != (v > 0) {
			t.Fatal("refused conform must leave the DEM untouched")
		}
	}
}

func TestConformLeavesDeepWaterAndFarCellsAlone(t *testing.T) {
	d, valid := conformFixture()
	for r := 0; r < 40; r++ {
		d[r*40+20] = -50 // deep trench the OSM line says is land
	}
	north := [][][2]float64{{{22, 39}, {22, 0}}}
	st, _, err := conformGrid(d, 40, 40, 48, 100, north, valid) // R = 3 cells
	if err != nil {
		t.Fatal(err)
	}
	if d[20*40+20] != -50 || st.SkippedGuard == 0 {
		t.Errorf("deep water must be skipped and counted: %v %+v", d[20*40+20], st)
	}
	if d[20*40+39] != -5 {
		t.Error("cells beyond the shift radius must not change")
	}
}

func TestConformOpenFragmentOnlyAffectsItsNeighbourhood(t *testing.T) {
	d, valid := conformFixture()
	// A fragment covering only the northern half: land and sea stay connected around its end,
	// which is exactly where flood-fill labelling breaks down. Nearest-sample sides must not care.
	half := [][][2]float64{{{22, 19}, {22, 0}}}
	_, _, err := conformGrid(d, 40, 40, 48, 300, half, valid)
	if err != nil {
		t.Fatal(err)
	}
	if d[10*40+20] != conformFlipM {
		t.Error("cell beside the fragment should become land")
	}
	if d[30*40+20] != -5 || d[30*40+21] != -5 {
		t.Error("cells far from the fragment (south of its end) must keep the DEM value")
	}
}
