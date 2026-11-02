// Package libreterrain exchanges land heightmaps with LibreTerrain through files.
// LibreTerrain is an optional external tool: nothing here links to it, and the
// MCP bridge is only used when its binary is found.
package libreterrain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/shaaanya/OceanBuilder/internal/gdal"
	"github.com/shaaanya/OceanBuilder/internal/validate"
	"github.com/shaaanya/OceanBuilder/internal/world"
)

const (
	ExchangeFile = "exchange.json"
	ExchangeKind = "ocean-libreterrain-exchange"
	HeightmapEXR = "heightmap.exr"
	HeightmapPNG = "heightmap.png"
	// Mapping is the exact coordinate relation between LibreTerrain world metres and UTM.
	Mapping = "LibreTerrain pixel (col,row) is world (x,y) = (col*cell_m, row*cell_m). Row 0 is the NORTH edge: " +
		"UTM easting = origin_easting + x, UTM northing = origin_northing_top - y. LibreTerrain's +Y therefore points south (down in its top view)."
)

// Exchange is OceanBuilder-owned metadata that makes pixels, UTM and geography unambiguous.
// LibreTerrain itself stores none of it (its build.json has only world size and height range).
type Exchange struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Tool          string `json:"tool"`
	Source        struct {
		World          string `json:"world"`
		BuildID        string `json:"build_id"`
		ManifestSHA256 string `json:"manifest_sha256"`
	} `json:"source"`
	EPSG int `json:"crs_epsg"`
	// WorldConfig is the source world's config, so import can write a config that rebuilds it with the edit layer.
	WorldConfig world.Config `json:"world_config"`
	Grid        struct {
		CellM        float64 `json:"cell_m"`
		Width        int     `json:"width"`  // real samples west to east
		Height       int     `json:"height"` // real samples north to south
		Side         int     `json:"side"`   // exported square size: LibreTerrain builds square rasters only
		PadCols      int     `json:"pad_cols"`
		PadRows      int     `json:"pad_rows"`
		OriginE      float64 `json:"origin_easting"`
		OriginNT     float64 `json:"origin_northing_top"`
		RowOrder     string  `json:"row_order"`
		Registration string  `json:"registration"`
	} `json:"grid"`
	LT struct {
		WorldSizeM      [2]float64 `json:"world_size_m"`
		HeightRangeM    [2]float64 `json:"height_range_m"`
		BuildResolution int        `json:"build_resolution"`
		FlipY           bool       `json:"flip_y"`
		Mapping         string     `json:"mapping"`
	} `json:"libreterrain"`
	Files struct {
		EXR       string `json:"heightmap_exr"`
		EXRSHA256 string `json:"heightmap_exr_sha256"`
		PNG       string `json:"heightmap_png16"`
		Project   string `json:"project,omitempty"`
	} `json:"files"`
	Stats struct {
		MinM        float64 `json:"min_m"`
		MaxM        float64 `json:"max_m"`
		NoDataCells int     `json:"nodata_cells_set_to_min"`
	} `json:"stats"`
	Checkpoints []Checkpoint `json:"checkpoints"`
	Notes       []string     `json:"notes"`
}

// Checkpoint ties a named world vertex to its pixel and elevation in the exported raster.
type Checkpoint struct {
	Name       string  `json:"name"`
	Easting    float64 `json:"easting"`
	Northing   float64 `json:"northing"`
	Col        int     `json:"col"`
	Row        int     `json:"row"`
	ElevationM float64 `json:"elevation_m"`
}

// ExportOptions control Export.
type ExportOptions struct {
	Headroom float64 // fraction of the elevation range added above the highest point for editing (default 0.25)
	Bridge   string  // libreterrain-mcp path; "" skips writing a .lterrain project
}

// LoadExchange reads and checks exchange.json from dir.
func LoadExchange(dir string) (*Exchange, error) {
	b, err := os.ReadFile(filepath.Join(dir, ExchangeFile))
	if err != nil {
		return nil, err
	}
	var x Exchange
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, fmt.Errorf("%s: %w", ExchangeFile, err)
	}
	if x.Kind != ExchangeKind || x.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: not an %s v1 file", ExchangeFile, ExchangeKind)
	}
	return &x, nil
}

