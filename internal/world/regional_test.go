package world

import (
	"strings"
	"testing"
)

func regionalFixture(n int, regionalOffset float32) (dem, reg []float32, meas []uint8) {
	dem, reg, meas = make([]float32, n), make([]float32, n), make([]uint8, n)
	for i := range dem {
		dem[i] = -30
		reg[i] = -30 + regionalOffset
		meas[i] = 1
	}
	return
}

func TestFillRegionalReplacesOnlyNonSurveyWater(t *testing.T) {
	dem, reg, meas := regionalFixture(1000, -0.5)
	// 100 fill cells (GEBCO) with a different regional value, 10 land cells, 10 fill cells outside regional coverage.
	for i := 0; i < 100; i++ {
		meas[i], dem[i], reg[i] = 0, -40, -52
	}
	for i := 100; i < 110; i++ {
		dem[i], reg[i] = 25, 30
	}
	for i := 110; i < 120; i++ {
		meas[i], dem[i], reg[i] = 0, -40, -9999
	}
	st, err := fillRegional(dem, reg, meas, len(dem), 1, 48, func(int) bool { return true }, func(v float32) bool { return v > -9000 })
	if err != nil {
		t.Fatal(err)
	}
	// 879, not 880: the survey cell next to the uncovered (-9999) block is not flat in the regional grid.
	if st.CellsReplaced != 100 || st.FillCellsLeft != 10 || st.OverlapCells != 879 {
		t.Fatalf("counts wrong: %+v", st)
	}
	if st.MedianOffsetM != -0.5 {
		t.Fatalf("median offset must be measured on survey cells only: %v", st.MedianOffsetM)
	}
	// Cell 0 sits 9 cells from the nearest kept water (the uncovered block at 110): within the 8-cell ramp's reach it is
	// partly blended, so it lies between baseline and regional value.
	if dem[0] > -40 || dem[0] < -52 || dem[500] != -30 || dem[100] != 25 || dem[115] != -40 {
		t.Fatal("replace fill water only; keep survey cells, land and uncovered fill")
	}
	if meas[0] != 0 {
		t.Fatal("replaced cells must not be flagged survey-backed")
	}
}

func TestFillRegionalRefusesDatumMismatchOrTooLittleOverlap(t *testing.T) {
	dem, reg, meas := regionalFixture(1000, 8) // regional 8 m shallower everywhere on shared cells
	before := append([]float32(nil), dem...)
	_, err := fillRegional(dem, reg, meas, len(dem), 1, 48, func(int) bool { return true }, func(float32) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("an 8 m median offset must be refused: %v", err)
	}
	for i := range dem {
		if dem[i] != before[i] {
			t.Fatal("a refused fill must not edit the DEM")
		}
	}
	dem, reg, meas = regionalFixture(100, 0)
	if _, err := fillRegional(dem, reg, meas, len(dem), 1, 48, func(int) bool { return true }, func(float32) bool { return true }); err == nil || !strings.Contains(err.Error(), "cannot validate") {
		t.Fatalf("too few shared survey cells must be refused: %v", err)
	}
}

