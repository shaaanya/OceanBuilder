package world_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ocean-compiler/ocean/internal/world"
)

// Regression oracle: the sha256 of every tile file of fixture builds, recorded from the 0.2.0 code. Refactors of
// the pipeline (source composition, windowing, new CRS handling) must keep these identical for old configs.
// Set UPDATE_GOLDEN=1 to rewrite testdata/golden/*.json after an intended output change (and say why in the log).
func goldenCases() map[string]*world.Config {
	plain := cfgFor(box)
	conf := cfgFor(box)
	conf.CoastConform, conf.CoastConformMaxM = "osm", 600
	lod := cfgFor(box)
	lod.LODLevels = 2
	return map[string]*world.Config{"plain": plain, "conform": conf, "lod2": lod}
}

func TestGoldenTileHashes(t *testing.T) {
	for name, cfg := range goldenCases() {
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			m, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: filepath.Join(out, "w")})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, tl := range m.Tiles {
				got[tl.ID+".height"], got[tl.ID+".depth"] = tl.SHA256.Height, tl.SHA256.Depth
				got[tl.ID+".shore"], got[tl.ID+".source"] = tl.SHA256.Shore, tl.SHA256.Source
			}
			path := filepath.Join("..", "..", "testdata", "golden", name+".json")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				b, _ := json.MarshalIndent(got, "", " ")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with UPDATE_GOLDEN=1 once)", err)
			}
			var want map[string]string
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}
			if len(want) != len(got) {
				t.Fatalf("%d files, golden has %d", len(got), len(want))
			}
			for k, w := range want {
				if got[k] != w {
					t.Errorf("%s: hash %s, golden %s", k, got[k], w)
				}
			}
		})
	}
}
