package oceanworld_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shaaanya/OceanBuilder/internal/world"
	"github.com/shaaanya/OceanBuilder/pkg/oceanworld"
)

// A synthetic island with an analytic height function gives ground truth that does not come from the pipeline:
// whatever CRS the source is in and whichever hemisphere the world lands in, sampling the built world at a
// longitude/latitude must return the function's value there.

const (
	islLon, islLat = 151.2, -33.9 // UTM zone 56 south
	degM           = 111320.0
)

// islandZ: a cone island 120 m high, 3 km radius, then a sea floor falling 1 m per 40 m. Linear pieces, so
// bilinear resampling is exact except at the apex and the shoreline kink.
func islandZ(lon, lat float64) float64 {
	dx := (lon - islLon) * degM * math.Cos(islLat*math.Pi/180)
	dy := (lat - islLat) * degM
	d := math.Hypot(dx, dy)
	if d < 3000 {
		return 120 * (1 - d/3000)
	}
	return -(d - 3000) / 40
}

// writeGeoTIFF samples islandZ on a regular lon/lat grid, optionally blanks a square hole, and writes a GeoTIFF.
func writeGeoTIFF(t *testing.T, path string, w, s, e, n float64, nx, ny int, hole [4]int) {
	t.Helper()
	raw := make([]byte, nx*ny*4)
	dx, dy := (e-w)/float64(nx), (n-s)/float64(ny)
	for r := 0; r < ny; r++ {
		for c := 0; c < nx; c++ {
			v := float32(islandZ(w+(float64(c)+0.5)*dx, n-(float64(r)+0.5)*dy))
			if c >= hole[0] && c < hole[1] && r >= hole[2] && r < hole[3] {
				v = -9999
			}
			binary.LittleEndian.PutUint32(raw[(r*nx+c)*4:], math.Float32bits(v))
		}
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "d.raw"), raw, 0o644)
	vrt := fmt.Sprintf(`<VRTDataset rasterXSize="%d" rasterYSize="%d"><SRS>EPSG:4326</SRS><GeoTransform>%.9f, %.12f, 0, %.9f, 0, %.12f</GeoTransform>
<VRTRasterBand dataType="Float32" band="1" subClass="VRTRawRasterBand"><NoDataValue>-9999</NoDataValue><SourceFilename relativeToVRT="1">d.raw</SourceFilename>
<ImageOffset>0</ImageOffset><PixelOffset>4</PixelOffset><LineOffset>%d</LineOffset><ByteOrder>LSB</ByteOrder></VRTRasterBand></VRTDataset>`, nx, ny, w, dx, n, -dy, nx*4)
	os.WriteFile(filepath.Join(dir, "d.vrt"), []byte(vrt), 0o644)
	if out, err := exec.Command("gdal_translate", "-q", "-of", "GTiff", filepath.Join(dir, "d.vrt"), path).CombinedOutput(); err != nil {
		t.Skipf("gdal_translate unavailable: %v %s", err, out)
	}
}

func buildIsland(t *testing.T, src string) (*oceanworld.World, *world.Manifest) {
	t.Helper()
	cfg := &world.Config{Name: "island", BBox: [4]float64{islLon - 0.08, islLat - 0.07, islLon + 0.08, islLat + 0.07}, CellM: 60, TileCells: 64,
		Coastline: "none", Local: &world.Local{Topo: src, License: "synthetic", Attribution: "test"}}
	dir := t.TempDir()
	m, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	w, err := oceanworld.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return w, m
}

