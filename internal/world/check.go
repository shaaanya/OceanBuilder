package world

import "fmt"

// SourceCheck is what `ocean world check` can say about one source without downloading anything: where it comes in
// the precedence, which vertical reference it is taken to have, and what the build would do to reach the target.
type SourceCheck struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Quality string `json:"quality"`
	Rank    int    `json:"precedence_rank"` // 1 = tried first (config order; "auto" is decided from fetched data and shown after the build)
	Datum   string `json:"datum"`
	Class   string `json:"datum_class,omitempty"`
	Plan    string `json:"vertical_plan"` // identity | user-offset | proj-grid | refused | not evaluated
	Detail  string `json:"detail,omitempty"`
	License string `json:"licence,omitempty"`
}

// knownVref is the vertical reference a source type carries when the config does not declare one.  Keep in step with resolveDatasets.
func knownVref(sp SourceSpec) VerticalRef {
	switch sp.Type {
	case "gmrt":
		return VerticalRef{Datum: "GMRT-mosaic"}
	case "etopo2022":
		return VerticalRef{Datum: "EGM2008"}
	case "gebco":
		return VerticalRef{Datum: "MSL"}
	case "cudem":
		return VerticalRef{Datum: "NAVD88"}
	}
	return VerticalRef{Datum: "unknown"}
}

// CheckSources evaluates the vertical model for a config offline: no download, no resampling.
func CheckSources(cfg *Config) ([]SourceCheck, string, error) {
	var dss []*dataset
	for i, sp := range cfg.effectiveSources() {
		d := &dataset{idx: i, spec: sp, vref: knownVref(sp)}
		if sp.Vertical != nil {
			d.vref = *sp.Vertical
		}
		dss = append(dss, d)
	}
	target, _, terr := chooseTarget(cfg, dss)
	var out []SourceCheck
	for i, d := range dss {
		c := SourceCheck{ID: d.spec.ID, Type: d.spec.Type, Quality: d.spec.Quality, Rank: i + 1, Datum: d.datumName(), License: d.spec.License}
		if dd, ok := lookupDatum(d.datumName()); ok {
			c.Class = dd.Class
		}
		switch {
		case terr != nil:
			c.Plan = "not evaluated"
		case target == "mixed":
			c.Plan = "none (allow_mixed)"
		default:
			horiz := 4326
			if d.spec.Type == "geotiff" {
				if g, err := inspectRaster(d.spec.Path); err == nil {
					horiz = g.EPSG
				} else {
					c.Detail = fmt.Sprintf("cannot inspect %s: %v", d.spec.Path, err)
				}
			}
			ap, err := planVertical(d.vref, target, horiz)
			if err != nil {
				c.Plan, c.Detail = "refused", err.Error()
			} else {
				c.Plan = ap.Kind
			}
		}
		out = append(out, c)
	}
	return out, target, terr
}
