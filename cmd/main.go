// @path main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	. "github.com/Protocol-Lattice/lattice-code/src"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	startDir, _ := os.Getwd()
	ctx := context.Background()
	var p *tea.Program

	fmt.Println("🚀 Initializing Lattice Code Agent + UTCP...")

	a, err := BuildAgent(ctx)
	flag.Parse() // Parse flags for qdrant-url etc.
	if err != nil {
		fmt.Println("❌ Failed to build agent:", err)
		os.Exit(1)
	}

	u, err := BuildUTCP(ctx)
	if err != nil {
		fmt.Println("⚠️ UTCP unavailable:", err)
	}

	m := NewModel(ctx, a, u, startDir)
	p = tea.NewProgram(m, tea.WithAltScreen())
	m.Program = p // Give the model a reference to the program.
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
	}
}
