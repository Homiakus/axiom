package main

import (
	"fmt"
	"os"

	"axiom-control-panel/internal/tui"
)

// main - отдельная точка входа панели управления.
//
// Панель находится в собственном example-модуле, поэтому ее TUI-зависимости
// не попадают в корневую библиотеку axiom.
func main() {
	startDir := "."
	if len(os.Args) > 1 {
		startDir = os.Args[1]
	}
	if err := tui.Run(startDir); err != nil {
		fmt.Fprintf(os.Stderr, "control panel: %v\n", err)
		os.Exit(1)
	}
}
