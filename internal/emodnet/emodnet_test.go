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
	if _, err := Fetch(t.TempDir(), -125, 40, -124, 41); err == nil {
		t.Error("fetch outside coverage must fail before any network call")
	}
}
