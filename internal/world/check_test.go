package world

import "testing"

func TestCheckSourcesOffline(t *testing.T) {
	cfg := &Config{Name: "x", Sources: []SourceSpec{
		{ID: "a", Type: "etopo2022", Quality: "compiled"},
		{ID: "b", Type: "gebco", Quality: "compiled"},
	}, Vertical: &VerticalTarget{Datum: "EGM2008"}}
	got, target, err := CheckSources(cfg)
	if err != nil || target != "EGM2008" || got[0].Plan != "identity" || got[1].Plan != "refused" || got[1].Datum != "MSL" {
		t.Fatalf("target %s err %v got %+v", target, err, got)
	}
	// no target and different datums: refused, nothing evaluated
	cfg = &Config{Name: "x", Sources: []SourceSpec{{ID: "a", Type: "etopo2022"}, {ID: "b", Type: "cudem"}}}
	if _, _, err := CheckSources(cfg); err == nil {
		t.Fatal("EGM2008 + NAVD88 without a target must be refused")
	}
	// a declared datum overrides the type's, and a documented offset is a plan
	off, src := 0.5, "tide gauge note"
	cfg = &Config{Name: "x", Sources: []SourceSpec{{ID: "a", Type: "geotiff", Path: "/nonexistent.tif", Vertical: &VerticalRef{Datum: "MLLW", OffsetM: &off, OffsetSource: src}}},
		Vertical: &VerticalTarget{Datum: "MSL"}}
	got, _, _ = CheckSources(cfg)
	if got[0].Plan != "user-offset" || got[0].Datum != "MLLW" {
		t.Fatalf("%+v", got)
	}
}
