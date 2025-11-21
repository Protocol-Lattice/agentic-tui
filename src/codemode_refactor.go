package src

import (
	"context"
	"fmt"
	"strings"

	"github.com/universal-tool-calling-protocol/go-utcp/src/plugins/codemode"
)

// CodeModeRefactor uses the codemode plugin to refactor files via natural language prompts
type CodeModeRefactor struct {
	cm *codemode.CodeModeUTCP
}

// NewCodeModeRefactor creates a new codemode-based refactoring engine
func NewCodeModeRefactor(cm *codemode.CodeModeUTCP) *CodeModeRefactor {
	return &CodeModeRefactor{cm: cm}
}

// SearchAndRefactor uses codemode to search for patterns and refactor code
func (cmr *CodeModeRefactor) SearchAndRefactor(ctx context.Context, prompt string) (string, error) {
	// Use CallTool with natural language prompt
	// Example: "Search for all occurrences of 'oldFunction' in src/ and replace with 'newFunction'"

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("codemode call failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("codemode call was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// GenerateFile uses codemode to generate a new file based on a prompt
func (cmr *CodeModeRefactor) GenerateFile(ctx context.Context, prompt string) (string, error) {
	// Example: "Create a new handler file in src/handlers/ with CRUD operations"

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("codemode call failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("codemode call was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// AnalyzeCodebase uses codemode to analyze the codebase structure
func (cmr *CodeModeRefactor) AnalyzeCodebase(ctx context.Context, path string) (string, error) {
	prompt := fmt.Sprintf("List all Go files in %s and show their structure", path)

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("codemode call failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("codemode call was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// RefactorWithPrompt uses natural language to perform complex refactoring
func (cmr *CodeModeRefactor) RefactorWithPrompt(ctx context.Context, prompt string) (string, error) {
	// This is the main entry point for prompt-based refactoring
	// The codemode plugin will interpret the natural language prompt and execute the appropriate tools

	// Enhance the prompt with context about available tools
	enhancedPrompt := fmt.Sprintf(`
You have access to these tools from the lattice_mcp_codebase provider:
- lattice_mcp_codebase.search_codebase: Search for code patterns
- lattice_mcp_codebase.read_file: Read file contents
- lattice_mcp_codebase.write_file: Create or update files
- lattice_mcp_codebase.refactor_file: Find and replace in files
- lattice_mcp_codebase.list_files: List files in a directory
- lattice_mcp_codebase.get_file_outline: Get file structure

User request: %s

Please use these tools to accomplish the task. For refactoring:
1. Use lattice_mcp_codebase.search_codebase to find relevant files
2. Use lattice_mcp_codebase.read_file to understand context
3. Determine what changes are needed
4. Use lattice_mcp_codebase.refactor_file to apply changes

Return a summary of what was done.
`, prompt)

	success, result, err := cmr.cm.CallTool(ctx, enhancedPrompt)
	if err != nil {
		return "", fmt.Errorf("codemode call failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("codemode call was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// SearchCode searches the codebase using natural language
func (cmr *CodeModeRefactor) SearchCode(ctx context.Context, query string) (string, error) {
	prompt := fmt.Sprintf("Use lattice_mcp_codebase.search_codebase to search the codebase in ./src for: %s", query)

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("search was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// ReadFile reads a file using natural language
func (cmr *CodeModeRefactor) ReadFile(ctx context.Context, path string, startLine, endLine int) (string, error) {
	var prompt string
	if startLine > 0 && endLine > 0 {
		prompt = fmt.Sprintf("Use lattice_mcp_codebase.read_file to read file %s from line %d to %d", path, startLine, endLine)
	} else {
		prompt = fmt.Sprintf("Use lattice_mcp_codebase.read_file to read file %s", path)
	}

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("read was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// WriteFile writes a file using natural language
func (cmr *CodeModeRefactor) WriteFile(ctx context.Context, path, content string) (string, error) {
	prompt := fmt.Sprintf("Use lattice_mcp_codebase.write_file to write the following content to file %s:\n\n%s", path, content)

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("write failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("write was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// RefactorFile refactors a specific file using natural language
func (cmr *CodeModeRefactor) RefactorFile(ctx context.Context, path, find, replace string) (string, error) {
	prompt := fmt.Sprintf("Use lattice_mcp_codebase.refactor_file to replace all occurrences of '%s' with '%s' in file %s", find, replace, path)

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("refactor failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("refactor was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// BatchRefactor performs batch refactoring across multiple files
func (cmr *CodeModeRefactor) BatchRefactor(ctx context.Context, pattern, find, replace string) (string, error) {
	prompt := fmt.Sprintf(`
Perform batch refactoring using lattice_mcp_codebase tools:
1. Use lattice_mcp_codebase.search_codebase to find files matching pattern: %s
2. For each file, use lattice_mcp_codebase.refactor_file to replace '%s' with '%s'
3. Report which files were modified

Please execute this refactoring and provide a summary.
`, pattern, find, replace)

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("batch refactor failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("batch refactor was not successful")
	}

	return fmt.Sprintf("%v", result), nil
}

// AnalyzeAndRefactor analyzes code and suggests/applies refactorings
func (cmr *CodeModeRefactor) AnalyzeAndRefactor(ctx context.Context, description string) (string, error) {
	prompt := fmt.Sprintf(`
Analyze the codebase and perform the following refactoring:

%s

Steps to follow:
1. Search for relevant code patterns
2. Analyze what needs to be changed
3. Apply the refactoring
4. Provide a detailed summary of changes

Please execute this and report the results.
`, description)

	success, result, err := cmr.cm.CallTool(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("analyze and refactor failed: %w", err)
	}

	if !success {
		return "", fmt.Errorf("analyze and refactor was not successful")
	}

	resultStr := fmt.Sprintf("%v", result)

	// Parse and format the result
	if strings.Contains(resultStr, "Successfully") || strings.Contains(resultStr, "complete") {
		return "✅ " + resultStr, nil
	}

	return resultStr, nil
}
