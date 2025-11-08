package src

import (
	"context"
	"fmt"

	agent "github.com/Protocol-Lattice/go-agent"
	adk "github.com/Protocol-Lattice/go-agent/src/adk"
	"github.com/Protocol-Lattice/go-agent/src/adk/modules"
	adkmodules "github.com/Protocol-Lattice/go-agent/src/adk/modules"
	"github.com/Protocol-Lattice/go-agent/src/memory"
	"github.com/Protocol-Lattice/go-agent/src/models"
	"github.com/Protocol-Lattice/go-agent/src/tools"
)

func BuildAgent(ctx context.Context) (*agent.Agent, error) {
	utcp, err := BuildUTCP(ctx)
	if err != nil {
		fmt.Println("⚠️ UTCP unavailable:", err)
	}
	memOpts := memory.DefaultOptions()
	builder, err := adk.New(
		ctx,
		adk.WithDefaultSystemPrompt(VibeSystemPrompt),
		adk.WithModules(
			modules.InMemoryMemoryModule(10000, memory.AutoEmbedder(), &memOpts),
			adkmodules.NewModelModule("gemini", func(_ context.Context) (models.Agent, error) {
				return models.NewGeminiLLM(ctx, "gemini-2.5-pro", "Universal code generator")
			}),
			adkmodules.NewToolModule("essentials",
				adkmodules.StaticToolProvider([]agent.Tool{&tools.EchoTool{}}, nil),
			),
		),
		adk.WithUTCP(utcp),
	)
	if err != nil {
		return nil, err
	}
	return builder.BuildAgent(ctx)
}
