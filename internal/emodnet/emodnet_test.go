package emodnet

import "testing"

func TestCovers(t *testing.T) {
	if !Covers(-5.2, 50.05, -4.95, 50.22) {
		t.Error("Falmouth is inside")
	}
	if !Covers(-25.15, 16.75, -24.79, 17.02) {
		t.Error("Cape Verde (Mindelo) is inside the declared extent")
	}
	if Covers(-125, 40, -124, 41) {
		t.Error("US west coast is outside")
	}
	if _, err := Fetch(t.TempDir(), "", -125, 40, -124, 41); err == nil {
		t.Error("fetch outside coverage must fail before any network call")
	}
}

func TestCoverageID(t *testing.T) {
	for rel, want := range map[string]string{"": "emodnet__mean", "2022": "emodnet__mean_2022"} {
		if got, ok := CoverageID(rel); !ok || got != want {
			t.Errorf("%q: %q %v", rel, got, ok)
		}
	}
	if _, ok := CoverageID("2024"); ok {
		t.Error("unlisted release must be rejected")
	}
	if _, err := Fetch(t.TempDir(), "1999", -5.2, 50.05, -4.95, 50.22); err == nil {
		t.Error("unknown release must fail before any network call")
	}
}
