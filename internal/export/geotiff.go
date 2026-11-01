package export

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/pkg/oceanworld"
)

// GeoTIFF writes one LOD level of a world as three georeferenced rasters any GIS can open:
// height.tif (Float32 metres, nodata as in the manifest), depth_class.tif and source.tif (Byte codes of the
// format spec). Pixel centres sit on the world's vertices, so a pixel is the sample, not an area average.
func GeoTIFF(worldDir, destDir string, level int) ([]string, error) {
	w, err := oceanworld.Open(worldDir)
	if err != nil {
		return nil, err
	}
	if level < 0 || level >= w.Levels() {
		return nil, fmt.Errorf("level %d not in 0..%d", level, w.Levels()-1)
	}
	N, S := w.M.Grid.TileCells, w.M.Grid.SamplesPerTile
	x0, x1, y0, y1 := math.MaxInt, math.MinInt, math.MaxInt, math.MinInt
	var tiles []*oceanworld.Tile
	for i := range w.M.Tiles {
		if t := &w.M.Tiles[i]; t.Level == level {
			tiles = append(tiles, t)
			x0, x1, y0, y1 = min(x0, t.X), max(x1, t.X), min(y0, t.Y), max(y1, t.Y)
		}
	}
	if len(tiles) == 0 {
		return nil, fmt.Errorf("level %d has no tiles", level)
	}
	W, H := (x1-x0+1)*N+1, (y1-y0+1)*N+1
	nd := float32(w.M.Height.NoData)
	height := make([]float32, W*H)
	for i := range height {
		height[i] = nd
	}
	class, source := make([]uint8, W*H), make([]uint8, W*H)
	for i := range class {
		class[i] = 255
	}
	for _, t := range tiles {
		d, err := w.Load(t)
		if err != nil {
			return nil, err
		}
		r0, c0 := (y1-t.Y)*N, (t.X-x0)*N
		for i := 0; i < S; i++ {
			for j := 0; j < S; j++ {
				o, s := (r0+i)*W+c0+j, i*S+j
				height[o], class[o], source[o] = d.Height[s], d.Class[s], d.Source[s]
			}
		}
	}
	span := w.M.Grid.TileSpanM * float64(int(1)<<level)
	cell := w.M.Grid.CellM * float64(int(1)<<level)
	originE, originN := float64(x0)*span, float64(y1+1)*span
	gt := fmt.Sprintf("%.6f, %.6f, 0, %.6f, 0, %.6f", originE-cell/2, cell, originN+cell/2, -cell)

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(destDir, ".geotiff-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	hb := make([]byte, W*H*4)
	for i, v := range height {
		binary.LittleEndian.PutUint32(hb[i*4:], math.Float32bits(v))
	}
	layers := []struct {
		name, dtype string
		pix         int
		raw         []byte
		nodata      string
	}{
		{"height", "Float32", 4, hb, fmt.Sprint(w.M.Height.NoData)},
		{"depth_class", "Byte", 1, class, "255"},
		{"source", "Byte", 1, source, "0"},
	}
	meta := []string{"-mo", "BUILD_ID=" + w.M.BuildID, "-mo", fmt.Sprintf("LEVEL=%d", level),
		"-mo", "REGISTRATION=pixel centres are the world's vertices (point samples), not area averages",
		"-mo", "NOT_FOR_NAVIGATION=game-development dataset; see ATTRIBUTION",
		"-mo", "ATTRIBUTION=" + strings.Join(w.M.Attribution, " | ")}
	var out []string
	for _, l := range layers {
		raw := filepath.Join(tmp, l.name+".raw")
		if err := os.WriteFile(raw, l.raw, 0o644); err != nil {
			return nil, err
		}
		vrt := filepath.Join(tmp, l.name+".vrt")
		xml := fmt.Sprintf(`<VRTDataset rasterXSize="%d" rasterYSize="%d"><SRS>EPSG:%d</SRS><GeoTransform>%s</GeoTransform>
<VRTRasterBand dataType="%s" band="1" subClass="VRTRawRasterBand"><NoDataValue>%s</NoDataValue>
<SourceFilename relativeToVRT="1">%s.raw</SourceFilename><ImageOffset>0</ImageOffset><PixelOffset>%d</PixelOffset><LineOffset>%d</LineOffset><ByteOrder>LSB</ByteOrder></VRTRasterBand></VRTDataset>`,
			W, H, w.M.CRS.EPSG, gt, l.dtype, l.nodata, l.name, l.pix, l.pix*W)
		if err := os.WriteFile(vrt, []byte(xml), 0o644); err != nil {
			return nil, err
		}
		dst := filepath.Join(destDir, l.name+".tif")
		args := append([]string{"-q", "-of", "GTiff", "-co", "COMPRESS=DEFLATE", "-co", "TILED=YES", "-co", "BIGTIFF=IF_SAFER"}, meta...)
		if _, err := gdal.Run("gdal_translate", append(args, vrt, dst)...); err != nil {
			return nil, err
		}
		out = append(out, dst)
	}
	return out, nil
}
