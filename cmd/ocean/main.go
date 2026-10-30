// Command ocean compiles geographic and nautical data into game-ready
// maritime worlds. See docs/architecture/overview.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ocean-compiler/ocean/internal/world"
)

const usage = `ocean — maritime world compiler

usage: ocean <command> [flags]

commands:
  sources list            list supported data sources and licenses
  world init <name>       write an example world.json for a built-in location
  build <world.json>      fetch, normalise, tile and write a world (-out, -cache, -json)
  validate <dir>          validate a built world (-json); exit 1 on errors
  preview <dir>           write preview.html and preview.png into the world dir
  export <dir>            export for an engine (-target unity -dest <project dir>)
  cache stats|clean       inspect or clear the download cache
  version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "build":
		err = cmdBuild(args)
	case "validate":
		err = cmdValidate(args)
	case "preview":
		err = cmdPreview(args)
	case "export":
		err = cmdExport(args)
	case "sources":
		err = cmdSources(args)
	case "world":
		err = cmdWorld(args)
	case "cache":
		err = cmdCache(args)
	case "version":
		fmt.Println("ocean", world.ToolVersion)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
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
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("out", "", "output directory (default output/<name>)")
	cache := fs.String("cache", defaultCache(), "download cache directory")
	asJSON := fs.Bool("json", false, "print the manifest summary as JSON")
	fs.Parse(reorder(args))
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: ocean build <world.json> [-out dir] [-cache dir] [-json]")
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
	logf := func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
	m, err := world.Build(cfg, world.Options{CacheDir: *cache, OutDir: *out, Log: logf})
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"out": *out, "build_id": m.BuildID, "tiles": len(m.Tiles), "stats": m.Stats})
	}
	fmt.Printf("built %s: %d tiles (%dx%d), build %s\n  -> %s\n", m.Name, len(m.Tiles), m.Grid.TilesX, m.Grid.TilesY, m.BuildID[:12], *out)
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

func isBool(s string) bool { return s == "-json" || s == "--json" }
