package main

import (
	"fmt"
	"path/filepath"

	"github.com/shaaanya/OceanBuilder/internal/export"
	"github.com/shaaanya/OceanBuilder/internal/preview"
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
	fs := newFlags("export", "export <world dir> -target geotiff -dest <dir> [-level k] | -target unity -dest <Unity project>")
	target := fs.String("target", "geotiff", "geotiff | unity")
	dest := fs.String("dest", "", "geotiff: output directory; unity: Unity project directory")
	level := fs.Int("level", 0, "geotiff: LOD level; unity: highest LOD level to copy (import it with -oceanMaxLevel)")
	if err := parse(fs, args); err != nil || fs.NArg() != 1 || *dest == "" {
		return fmt.Errorf("usage: ocean export <world dir> -target geotiff|unity -dest <dir> (ocean export -h)")
	}
	switch *target {
	case "geotiff":
		files, err := export.GeoTIFF(fs.Arg(0), *dest, *level)
		if err != nil {
			return err
		}
		for _, f := range files {
			fmt.Println("wrote", f)
		}
		return nil
	case "unity":
		out, err := export.Unity(fs.Arg(0), *dest, *level)
		if err != nil {
			return err
		}
		fmt.Printf("exported to %s\nnext: Unity menu Ocean > Import World, or batchmode -executeMethod OceanWorldImporter.ImportFromCommandLine -oceanMaxLevel %d\n", out, *level)
		return nil
	}
	return fmt.Errorf("target %q not supported yet (geotiff, unity; see docs/roadmap.md)", *target)
}
