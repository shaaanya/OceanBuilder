package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/ocean-compiler/ocean/internal/world"
	"github.com/ocean-compiler/ocean/pkg/oceanworld"
)

func newFlags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ocean %s\n\n%s\n\nflags:\n", usage, helpText[name])
		fs.PrintDefaults()
	}
	return fs
}

// parse handles -h (exit 0 after the help) and flags after positional arguments.
func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(reorder(args)); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		return err
	}
	return nil
}

func writeJSONOut(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// cmdInfo prints what a built world is: region, grid, levels, sources with licences, quality, and how it was built.
func cmdInfo(args []string) error {
	fs := newFlags("info", "info <world dir> [-json]")
	asJSON := fs.Bool("json", false, "print the manifest instead of a summary")
	if err := parse(fs, args); err != nil || fs.NArg() != 1 {
		return fmt.Errorf("usage: ocean info <world dir> [-json]")
	}
	m, err := world.LoadManifest(fs.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSONOut(m)
	}
	g := m.Grid
	fmt.Printf("%s  (build %s, schema %d, ocean %s, GDAL %s)\n", m.Name, short12(m.BuildID), m.SchemaVersion, m.Tool.Version, m.Tool.GDAL)
	fmt.Printf("  region    lon %.4f..%.4f, lat %.4f..%.4f  (%s)\n", g.BBoxWGS84[0], g.BBoxWGS84[2], g.BBoxWGS84[1], g.BBoxWGS84[3], m.CRS.Name)
	fmt.Printf("  grid      %d x %d tiles of %.0f m, %d cells each, %.0f m cells; %.1f x %.1f km\n", g.TilesX, g.TilesY, g.TileSpanM, g.TileCells, g.CellM, float64(g.TilesX)*g.TileSpanM/1000, float64(g.TilesY)*g.TileSpanM/1000)
	fmt.Printf("  heights   %s %s, %.1f .. %.1f m, nodata %g\n", m.Height.Type, m.Height.Unit, m.Height.MinM, m.Height.MaxM, m.Height.NoData)
	fmt.Printf("  levels    ")
	for _, l := range m.LOD.Levels {
		fmt.Printf("L%d: %d tiles @ %.0f m   ", l.Level, l.Tiles, l.CellM)
	}
	fmt.Printf("\n  quality   %.1f%% water; %.1f%% of water cells are survey measurements; nodata %.2f%%\n", 100*m.Stats.WaterFraction, 100*m.Stats.MeasuredWater, 100*m.Stats.NoDataFraction)
	var names []string
	for k := range m.Stats.BySource {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Printf("  cells by source (level 0):")
	for _, k := range names {
		fmt.Printf("  %s %.1f%%", k, 100*float64(m.Stats.BySource[k])/float64(m.Stats.Cells))
	}
	fmt.Printf("\n  resample  %s from ~%.0f m source cells to %.0f m", m.Resampling.Method, m.Resampling.SourceResM, m.Resampling.OutputCellM)
	if m.Resampling.FillResM > 0 {
		fmt.Printf(" (%.0f m background grid)", m.Resampling.FillResM)
	}
	if m.Resampling.Upsampled {
		fmt.Print("  -- UPSAMPLED: detail finer than the source is interpolation")
	}
	fmt.Println()
	if m.Stats.UTMScaleMax != 0 {
		fmt.Printf("  projection grid metre = %.4f..%.4f ground metres\n", 1/m.Stats.UTMScaleMax, 1/m.Stats.UTMScaleMin)
	}
	fmt.Printf("  vertical  %s  (%s)\n", m.Vertical.Datum, m.Vertical.Note)
	if len(m.Datasets) > 0 {
		fmt.Println("  datasets (precedence order; cells = level-0 samples in the output)")
		ds := append([]world.DatasetInfo(nil), m.Datasets...)
		sort.SliceStable(ds, func(i, j int) bool { return ds[i].Rank < ds[j].Rank })
		for _, d := range ds {
			fmt.Printf("    %2d %-14s %-9s %-13s cells %-9d datum %s", d.Rank, d.ID, d.Type, d.Role, m.Stats.CellsByDataset[d.ID], d.DeclaredDtm)
			if d.Vertical != nil && d.Vertical.Kind != "identity" {
				fmt.Printf(" -> %s (%s)", d.Vertical.To, d.Vertical.Kind)
			}
			if d.NativeResM > 0 {
				fmt.Printf("  ~%.0f m", d.NativeResM)
			}
			fmt.Println()
		}
	}
	fmt.Println("  sources")
	for _, s := range m.Sources {
		fmt.Printf("    %-28s %s  sha256 %s  %s\n      licence: %s\n", s.Source, s.RetrievedAt, short12(s.SHA256), trunc(s.URL, 70), s.License)
	}
	for _, n := range m.Stats.Notes {
		fmt.Println("  note:", n)
	}
	fmt.Println("  NOT FOR NAVIGATION. Attribution is in ATTRIBUTION.txt; keep it with any redistribution.")
	return nil
}

// cmdSample answers elevation/depth/source questions from the built tiles, without GDAL.
func cmdSample(args []string) error {
	fs := newFlags("sample", "sample <world dir> (-lonlat lon,lat | -utm e,n | -line lon,lat:lon,lat) [-steps n] [-level k] [-json]")
	ll := fs.String("lonlat", "", "WGS 84 longitude,latitude")
	utm := fs.String("utm", "", "easting,northing in the world's UTM zone")
	line := fs.String("line", "", "depth profile between two lon,lat points, e.g. -5.1,50.1:-5.0,50.15")
	steps := fs.Int("steps", 20, "intervals along -line")
	level := fs.Int("level", 0, "LOD level to sample")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := parse(fs, args); err != nil || fs.NArg() != 1 {
		return fmt.Errorf("usage: ocean sample <world dir> -lonlat lon,lat | -utm e,n | -line a:b")
	}
	w, err := oceanworld.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	var out []oceanworld.Sample
	var where [][2]float64
	switch {
	case *line != "":
		p := strings.Split(*line, ":")
		if len(p) != 2 {
			return fmt.Errorf("-line wants lon,lat:lon,lat")
		}
		a, err1 := pair(p[0])
		b, err2 := pair(p[1])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("-line: %v %v", err1, err2)
		}
		ae, an := w.ToUTM(a[0], a[1])
		be, bn := w.ToUTM(b[0], b[1])
		if out, err = w.Profile(ae, an, be, bn, *steps, *level); err != nil {
			return err
		}
		for i := range out {
			f := float64(i) / float64(*steps)
			where = append(where, [2]float64{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f})
		}
	case *ll != "":
		a, err := pair(*ll)
		if err != nil {
			return err
		}
		s, err := w.SampleLonLat(a[0], a[1], *level)
		if err != nil {
			return err
		}
		out, where = []oceanworld.Sample{s}, [][2]float64{a}
	case *utm != "":
		a, err := pair(*utm)
		if err != nil {
			return err
		}
		s, err := w.SampleUTM(a[0], a[1], *level)
		if err != nil {
			return err
		}
		lo, la := w.ToLonLat(a[0], a[1])
		out, where = []oceanworld.Sample{s}, [][2]float64{{lo, la}}
	default:
		return fmt.Errorf("give -lonlat, -utm or -line")
	}
	if *asJSON {
		type row struct {
			Lon, Lat float64
			oceanworld.Sample
		}
		var rows []row
		for i, s := range out {
			rows = append(rows, row{where[i][0], where[i][1], s})
		}
		return writeJSONOut(rows)
	}
	for i, s := range out {
		switch {
		case s.Tile == "":
			fmt.Printf("%.5f,%.5f  outside the world\n", where[i][0], where[i][1])
		case !s.Valid:
			fmt.Printf("%.5f,%.5f  tile %s  no data\n", where[i][0], where[i][1], s.Tile)
		default:
			kind := "land"
			if s.ElevationM < 0 {
				kind = fmt.Sprintf("water, depth %.1f m", s.DepthM)
			}
			fmt.Printf("%.5f,%.5f  %-24s elevation %+8.2f m  class %d  source %s%s%s  tile %s\n", where[i][0], where[i][1], kind, s.ElevationM, s.Class, s.Source, map[bool]string{true: " (measured)", false: ""}[s.Measured], map[bool]string{true: " dataset " + s.Dataset, false: ""}[s.Dataset != ""], s.Tile)
		}
	}
	return nil
}

func pair(s string) ([2]float64, error) {
	p := strings.Split(strings.TrimSpace(s), ",")
	if len(p) != 2 {
		return [2]float64{}, fmt.Errorf("%q: want two comma-separated numbers", s)
	}
	a, e1 := strconv.ParseFloat(p[0], 64)
	b, e2 := strconv.ParseFloat(p[1], 64)
	if e1 != nil || e2 != nil {
		return [2]float64{}, fmt.Errorf("%q: not numbers", s)
	}
	return [2]float64{a, b}, nil
}

// cmdDiff compares two built worlds by manifest.
func cmdDiff(args []string) error {
	fs := newFlags("diff", "diff <world dir A> <world dir B> [-json]")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := parse(fs, args); err != nil || fs.NArg() != 2 {
		return fmt.Errorf("usage: ocean diff <before dir> <after dir>")
	}
	a, err := world.LoadManifest(fs.Arg(0))
	if err != nil {
		return err
	}
	b, err := world.LoadManifest(fs.Arg(1))
	if err != nil {
		return err
	}
	d := world.Diff(a, b)
	if *asJSON {
		return writeJSONOut(d)
	}
	if d.SameBuild {
		fmt.Println("identical builds (same build id)")
	}
	for _, sec := range []struct {
		name string
		l    []string
	}{{"config", d.ConfigChanges}, {"tool", d.ToolChanges}, {"sources", d.SourceChanges}, {"tiles added", d.TilesAdded}, {"tiles removed", d.TilesRemoved}, {"tiles changed", d.TilesChanged}} {
		if len(sec.l) > 0 {
			fmt.Printf("%s (%d):\n", sec.name, len(sec.l))
			for _, x := range sec.l[:min(len(sec.l), 20)] {
				fmt.Println("  ", x)
			}
			if len(sec.l) > 20 {
				fmt.Printf("   ... %d more\n", len(sec.l)-20)
			}
		}
	}
	fmt.Printf("tiles unchanged: %d\n", d.TilesUnchanged)
	return nil
}

// checkWorld validates a config and reports what building it would involve, without downloading anything.
func checkWorld(path string, asJSON bool) error {
	cfg, err := world.LoadConfig(path)
	if err != nil {
		return err
	}
	p, err := world.PlanGrid(cfg)
	if err != nil {
		return err
	}
	cells := float64(p.W) * float64(p.H)
	est := map[string]any{
		"config": path, "name": cfg.Name, "utm": p.SRS, "tiles_x": p.Grid.TilesX, "tiles_y": p.Grid.TilesY,
		"samples": [2]int{p.W, p.H}, "cells": int(cells), "tile_span_m": p.Grid.TileSpanM,
		"memory_estimate_mib": cells * 24 / (1 << 20), "tile_data_estimate_mib": cells * 8 / (1 << 20) * 4 / 3,
		"scale_factor": [2]float64{p.ScaleMin, p.ScaleMax}, "lod_levels_config": cfg.LODLevels,
	}
	if asJSON {
		return writeJSONOut(est)
	}
	fmt.Printf("%s: valid\n  %s, %d x %d tiles of %.0f m (%d x %d samples, %.1f M cells)\n  working memory about %.0f MiB, tile data about %.0f MiB before PNG compression\n  UTM point scale %.5f..%.5f\n",
		cfg.Name, p.SRS, p.Grid.TilesX, p.Grid.TilesY, p.Grid.TileSpanM, p.W, p.H, cells/1e6, est["memory_estimate_mib"], est["tile_data_estimate_mib"], p.ScaleMin, p.ScaleMax)
	if p.Note != "" {
		fmt.Println("  WARNING:", p.Note)
	}
	if cells > 150e6 {
		fmt.Println("  WARNING: the whole grid is held in memory; consider a larger cell_m or a smaller bbox")
	}
	return nil
}

func short12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}
