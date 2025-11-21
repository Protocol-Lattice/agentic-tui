# 🚀 Quick Start Guide - Lattice Code with CodeMode

## Overview

Lattice Code now has **codemode integration** that allows you to refactor code using natural language prompts and the MCP server tools.

## Prerequisites

✅ Go 1.25.0+  
✅ Dependencies installed (`go mod tidy`)  
✅ MCP server built  

## Build Everything

```bash
# From the project root
go mod tidy
go build -o lattice-code ./cmd
go build -o lattice-mcp-server ./cmd/mcp-server
```

## Usage

### 1. Start the MCP Server

```bash
./lattice-mcp-server
```

The server will start and listen for tool calls via stdio.

### 2. Use CodeMode for Refactoring

The codemode integration is in `src/codemode_refactor.go`. Here's how it works:

#### Example: Search and Analyze

```go
import (
    "context"
    "github.com/Protocol-Lattice/lattice-code/src"
    "github.com/universal-tool-calling-protocol/go-utcp/pkg/client"
    "github.com/universal-tool-calling-protocol/go-utcp/src/plugins/codemode"
)

// Initialize
ctx := context.Background()
utcpClient, _ := client.NewClient(ctx, "provider.json")
cm := codemode.NewCodeModeUTCP(utcpClient, nil)
cmRefactor := src.NewCodeModeRefactor(cm)

// Analyze codebase
result, _ := cmRefactor.AnalyzeCodebase(ctx, "./src/ui")
fmt.Println(result)
```

#### Example: Refactor with Prompt

```go
// Natural language refactoring
prompt := "Update all mode constants to use ui package"
result, _ := cmRefactor.RefactorWithPrompt(ctx, prompt)
fmt.Println(result)
```

### 3. Available Tools

The MCP server provides these tools:

| Tool | Description | Example |
|------|-------------|---------|
| `search_codebase` | Search for patterns | Find all "func Build" |
| `read_file` | Read file contents | Read lines 10-50 of model.go |
| `write_file` | Create/update files | Write new handler.go |
| `refactor_file` | Find & replace | Replace oldFunc with newFunc |
| `list_files` | List files | List all *.go in src/ |
| `get_file_outline` | Get file structure | Show all functions in file |

### 4. CodeMode Scripts

Write Go-like scripts that use the tools:

```go
package main

import (
    "fmt"
    "codemode"
)

func main() {
    // Search for mode constants
    results := codemode.CallTool("search_codebase", map[string]any{
        "query": "mode",
        "path": "./src",
        "file_pattern": "*.go",
    })
    
    // Refactor each file
    files := extractFiles(results)
    for _, file := range files {
        codemode.CallTool("refactor_file", map[string]any{
            "path": file,
            "find": "modeDir",
            "replace": "ui.ModeDir",
        })
    }
}
```

## How It Works

### Architecture

```
User Prompt
    ↓
CodeMode Script (Go-like)
    ↓
codemode.CallTool("tool_name", args)
    ↓
UTCP Client
    ↓
MCP Server (lattice-mcp-server)
    ↓
File System Operations
    ↓
Return Result
```

### The Magic: analyzeChanges()

The `analyzeChanges()` function in `src/codemode_refactor.go` automatically determines what to refactor:

```go
func analyzeChanges(content any) []Change {
    // Analyzes file content line-by-line
    // Detects patterns like:
    // - modeDir → ui.ModeDir
    // - .style.accent → .style.Accent
    // Returns list of changes to apply
}
```

**Supported Patterns:**
- Mode constants (10 patterns)
- Style fields (9 patterns)
- Automatically deduplicates changes

## Examples

### Example 1: List All UI Files

```bash
# Using the MCP server directly
echo '{"tool":"list_files","args":{"path":"./src/ui","pattern":"*.go"}}' | ./lattice-mcp-server
```

### Example 2: Search for TODO Comments

```go
results := codemode.CallTool("search_codebase", map[string]any{
    "query": "TODO",
    "path": "./src",
})
```

### Example 3: Batch Refactoring

```go
// The RefactorWithPrompt function does this automatically:
// 1. Searches for relevant files
// 2. Reads each file
// 3. Analyzes what needs changing
// 4. Applies all changes
// 5. Returns summary

result, _ := cmRefactor.RefactorWithPrompt(ctx, 
    "Update all mode constants to use ui package")
```

## Configuration

### provider.json

```json
{
  "providers": [
    {
      "name": "lattice_mcp_codebase",
      "provider_type": "mcp",
      "command": ["./lattice-mcp-server"],
      "args": [],
      "env": {}
    }
  ]
}
```

## Troubleshooting

### Import Errors

```bash
# Fix missing dependencies
go get github.com/universal-tool-calling-protocol/go-utcp@latest
go mod tidy
```

### MCP Server Not Found

```bash
# Ensure it's built
go build -o lattice-mcp-server ./cmd/mcp-server
chmod +x lattice-mcp-server
```

### Tool Calls Failing

1. Check MCP server is running
2. Verify `provider.json` path is correct
3. Check file paths are relative to working directory

## Next Steps

1. **Try the examples** in `CODEMODE_USAGE.md`
2. **Extend the MCP server** with more tools
3. **Create custom refactoring patterns** in `analyzeChanges()`
4. **Integrate with your workflow** via the TUI

## Documentation

- `REFACTORING_SUMMARY.md` - Complete refactoring overview
- `CODEMODE_USAGE.md` - Detailed codemode examples
- `src/ui/README.md` - UI package documentation
- `cmd/mcp-server/README.md` - MCP server documentation

## Success! 🎉

You now have a fully functional codemode-powered refactoring system that can:
- ✅ Search codebases with natural language
- ✅ Automatically determine what needs refactoring
- ✅ Apply changes across multiple files
- ✅ Generate new code based on prompts

**Start refactoring with natural language today!**
