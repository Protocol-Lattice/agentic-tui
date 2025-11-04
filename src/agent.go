package src

import (
	"context"

	agent "github.com/Protocol-Lattice/go-agent"
	adk "github.com/Protocol-Lattice/go-agent/src/adk"
	"github.com/Protocol-Lattice/go-agent/src/adk/modules"
	adkmodules "github.com/Protocol-Lattice/go-agent/src/adk/modules"
	"github.com/Protocol-Lattice/go-agent/src/memory"
	"github.com/Protocol-Lattice/go-agent/src/models"
	"github.com/Protocol-Lattice/go-agent/src/tools"
)

func BuildAgent(ctx context.Context) (*agent.Agent, error) {
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
	)
	if err != nil {
		return nil, err
	}
	return builder.BuildAgent(ctx)
}
