package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ocean-compiler/ocean/internal/world"
)

func TestUnityInstallsPackageAndRemovesLegacyScript(t *testing.T) {
	fx := "../../testdata/falmouth/"
	cfg := &world.Config{Name: "fixture", BBox: [4]float64{-5.12, 50.155, -5.085, 50.185}, CellM: 100, TileCells: 16, Resampling: "average",
		Local: &world.Local{Topo: fx + "topo.tif", Mask: fx + "mask.tif", Coastline: fx + "coast.geojson", License: "test", Attribution: "test"}}
	wdir := t.TempDir()
	m, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: wdir})
	if err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	legacy := filepath.Join(proj, "Assets", "Editor", "OceanWorldImporter.cs")
	os.MkdirAll(filepath.Dir(legacy), 0o755)
	os.WriteFile(legacy, []byte("// old"), 0o644)
	dst, err := Unity(wdir, proj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy importer script must be removed: it clashes with the package class")
	}
	pkg := filepath.Join(proj, "Packages", UnityPackageName)
	for _, f := range []string{"package.json", "Runtime/OceanWorldIndex.cs", "Runtime/OceanCompiler.World.asmdef", "Editor/OceanWorldImporter.cs",
		"Editor/OceanCompiler.World.Editor.asmdef", "EditorAddressables/OceanAddressables.cs"} {
		if _, err := os.Stat(filepath.Join(pkg, f)); err != nil {
			t.Errorf("package file %s missing: %v", f, err)
		}
	}
	// The Addressables module must not compile (or reference missing assemblies) without the package.
	var asm struct {
		DefineConstraints []string `json:"defineConstraints"`
	}
	b, _ := os.ReadFile(filepath.Join(pkg, "EditorAddressables", "OceanCompiler.World.Editor.Addressables.asmdef"))
	if err := json.Unmarshal(b, &asm); err != nil || len(asm.DefineConstraints) != 1 || asm.DefineConstraints[0] != "OCEAN_ADDRESSABLES" {
		t.Errorf("addressables asmdef must be gated by OCEAN_ADDRESSABLES: %v %s", err, b)
	}
	for _, tl := range m.Tiles {
		if _, err := os.Stat(filepath.Join(dst, tl.Height)); err != nil {
			t.Errorf("tile %s not copied", tl.ID)
		}
	}
}
