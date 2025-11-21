// path: cmd/agentic-tui/main.go
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

	fmt.Println("⚡ Initializing Lattice Agentic TUI...")
	fmt.Println("🤖 Loading autonomous code intelligence...")

	a, err := BuildAgent(ctx)
	flag.Parse()
	if err != nil {
		fmt.Println("❌ Failed to build agent:", err)
		os.Exit(1)
	}

	// Create agentic model instead of base model
	m := NewAgenticModel(ctx, a, startDir)

	// Add welcome activity
	m.AddActivity("system", "Agent initialized", "success", "Ready for autonomous operation")
	m.AddThought("Analyzing workspace structure...")

	p = tea.NewProgram(m, tea.WithAltScreen())
	m.Program = p

	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
