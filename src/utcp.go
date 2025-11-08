package src

import (
	"context"

	utcp "github.com/universal-tool-calling-protocol/go-utcp"
)

func BuildUTCP(ctx context.Context) (utcp.UtcpClientInterface, error) {
	cfg := &utcp.UtcpClientConfig{ProvidersFilePath: "~/utcp/provider.json"}
	client, err := utcp.NewUTCPClient(ctx, cfg, nil, nil)
	if err != nil {
		return nil, err
	}
	return client, nil
}
