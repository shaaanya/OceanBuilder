package libreterrain

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/ocean-compiler/ocean/internal/world"
)

// LTOrientation is the statement LibreTerrain writes into build.json; import refuses anything else.
const LTOrientation = "row 0 = world Y 0, column 0 = world X 0"

// LTBuild mirrors the parts of LibreTerrain's build.json that import relies on.
type LTBuild struct {
	Generator    string     `json:"generator"`
	AppVersion   string     `json:"app_version"`
	WorldSizeM   [2]float64 `json:"world_size_m"`
	HeightRangeM [2]float64 `json:"height_range_m"`
	Resolution   [2]int     `json:"resolution"`
	CellSizeM    [2]float64 `json:"cell_size_m"`
	Orientation  string     `json:"orientation"`
	Files        []LTFile   `json:"files"`
}

// LTFile is one entry of build.json's file list.
type LTFile struct {
	File     string  `json:"file"`
	Node     string  `json:"node"`
	Port     string  `json:"port"`
	Data     string  `json:"data"`
	Format   string  `json:"format"`
	Encoding string  `json:"encoding"`
	DataMin  float64 `json:"data_min"`
	DataMax  float64 `json:"data_max"`
}

// ReadBuild parses build.json from a LibreTerrain output folder.
func ReadBuild(dir string) (*LTBuild, error) {
	b, err := os.ReadFile(filepath.Join(dir, "build.json"))
	if err != nil {
		return nil, fmt.Errorf("not a LibreTerrain output folder: %w", err)
	}
	var lt LTBuild
	if err := json.Unmarshal(b, &lt); err != nil {
		return nil, fmt.Errorf("build.json: %w", err)
	}
	return &lt, nil
}

// Inspect describes a LibreTerrain output folder without needing an exchange folder.
func Inspect(dir string) (string, error) {
	lt, err := ReadBuild(dir)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "generator: %s %s\nworld: %g x %g m, height range %g..%g m\nraster: %d x %d samples, cell %g x %g m\norientation: %q\n",
		lt.Generator, lt.AppVersion, lt.WorldSizeM[0], lt.WorldSizeM[1], lt.HeightRangeM[0], lt.HeightRangeM[1],
		lt.Resolution[0], lt.Resolution[1], lt.CellSizeM[0], lt.CellSizeM[1], lt.Orientation)
	if lt.Generator != "LibreTerrain" {
		fmt.Fprintf(&sb, "WARNING: generator is %q, not LibreTerrain\n", lt.Generator)
	}
	if lt.CellSizeM[0] != lt.CellSizeM[1] {
		fmt.Fprintf(&sb, "WARNING: cell is anisotropic (%g x %g m): a non-square world was resampled to a square raster\n", lt.CellSizeM[0], lt.CellSizeM[1])
	}
	for _, f := range lt.Files {
		st, err := os.Stat(filepath.Join(dir, f.File))
		state := fmt.Sprintf("%d bytes", func() int64 {
			if st != nil {
				return st.Size()
			}
			return 0
		}())
		if err != nil {
			state = "MISSING"
		}
		fmt.Fprintf(&sb, "file %-34s %-10s %-7s %-8s data %.3f..%.3f  %s\n", f.File, f.Data, f.Format, f.Port, f.DataMin, f.DataMax, state)
	}
	return sb.String(), nil
}

// ImportOptions control Import.
type ImportOptions struct {
	File       string // build.json file name to import when several heightfields were exported
	BlendCells int    // cells inland over which an edit ramps in from the shoreline (default 4)
	License    string // licence statement for the edit layer (default: user-supplied)
}

// CheckpointResult compares a checkpoint of the export with what came back.
type CheckpointResult struct {
	Name      string  `json:"name"`
	Col       int     `json:"col"`
	Row       int     `json:"row"`
	ExportedM float64 `json:"exported_m"`
	ReturnedM float64 `json:"returned_m"`
	DiffM     float64 `json:"diff_m"`
}

