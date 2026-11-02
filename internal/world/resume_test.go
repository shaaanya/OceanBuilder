package world_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/shaaanya/OceanBuilder/internal/world"
)

// treeHash digests every file of a built world (path and bytes), so two outputs compare equal only if they are
// byte-identical. The output directory name is not part of it.
func treeHash(t *testing.T, dir string) string {
	t.Helper()
	var files []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	h := sha256.New()
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(b))
		h.Write(b)
	}
	if len(files) == 0 {
		t.Fatalf("%s is empty", dir)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func resumeCfg(t *testing.T) *world.Config {
	cfg, _ := twoSourceCfg(t, []string{"fine", "coarse"}, "")
	cfg.LODLevels = 1
	return cfg
}

func buildLogged(cfg *world.Config, cache, out string) (log []string, err error) {
	_, err = world.Build(cfg, world.Options{CacheDir: cache, OutDir: out, Force: true,
		Log: func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) }})
	return
}

func countLog(log []string, sub string) (n int) {
	for _, l := range log {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return
}

func leftovers(cache string) (tmp []string) {
	filepath.WalkDir(cache, func(p string, d fs.DirEntry, err error) error {
		if err == nil && (strings.Contains(d.Name(), ".tmp-") || strings.HasSuffix(d.Name(), ".seal.tmp")) {
			tmp = append(tmp, p)
		}
		return nil
	})
	return
}

// Interrupt the build at every point a stage can be cut (before it, after its data is written but before it is
// sealed, after it is committed), then build again on the same cache: the result is byte-identical to an
// uninterrupted build, finished stages are reused, and the interrupted one leaves nothing that looks complete.
func TestResumeAfterInterruptAtEveryStage(t *testing.T) {
	if testing.Short() {
		t.Skip("slow (seconds to tens of seconds): runs in the integration tier, i.e. without -short")
	}
	cfg := resumeCfg(t)
	var points []string
	world.StageHook = func(p string) error { points = append(points, p); return nil }
	t.Cleanup(func() { world.StageHook = nil })
	refOut := filepath.Join(t.TempDir(), "ref")
	if _, err := buildLogged(cfg, t.TempDir(), refOut); err != nil {
		t.Fatal(err)
	}
	want := treeHash(t, refOut)
	var stages int
	for _, p := range points {
		if strings.HasPrefix(p, "done:") {
			stages++
		}
	}
	if stages < 4 { // two warps (fine, coarse), compose, one LOD level
		t.Fatalf("only %d stages were reported: %v", stages, points)
	}
	for i, cut := range points {
		if strings.HasPrefix(cut, "done:") && !strings.Contains(cut, "compose") {
			continue // a cut right after a commit equals a cut right before the next stage, which is covered
		}
		t.Run(fmt.Sprintf("%02d-%s", i, cut), func(t *testing.T) {
			cache, out := t.TempDir(), filepath.Join(t.TempDir(), "w")
			doneBefore, seen := 0, 0
			errCut := errors.New("interrupted")
			world.StageHook = func(p string) error {
				if p == cut {
					if seen++; seen == 1 {
						return errCut
					}
				}
				if strings.HasPrefix(p, "done:") && seen == 0 {
					doneBefore++
				}
				return nil
			}
			if _, err := buildLogged(cfg, cache, out); !errors.Is(err, errCut) {
				t.Fatalf("expected the injected interruption, got %v", err)
			}
			if tmp := leftovers(cache); len(tmp) > 0 {
				t.Errorf("temporary artifacts left after the cut: %v", tmp)
			}
			world.StageHook = nil
			log, err := buildLogged(cfg, cache, out)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if got := treeHash(t, out); got != want {
				t.Fatalf("resumed output differs from an uninterrupted build")
			}
			reused := countLog(log, "stage cache hit") + countLog(log, "reusing verified artifact")
			if reused < doneBefore {
				t.Errorf("%d stages were finished before the cut but only %d were reused\n%s", doneBefore, reused, strings.Join(log, "\n"))
			}
			if countLog(log, "failed verification") != 0 {
				t.Errorf("a clean interruption must not look like corruption:\n%s", strings.Join(log, "\n"))
			}
		})
	}
}

// Output must not depend on where the cache or output live, on GOMAXPROCS, or on the memory budget; and a second
// build on a warm cache reuses every stage and still gives the same bytes.
func TestStageCacheDeterministic(t *testing.T) {
	cfg := resumeCfg(t)
	out1 := filepath.Join(t.TempDir(), "a")
	cache := t.TempDir()
	if _, err := buildLogged(cfg, cache, out1); err != nil {
		t.Fatal(err)
	}
	want := treeHash(t, out1)

	out2 := filepath.Join(t.TempDir(), "b")
	log, err := buildLogged(cfg, cache, out2)
	if err != nil {
		t.Fatal(err)
	}
	if got := treeHash(t, out2); got != want {
		t.Error("warm-cache build differs from the cold one")
	}
	if countLog(log, "reusing verified artifact") < 2 || countLog(log, "stage cache hit") < 2 {
		t.Errorf("warm build did not reuse its stages:\n%s", strings.Join(log, "\n"))
	}

	old := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(old) })
	out3 := filepath.Join(t.TempDir(), "c")
	if _, err := world.Build(cfg, world.Options{CacheDir: t.TempDir(), OutDir: out3, MemoryMB: 64, WindowTiles: 1}); err != nil {
		t.Fatal(err)
	}
	if got := treeHash(t, out3); got != want {
		t.Error("GOMAXPROCS=1 with a 64 MB budget and 1-tile windows on a new cache gives different bytes")
	}
}

