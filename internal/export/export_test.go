package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaaanya/OceanBuilder/internal/world"
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
	dst, err := Unity(wdir, proj, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy importer script must be removed: it clashes with the package class")
	}
	pkg := filepath.Join(proj, "Packages", UnityPackageName)
	for _, f := range []string{"package.json", "Runtime/OceanWorldIndex.cs", "Runtime/OceanBuilder.World.asmdef", "Editor/OceanWorldImporter.cs",
		"Editor/OceanBuilder.World.Editor.asmdef", "EditorAddressables/OceanAddressables.cs"} {
		if _, err := os.Stat(filepath.Join(pkg, f)); err != nil {
			t.Errorf("package file %s missing: %v", f, err)
		}
	}
	// The Addressables module must not compile (or reference missing assemblies) without the package.
	var asm struct {
		DefineConstraints []string `json:"defineConstraints"`
	}
	b, _ := os.ReadFile(filepath.Join(pkg, "EditorAddressables", "OceanBuilder.World.Editor.Addressables.asmdef"))
	if err := json.Unmarshal(b, &asm); err != nil || len(asm.DefineConstraints) != 1 || asm.DefineConstraints[0] != "OCEAN_ADDRESSABLES" {
		t.Errorf("addressables asmdef must be gated by OCEAN_ADDRESSABLES: %v %s", err, b)
	}
	for _, tl := range m.TilesAt(0) {
		if _, err := os.Stat(filepath.Join(dst, tl.Height)); err != nil {
			t.Errorf("tile %s not copied", tl.ID)
		}
	}
}

func TestUnityCopiesSeamarksAndDropsStaleOnes(t *testing.T) {
	fx := "../../testdata/falmouth/"
	cfg := &world.Config{Name: "fixture", BBox: [4]float64{-5.12, 50.155, -5.085, 50.185}, CellM: 100, TileCells: 16, Resampling: "average",
		Local: &world.Local{Topo: fx + "topo.tif", Mask: fx + "mask.tif", Coastline: fx + "coast.geojson", License: "test", Attribution: "test"}}
	wdir := t.TempDir()
	if _, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: wdir}); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, "Assets"), 0o755)
	stale := filepath.Join(proj, "Assets", "OceanWorld", "fixture", "seamarks.geojson")
	os.MkdirAll(filepath.Dir(stale), 0o755)
	os.WriteFile(stale, []byte("old"), 0o644)
	if _, err := Unity(wdir, proj, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a world built without seamarks must not leave an old seamarks.geojson in the project")
	}
	// With seamarks in the manifest the file is copied.
	b, _ := os.ReadFile(filepath.Join(wdir, world.ManifestFile))
	var m map[string]any
	json.Unmarshal(b, &m)
	m["files"].(map[string]any)["seamarks"] = "seamarks.geojson"
	b, _ = json.Marshal(m)
	os.WriteFile(filepath.Join(wdir, world.ManifestFile), b, 0o644)
	os.WriteFile(filepath.Join(wdir, "seamarks.geojson"), []byte(`{"type":"FeatureCollection","features":[]}`), 0o644)
	if _, err := Unity(wdir, proj, 0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(stale)
	if err != nil || string(got) != `{"type":"FeatureCollection","features":[]}` {
		t.Fatalf("seamarks.geojson not copied: %v %q", err, got)
	}
}

// A re-export keeps Unity's .meta files (script GUIDs stay stable) and drops files that left the package.
func TestUnityPackageReinstallKeepsMeta(t *testing.T) {
	pkg := filepath.Join(t.TempDir(), "Packages", UnityPackageName)
	if err := installPackage(pkg); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(pkg, "Runtime", "OceanWorldIndex.cs.meta")
	stale := filepath.Join(pkg, "Runtime", "Removed.cs")
	staleMeta := stale + ".meta"
	for f, c := range map[string]string{meta: "guid: abc", stale: "x", staleMeta: "guid: def", filepath.Join(pkg, "Runtime.meta"): "guid: folder"} {
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := installPackage(pkg); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(meta); string(b) != "guid: abc" {
		t.Errorf("meta of a kept file must survive, got %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(pkg, "Runtime.meta")); string(b) != "guid: folder" {
		t.Errorf("folder meta must survive, got %q", b)
	}
	for _, f := range []string{stale, staleMeta} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s belongs to no file of the package and must be removed", f)
		}
	}
}
