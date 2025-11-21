package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	toolSearchCodebase = "search_codebase"
	toolReadFile       = "read_file"
	toolWriteFile      = "write_file"
	toolRefactorFile   = "refactor_file"
	toolListFiles      = "list_files"
	toolGetFileOutline = "get_file_outline"
)

func main() {
	// Create MCP server
	s := server.NewMCPServer(
		"Lattice Code MCP Server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register tools
	registerTools(s)

	// Start server
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func registerTools(s *server.MCPServer) {
	// Tool 1: Search codebase
	s.AddTool(mcp.Tool{
		Name:        toolSearchCodebase,
		Description: "Search for code patterns, functions, or text across the codebase using grep-like functionality",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query or pattern to find in the codebase",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory path to search in (defaults to current directory)",
				},
				"file_pattern": map[string]interface{}{
					"type":        "string",
					"description": "File pattern to filter (e.g., '*.go', '*.js')",
				},
				"case_sensitive": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether search should be case sensitive",
					"default":     false,
				},
			},
			Required: []string{"query"},
		},
	}, handleSearchCodebase)

	// Tool 2: Read file
	s.AddTool(mcp.Tool{
		Name:        toolReadFile,
		Description: "Read the contents of a file from the codebase",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute or relative path to the file to read",
				},
				"start_line": map[string]interface{}{
					"type":        "integer",
					"description": "Optional starting line number (1-indexed)",
				},
				"end_line": map[string]interface{}{
					"type":        "integer",
					"description": "Optional ending line number (1-indexed)",
				},
			},
			Required: []string{"path"},
		},
	}, handleReadFile)

	// Tool 3: Write file
	s.AddTool(mcp.Tool{
		Name:        toolWriteFile,
		Description: "Write or update a file in the codebase",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write to the file",
				},
				"create_dirs": map[string]interface{}{
					"type":        "boolean",
					"description": "Create parent directories if they don't exist",
					"default":     true,
				},
			},
			Required: []string{"path", "content"},
		},
	}, handleWriteFile)

	// Tool 4: Refactor file
	s.AddTool(mcp.Tool{
		Name:        toolRefactorFile,
		Description: "Refactor a file by replacing specific content with new content",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to refactor",
				},
				"find": map[string]interface{}{
					"type":        "string",
					"description": "Content to find and replace",
				},
				"replace": map[string]interface{}{
					"type":        "string",
					"description": "Replacement content",
				},
				"start_line": map[string]interface{}{
					"type":        "integer",
					"description": "Optional starting line to search within",
				},
				"end_line": map[string]interface{}{
					"type":        "integer",
					"description": "Optional ending line to search within",
				},
			},
			Required: []string{"path", "find", "replace"},
		},
	}, handleRefactorFile)

	// Tool 5: List files
	s.AddTool(mcp.Tool{
		Name:        toolListFiles,
		Description: "List files and directories in a given path",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory path to list (defaults to current directory)",
				},
				"recursive": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to list files recursively",
					"default":     false,
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "File pattern to filter (e.g., '*.go')",
				},
			},
		},
	}, handleListFiles)

	// Tool 6: Get file outline
	s.AddTool(mcp.Tool{
		Name:        toolGetFileOutline,
		Description: "Get an outline of a code file showing functions, classes, and structure",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to analyze",
				},
			},
			Required: []string{"path"},
		},
	}, handleGetFileOutline)
}

// Tool handlers
func handleSearchCodebase(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := request.GetString("query", "")
	searchPath := request.GetString("path", ".")
	filePattern := request.GetString("file_pattern", "")
	caseSensitive := request.GetBool("case_sensitive", false)

	results := []string{}
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files with errors
		}

		if info.IsDir() {
			return nil
		}

		// Filter by file pattern if provided
		if filePattern != "" {
			matched, _ := filepath.Match(filePattern, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		// Read and search file
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			var found bool
			if caseSensitive {
				found = strings.Contains(line, query)
			} else {
				found = strings.Contains(strings.ToLower(line), strings.ToLower(query))
			}

			if found {
				results = append(results, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
			}
		}

		return nil
	})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Search failed: %v", err)), nil
	}

	output := strings.Join(results, "\n")
	if output == "" {
		output = "No results found"
	}

	return mcp.NewToolResultText(output), nil
}

func handleReadFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	startLine := request.GetFloat("start_line", 0)
	endLine := request.GetFloat("end_line", 0)

	content, err := os.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read file: %v", err)), nil
	}

	lines := strings.Split(string(content), "\n")

	// Apply line range if specified
	if startLine > 0 && endLine > 0 {
		start := int(startLine) - 1
		end := int(endLine)
		if start < 0 {
			start = 0
		}
		if end > len(lines) {
			end = len(lines)
		}
		lines = lines[start:end]
	}

	output := strings.Join(lines, "\n")
	return mcp.NewToolResultText(output), nil
}

func handleWriteFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	content := request.GetString("content", "")
	createDirs := request.GetBool("create_dirs", true)

	if createDirs {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create directories: %v", err)), nil
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to write file: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote to %s", path)), nil
}

func handleRefactorFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")
	find := request.GetString("find", "")
	replace := request.GetString("replace", "")
	startLine := request.GetFloat("start_line", 0)
	endLine := request.GetFloat("end_line", 0)

	content, err := os.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read file: %v", err)), nil
	}

	lines := strings.Split(string(content), "\n")

	// Determine range
	start := 0
	end := len(lines)
	if startLine > 0 {
		start = int(startLine) - 1
	}
	if endLine > 0 {
		end = int(endLine)
	}

	// Perform replacement in the specified range
	modified := false
	for i := start; i < end && i < len(lines); i++ {
		if strings.Contains(lines[i], find) {
			lines[i] = strings.ReplaceAll(lines[i], find, replace)
			modified = true
		}
	}

	if !modified {
		return mcp.NewToolResultText("No matches found to replace"), nil
	}

	// Write back
	newContent := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to write file: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Successfully refactored %s", path)), nil
}

func handleListFiles(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", ".")
	recursive := request.GetBool("recursive", false)
	pattern := request.GetString("pattern", "")

	var files []string

	if recursive {
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}

			if pattern != "" {
				matched, _ := filepath.Match(pattern, filepath.Base(p))
				if !matched {
					return nil
				}
			}

			relPath, _ := filepath.Rel(path, p)
			if info.IsDir() {
				files = append(files, fmt.Sprintf("[DIR]  %s", relPath))
			} else {
				files = append(files, fmt.Sprintf("[FILE] %s (%d bytes)", relPath, info.Size()))
			}

			return nil
		})

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list files: %v", err)), nil
		}
	} else {
		entries, err := os.ReadDir(path)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read directory: %v", err)), nil
		}

		for _, entry := range entries {
			if pattern != "" {
				matched, _ := filepath.Match(pattern, entry.Name())
				if !matched {
					continue
				}
			}

			if entry.IsDir() {
				files = append(files, fmt.Sprintf("[DIR]  %s", entry.Name()))
			} else {
				info, _ := entry.Info()
				files = append(files, fmt.Sprintf("[FILE] %s (%d bytes)", entry.Name(), info.Size()))
			}
		}
	}

	output := strings.Join(files, "\n")
	if output == "" {
		output = "No files found"
	}

	return mcp.NewToolResultText(output), nil
}

func handleGetFileOutline(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path := request.GetString("path", "")

	content, err := os.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read file: %v", err)), nil
	}

	lines := strings.Split(string(content), "\n")
	outline := []string{}

	// Simple outline extraction for Go files
	ext := filepath.Ext(path)
	if ext == ".go" {
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Find functions, types, interfaces
			if strings.HasPrefix(trimmed, "func ") ||
				strings.HasPrefix(trimmed, "type ") ||
				strings.HasPrefix(trimmed, "const ") ||
				strings.HasPrefix(trimmed, "var ") {
				outline = append(outline, fmt.Sprintf("Line %d: %s", i+1, trimmed))
			}
		}
	} else {
		// Generic outline for other files - show first 20 lines
		maxLines := 20
		if len(lines) < maxLines {
			maxLines = len(lines)
		}
		for i := 0; i < maxLines; i++ {
			outline = append(outline, fmt.Sprintf("Line %d: %s", i+1, strings.TrimSpace(lines[i])))
		}
	}

	output := strings.Join(outline, "\n")
	if output == "" {
		output = "No outline available"
	}

	return mcp.NewToolResultText(output), nil
}
