package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	lattice "github.com/Protocol-Lattice/lattice-code/src"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Define and parse flags.
	startDir := flag.String("dir", wd, "The starting directory for the application.")
	flag.Parse()

	ctx := context.Background()

	fmt.Println("🚀 Initializing Lattice Code Agent + UTCP...")

	a, err := lattice.BuildAgent(ctx)
	if err != nil {
		return fmt.Errorf("failed to build agent: %w", err)
	}

	u, err := lattice.BuildUTCP(ctx)
	if err != nil {
		// Log UTCP unavailability but continue, as the original code did.
		fmt.Println("⚠️ UTCP unavailable:", err)
	}

	m := lattice.NewModel(ctx, a, u, *startDir)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.Program = p // Give the model a reference to the program.

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("application exited with an error: %w", err)
	}

	return nil
}