// Every kind of cached artifact is verified before use: a flipped byte (same size), a truncation and a lost seal are
// each reported, the artifact is rebuilt, and the output is unchanged.
func TestCorruptStageCacheIsDetected(t *testing.T) {
	if testing.Short() {
		t.Skip("slow (seconds to tens of seconds): runs in the integration tier, i.e. without -short")
	}
	cfg := resumeCfg(t)
	cache := t.TempDir()
	ref := filepath.Join(t.TempDir(), "ref")
	if _, err := buildLogged(cfg, cache, ref); err != nil {
		t.Fatal(err)
	}
	want := treeHash(t, ref)
	var victims []string
	filepath.WalkDir(filepath.Join(cache, "stages"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".bin") || strings.HasSuffix(p, ".dem")) {
			victims = append(victims, p)
		}
		return nil
	})
	if len(victims) < 4 {
		t.Fatalf("expected warp, compose and LOD artifacts in the cache, found %v", victims)
	}
	damage := map[string]func(string){
		"flip": func(p string) {
			b, _ := os.ReadFile(p)
			b[len(b)/2] ^= 0x40
			os.WriteFile(p, b, 0o644)
		},
		"truncate": func(p string) {
			b, _ := os.ReadFile(p)
			os.WriteFile(p, b[:len(b)-4], 0o644)
		},
		"unseal": func(p string) {
			if os.Remove(p+".seal") != nil { // directory artifacts keep the seal in STAGE.json
				os.Remove(filepath.Join(filepath.Dir(p), "STAGE.json"))
			}
		},
	}
	// Every damage kind on a warp raster and on a directory artifact; a flip on every artifact.
	type hit struct{ dmg, victim string }
	var cases []hit
	for _, v := range victims {
		cases = append(cases, hit{"flip", v})
	}
	cases = append(cases, hit{"truncate", victims[0]}, hit{"truncate", victims[len(victims)-2]},
		hit{"unseal", victims[0]}, hit{"unseal", victims[len(victims)-1]})
	for _, c := range cases {
		t.Run(c.dmg+"-"+filepath.Base(filepath.Dir(c.victim))+"-"+filepath.Base(c.victim), func(t *testing.T) {
			damage[c.dmg](c.victim)
			out := filepath.Join(t.TempDir(), "w")
			log, err := buildLogged(cfg, cache, out)
			if err != nil {
				t.Fatalf("build on damaged cache: %v", err)
			}
			if countLog(log, "failed verification")+countLog(log, "want ") == 0 {
				t.Errorf("damage not reported:\n%s", strings.Join(log, "\n"))
			}
			if got := treeHash(t, out); got != want {
				t.Error("output changed after rebuilding a damaged artifact")
			}
		})
	}
}
