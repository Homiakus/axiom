package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Homiakus/axiom/cmd/axiomgen/internal/generate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "axiomgen: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	request, err := parseArgs(args)
	if err != nil {
		return err
	}
	result, err := generate.Run(request)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func parseArgs(args []string) (generate.Request, error) {
	var request generate.Request
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--file":
			index++
			if index >= len(args) {
				return request, fmt.Errorf("--file requires a value")
			}
			request.File = args[index]
		case "--out":
			index++
			if index >= len(args) {
				return request, fmt.Errorf("--out requires a value")
			}
			request.OutDir = args[index]
		case "--package":
			index++
			if index >= len(args) {
				return request, fmt.Errorf("--package requires a value")
			}
			request.PackageName = args[index]
		default:
			return request, fmt.Errorf("usage: axiomgen --file <workflow.axm> [--out <dir>] [--package <name>]")
		}
	}
	if request.File == "" {
		return request, fmt.Errorf("--file is required")
	}
	return request, nil
}
