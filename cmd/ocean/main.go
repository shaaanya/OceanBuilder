// Command ocean compiles geographic and nautical data into game-ready
// maritime worlds. See docs/architecture/overview.md.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/internal/world"
)

const usage = `ocean — build game-ready maritime worlds from open geographic data

usage: ocean <command> [flags]      (ocean help <command> for details)

make a world
  world init <name>       write an example world.json for a built-in location (falmouth, mindelo)
  world check <file>      validate a world.json and estimate size and projection distortion, no download
  build <world.json>      fetch, normalise, tile (with LOD levels) and write a world
inspect it
  info <dir>              region, grid, levels, quality, sources and licences of a built world
  lod-report <dir>        measure each LOD level against level 0 (rms, p95, max, too-deep, land/water flips)
  validate <dir>          check a built world against its manifest and invariants (exit 1 on errors)
  sample <dir>            elevation / depth / source at a point or along a line, without GDAL
  preview <dir>           write preview.png and preview.html
  diff <dirA> <dirB>      what changed between two builds (config, sources, tiles)
use it
  export <dir>            geotiff (GIS and generic use) or unity (engine adapter)
  libreterrain ...        refine land in LibreTerrain (optional external tool) and bring it back
housekeeping
  sources list|explain <id>  supported data sources and licences; datum, resolution, checks per source
  cache stats|clean       inspect or clear the download and stage cache
  version

Exit codes: 0 ok, 1 error or failed validation, 2 usage. Ctrl-C cancels cleanly; a cancelled or failed
build leaves any existing output untouched.
`

var helpText = map[string]string{
	"build": `Build a world from a config file. Sources are downloaded into the cache (.ocean-cache, or $OCEAN_CACHE),
hashed, and recorded in the manifest with URL, retrieval time, licence and attribution. Warped rasters are cached,
so changing depth bands, conforming, LOD or seamarks does not repeat gdalwarp. If the existing output was made from
identical inputs the build is skipped (use -force). Output is assembled aside and swapped in when complete.`,
	"info":       "Summarise a built world from its manifest: region, grid, LOD levels, share of survey-measured water, cells by source, sources and licences.",
	"sample":     "Read elevation, depth, depth class and the source of the nearest vertex from the built tiles. Pure Go, no GDAL needed. Elevation is bilinear between four vertices and reported invalid if any of them has no data.",
	"diff":       "Compare two built worlds from their manifests: config differences, changed source hashes, and which tiles were added, removed or changed (by file hash).",
	"export":     "Export a built world. geotiff: height.tif, depth_class.tif and source.tif for one LOD level (-level), pixel centres on the world's vertices. unity: copy level 0 and install the Unity package into a project (URP).",
	"lod-report": "Compare every LOD level with level 0 at level-0 vertices, sampling the level surface bilinearly as a consumer would: rms, p95 and max difference, the largest amount a level is deeper than the truth (unsafe direction), land/water flips and uncovered vertices. -stride n samples every n-th vertex; -json for machines.",
	"validate":   "Recompute every metric from the tile files and compare with the manifest: hashes, layer consistency, shared tile edges per level, parent/child samples across levels, provenance, licences.",
	"world":      "world init <preset> writes an example config. world check <file> validates a config and prints the grid, memory estimate and projection scale without downloading anything.",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	gdal.Ctx = ctx
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "build":
		err = cmdBuild(args)
	case "validate":
		err = cmdValidate(args)
	case "lod-report":
		err = cmdLODReport(args)
	case "preview":
		err = cmdPreview(args)
	case "export":
		err = cmdExport(args)
	case "info":
		err = cmdInfo(args)
	case "sample":
		err = cmdSample(args)
	case "diff":
		err = cmdDiff(args)
	case "libreterrain":
		err = cmdLibreTerrain(args)
	case "sources":
		err = cmdSources(args)
	case "world":
		err = cmdWorld(args)
	case "cache":
		err = cmdCache(args)
	case "version":
		fmt.Printf("ocean %s (world format schema %d)\n", world.ToolVersion, world.SchemaVersion)
	case "-h", "--help", "help":
		if len(args) > 0 {
			if h, ok := helpText[args[0]]; ok {
				fmt.Printf("ocean %s\n\n%s\n\nrun ocean %s -h for its flags\n", args[0], h, args[0])
				return
			}
		}
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "cancelled")
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func defaultCache() string {
	if d := os.Getenv("OCEAN_CACHE"); d != "" {
		return d
	}
	return ".ocean-cache"
}

