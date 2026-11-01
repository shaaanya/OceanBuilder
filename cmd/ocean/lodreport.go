package main

import (
	"flag"
	"fmt"

	"github.com/ocean-compiler/ocean/internal/validate"
	"github.com/ocean-compiler/ocean/internal/world"
)

func cmdLODReport(args []string) error {
	fs := flag.NewFlagSet("lod-report", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	stride := fs.Int("stride", 1, "compare every n-th level-0 vertex in each direction")
	fs.Parse(reorder(args))
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: ocean lod-report <world dir> [-stride n] [-json]")
	}
	m, err := world.LoadManifest(fs.Arg(0))
	if err != nil {
		return err
	}
	r, err := validate.LODReport(fs.Arg(0), *stride)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSONOut(map[string]any{"method": m.LOD.Method, "levels": r})
	}
	fmt.Print(validate.FormatLOD(m.LOD.Method, r))
	return nil
}
