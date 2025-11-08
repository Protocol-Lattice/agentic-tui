<p align="center">
<img width="1000" height="200" alt="Zrzut ekranu 2025-11-1 o 12 32 24" src="https://github.com/user-attachments/assets/f5398bc7-05b4-4777-bdb2-87995751bb57" />
</p>


`lattice-code` is a powerful command-line tool that leverages an AI agent for code generation and modification tasks directly in your local workspace. It can be run as an interactive TUI or in a headless mode for single-shot generation.

It is built on top of the Lattice Go Agent Framework.

## Features

- **Interactive TUI**: A terminal-based user interface for conversational code generation.
- **Headless Mode**: Run a single generation task from the command line and have the files written directly to your workspace.
- **Workspace Awareness**: The agent is provided with the file tree of your current project, allowing it to understand the context and make relevant changes.
- **File Operations**: The agent can create, modify, and delete files as needed to complete its task.

## Installation

To build the `lattice-code` CLI from source:

```bash
# Clone the repository
git clone https://github.com/Protocol-Lattice/lattice-code.git
cd lattice-code

# Build the binary
go build -o lattice-code ./cmd

# Build mcp
chmod +x install.sh
./install.sh
```

## Configuration

The agent requires API keys for the underlying LLM provider (e.g., Gemini). Ensure you have the necessary environment variables set.

```bash
export GEMINI_API_KEY="YOUR_API_KEY"
```

## Usage

### Interactive Mode

To start the interactive terminal UI, simply run the command:

```bash
./lattice-code
```

### Headless Mode (Example)

The `headless` package provides functionality to run a single generation turn.

```go
// See src/headless.go for an example of how to run a single generation turn.
RunHeadless(ctx, agent, "./workspace", "My task is to create a new Go web server.")
```

## How It Works

`lattice-code` works by:
1.  Collecting the file structure of your current workspace.
2.  Constructing a detailed prompt that includes your task and the file tree.
3.  Sending this context to a powerful AI agent.
4.  The agent responds with a plan and a series of markdown code blocks.
5.  `lattice-code` parses these blocks, extracts file paths from special `// path: ...` comments, and writes the content to your local file system.