// Report records what import measured. It is written next to the refinement.
type Report struct {
	LTApp        string             `json:"libreterrain_app_version"`
	LTFile       string             `json:"libreterrain_file"`
	LTFormat     string             `json:"format"`
	Orientation  string             `json:"orientation_check"`
	OrientErrM   map[string]float64 `json:"orientation_mean_abs_diff_m"`
	Cells        int                `json:"cells"`
	ChangedCells int                `json:"cells_changed_over_1mm"`
	MaxAbsM      float64            `json:"max_abs_change_m"`
	MeanAbsM     float64            `json:"mean_abs_change_m"`
	MinChangeM   float64            `json:"min_change_m"`
	MaxChangeM   float64            `json:"max_change_m"`
	PadEdited    int                `json:"padding_cells_edited_discarded"`
	Checkpoints  []CheckpointResult `json:"checkpoints"`
	Warnings     []string           `json:"warnings"`
}

var transforms = []string{"identity", "mirrored east-west", "mirrored north-south", "rotated 180°", "transposed", "anti-transposed", "rotated 90°", "rotated 270°"}

// at samples a square raster of side n under dihedral transform t at (r,c).
func at(a []float32, n, r, c, t int) float32 {
	switch t {
	case 1:
		c = n - 1 - c
	case 2:
		r = n - 1 - r
	case 3:
		r, c = n-1-r, n-1-c
	case 4:
		r, c = c, r
	case 5:
		r, c = n-1-c, n-1-r
	case 6:
		r, c = n-1-c, r
	case 7:
		r, c = c, n-1-r
	}
	return a[r*n+c]
}

// detectOrientation measures how well each flip/rotation of lt matches base over the real area.
func detectOrientation(base, lt []float32, n, w, h int) (best int, errs map[string]float64) {
	step := max(1, n/64)
	errs = map[string]float64{}
	var all [8]float64
	for t := range transforms {
		var sum float64
		var cnt int
		for r := 0; r < h; r += step {
			for c := 0; c < w; c += step {
				sum += math.Abs(float64(at(lt, n, r, c, t) - base[r*n+c]))
				cnt++
			}
		}
		all[t] = sum / float64(cnt)
		errs[transforms[t]] = all[t]
		if all[t] < all[best] {
			best = t
		}
	}
	return best, errs
}