func cmdBuild(args []string) error {
	fs := newFlags("build", "build <world.json> [-out dir] [-cache dir] [-force] [-q] [-log-json] [-mem MB] [-json] [-lock file|off] [-update-lock]")
	out := fs.String("out", "", "output directory (default output/<name>)")
	cache := fs.String("cache", defaultCache(), "download and stage cache directory")
	asJSON := fs.Bool("json", false, "print the build summary as JSON on stdout")
	lock := fs.String("lock", "", "source lock file (default <world>.lock.json beside the config; \"off\" disables)")
	updLock := fs.Bool("update-lock", false, "write the lock from this build's sources instead of checking it")
	force := fs.Bool("force", false, "rebuild even if the output was made from identical inputs")
	mem := fs.Int("mem", 0, "memory budget in MB for windowed processing and in-memory steps (default 1024); never changes the output")
	quiet := fs.Bool("q", false, "no progress output")
	logJSON := fs.Bool("log-json", false, "progress as JSON lines on stderr (time, msg)")
	if err := parse(fs, args); err != nil || fs.NArg() != 1 {
		return fmt.Errorf("usage: ocean build <world.json> [flags] (ocean build -h)")
	}
	cfg, err := world.LoadConfig(fs.Arg(0))
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join("output", cfg.Name)
	}
	if err := os.MkdirAll(*cache, 0o755); err != nil {
		return err
	}
	if *lock == "" {
		*lock = strings.TrimSuffix(fs.Arg(0), ".json") + ".lock.json"
	} else if *lock == "off" {
		*lock = ""
	}
	start := time.Now()
	logf := func(f string, a ...any) {
		switch {
		case *quiet:
		case *logJSON:
			json.NewEncoder(os.Stderr).Encode(map[string]string{"time": time.Now().UTC().Format(time.RFC3339), "elapsed": time.Since(start).Round(time.Millisecond).String(), "msg": fmt.Sprintf(f, a...)})
		default:
			fmt.Fprintf(os.Stderr, "[%5.1fs] "+f+"\n", append([]any{time.Since(start).Seconds()}, a...)...)
		}
	}
	m, err := world.Build(cfg, world.Options{CacheDir: *cache, OutDir: *out, LockPath: *lock, UpdateLock: *updLock, Force: *force, MemoryMB: *mem, Log: logf})
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"out": *out, "build_id": m.BuildID, "input_key": m.InputKey, "tiles": len(m.Tiles),
			"levels": len(m.LOD.Levels), "seconds": time.Since(start).Seconds(), "stats": m.Stats})
	}
	fmt.Printf("built %s: %d tiles in %d levels (level 0: %dx%d tiles of %.0f m cells), build %s, %.1f s\n  %.1f%% water, %.1f%% of it survey-measured; next: ocean validate %s\n  -> %s\n",
		m.Name, len(m.Tiles), len(m.LOD.Levels), m.Grid.TilesX, m.Grid.TilesY, m.Grid.CellM, m.BuildID[:12], time.Since(start).Seconds(),
		100*m.Stats.WaterFraction, 100*m.Stats.MeasuredWater, *out, *out)
	return nil
}

// reorder lets flags appear after positional args (ocean build world.json -out x).
func reorder(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			if !hasEq(a) && i+1 < len(args) && !isBool(a) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}

func hasEq(s string) bool {
	for _, c := range s {
		if c == '=' {
			return true
		}
	}
	return false
}

func isBool(s string) bool {
	switch strings.TrimLeft(s, "-") {
	case "json", "update-lock", "force", "q", "log-json", "stages":
		return true
	}
	return false
}
