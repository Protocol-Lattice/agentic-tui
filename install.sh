#!/usr/bin/env bash
set -e

echo "🔧 Building lattice-code-runner from cmd/mcp..."
go build -o lattice-code-runner ./cmd/mcp/main.go

echo "🚀 Moving binary to /usr/local/bin (requires sudo)..."
sudo mv lattice-code-runner /usr/local/bin/

echo "✅ Installation complete!"
echo "You can now run: lattice-code-runner --help"