func checkIsland(t *testing.T, w *oceanworld.World, m *world.Manifest, tol float64) {
	t.Helper()
	if m.CRS.EPSG != 32756 {
		t.Fatalf("EPSG %d, want 32756 (UTM 56 south)", m.CRS.EPSG)
	}
	n, bad := 0, 0
	worst := 0.0
	for i := 0; i < 25; i++ {
		for j := 0; j < 25; j++ {
			lon := islLon - 0.06 + 0.12*float64(i)/24
			lat := islLat - 0.05 + 0.10*float64(j)/24
			d := math.Hypot((lon-islLon)*degM*math.Cos(islLat*math.Pi/180), (lat-islLat)*degM)
			if d < 150 || math.Abs(d-3000) < 150 { // apex and shoreline kinks are not linear
				continue
			}
			s, err := w.SampleLonLat(lon, lat, 0)
			if err != nil || !s.Valid {
				t.Fatalf("(%.4f,%.4f): %+v %v", lon, lat, s, err)
			}
			e2 := math.Abs(s.ElevationM - islandZ(lon, lat))
			worst = math.Max(worst, e2)
			n++
			if e2 > tol {
				bad++
			}
			if (islandZ(lon, lat) >= 0) != (s.Class == 0) {
				t.Fatalf("(%.4f,%.4f): land/water class %d disagrees with z=%.1f", lon, lat, s.Class, islandZ(lon, lat))
			}
		}
	}
	if n < 400 || bad > 0 {
		t.Fatalf("%d of %d points off by more than %.1f m (worst %.2f m)", bad, n, tol, worst)
	}
	t.Logf("%d points, worst error %.2f m", n, worst)
}

func TestSyntheticIslandSouthernHemisphereWGS84Source(t *testing.T) {
	src := filepath.Join(t.TempDir(), "isl.tif")
	writeGeoTIFF(t, src, islLon-0.2, islLat-0.15, islLon+0.2, islLat+0.15, 800, 600, [4]int{})
	w, m := buildIsland(t, src)
	checkIsland(t, w, m, 1.0)
	// LOD level 2 keeps every 4th vertex: still within a few metres on this smooth surface.
	s, _ := w.SampleLonLat(islLon+0.03, islLat+0.02, 2)
	if !s.Valid || math.Abs(s.ElevationM-islandZ(islLon+0.03, islLat+0.02)) > 6 {
		t.Fatalf("level 2: %+v", s)
	}
}

// The same terrain delivered in Web Mercator must give the same world: the reprojection is the pipeline's job.
func TestSyntheticIslandWebMercatorSource(t *testing.T) {
	d := t.TempDir()
	wgs, merc := filepath.Join(d, "w.tif"), filepath.Join(d, "m.tif")
	writeGeoTIFF(t, wgs, islLon-0.2, islLat-0.15, islLon+0.2, islLat+0.15, 1600, 1200, [4]int{})
	if out, err := exec.Command("gdalwarp", "-q", "-t_srs", "EPSG:3857", "-r", "near", "-dstnodata", "-9999", wgs, merc).CombinedOutput(); err != nil {
		t.Skipf("gdalwarp: %v %s", err, out)
	}
	w, m := buildIsland(t, merc)
	checkIsland(t, w, m, 2.0)
}

// A hole in the source must come out as nodata (class 255, source 0), never filled with plausible values.
func TestSyntheticNoDataHoleIsKept(t *testing.T) {
	src := filepath.Join(t.TempDir(), "isl.tif")
	writeGeoTIFF(t, src, islLon-0.2, islLat-0.15, islLon+0.2, islLat+0.15, 800, 600, [4]int{500, 560, 250, 310})
	w, _ := buildIsland(t, src)
	// centre of source pixel (530, 280): lon = W + 530.5*dx, lat = N - 280.5*dy
	lon, lat := islLon-0.2+530.5*0.4/800, islLat+0.15-280.5*0.3/600
	s, err := w.SampleLonLat(lon, lat, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Valid || s.Class != 255 || s.Source != "nodata" {
		t.Fatalf("hole was not reported as nodata: %+v", s)
	}
	if s2, _ := w.SampleLonLat(islLon, islLat-0.02, 0); !s2.Valid {
		t.Fatalf("data next to the hole is lost: %+v", s2)
	}
}
