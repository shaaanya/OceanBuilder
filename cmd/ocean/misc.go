package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sourceInfo struct {
	ID, Kind, Coverage, Access, License string
}

var sources = []sourceInfo{
	{"gmrt", "bathymetry+topography (merged, with survey mask)", "global; up to ~1 m where multibeam exists", "GridServer HTTP API, no key", "CC BY 4.0 for GMRT; contributed grids carry own credits"},
	{"osm-coastline", "coastline vectors (natural=coastline)", "global", "Overpass API (public mirrors)", "ODbL 1.0, © OpenStreetMap contributors"},
}

func cmdSources(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: ocean sources list")
	}
	for _, s := range sources {
		fmt.Printf("%-14s %s\n    coverage: %s\n    access:   %s\n    license:  %s\n", s.ID, s.Kind, s.Coverage, s.Access, s.License)
	}
	return nil
}

var presets = map[string][4]float64{
	"falmouth": {-5.20, 50.05, -4.95, 50.22},   // Falmouth, Carrick Roads, Cornwall UK
	"mindelo":  {-25.08, 16.82, -24.86, 16.95}, // Mindelo, São Vicente, Cabo Verde
}

func cmdWorld(args []string) error {
	if len(args) >= 2 && args[0] == "check" {
		fs := newFlags("world", "world check <world.json> [-json]")
		asJSON := fs.Bool("json", false, "machine-readable output")
		if err := parse(fs, args[1:]); err != nil || fs.NArg() != 1 {
			return fmt.Errorf("usage: ocean world check <world.json>")
		}
		return checkWorld(fs.Arg(0), *asJSON)
	}
	fs := flag.NewFlagSet("world init", flag.ExitOnError)
	out := fs.String("out", "", "output file (default <name>.world.json)")
	cell := fs.Float64("cell", 48, "grid spacing in metres")
	if len(args) < 2 || args[0] != "init" {
		return fmt.Errorf("usage: ocean world init <falmouth|mindelo> [-out file] [-cell metres] | ocean world check <world.json>")
	}
	fs.Parse(reorder(args[2:]))
	name := strings.ToLower(args[1])
	bb, ok := presets[name]
	if !ok {
		names := make([]string, 0, len(presets))
		for k := range presets {
			names = append(names, k)
		}
		sort.Strings(names)
		return fmt.Errorf("unknown preset %q (have: %s); edit the file to use any bbox", name, strings.Join(names, ", "))
	}
	if *out == "" {
		*out = name + ".world.json"
	}
	cfg := map[string]any{"name": name, "bbox": bb, "cell_m": *cell, "tile_cells": 128, "resampling": "bilinear", "coastline": "osm"}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s exists; refusing to overwrite", *out)
	}
	fmt.Println("wrote", *out)
	return os.WriteFile(*out, append(b, '\n'), 0o644)
}

func cmdCache(args []string) error {
	fs := flag.NewFlagSet("cache", flag.ExitOnError)
	dir := fs.String("cache", defaultCache(), "cache directory")
	stages := fs.Bool("stages", false, "clean: remove only the warped stage rasters, keep downloads")
	if len(args) < 1 {
		return fmt.Errorf("usage: ocean cache stats|clean [-stages] [-cache dir]")
	}
	fs.Parse(reorder(args[1:]))
	switch args[0] {
	case "stats":
		count := func(root string) (files int, size int64) {
			filepath.Walk(root, func(p string, i os.FileInfo, err error) error {
				if err == nil && !i.IsDir() && !strings.HasSuffix(p, ".json") {
					files++
					size += i.Size()
				}
				return nil
			})
			return
		}
		sf, ss := count(filepath.Join(*dir, "stages"))
		tf, ts := count(*dir)
		fmt.Printf("%s: %d downloaded source files, %.1f MiB; %d warped stage rasters, %.1f MiB (rebuildable)\n", *dir, tf-sf, float64(ts-ss)/(1<<20), sf, float64(ss)/(1<<20))
	case "clean":
		abs, err := filepath.Abs(*dir)
		if err != nil || abs == "/" || abs == os.Getenv("HOME") {
			return fmt.Errorf("refusing to clean %q", *dir)
		}
		if *stages {
			abs = filepath.Join(abs, "stages")
		}
		if err := os.RemoveAll(abs); err != nil {
			return err
		}
		fmt.Println("removed", abs)
	default:
		return fmt.Errorf("usage: ocean cache stats|clean")
	}
	return nil
}
