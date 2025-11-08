package src

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	utcp "github.com/universal-tool-calling-protocol/go-utcp"
)

// BuildUTCP initializes a UTCP client with a resolved provider.json path.
func BuildUTCP(ctx context.Context) (utcp.UtcpClientInterface, error) {
	// Expand home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve home directory: %w", err)
	}

	providerPath := filepath.Join(home, "utcp", "provider.json")

	// Check that the file exists
	if _, err := os.Stat(providerPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("UTCP unavailable: providers file missing at %s", providerPath)
	}

	cfg := &utcp.UtcpClientConfig{
		ProvidersFilePath: providerPath,
	}

	client, err := utcp.NewUTCPClient(ctx, cfg, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("UTCP unavailable: %w", err)
	}
	return client, nil
}
