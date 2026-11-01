package export

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ocean-compiler/ocean/internal/world"
	"github.com/ocean-compiler/ocean/pkg/oceanworld"
)

func fixtureWorld(t *testing.T) string {
	t.Helper()
	fx := "../../testdata/falmouth/"
	cfg := &world.Config{Name: "fixture", BBox: [4]float64{-5.12, 50.155, -5.085, 50.185}, CellM: 100, TileCells: 16, Resampling: "average",
		Local: &world.Local{Topo: fx + "topo.tif", Mask: fx + "mask.tif", Coastline: fx + "coast.geojson",
			License: "test fixture", Attribution: "GMRT; OSM"}}
	dir := t.TempDir()
	if _, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: dir}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// GDAL itself (not our reader) must find the tile value at a vertex's position, at every level.
func TestGeoTIFFAgreesWithTilesAtVertices(t *testing.T) {
	dir := fixtureWorld(t)
	w, err := oceanworld.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 1} {
		dest := filepath.Join(t.TempDir(), "out")
		files, err := GeoTIFF(dir, dest, level)
		if err != nil || len(files) != 3 {
			t.Fatalf("level %d: %v %v", level, files, err)
		}
		info, err := exec.Command("gdalinfo", files[0]).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"32630", "NoData Value=-9999", "Type=Float32"} {
			if !strings.Contains(string(info), want) {
				t.Errorf("gdalinfo lacks %q:\n%s", want, info)
			}
		}
		checked := 0
		for i := range w.M.Tiles {
			tl := &w.M.Tiles[i]
			if tl.Level != level || checked >= 12 {
				continue
			}
			d, _ := w.Load(tl)
			S := w.M.Grid.SamplesPerTile
			cell := w.M.Grid.CellM * float64(int(1)<<level)
			for _, ij := range [][2]int{{2, 3}, {7, 9}, {12, 4}} {
				v := d.Height[ij[0]*S+ij[1]]
				if float64(v) == w.M.Height.NoData {
					continue
				}
				e, n := tl.BoundsUTM[0]+float64(ij[1])*cell, tl.BoundsUTM[3]-float64(ij[0])*cell
				out, err := exec.Command("gdallocationinfo", "-valonly", "-geoloc", files[0], strconv.FormatFloat(e, 'f', 3, 64), strconv.FormatFloat(n, 'f', 3, 64)).Output()
				if err != nil {
					t.Fatal(err)
				}
				got, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
				if err != nil || math.Abs(got-float64(v)) > 1e-3 {
					t.Fatalf("level %d tile %s (%d,%d): GDAL %q, tile %v", level, tl.ID, ij[0], ij[1], out, v)
				}
				checked++
			}
		}
		if checked < 6 {
			t.Fatalf("level %d: only %d vertices compared", level, checked)
		}
		if _, err := os.Stat(filepath.Join(dest, "source.tif")); err != nil {
			t.Fatal(err)
		}
	}
}
