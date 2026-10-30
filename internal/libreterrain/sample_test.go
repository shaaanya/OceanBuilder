package libreterrain

import (
	"strings"
	"testing"
)

// The sample is unmodified output of a real libreterrain-mcp run (see testdata/libreterrain/README.md).
func TestInspectRealLibreTerrainSample(t *testing.T) {
	dir := "../../testdata/libreterrain"
	lt, err := ReadBuild(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lt.Generator != "LibreTerrain" || lt.Resolution != [2]int{65, 65} || lt.CellSizeM != [2]float64{48, 24} {
		t.Fatalf("unexpected build.json parse: %+v", lt)
	}
	out, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "anisotropic") || !strings.Contains(out, "file-n_0001_out_65.exr") {
		t.Fatalf("inspect output missing expected lines:\n%s", out)
	}
}
