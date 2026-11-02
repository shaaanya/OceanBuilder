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
	var th validate.LODThresholds
	fs.Float64Var(&th.MaxRMSM, "max-rms", 0, "fail if any level's rms error exceeds this many metres (0 = not checked)")
	fs.Float64Var(&th.MaxTooDeepM, "max-too-deep", 0, "fail if a level is deeper than level 0 in water by more than this many metres")
	fs.Float64Var(&th.MaxShallowRMSM, "max-shallow-rms", 0, "fail if the rms error in water shallower than 20 m exceeds this many metres")
	fs.Float64Var(&th.MaxFlipFraction, "max-flip-fraction", 0, "fail if more than this fraction of vertices change side of the waterline")
	fs.Float64Var(&th.MaxClassDisagree, "max-class-disagree", 0, "fail if more than this fraction of vertices change depth class")
	fs.IntVar(&th.MaxLevel, "check-up-to", 0, "check thresholds only for levels up to this one (0 = all)")
	fs.Parse(reorder(args))
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: ocean lod-report <world dir> [-stride n] [-json] [-max-rms m] [-max-too-deep m] [-max-shallow-rms m] [-max-flip-fraction f] [-max-class-disagree f] [-check-up-to level]")
	}
	m, err := world.LoadManifest(fs.Arg(0))
	if err != nil {
		return err
	}
	r, err := validate.LODReport(fs.Arg(0), *stride)
	if err != nil {
		return err
	}
	broken := validate.LODCheck(r, th)
	if *asJSON {
		if err := writeJSONOut(map[string]any{"method": m.LOD.Method, "levels": r, "thresholds": th, "exceeded": broken}); err != nil {
			return err
		}
	} else {
		fmt.Print(validate.FormatLOD(m.LOD.Method, r))
		for _, b := range broken {
			fmt.Println("EXCEEDED", b)
		}
	}
	if len(broken) > 0 {
		return fmt.Errorf("%d LOD threshold(s) exceeded: the pyramid is outside the limits you set (see the lines above; lod_method \"shoal\" trades accuracy for a conservative shoal bias)", len(broken))
	}
	return nil
}
