# Lattice Code MCP Server

An MCP (Model Context Protocol) server that provides powerful tools for searching, reading, writing, and refactoring codebases.

## Features

### 🔍 **search_codebase**
Search for code patterns, functions, or text across the entire codebase using grep-like functionality.

**Parameters:**
- `query` (required): Search query or pattern to find
- `path` (optional): Directory path to search in (defaults to current directory)
- `file_pattern` (optional): File pattern to filter (e.g., '*.go', '*.js')
- `case_sensitive` (optional): Whether search should be case sensitive (default: false)

**Example:**
```json
{
  "query": "func BuildAgent",
  "path": "./src",
  "file_pattern": "*.go",
  "case_sensitive": true
}
```

### 📖 **read_file**
Read the contents of a file from the codebase, optionally specifying a line range.

**Parameters:**
- `path` (required): Absolute or relative path to the file
- `start_line` (optional): Starting line number (1-indexed)
- `end_line` (optional): Ending line number (1-indexed)

**Example:**
```json
{
  "path": "src/model.go",
  "start_line": 10,
  "end_line": 50
}
```

### ✍️ **write_file**
Write or update a file in the codebase.

**Parameters:**
- `path` (required): Path to the file to write
- `content` (required): Content to write to the file
- `create_dirs` (optional): Create parent directories if they don't exist (default: true)

**Example:**
```json
{
  "path": "src/new_feature.go",
  "content": "package src\n\nfunc NewFeature() {}\n",
  "create_dirs": true
}
```

### 🔧 **refactor_file**
Refactor a file by replacing specific content with new content, optionally within a line range.

**Parameters:**
- `path` (required): Path to the file to refactor
- `find` (required): Content to find and replace
- `replace` (required): Replacement content
- `start_line` (optional): Starting line to search within
- `end_line` (optional): Ending line to search within

**Example:**
```json
{
  "path": "src/model.go",
  "find": "modeDir",
  "replace": "ui.ModeDir",
  "start_line": 1,
  "end_line": 100
}
```

### 📁 **list_files**
List files and directories in a given path.

**Parameters:**
- `path` (optional): Directory path to list (defaults to current directory)
- `recursive` (optional): Whether to list files recursively (default: false)
- `pattern` (optional): File pattern to filter (e.g., '*.go')

**Example:**
```json
{
  "path": "./src",
  "recursive": true,
  "pattern": "*.go"
}
```

### 📋 **get_file_outline**
Get an outline of a code file showing functions, classes, and structure.

**Parameters:**
- `path` (required): Path to the file to analyze

**Example:**
```json
{
  "path": "src/model.go"
}
```

## Installation

### Build the server:
```bash
cd cmd/mcp-server
go build -o lattice-mcp-server
```

### Run the server:
```bash
./lattice-mcp-server
```

## Usage with MCP Clients

### Claude Desktop

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "lattice-code": {
      "command": "/path/to/lattice-code/cmd/mcp-server/lattice-mcp-server",
      "args": []
    }
  }
}
```

### Cline / Other MCP Clients

Configure the server endpoint in your MCP client settings to point to the server binary.

## Example Workflows

### 1. Search and Refactor
```bash
# Search for all occurrences of old pattern
search_codebase(query="oldFunction", path="./src")

# Read the file to understand context
read_file(path="src/target.go")

# Refactor the file
refactor_file(
  path="src/target.go",
  find="oldFunction",
  replace="newFunction"
)
```

### 2. Analyze Codebase Structure
```bash
# List all Go files
list_files(path="./src", recursive=true, pattern="*.go")

# Get outline of each file
get_file_outline(path="src/model.go")
get_file_outline(path="src/view.go")
```

### 3. Create New Feature
```bash
# Create new file with boilerplate
write_file(
  path="src/features/new_feature.go",
  content="package features\n\n// NewFeature implementation\n",
  create_dirs=true
)

# Verify it was created
read_file(path="src/features/new_feature.go")
```

## Architecture

The MCP server is built using:
- **mark3labs/mcp-go**: Official Go implementation of the Model Context Protocol
- **Standard library**: File operations, path manipulation, and text processing

### Tool Implementation

Each tool is implemented as a handler function that:
1. Parses input parameters from the MCP request
2. Performs the requested operation (search, read, write, etc.)
3. Returns results in MCP-compliant format

### Error Handling

All tools include comprehensive error handling:
- File not found errors
- Permission errors
- Invalid path errors
- Content parsing errors

Errors are returned as MCP error results with descriptive messages.

## Security Considerations

⚠️ **Important**: This MCP server has full file system access within its working directory.

**Best Practices:**
- Run the server with appropriate user permissions
- Use in trusted environments only
- Consider adding path validation/sandboxing for production use
- Review all file operations before execution

## Development

### Adding New Tools

1. Define the tool schema in `registerTools()`
2. Implement the handler function
3. Register the tool with the server

Example:
```go
s.AddTool(mcp.Tool{
    Name:        "my_new_tool",
    Description: "Description of what the tool does",
    InputSchema: mcp.ToolInputSchema{
        Type: "object",
        Properties: map[string]interface{}{
            "param1": map[string]interface{}{
                "type":        "string",
                "description": "Parameter description",
            },
        },
        Required: []string{"param1"},
    },
}, handleMyNewTool)
```

### Testing

Test the server using the MCP inspector or any MCP-compatible client:

```bash
# Build
go build -o lattice-mcp-server

# Run
./lattice-mcp-server

# In another terminal, use an MCP client to test
```

## Roadmap

- [ ] Add support for more file types in outline generation
- [ ] Implement regex support in search
- [ ] Add file diff generation
- [ ] Support for batch operations
- [ ] Caching for improved performance
- [ ] Integration with LSP for better code analysis

## License

Same as the parent lattice-code project.

## Contributing

Contributions are welcome! Please ensure:
- All tools follow the MCP specification
- Error handling is comprehensive
- Documentation is updated
- Code is well-tested
