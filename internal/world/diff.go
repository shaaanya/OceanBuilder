package world

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// DiffReport says what differs between two built worlds, from their manifests alone (tile files are compared by
// their recorded hashes), so two builds can be compared without reading any tile.
type DiffReport struct {
	SameBuild      bool     `json:"same_build"`
	ConfigChanges  []string `json:"config_changes,omitempty"`
	SourceChanges  []string `json:"source_changes,omitempty"`
	ToolChanges    []string `json:"tool_changes,omitempty"`
	TilesAdded     []string `json:"tiles_added,omitempty"`
	TilesRemoved   []string `json:"tiles_removed,omitempty"`
	TilesChanged   []string `json:"tiles_changed,omitempty"` // any of the four files differs
	TilesUnchanged int      `json:"tiles_unchanged"`
}

// Diff compares a (before) with b (after).
func Diff(a, b *Manifest) DiffReport {
	var r DiffReport
	r.SameBuild = a.BuildID == b.BuildID
	ca, cb := toMap(a.Config), toMap(b.Config)
	for _, k := range unionKeys(ca, cb) {
		if !reflect.DeepEqual(ca[k], cb[k]) {
			r.ConfigChanges = append(r.ConfigChanges, fmt.Sprintf("%s: %v -> %v", k, show(ca[k]), show(cb[k])))
		}
	}
	if a.Tool.Version != b.Tool.Version {
		r.ToolChanges = append(r.ToolChanges, fmt.Sprintf("tool %s -> %s", a.Tool.Version, b.Tool.Version))
	}
	if a.Tool.GDAL != b.Tool.GDAL {
		r.ToolChanges = append(r.ToolChanges, fmt.Sprintf("gdal %q -> %q", a.Tool.GDAL, b.Tool.GDAL))
	}
	sa, sb := map[string]string{}, map[string]string{}
	names := map[string]bool{}
	for _, s := range a.Sources {
		sa[s.Source], names[s.Source] = s.SHA256, true
	}
	for _, s := range b.Sources {
		sb[s.Source], names[s.Source] = s.SHA256, true
	}
	var ks []string
	for k := range names {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		switch {
		case sa[k] == "":
			r.SourceChanges = append(r.SourceChanges, k+": added")
		case sb[k] == "":
			r.SourceChanges = append(r.SourceChanges, k+": removed")
		case sa[k] != sb[k]:
			r.SourceChanges = append(r.SourceChanges, fmt.Sprintf("%s: content changed (%s -> %s)", k, short(sa[k]), short(sb[k])))
		}
	}
	ta := map[string]Tile{}
	for _, t := range a.Tiles {
		ta[t.ID] = t
	}
	for _, t := range b.Tiles {
		old, ok := ta[t.ID]
		switch {
		case !ok:
			r.TilesAdded = append(r.TilesAdded, t.ID)
		case old.SHA256 != t.SHA256:
			r.TilesChanged = append(r.TilesChanged, t.ID)
		default:
			r.TilesUnchanged++
		}
		delete(ta, t.ID)
	}
	for id := range ta {
		r.TilesRemoved = append(r.TilesRemoved, id)
	}
	sort.Strings(r.TilesRemoved)
	return r
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{}
	json.Unmarshal(b, &m)
	return m
}

func unionKeys(a, b map[string]any) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func show(v any) any {
	if v == nil {
		return "(unset)"
	}
	return v
}
