package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/Homiakus/axiom"
)

func main() {
	path := filepath.Join("examples", "triz", "hydropilot_mini.axm")
	source, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	result, err := axiom.NormalizeTRIZ(source, axiom.WithSourceName(path))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("domain=%s diagnostics=%d sourceMap=%d\n", result.Module.Domain.Name, len(result.Diagnostics), len(result.SourceMap))
	fmt.Println("--- normalized AXM ---")
	fmt.Print(string(result.NormalizedSource))
}
