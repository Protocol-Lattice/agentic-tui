#!/usr/bin/env bash
set -e

# ──────────────────────────────────────────────
# 🌐 Pretty install script for lattice-code
# ──────────────────────────────────────────────

# Colors
GREEN="\033[1;32m"
YELLOW="\033[1;33m"
BLUE="\033[1;34m"
RED="\033[1;31m"
RESET="\033[0m"

# Check dependencies
if ! command -v go >/dev/null 2>&1; then
    echo -e "${RED}❌ Go is not installed. Please install Go first.${RESET}"
    exit 1
fi

if ! command -v sudo >/dev/null 2>&1; then
    echo -e "${RED}❌ 'sudo' is required to move binaries globally.${RESET}"
    exit 1
fi

echo -e "${BLUE}🔧 Starting installation for ${GREEN}lattice-code${RESET}..."
sleep 0.5

# ──────────────────────────────────────────────
# Build lattice-code
# ──────────────────────────────────────────────
echo -e "${YELLOW}→ Building lattice-code from ./cmd...${RESET}"
go build -o lattice-code ./cmd || { echo -e "${RED}❌ Failed to build lattice-code.${RESET}"; exit 1; }

echo -e "${BLUE}→ Moving binary to /usr/local/bin...${RESET}"
sudo mv lattice-code /usr/local/bin/ || { echo -e "${RED}❌ Failed to move lattice-code.${RESET}"; exit 1; }

# ──────────────────────────────────────────────
# Build lattice-code-runner
# ──────────────────────────────────────────────
echo -e "${YELLOW}→ Building lattice-code-runner from ./cmd/mcp...${RESET}"
go build -o lattice-code-runner ./cmd/mcp/main.go || { echo -e "${RED}❌ Failed to build lattice-code-runner.${RESET}"; exit 1; }

echo -e "${BLUE}→ Moving binary to /usr/local/bin...${RESET}"
sudo mv lattice-code-runner /usr/local/bin/ || { echo -e "${RED}❌ Failed to move lattice-code-runner.${RESET}"; exit 1; }

# ──────────────────────────────────────────────
# Move provider.json
# ──────────────────────────────────────────────
echo -e "${YELLOW}→ Copying provider.json to ~/utcp...${RESET}"
mkdir -p ~/utcp
if [[ ! -f provider.json ]]; then
    echo -e "${YELLOW}⚠ provider.json not found locally. Downloading default from GitHub...${RESET}"
    curl -fsSL -o provider.json https://raw.githubusercontent.com/Protocol-Lattice/lattice-code/main/provider.json || {
        echo -e "${RED}❌ Failed to download provider.json. Please add it manually.${RESET}"
        exit 1
    }
fi

cp provider.json ~/utcp/provider.json

# ──────────────────────────────────────────────
# Done
# ──────────────────────────────────────────────
echo ""
echo -e "${GREEN}✅ Installation complete!${RESET}"
echo ""
echo -e "You can now run:"
echo -e "   ${BLUE}lattice-code --help${RESET}"
echo -e "   ${BLUE}lattice-code-runner --help${RESET}"
echo ""
echo -e "${YELLOW}Happy coding with Protocol Lattice 🧠${RESET}"
