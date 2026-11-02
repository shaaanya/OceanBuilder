package world

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/shaaanya/OceanBuilder/internal/fetch"
)

const (
	// RefinementKind identifies a refinement.json written by `ocean libreterrain import`.
	RefinementKind = "ocean-refinement"
	// DefaultBlendCells is how many cells inland an edit ramps from 0 to full strength.
	DefaultBlendCells = 4
	// ignoreBelowM: edits smaller than this on water/nodata cells are float noise, not edits.
	ignoreBelowM = 0.001
)

// Refinement is an edit layer on top of a built world's land elevation: a
// float32 delta (metres, north row first) on the world's vertex grid. Water and
// nodata cells of the base build are never changed by it.
type Refinement struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	BaseBuildID   string `json:"base_build_id"`
	BaseWorld     string `json:"base_world"`
	EPSG          int    `json:"crs_epsg"`
	Grid          struct {
		CellM    float64 `json:"cell_m"`
		Width    int     `json:"width"`
		Height   int     `json:"height"`
		OriginE  float64 `json:"origin_easting"`
		OriginNT float64 `json:"origin_northing_top"`
	} `json:"grid"`
	DeltaFile   string `json:"delta_file"`
	DeltaSHA256 string `json:"delta_sha256"`
	BlendCells  int    `json:"blend_cells"`
	Editor      string `json:"editor"` // e.g. "LibreTerrain 2026.2.0"
	EditorFile  string `json:"editor_file"`
	EditorSHA   string `json:"editor_file_sha256"`
	License     string `json:"license"`
	Report      any    `json:"import_report,omitempty"`
}

// RefineStats is recorded in the built manifest so the effect of an edit layer is auditable.
type RefineStats struct {
	BaseBuildID       string  `json:"base_build_id"`
	BlendCells        int     `json:"blend_cells"`
	CellsChanged      int     `json:"land_cells_changed"`
	MaxAbsChangeM     float64 `json:"max_abs_change_m"`
	MeanAbsChangeM    float64 `json:"mean_abs_change_m"`
	ClampedLandCells  int     `json:"land_cells_clamped_at_sea_level"`
	IgnoredWaterCells int     `json:"edits_ignored_on_water_or_nodata"`
}

// LoadRefinement reads refinement.json and its delta raster and verifies the delta's hash.
func LoadRefinement(path string) (*Refinement, []float32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var r Refinement
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	if r.Kind != RefinementKind || r.SchemaVersion != 1 {
		return nil, nil, fmt.Errorf("%s: not an %s v1 file", path, RefinementKind)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), r.DeltaFile))
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != r.DeltaSHA256 {
		return nil, nil, fmt.Errorf("%s: delta file hash differs from refinement.json (file edited after import?)", r.DeltaFile)
	}
	if len(raw) != r.Grid.Width*r.Grid.Height*4 {
		return nil, nil, fmt.Errorf("%s: %d bytes, want %d", r.DeltaFile, len(raw), r.Grid.Width*r.Grid.Height*4)
	}
	d := make([]float32, r.Grid.Width*r.Grid.Height)
	for i := range d {
		d[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		if math.IsNaN(float64(d[i])) || math.IsInf(float64(d[i]), 0) {
			return nil, nil, fmt.Errorf("%s: non-finite delta at sample %d", r.DeltaFile, i)
		}
	}
	return &r, d, nil
}

// Check refuses a refinement made for a different grid: applying it would shift terrain silently.
func (r *Refinement) Check(g *Grid, epsg, W, H int) error {
	switch {
	case r.EPSG != epsg:
		return fmt.Errorf("refinement is for EPSG:%d, world is EPSG:%d", r.EPSG, epsg)
	case r.Grid.Width != W || r.Grid.Height != H:
		return fmt.Errorf("refinement grid is %dx%d samples, world grid is %dx%d (bbox or tile_cells changed since export)", r.Grid.Width, r.Grid.Height, W, H)
	case r.Grid.CellM != g.CellM:
		return fmt.Errorf("refinement cell is %g m, world cell is %g m", r.Grid.CellM, g.CellM)
	case r.Grid.OriginE != g.OriginE || r.Grid.OriginNT != g.OriginN:
		return fmt.Errorf("refinement origin (%g,%g) differs from world origin (%g,%g)", r.Grid.OriginE, r.Grid.OriginNT, g.OriginE, g.OriginN)
	}
	return nil
}

// Provenance describes the edit layer as a source of the built world.
func (r *Refinement) Provenance() fetch.Provenance {
	return fetch.Provenance{
		Source:      "refinement: " + r.Editor,
		URL:         "file://" + r.DeltaFile,
		SHA256:      r.DeltaSHA256,
		Bytes:       int64(r.Grid.Width * r.Grid.Height * 4),
		License:     r.License,
		Attribution: fmt.Sprintf("land elevation edited in %s on top of build %s; water and nodata cells are unchanged from the base sources", r.Editor, short(r.BaseBuildID)),
		RetrievedAt: "n/a (user-created edit layer)",
	}
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// applyRefinement adds delta to land cells (dem >= 0), ramping from 0 at the
// shoreline to full strength blend cells inland, and never lets land go below sea
// level. Water and nodata cells are left exactly as the base sources have them.
func applyRefinement(dem []float32, W, H int, delta []float32, blend int) RefineStats {
	if blend < 1 {
		blend = DefaultBlendCells
	}
	land := func(i int) bool { return dem[i] > noData+1 && dem[i] >= 0 }
	// L1 distance in cells to the nearest non-land cell (two-pass chamfer; exact for 4-neighbourhood).
	const inf = math.MaxInt32 / 2
	dist := make([]int32, W*H)
	for i := range dist {
		if land(i) {
			dist[i] = inf
		}
	}
	for r := 0; r < H; r++ {
		for c := 0; c < W; c++ {
			i := r*W + c
			if dist[i] == 0 {
				continue
			}
			if r > 0 && dist[i-W]+1 < dist[i] {
				dist[i] = dist[i-W] + 1
			}
			if c > 0 && dist[i-1]+1 < dist[i] {
				dist[i] = dist[i-1] + 1
			}
		}
	}
	for r := H - 1; r >= 0; r-- {
		for c := W - 1; c >= 0; c-- {
			i := r*W + c
			if r < H-1 && dist[i+W]+1 < dist[i] {
				dist[i] = dist[i+W] + 1
			}
			if c < W-1 && dist[i+1]+1 < dist[i] {
				dist[i] = dist[i+1] + 1
			}
		}
	}
	var st RefineStats
	st.BlendCells = blend
	var sum float64
	for i, d := range delta {
		if d == 0 {
			continue
		}
		if !land(i) {
			if math.Abs(float64(d)) > ignoreBelowM {
				st.IgnoredWaterCells++
			}
			continue
		}
		w := math.Min(1, float64(dist[i])/float64(blend))
		nv := float64(dem[i]) + w*float64(d)
		if nv < 0 {
			nv = 0
			st.ClampedLandCells++
		}
		ch := math.Abs(nv - float64(dem[i]))
		if ch == 0 {
			continue
		}
		dem[i] = float32(nv)
		st.CellsChanged++
		sum += ch
		st.MaxAbsChangeM = math.Max(st.MaxAbsChangeM, ch)
	}
	if st.CellsChanged > 0 {
		st.MeanAbsChangeM = sum / float64(st.CellsChanged)
	}
	return st
}
