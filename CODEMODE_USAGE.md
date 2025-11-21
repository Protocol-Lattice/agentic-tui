# Using CodeMode with MCP Tools

This guide shows how to use the codemode plugin to interact with the MCP codebase tools via natural language prompts and Go-like scripts.

## Overview

The codemode plugin allows you to write small Go-like scripts that call tools to perform complex operations. Instead of manually calling each tool, you can write a script that orchestrates multiple tool calls.

## Architecture

```
User Prompt
    ↓
CodeMode Script (Go-like)
    ↓
codemode.CallTool("tool_name", args)
    ↓
MCP Server (lattice-mcp-server)
    ↓
File System Operations
```

## Basic Usage

### 1. Search and Read

```go
package main

import (
	"fmt"
	"codemode"
)

func main() {
	// Search for a pattern
	results := codemode.CallTool("search_codebase", map[string]any{
		"query": "func BuildAgent",
		"path": "./src",
		"file_pattern": "*.go",
	})
	
	fmt.Println("Search results:", results)
	
	// Read a specific file
	content := codemode.CallTool("read_file", map[string]any{
		"path": "src/agent.go",
		"start_line": 10,
		"end_line": 30,
	})
	
	fmt.Println("File content:", content)
}
```

### 2. Refactor Code

```go
package main

import (
	"fmt"
	"codemode"
)

func main() {
	// Refactor: replace old function name with new one
	result := codemode.CallTool("refactor_file", map[string]any{
		"path": "src/model.go",
		"find": "oldFunctionName",
		"replace": "newFunctionName",
	})
	
	fmt.Println("Refactoring result:", result)
}
```

### 3. Generate New Files

```go
package main

import (
	"fmt"
	"codemode"
)

func main() {
	// Generate a new handler file
	content := `package handlers

import "net/http"

// NewHandler creates a new HTTP handler
func NewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	}
}
`
	
	result := codemode.CallTool("write_file", map[string]any{
		"path": "src/handlers/new_handler.go",
		"content": content,
		"create_dirs": true,
	})
	
	fmt.Println("File created:", result)
}
```

## Advanced Examples

### Multi-Step Refactoring

```go
package main

import (
	"fmt"
	"strings"
	"codemode"
)

func main() {
	// Step 1: Find all files with old pattern
	searchResults := codemode.CallTool("search_codebase", map[string]any{
		"query": "type OldStruct",
		"path": "./src",
		"file_pattern": "*.go",
	})
	
	// Step 2: Parse results to get file list
	files := parseSearchResults(searchResults)
	
	// Step 3: Refactor each file
	for _, file := range files {
		// Read file first
		content := codemode.CallTool("read_file", map[string]any{
			"path": file,
		})
		
		// Check if it needs refactoring
		if strings.Contains(content.(string), "OldStruct") {
			// Refactor the file
			result := codemode.CallTool("refactor_file", map[string]any{
				"path": file,
				"find": "OldStruct",
				"replace": "NewStruct",
			})
			
			fmt.Printf("Refactored %s: %v\n", file, result)
		}
	}
}

func parseSearchResults(results any) []string {
	// Parse search results and extract unique file paths
	// Implementation depends on search result format
	return []string{"src/model.go", "src/types.go"}
}
```

### Codebase Analysis

```go
package main

import (
	"fmt"
	"codemode"
)

func main() {
	// Step 1: List all Go files
	files := codemode.CallTool("list_files", map[string]any{
		"path": "./src",
		"recursive": true,
		"pattern": "*.go",
	})
	
	fmt.Println("=== Go Files ===")
	fmt.Println(files)
	
	// Step 2: Get outline of each file
	fileList := []string{"src/model.go", "src/view.go", "src/update.go"}
	
	for _, file := range fileList {
		outline := codemode.CallTool("get_file_outline", map[string]any{
			"path": file,
		})
		
		fmt.Printf("\n=== %s ===\n", file)
		fmt.Println(outline)
	}
}
```

### Batch Refactoring with Validation

