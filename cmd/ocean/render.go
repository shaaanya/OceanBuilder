package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/ocean-compiler/ocean/internal/export"
	"github.com/ocean-compiler/ocean/internal/preview"
)

func cmdPreview(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: ocean preview <world dir>")
	}
	if err := preview.Render(args[0]); err != nil {
		return err
	}
	fmt.Println("wrote", filepath.Join(args[0], "preview.png"), "and preview.html")
	return nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	target := fs.String("target", "unity", "engine target: unity")
	dest := fs.String("dest", "", "engine project directory")
	fs.Parse(reorder(args))
	if fs.NArg() != 1 || *dest == "" {
		return fmt.Errorf("usage: ocean export <world dir> -target unity -dest <Unity project dir>")
	}
	switch *target {
	case "unity":
		out, err := export.Unity(fs.Arg(0), *dest)
		if err != nil {
			return err
		}
		fmt.Printf("exported to %s\nnext: Unity menu Ocean > Import World, or batchmode -executeMethod OceanWorldImporter.ImportFromCommandLine\n", out)
		return nil
	}
	return fmt.Errorf("target %q not supported yet (unity only; see docs/roadmap.md)", *target)
}
