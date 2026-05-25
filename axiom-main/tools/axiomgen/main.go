package main

import (
	"encoding/json"
	"fmt"
	"os"

	"axiom/tools/axiomgen/internal/generate"
	"axiom/tools/axiomgen/internal/tui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "axiomgen: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "--file" {
		req, err := parseGenerateArgs(args)
		if err != nil {
			return err
		}
		result, err := generate.Run(req)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	startDir := "."
	if len(args) > 0 {
		if len(args) > 1 {
			return fmt.Errorf("usage: axiomgen [start-dir] or axiomgen --file <file.axm> --out <dir> --package <name>")
		}
		startDir = args[0]
	}
	return tui.Run(startDir)
}

func parseGenerateArgs(args []string) (generate.Request, error) {
	var req generate.Request
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--file":
			i++
			if i >= len(args) {
				return req, fmt.Errorf("--file requires a value")
			}
			req.File = args[i]
		case "--out":
			i++
			if i >= len(args) {
				return req, fmt.Errorf("--out requires a value")
			}
			req.OutDir = args[i]
		case "--package":
			i++
			if i >= len(args) {
				return req, fmt.Errorf("--package requires a value")
			}
			req.PackageName = args[i]
		default:
			return req, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	if req.File == "" {
		return req, fmt.Errorf("--file is required")
	}
	return req, nil
}