// A steep, noisy slope must not be used to judge the datum: with +-40 m sampling scatter and a true offset of
// -1 m, an all-cells median would be swamped, but the flat shelf still shows the offset.
func TestFillRegionalOffsetComesFromFlatCellsOnly(t *testing.T) {
	const n = 2000
	dem, reg, meas := make([]float32, n), make([]float32, n), make([]uint8, n)
	for i := 0; i < 1000; i++ { // flat shelf, regional 1 m deeper
		dem[i], reg[i], meas[i] = -50, -51, 1
	}
	for i := 1000; i < n; i++ { // steep slope (200 m per cell), regional scattered by +-40 m
		dem[i] = float32(-300 - 200*(i-1000))
		reg[i] = dem[i] + float32(40*(1-2*(i%2)))
		meas[i] = 1
	}
	st, err := fillRegional(dem, reg, meas, n, 1, 48, func(int) bool { return true }, func(float32) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if st.MedianOffsetM != -1 || st.OverlapCells < 990 || st.OverlapCells > 1001 {
		t.Fatalf("offset must come from the flat shelf (-1 m, ~1000 cells): %+v", st)
	}
}

// The seam must not be a cliff: kept GMRT cells on the left, replaced region on the right disagreeing by 20 m.
// The first replaced cell moves by only 1/blend of that and the ramp deepens monotonically.
func TestFillRegionalBlendsFromKeptCells(t *testing.T) {
	const w, rows = 40, 300 // tall enough for the 200-flat-cell offset check
	D, R, M := make([]float32, w*rows), make([]float32, w*rows), make([]uint8, w*rows)
	for r := 0; r < rows; r++ {
		for i := 0; i < w; i++ {
			D[r*w+i], R[r*w+i] = -100, -120
			if i < 20 {
				M[r*w+i], R[r*w+i] = 1, -100
			}
		}
	}
	st, err := fillRegional(D, R, M, w, rows, 48, func(int) bool { return true }, func(float32) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	row := D[150*w : 151*w]
	if st.BlendCells != regionalBlendCells {
		t.Fatalf("blend cells not reported: %+v", st)
	}
	if d := float64(row[19] - row[20]); d < 0 || d > 20.0/regionalBlendCells+0.01 {
		t.Fatalf("first replaced cell should differ from the kept one by 20/blend, got %v", d)
	}
	for i := 21; i < 20+regionalBlendCells; i++ {
		if row[i] > row[i-1] {
			t.Fatalf("ramp must deepen monotonically toward the regional value: %v", row[18:32])
		}
	}
	if row[39] != -120 {
		t.Fatalf("far from kept cells the regional value applies in full: %v", row[39])
	}
}

// Misregistration: a regional grid that is the GMRT field moved two cells east. A one-cell shift halves the rms,
// which the check must catch (a flat shelf alone could not show it).
func TestFillRegionalRefusesMisregisteredGrid(t *testing.T) {
	const w, h = 60, 60
	dem, reg, meas := make([]float32, w*h), make([]float32, w*h), make([]uint8, w*h)
	// West half flat (enough flat cells for the offset check), east half slopes with texture.
	field := func(x, y int) float32 {
		if x < 30 {
			return -100
		}
		return -100 - 3*float32(x-30) - 2*float32(y%7)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dem[y*w+x], meas[y*w+x] = field(x, y), 1
			reg[y*w+x] = field(x-2, y) // the same field, two cells east
		}
	}
	_, err := fillRegional(dem, reg, meas, w, h, 48, func(int) bool { return true }, func(float32) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "misregistration") {
		t.Fatalf("a two-cell shift must be refused: %v", err)
	}
	// And a relative offset (depth reported in feet) is refused whatever the slope.
	for i := range reg {
		reg[i] = dem[i] * 3.28
	}
	if _, err := fillRegional(dem, reg, meas, w, h, 48, func(int) bool { return true }, func(float32) bool { return true }); err == nil || !strings.Contains(err.Error(), "units") {
		t.Fatalf("feet-for-metres must be refused: %v", err)
	}
}

func TestRegionalCaveats(t *testing.T) {
	ok := RegionalStats{FlatMedianDepthM: 40, CellsReplaced: 10, MaxChangeM: 15}
	ok.setStatus(100)
	if ok.Status != "accepted" || len(ok.Caveats) != 0 {
		t.Fatalf("%+v", ok)
	}
	deep := RegionalStats{FlatMedianDepthM: 370, CellsReplaced: 63, MaxChangeM: 574} // Mindelo
	deep.setStatus(100)
	if deep.Status != "accepted_with_caveats" || len(deep.Caveats) != 3 {
		t.Fatalf("%+v", deep)
	}
}