```go
package main

import (
	"fmt"
	"strings"
	"codemode"
)

func main() {
	// Define refactoring rules
	rules := []struct {
		Find    string
		Replace string
	}{
		{"modeDir", "ui.ModeDir"},
		{"modeList", "ui.ModeList"},
		{"modeChat", "ui.ModeChat"},
	}
	
	// Get all Go files
	files := []string{"src/update.go", "src/view.go"}
	
	// Apply each rule to each file
	for _, file := range files {
		fmt.Printf("\nProcessing %s...\n", file)
		
		for _, rule := range rules {
			// Check if file contains the pattern
			content := codemode.CallTool("read_file", map[string]any{
				"path": file,
			})
			
			if strings.Contains(content.(string), rule.Find) {
				// Apply refactoring
				result := codemode.CallTool("refactor_file", map[string]any{
					"path": file,
					"find": rule.Find,
					"replace": rule.Replace,
				})
				
				fmt.Printf("  ✓ %s → %s: %v\n", rule.Find, rule.Replace, result)
			}
		}
	}
	
	fmt.Println("\n✅ Batch refactoring complete!")
}
```

## Integration with Lattice Code

### Using in the TUI

The codemode refactoring can be integrated into the Lattice Code TUI:

```go
// In src/update.go
case "refactor with codemode":
	raw := strings.TrimSpace(m.textarea.Value())
	
	// Create codemode refactor instance
	cmRefactor := NewCodeModeRefactor(m.codemode)
	
	// Execute refactoring based on prompt
	result, err := cmRefactor.RefactorWithPrompt(m.ctx, raw)
	if err != nil {
		m.output += fmt.Sprintf("❌ Error: %v\n", err)
	} else {
		m.output += fmt.Sprintf("✅ Refactoring complete:\n%s\n", result)
	}
	
	m.renderOutput(true)
```

### Example Prompts

1. **"Refactor all mode constants to use ui package"**
   - Searches for `modeDir`, `modeList`, etc.
   - Replaces with `ui.ModeDir`, `ui.ModeList`, etc.

2. **"Update all style field names to be capitalized"**
   - Finds `.style.accent`, `.style.error`, etc.
   - Replaces with `.style.Accent`, `.style.Error`, etc.

3. **"Create a new feature module in src/features/"**
   - Generates boilerplate code
   - Creates directory structure
   - Writes initial files

## Best Practices

### 1. Always Validate Before Refactoring

```go
// Read file first
content := codemode.CallTool("read_file", map[string]any{"path": file})

// Check if pattern exists
if strings.Contains(content.(string), "pattern") {
	// Then refactor
	codemode.CallTool("refactor_file", ...)
}
```

### 2. Use Specific Search Patterns

```go
// Good: Specific pattern
codemode.CallTool("search_codebase", map[string]any{
	"query": "func BuildAgent",
	"file_pattern": "*.go",
})

// Bad: Too broad
codemode.CallTool("search_codebase", map[string]any{
	"query": "func",
})
```

### 3. Handle Errors Gracefully

```go
result := codemode.CallTool("write_file", map[string]any{
	"path": "new_file.go",
	"content": content,
})

// Check if result indicates success
if strings.Contains(result.(string), "Successfully") {
	fmt.Println("✅ File written")
} else {
	fmt.Println("❌ Failed:", result)
}
```

### 4. Use Line Ranges for Large Files

```go
// Only refactor specific sections
codemode.CallTool("refactor_file", map[string]any{
	"path": "large_file.go",
	"find": "oldPattern",
	"replace": "newPattern",
	"start_line": 100,
	"end_line": 200,
})
```

## Troubleshooting

### MCP Server Not Running

Ensure the MCP server is built and accessible:

```bash
cd /Users/raezil/Desktop/lattice-code
go build -o lattice-mcp-server ./cmd/mcp-server
chmod +x lattice-mcp-server
```

### Provider Configuration

Verify `provider.json` points to the correct binary:

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

### Codemode Script Errors

Check that your codemode script:
- Uses correct function signatures
- Properly handles tool results
- Has valid Go syntax

## Next Steps

1. **Extend the MCP server** with more tools (e.g., git operations, test running)
2. **Create prompt templates** for common refactoring patterns
3. **Add validation** to ensure refactorings don't break code
4. **Integrate with CI/CD** to run refactorings automatically

## Resources

- [CodeMode Plugin Documentation](https://github.com/universal-tool-calling-protocol/go-utcp/tree/main/src/plugins/codemode)
- [MCP Specification](https://spec.modelcontextprotocol.io/)
- [UTCP Documentation](https://utcp.io/)