// Import validates a LibreTerrain output against an exchange folder and writes a refinement
// (edit layer) plus a world config that applies it. Inputs are only read; outDir must be new or empty.
func Import(exchangeDir, ltDir, outDir string, opt ImportOptions) (*Report, error) {
	x, err := LoadExchange(exchangeDir)
	if err != nil {
		return nil, err
	}
	if err := checkOutDir(outDir, exchangeDir, ltDir); err != nil {
		return nil, err
	}
	sha, err := sha256File(filepath.Join(exchangeDir, x.Files.EXR))
	if err != nil {
		return nil, err
	}
	if sha != x.Files.EXRSHA256 {
		return nil, fmt.Errorf("%s changed since export (hash mismatch): the exchange folder is no longer a trustworthy baseline", x.Files.EXR)
	}
	n := x.Grid.Side
	base, bw, bh, err := readRaster(filepath.Join(exchangeDir, x.Files.EXR))
	if err != nil {
		return nil, err
	}
	if bw != n || bh != n {
		return nil, fmt.Errorf("baseline raster is %dx%d, exchange says %d", bw, bh, n)
	}

	lt, err := ReadBuild(ltDir)
	if err != nil {
		return nil, err
	}
	var bad []string
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-6*math.Max(1, math.Abs(b)) }
	if lt.Generator != "LibreTerrain" {
		bad = append(bad, fmt.Sprintf("generator is %q", lt.Generator))
	}
	if lt.Resolution != [2]int{n, n} {
		bad = append(bad, fmt.Sprintf("resolution is %dx%d, export was %dx%d: LibreTerrain resampled the terrain, so cell positions no longer match", lt.Resolution[0], lt.Resolution[1], n, n))
	}
	if !near(lt.WorldSizeM[0], x.LT.WorldSizeM[0]) || !near(lt.WorldSizeM[1], x.LT.WorldSizeM[1]) {
		bad = append(bad, fmt.Sprintf("world size is %v m, export was %v m", lt.WorldSizeM, x.LT.WorldSizeM))
	}
	if !near(lt.CellSizeM[0], x.Grid.CellM) || !near(lt.CellSizeM[1], x.Grid.CellM) {
		bad = append(bad, fmt.Sprintf("cell size is %v m, export was %g m", lt.CellSizeM, x.Grid.CellM))
	}
	if !near(lt.HeightRangeM[0], x.LT.HeightRangeM[0]) || !near(lt.HeightRangeM[1], x.LT.HeightRangeM[1]) {
		bad = append(bad, fmt.Sprintf("height range is %v m, export was %v m (PNG16 values would be misread)", lt.HeightRangeM, x.LT.HeightRangeM))
	}
	if lt.Orientation != LTOrientation {
		bad = append(bad, fmt.Sprintf("orientation statement is %q, this importer knows %q", lt.Orientation, LTOrientation))
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("LibreTerrain output is incompatible with the export:\n  - %s", strings.Join(bad, "\n  - "))
	}

	f, err := pickFile(lt, opt.File)
	if err != nil {
		return nil, err
	}
	rep := &Report{LTApp: lt.AppVersion, LTFile: f.File, LTFormat: f.Format}
	var out []float32
	var w, h int
	switch f.Format {
	case "exr32":
		out, w, h, err = readRaster(filepath.Join(ltDir, f.File))
	case "png16":
		out, w, h, err = readPNG16(filepath.Join(ltDir, f.File), lt.HeightRangeM[0], lt.HeightRangeM[1])
		step := (lt.HeightRangeM[1] - lt.HeightRangeM[0]) / 65535
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("imported from PNG16: heights are quantised to %.4f m steps over %g..%g m; re-export EXR32 for exact metres", step, lt.HeightRangeM[0], lt.HeightRangeM[1]))
		if f.DataMin < lt.HeightRangeM[0] || f.DataMax > lt.HeightRangeM[1] {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("PNG16 is CLIPPED: terrain spans %.2f..%.2f m but the PNG only holds %g..%g m; export EXR32 or widen the world height range", f.DataMin, f.DataMax, lt.HeightRangeM[0], lt.HeightRangeM[1]))
		}
	default:
		err = fmt.Errorf("%s: format %q cannot be imported as a heightfield", f.File, f.Format)
	}
	if err != nil {
		return nil, err
	}
	if w != n || h != n {
		return nil, fmt.Errorf("%s is %dx%d samples, expected %dx%d", f.File, w, h, n, n)
	}
	for i, v := range out {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("%s: non-finite value at sample %d", f.File, i)
		}
	}

	best, errs := detectOrientation(base, out, n, x.Grid.Width, x.Grid.Height)
	rep.OrientErrM = errs
	if best != 0 && errs[transforms[0]] > 2*errs[transforms[best]]+0.5 {
		return nil, fmt.Errorf("%s looks %s relative to the export (mean difference %.2f m as-is, %.2f m after correcting); refusing to guess", f.File, transforms[best], errs[transforms[0]], errs[transforms[best]])
	}
	rep.Orientation = "matches export (identity)"

	W, H := x.Grid.Width, x.Grid.Height
	delta := make([]float32, W*H)
	rep.MinChangeM, rep.MaxChangeM = math.Inf(1), math.Inf(-1)
	var sum float64
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			d := out[r*n+c] - base[r*n+c]
			if r >= H || c >= W {
				if math.Abs(float64(d)) > 0.001 {
					rep.PadEdited++
				}
				continue
			}
			delta[r*W+c] = d
			a := math.Abs(float64(d))
			sum += a
			rep.MaxAbsM = math.Max(rep.MaxAbsM, a)
			rep.MinChangeM, rep.MaxChangeM = math.Min(rep.MinChangeM, float64(d)), math.Max(rep.MaxChangeM, float64(d))
			if a > 0.001 {
				rep.ChangedCells++
			}
		}
	}
	rep.Cells = W * H
	rep.MeanAbsM = sum / float64(W*H)
	for _, c := range x.Checkpoints {
		got := float64(out[c.Row*n+c.Col])
		rep.Checkpoints = append(rep.Checkpoints, CheckpointResult{c.Name, c.Col, c.Row, c.ElevationM, got, got - c.ElevationM})
	}
	if rep.PadEdited > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d edits fell in the padding outside the world and were discarded", rep.PadEdited))
	}
	if rep.ChangedCells == 0 {
		rep.Warnings = append(rep.Warnings, "no cell differs from the export by more than 1 mm: this is an unmodified round trip")
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	raw := make([]byte, len(delta)*4)
	for i, v := range delta {
		binary.LittleEndian.PutUint32(raw[i*4:], math.Float32bits(v))
	}
	if err := os.WriteFile(filepath.Join(outDir, "delta.f32"), raw, 0o644); err != nil {
		return nil, err
	}
	dsha, err := sha256File(filepath.Join(outDir, "delta.f32"))
	if err != nil {
		return nil, err
	}
	lsha, err := sha256File(filepath.Join(ltDir, f.File))
	if err != nil {
		return nil, err
	}
	ref := world.Refinement{SchemaVersion: 1, Kind: world.RefinementKind, BaseBuildID: x.Source.BuildID, BaseWorld: x.Source.World, EPSG: x.EPSG,
		DeltaFile: "delta.f32", DeltaSHA256: dsha, BlendCells: opt.BlendCells, Editor: "LibreTerrain " + lt.AppVersion, EditorFile: f.File, EditorSHA: lsha,
		License: opt.License, Report: rep}
	ref.Grid.CellM, ref.Grid.Width, ref.Grid.Height, ref.Grid.OriginE, ref.Grid.OriginNT = x.Grid.CellM, W, H, x.Grid.OriginE, x.Grid.OriginNT
	if ref.BlendCells < 1 {
		ref.BlendCells = world.DefaultBlendCells
	}
	if ref.License == "" {
		ref.License = "user-created edit layer; the underlying sources keep their own licences (see the base world's ATTRIBUTION.txt)"
	}
	if err := writeJSON(filepath.Join(outDir, "refinement.json"), ref); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(outDir, "import-report.json"), rep); err != nil {
		return nil, err
	}
	cfg := x.WorldConfig
	cfg.Name = x.Source.World + "-refined"
	cfg.Refinement = "refinement.json"
	if err := writeJSON(filepath.Join(outDir, cfg.Name+".world.json"), cfg); err != nil {
		return nil, err
	}
	return rep, nil
}

