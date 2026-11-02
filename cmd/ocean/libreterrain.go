package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shaaanya/OceanBuilder/internal/libreterrain"
)

const ltUsage = `usage:
  ocean libreterrain export <world dir> [-out dir] [-headroom 0.25] [-lt path]   heightmap + metadata (+ .lterrain if LibreTerrain is found)
  ocean libreterrain run <exchange dir> [-lt path]                               headless LibreTerrain build of the exported project (unedited)
  ocean libreterrain inspect <libreterrain output dir>                           describe build.json and files
  ocean libreterrain import <exchange dir> <libreterrain output dir> -out dir [-file name] [-blend n]
`

func cmdLibreTerrain(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", ltUsage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "export":
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		out := fs.String("out", "", "exchange folder (default <world dir>-libreterrain)")
		head := fs.Float64("headroom", 0.25, "extra height above the highest point, as a fraction of the elevation range")
		lt := fs.String("lt", "", "path of libreterrain-mcp (default: $LIBRETERRAIN_MCP, ../LibreTerrain, PATH)")
		fs.Parse(reorder(rest))
		if fs.NArg() != 1 {
			return fmt.Errorf("%s", ltUsage)
		}
		if *out == "" {
			*out = filepath.Clean(fs.Arg(0)) + "-libreterrain"
		}
		bridge := libreterrain.FindBridge(*lt)
		x, err := libreterrain.Export(fs.Arg(0), *out, libreterrain.ExportOptions{Headroom: *head, Bridge: bridge})
		if err != nil {
			return err
		}
		fmt.Printf("exported %s (build %s) -> %s\n  raster %d x %d samples (+%d/%d padding), cell %g m, EPSG:%d\n  LibreTerrain world %g m, height range %g..%g m\n",
			x.Source.World, x.Source.BuildID[:12], *out, x.Grid.Width, x.Grid.Height, x.Grid.PadCols, x.Grid.PadRows, x.Grid.CellM, x.EPSG,
			x.LT.WorldSizeM[0], x.LT.HeightRangeM[0], x.LT.HeightRangeM[1])
		if x.Files.Project != "" {
			fmt.Printf("  project: %s (opens in LibreTerrain; exports to lt-output/)\n", filepath.Join(*out, x.Files.Project))
		} else {
			fmt.Println("  libreterrain-mcp not found: no .lterrain written. In LibreTerrain add a File node (values: metres) reading heightmap.exr,")
			fmt.Printf("  set world size %g m and height range %g..%g m, build resolution %d, and export exr32.\n", x.LT.WorldSizeM[0], x.LT.HeightRangeM[0], x.LT.HeightRangeM[1], x.LT.BuildResolution)
		}
		return nil
	case "run":
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		lt := fs.String("lt", "", "path of libreterrain-mcp")
		fs.Parse(reorder(rest))
		if fs.NArg() != 1 {
			return fmt.Errorf("%s", ltUsage)
		}
		x, err := libreterrain.LoadExchange(fs.Arg(0))
		if err != nil {
			return err
		}
		bridge := libreterrain.FindBridge(*lt)
		if bridge == "" || x.Files.Project == "" {
			return fmt.Errorf("needs libreterrain-mcp and a .lterrain project in the exchange folder (export with the bridge available)")
		}
		c, err := libreterrain.Open(bridge, filepath.Join(fs.Arg(0), x.Files.Project))
		if err != nil {
			return err
		}
		defer c.Close()
		files, err := c.Build(x.LT.BuildResolution, 10*time.Minute)
		if err != nil {
			return err
		}
		for _, f := range files {
			fmt.Println(f)
		}
		return nil
	case "inspect":
		if len(rest) != 1 {
			return fmt.Errorf("%s", ltUsage)
		}
		s, err := libreterrain.Inspect(rest[0])
		if err != nil {
			return err
		}
		fmt.Print(s)
		return nil
	case "import":
		fs := flag.NewFlagSet("import", flag.ExitOnError)
		out := fs.String("out", "", "new folder for the refinement (required)")
		file := fs.String("file", "", "build.json file to import when several heightfields exist")
		blend := fs.Int("blend", 0, "cells inland over which edits ramp in from the shoreline (default 4)")
		fs.Parse(reorder(rest))
		if fs.NArg() != 2 || *out == "" {
			return fmt.Errorf("%s", ltUsage)
		}
		rep, err := libreterrain.Import(fs.Arg(0), fs.Arg(1), *out, libreterrain.ImportOptions{File: *file, BlendCells: *blend})
		if err != nil {
			return err
		}
		fmt.Printf("imported %s (%s): orientation %s\n  %d of %d cells changed >1 mm; max |change| %.3f m, mean %.4f m, range %.2f..%.2f m\n",
			rep.LTFile, rep.LTFormat, rep.Orientation, rep.ChangedCells, rep.Cells, rep.MaxAbsM, rep.MeanAbsM, rep.MinChangeM, rep.MaxChangeM)
		for _, c := range rep.Checkpoints {
			fmt.Printf("  checkpoint %-8s exported %9.3f returned %9.3f diff %+.4f m\n", c.Name, c.ExportedM, c.ReturnedM, c.DiffM)
		}
		for _, w := range rep.Warnings {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		fmt.Printf("  next: ocean build %s\n", filepath.Join(*out, "*.world.json"))
		return nil
	}
	return fmt.Errorf("unknown libreterrain command %q\n%s", sub, ltUsage)
}
