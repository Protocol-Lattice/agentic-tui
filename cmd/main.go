// @path main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	. "github.com/Protocol-Lattice/lattice-code/src"

	tea "github.com/charmbracelet/bubbletea"

	utcp "github.com/universal-tool-calling-protocol/go-utcp"

	agent "github.com/Protocol-Lattice/go-agent"
	adk "github.com/Protocol-Lattice/go-agent/src/adk"
	"github.com/Protocol-Lattice/go-agent/src/adk/modules"
	adkmodules "github.com/Protocol-Lattice/go-agent/src/adk/modules"
	"github.com/Protocol-Lattice/go-agent/src/memory"
	"github.com/Protocol-Lattice/go-agent/src/models"
	"github.com/Protocol-Lattice/go-agent/src/tools"
)

// -----------------------------------------------------------------------------
// AGENT & UTCP BUILDERS
// -----------------------------------------------------------------------------

func buildAgent(ctx context.Context) (*agent.Agent, error) {
	qdrantURL := flag.String("qdrant-url", "http://localhost:6333", "Qdrant base URL")
	qdrantCollection := flag.String("qdrant-collection", "raezil", "Qdrant collection name")

	memOpts := memory.DefaultOptions()
	builder, err := adk.New(
		ctx,
		adk.WithDefaultSystemPrompt(VibeSystemPrompt),
		adk.WithModules(
			modules.InQdrantMemory(100000, *qdrantURL, *qdrantCollection, memory.AutoEmbedder(), &memOpts),

			adkmodules.NewModelModule("gemini", func(_ context.Context) (models.Agent, error) {
				return models.NewGeminiLLM(ctx, "gemini-2.5-pro", "Universal code generator")
			}),
			adkmodules.NewToolModule("essentials",
				adkmodules.StaticToolProvider([]agent.Tool{&tools.EchoTool{}}, nil),
			),
		),
	)
	if err != nil {
		return nil, err
	}
	return builder.BuildAgent(ctx)
}

func buildUTCP(ctx context.Context) (utcp.UtcpClientInterface, error) {
	cfg := &utcp.UtcpClientConfig{ProvidersFilePath: "provider.json"}
	client, err := utcp.NewUTCPClient(ctx, cfg, nil, nil)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func main() {
	startDir, _ := os.Getwd()
	ctx := context.Background()
	var p *tea.Program

	fmt.Println("🚀 Initializing Lattice Code Agent + UTCP...")

	a, err := buildAgent(ctx)
	flag.Parse() // Parse flags for qdrant-url etc.
	if err != nil {
		fmt.Println("❌ Failed to build agent:", err)
		os.Exit(1)
	}

	u, err := buildUTCP(ctx)
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