func pickFile(lt *LTBuild, name string) (*LTFile, error) {
	var exr, pngs []*LTFile
	for i := range lt.Files {
		f := &lt.Files[i]
		if name != "" {
			if f.File == name {
				return f, nil
			}
			continue
		}
		if f.Data != "heightfield" {
			continue
		}
		if f.Format == "exr32" {
			exr = append(exr, f)
		} else if f.Format == "png16" {
			pngs = append(pngs, f)
		}
	}
	if name != "" {
		return nil, fmt.Errorf("build.json lists no file named %q", name)
	}
	c := exr
	if len(c) == 0 {
		c = pngs
	}
	switch len(c) {
	case 0:
		return nil, fmt.Errorf("build.json has no heightfield export (exr32 or png16); mark the final terrain for export in LibreTerrain")
	case 1:
		return c[0], nil
	}
	var names []string
	for _, f := range c {
		names = append(names, f.File)
	}
	return nil, fmt.Errorf("several heightfields were exported (%s); choose one with -file", strings.Join(names, ", "))
}

func checkOutDir(out string, others ...string) error {
	for _, o := range others {
		if isUnder(out, o) || isUnder(o, out) {
			return fmt.Errorf("output %s must be separate from %s: refined data never overwrites inputs", out, o)
		}
	}
	if es, err := os.ReadDir(out); err == nil && len(es) > 0 {
		return fmt.Errorf("output %s is not empty", out)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
