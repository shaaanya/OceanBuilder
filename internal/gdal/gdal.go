// Package gdal wraps the GDAL/OGR command-line tools. Shelling out keeps the
// build cgo-free; the GDAL version is recorded in every manifest.
package gdal

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Run executes a GDAL tool and returns stdout.
func Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Version returns the GDAL release string.
func Version() string {
	out, err := Run("gdalinfo", "--version")
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(out)
}

// UTMEPSG returns the WGS84/UTM zone EPSG code for a lon/lat.
func UTMEPSG(lon, lat float64) int {
	zone := int(math.Floor((lon+180)/6)) + 1
	if lat >= 0 {
		return 32600 + zone
	}
	return 32700 + zone
}

// Transform converts x,y points (traditional lon/lat or easting/northing
// order) between CRSs using gdaltransform.
func Transform(srcSRS, dstSRS string, pts [][2]float64) ([][2]float64, error) {
	var in strings.Builder
	for _, p := range pts {
		fmt.Fprintf(&in, "%.9f %.9f\n", p[0], p[1])
	}
	cmd := exec.Command("gdaltransform", "-s_srs", srcSRS, "-t_srs", dstSRS)
	cmd.Env = append(os.Environ(), "OSR_AXIS_MAPPING_STRATEGY=TRADITIONAL_GIS_ORDER")
	cmd.Stdin = strings.NewReader(in.String())
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gdaltransform: %w: %s", err, errb.String())
	}
	var res [][2]float64
	for _, ln := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			return nil, fmt.Errorf("gdaltransform: bad line %q", ln)
		}
		x, e1 := strconv.ParseFloat(f[0], 64)
		y, e2 := strconv.ParseFloat(f[1], 64)
		if e1 != nil || e2 != nil {
			return nil, fmt.Errorf("gdaltransform: bad line %q", ln)
		}
		res = append(res, [2]float64{x, y})
	}
	if len(res) != len(pts) {
		return nil, fmt.Errorf("gdaltransform: got %d points for %d", len(res), len(pts))
	}
	return res, nil
}

// Raster is a dense float32 grid; Data[row*W+col], row 0 is the north edge.
type Raster struct {
	W, H int
	Data []float32
}

// ReadF32 reads a headerless little-endian float32 raster (ENVI .bin).
func ReadF32(path string, w, h int) (*Raster, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) != w*h*4 {
		return nil, fmt.Errorf("%s: %d bytes, want %d", path, len(b), w*h*4)
	}
	r := &Raster{W: w, H: h, Data: make([]float32, w*h)}
	for i := range r.Data {
		r.Data[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return r, nil
}

// ReadU8 reads a headerless byte raster.
func ReadU8(path string, w, h int) ([]uint8, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) != w*h {
		return nil, fmt.Errorf("%s: %d bytes, want %d", path, len(b), w*h)
	}
	return b, nil
}