// Export stitches a built world's tiles into one raster and writes the LibreTerrain exchange folder.
// The built world is only read.
func Export(worldDir, outDir string, opt ExportOptions) (*Exchange, error) {
	if isUnder(outDir, worldDir) || isUnder(worldDir, outDir) {
		return nil, fmt.Errorf("exchange folder must be separate from the built world")
	}
	m, err := world.LoadManifest(worldDir)
	if err != nil {
		return nil, err
	}
	if m.CRS.Kind == "tmerc" {
		return nil, fmt.Errorf("world %s is in a local transverse Mercator grid (schema 3); the LibreTerrain exchange records an EPSG code only and has not been checked with such worlds", m.Name)
	}
	g := m.Grid
	N := g.TileCells
	W, H := g.TilesX*N+1, g.TilesY*N+1
	z := make([]float32, W*H)
	nodata := make([]bool, W*H)
	for _, t := range m.TilesAt(0) {
		h, err := validate.ReadHeights(filepath.Join(worldDir, t.Height), m)
		if err != nil {
			return nil, err
		}
		cls, err := readGrayPNG(filepath.Join(worldDir, t.Depth), g.SamplesPerTile)
		if err != nil {
			return nil, err
		}
		for i := 0; i <= N; i++ {
			for j := 0; j <= N; j++ {
				gi := (t.Row*N+i)*W + t.Col*N + j
				z[gi] = float32(h[i*g.SamplesPerTile+j])
				nodata[gi] = cls[i*g.SamplesPerTile+j] == world.ClassNoData
			}
		}
	}
	zmin, zmax := math.Inf(1), math.Inf(-1)
	for i, v := range z {
		if !nodata[i] {
			zmin, zmax = math.Min(zmin, float64(v)), math.Max(zmax, float64(v))
		}
	}
	if math.IsInf(zmin, 1) {
		return nil, fmt.Errorf("world has no valid elevation samples")
	}
	var nd int
	for i := range z {
		if nodata[i] {
			z[i] = float32(zmin)
			nd++
		}
	}
	if opt.Headroom <= 0 {
		opt.Headroom = 0.25
	}
	lo, hi := math.Floor(zmin), math.Ceil(zmax+opt.Headroom*(zmax-zmin))
	side := max(W, H)
	if side > 8193 {
		return nil, fmt.Errorf("world is %d samples wide; LibreTerrain asks for confirmation above 8193 and an unattended build would silently not run", side)
	}
	sq := padSquare(z, W, H, side)

	x := &Exchange{SchemaVersion: 1, Kind: ExchangeKind, Tool: "ocean " + world.ToolVersion}
	x.Source.World, x.Source.BuildID = m.Name, m.BuildID
	mb, err := os.ReadFile(filepath.Join(worldDir, world.ManifestFile))
	if err != nil {
		return nil, err
	}
	ms := sha256.Sum256(mb)
	x.Source.ManifestSHA256 = hex.EncodeToString(ms[:])
	x.EPSG, x.WorldConfig = m.CRS.EPSG, m.Config
	x.Grid.CellM, x.Grid.Width, x.Grid.Height, x.Grid.Side = g.CellM, W, H, side
	x.Grid.PadCols, x.Grid.PadRows = side-W, side-H
	x.Grid.OriginE, x.Grid.OriginNT = g.OriginE, g.OriginN
	x.Grid.RowOrder, x.Grid.Registration = "north_to_south", "vertex (sample k at origin + k*cell_m)"
	ws := float64(side-1) * g.CellM
	x.LT.WorldSizeM, x.LT.HeightRangeM, x.LT.BuildResolution, x.LT.Mapping = [2]float64{ws, ws}, [2]float64{lo, hi}, side, Mapping
	x.Stats.MinM, x.Stats.MaxM, x.Stats.NoDataCells = zmin, zmax, nd
	for _, c := range m.Checkpoints {
		col, row := (c.Easting-g.OriginE)/g.CellM, (g.OriginN-c.Northing)/g.CellM
		ci, ri := int(math.Round(col)), int(math.Round(row))
		if math.Abs(col-float64(ci)) > 1e-6 || math.Abs(row-float64(ri)) > 1e-6 || ci < 0 || ri < 0 || ci >= W || ri >= H {
			return nil, fmt.Errorf("checkpoint %s is not on a grid vertex inside the world", c.Name)
		}
		// Self-check of the stitching and row order against the manifest's independent values.
		if d := math.Abs(float64(z[ri*W+ci]) - c.ElevationM); d > 0.01 {
			return nil, fmt.Errorf("checkpoint %s: stitched raster has %.3f m, manifest says %.3f m (orientation or stitching bug)", c.Name, z[ri*W+ci], c.ElevationM)
		}
		x.Checkpoints = append(x.Checkpoints, Checkpoint{c.Name, c.Easting, c.Northing, ci, ri, float64(z[ri*W+ci])})
	}
	x.Notes = []string{
		"heightmap.exr is float32 metres relative to the world's own vertical reference (not a surveyed chart datum); it holds land and sea floor together.",
		"Edit land only: `ocean libreterrain import` applies changes to cells that are land in the source build, ramped in from the shoreline; sea floor, coastline and nodata are kept from the original sources.",
		"Padding rows/cols (if any) repeat the last real row/column and are discarded on import.",
		"Do not change the project's world size, height range or resolution, or import is refused.",
		"NOT FOR NAVIGATION: derived from GMRT/GEBCO game-development bathymetry; see the source world's ATTRIBUTION.txt for licences.",
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	if err := writeEXR(filepath.Join(outDir, HeightmapEXR), sq, side); err != nil {
		return nil, err
	}
	es, err := sha256File(filepath.Join(outDir, HeightmapEXR))
	if err != nil {
		return nil, err
	}
	x.Files.EXR, x.Files.EXRSHA256, x.Files.PNG = HeightmapEXR, es, HeightmapPNG
	if err := writePNG16(filepath.Join(outDir, HeightmapPNG), sq, side, lo, hi); err != nil {
		return nil, err
	}
	if opt.Bridge != "" {
		name := m.Name + ".lterrain"
		if err := writeProject(opt.Bridge, filepath.Join(outDir, name), x); err != nil {
			return nil, fmt.Errorf("writing LibreTerrain project: %w", err)
		}
		x.Files.Project = name
	}
	jb, err := json.MarshalIndent(x, "", "  ")
	if err != nil {
		return nil, err
	}
	return x, os.WriteFile(filepath.Join(outDir, ExchangeFile), append(jb, '\n'), 0o644)
}

// writeProject creates a native .lterrain by letting LibreTerrain itself save it: a File node
// reading heightmap.exr in metres, marked for EXR32 and PNG16 export into ./lt-output.
func writeProject(bridge, path string, x *Exchange) error {
	tmp, err := os.MkdirTemp("", "ocean-lt-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	seed := map[string]any{"format_version": 1, "app_version": "0",
		"world": map[string]any{"size_m": x.LT.WorldSizeM, "height_range_m": x.LT.HeightRangeM, "seed": 0},
		"nodes": []any{}, "links": []any{}, "next_node_id": 1, "frames": []any{}, "custom_nodes": []any{}, "exports": []any{},
		"build": map[string]any{"resolution": x.LT.BuildResolution, "folder": "lt-output"}, "ui": map[string]any{}}
	sb, _ := json.Marshal(seed)
	seedPath := filepath.Join(tmp, "seed.lterrain")
	if err := os.WriteFile(seedPath, sb, 0o644); err != nil {
		return err
	}
	c, err := Open(bridge, seedPath)
	if err != nil {
		return err
	}
	defer c.Close()
	refs, err := c.Edit(map[string]any{"op": "add_node", "type": "primitive.file", "ref": "src",
		"settings": map[string]any{"path": HeightmapEXR, "values": "metres"}})
	if err != nil {
		return err
	}
	if err := c.MarkExport(refs["src"], "exr32", "png16"); err != nil {
		return err
	}
	return c.Save(path)
}

// padSquare copies z into the top-left of a side x side raster and fills the rest by repeating the
// last real column and row, so filters and erosion near the edge are not pulled toward an artificial cliff.
func padSquare(z []float32, W, H, side int) []float32 {
	out := make([]float32, side*side)
	for r := 0; r < side; r++ {
		sr := min(r, H-1)
		for c := 0; c < side; c++ {
			out[r*side+c] = z[sr*W+min(c, W-1)]
		}
	}
	return out
}

func readGrayPNG(path string, size int) ([]uint8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	g, ok := img.(*image.Gray)
	if !ok || g.Bounds().Dx() != size || g.Bounds().Dy() != size {
		return nil, fmt.Errorf("%s: want %dx%d 8-bit gray", path, size, size)
	}
	return g.Pix, nil
}

// writeEXR writes a float32 raster (row 0 first) as a single-channel EXR through GDAL.
func writeEXR(path string, data []float32, side int) error {
	tmp, err := os.MkdirTemp("", "ocean-exr-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	buf := make([]byte, len(data)*4)
	for i, v := range data {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	bin := filepath.Join(tmp, "in.bin")
	hdr := fmt.Sprintf("ENVI\nsamples = %d\nlines = %d\nbands = 1\nheader offset = 0\nfile type = ENVI Standard\ndata type = 4\ninterleave = bsq\nbyte order = 0\n", side, side)
	if err := os.WriteFile(bin, buf, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "in.hdr"), []byte(hdr), 0o644); err != nil {
		return err
	}
	_, err = gdal.Run("gdal_translate", "-q", "-of", "EXR", "-ot", "Float32", bin, path)
	return err
}

// readRaster reads any GDAL raster band 1 as float32, returning width and height.
func readRaster(path string) ([]float32, int, int, error) {
	out, err := gdal.Run("gdalinfo", "-json", path)
	if err != nil {
		return nil, 0, 0, err
	}
	var info struct {
		Size []int `json:"size"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil || len(info.Size) != 2 {
		return nil, 0, 0, fmt.Errorf("%s: cannot read raster size", path)
	}
	tmp, err := os.MkdirTemp("", "ocean-rd-")
	if err != nil {
		return nil, 0, 0, err
	}
	defer os.RemoveAll(tmp)
	bin := filepath.Join(tmp, "o.bin")
	if _, err := gdal.Run("gdal_translate", "-q", "-of", "ENVI", "-ot", "Float32", "-b", "1", path, bin); err != nil {
		return nil, 0, 0, err
	}
	r, err := gdal.ReadF32(bin, info.Size[0], info.Size[1])
	if err != nil {
		return nil, 0, 0, err
	}
	return r.Data, info.Size[0], info.Size[1], nil
}

// writePNG16 quantises metres into 0..65535 over [lo,hi]; values outside are clipped.
func writePNG16(path string, data []float32, side int, lo, hi float64) error {
	img := image.NewGray16(image.Rect(0, 0, side, side))
	for i, v := range data {
		u := math.Round((float64(v) - lo) / (hi - lo) * 65535)
		u = math.Max(0, math.Min(65535, u))
		binary.BigEndian.PutUint16(img.Pix[i*2:], uint16(u))
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// readPNG16 dequantises a 16-bit PNG over [lo,hi] (LibreTerrain: 0..65535 = height_range_m min..max).
func readPNG16(path string, lo, hi float64) ([]float32, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%s: %w", path, err)
	}
	g, ok := img.(*image.Gray16)
	if !ok {
		return nil, 0, 0, fmt.Errorf("%s: not a 16-bit grayscale PNG (got %T)", path, img)
	}
	w, h := g.Bounds().Dx(), g.Bounds().Dy()
	out := make([]float32, w*h)
	for i := range out {
		out[i] = float32(lo + float64(binary.BigEndian.Uint16(g.Pix[i*2:]))/65535*(hi-lo))
	}
	return out, w, h, nil
}

func sha256File(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

func isUnder(child, parent string) bool {
	a, _ := filepath.Abs(child)
	b, _ := filepath.Abs(parent)
	rel, err := filepath.Rel(b, a)
	return err == nil && !strings.HasPrefix(rel, "..")
}
