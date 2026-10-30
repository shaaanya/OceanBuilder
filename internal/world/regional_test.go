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
	if dem[0] != -52 || dem[500] != -30 || dem[100] != 25 || dem[115] != -40 {
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
