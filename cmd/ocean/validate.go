package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/shaaanya/OceanBuilder/internal/validate"
)

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	fs.Parse(reorder(args))
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: ocean validate <world dir> [-json]")
	}
	rep, err := validate.Run(fs.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(rep)
	} else {
		fmt.Print(rep.Format())
	}
	if rep.Result == "FAILED" {
		os.Exit(1)
	}
	return nil
}
