package main

import "testing"

// Every listed source can be explained, and every explanation answers every question.
func TestEverySourceIsExplained(t *testing.T) {
	for _, s := range sources {
		d, ok := sourceDetails[s.ID]
		if !ok {
			t.Errorf("%s: no detail record", s.ID)
			continue
		}
		for name, v := range map[string]string{"datum": d.Datum, "resolution": d.Resolution, "measured": d.Measured,
			"antimeridian": d.Antimeridian, "checked live": d.CheckedLive, "limits": d.Limits} {
			if v == "" {
				t.Errorf("%s: %s is empty", s.ID, name)
			}
		}
		if err := explainSource(s.ID); err != nil {
			t.Errorf("%s: %v", s.ID, err)
		}
	}
	if err := explainSource("nope"); err == nil {
		t.Error("an unknown source must be an error")
	}
	for id := range sourceDetails {
		found := false
		for _, s := range sources {
			found = found || s.ID == id
		}
		if !found {
			t.Errorf("detail record %q has no listed source", id)
		}
	}
}
