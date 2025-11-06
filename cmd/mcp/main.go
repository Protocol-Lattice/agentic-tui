// path: main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Language configurations
type LanguageConfig struct {
	Cmd          string
	Args         []string
	Extension    string
	CompileArgs  []string
	NeedsCompile bool
	RunCompiled  bool
}

var languageConfigs = map[string]LanguageConfig{
	"python":     {Cmd: "python3", Extension: ".py"},
	"python2":    {Cmd: "python2", Extension: ".py"},
	"javascript": {Cmd: "node", Extension: ".js"},
	"typescript": {Cmd: "ts-node", Extension: ".ts"},
	"go":         {Cmd: "go", Args: []string{"run"}, Extension: ".go"},
	"rust":       {Cmd: "rustc", Extension: ".rs", NeedsCompile: true, RunCompiled: true},
	"java":       {Cmd: "javac", Extension: ".java", NeedsCompile: true, RunCompiled: true},
	"c":          {Cmd: "gcc", CompileArgs: []string{"-o"}, Extension: ".c", NeedsCompile: true, RunCompiled: true},
	"cpp":        {Cmd: "g++", CompileArgs: []string{"-o"}, Extension: ".cpp", NeedsCompile: true, RunCompiled: true},
	"ruby":       {Cmd: "ruby", Extension: ".rb"},
	"php":        {Cmd: "php", Extension: ".php"},
	"perl":       {Cmd: "perl", Extension: ".pl"},
	"r":          {Cmd: "Rscript", Extension: ".r"},
	"lua":        {Cmd: "lua", Extension: ".lua"},
	"bash":       {Cmd: "bash", Extension: ".sh"},
	"shell":      {Cmd: "sh", Extension: ".sh"},
	"kotlin":     {Cmd: "kotlinc", Args: []string{"-script"}, Extension: ".kts"},
	"scala":      {Cmd: "scala", Extension: ".scala"},
	"swift":      {Cmd: "swift", Extension: ".swift"},
	"dart":       {Cmd: "dart", Extension: ".dart"},
}

type RunCodeParams struct {
	Language string   `json:"language"`
	Code     string   `json:"code,omitempty"`
	Path     string   `json:"path,omitempty"`
	File     string   `json:"file,omitempty"`
	Args     []string `json:"args,omitempty"`
	Timeout  int      `json:"timeout,omitempty"`
	Cwd      string   `json:"cwd,omitempty"`
}

type CodeRunResult struct {
	Success  bool   `json:"success"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exitCode"`
	Duration string `json:"duration"`
	Command  string `json:"command"`
}

func runCode(params mcp.CallToolParams) (*CodeRunResult, error) {
	lang := params.Arguments.(map[string]any)["language"].(string)
	config, ok := languageConfigs[lang]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	timeout := 30 * time.Second
	if t, ok := params.Arguments.(map[string]any)["timeout"].(float64); ok {
		timeout = time.Duration(int(t)) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	code, _ := params.Arguments.(map[string]any)["code"].(string)
	path, _ := params.Arguments.(map[string]any)["path"].(string)
	file, _ := params.Arguments.(map[string]any)["file"].(string)
	cwd, _ := params.Arguments.(map[string]any)["cwd"].(string)
	argsAny, _ := params.Arguments.(map[string]any)["args"].([]any)
	args := make([]string, len(argsAny))
	for i, v := range argsAny {
		args[i] = fmt.Sprintf("%v", v)
	}

	var targetFile, workDir string
	var cleanupFiles []string

	if code != "" {
		tmpFile, err := os.CreateTemp("", fmt.Sprintf("mcp-code-*%s", config.Extension))
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %v", err)
		}
		cleanupFiles = append(cleanupFiles, tmpFile.Name())
		if _, err := tmpFile.WriteString(code); err != nil {
			return nil, fmt.Errorf("failed to write code: %v", err)
		}
		tmpFile.Close()
		targetFile = tmpFile.Name()
		workDir = filepath.Dir(targetFile)
	} else if path != "" {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("invalid path: %v", err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("path does not exist: %v", err)
		}
		if info.IsDir() {
			if file == "" {
				return nil, fmt.Errorf("file parameter required when path is a directory")
			}
			targetFile = filepath.Join(absPath, file)
			workDir = absPath
		} else {
			targetFile = absPath
			workDir = filepath.Dir(absPath)
		}
	} else {
		return nil, fmt.Errorf("either 'code' or 'path' must be provided")
	}

	if cwd != "" {
		if abs, err := filepath.Abs(cwd); err == nil {
			workDir = abs
		}
	}

	defer func() {
		for _, f := range cleanupFiles {
			_ = os.Remove(f)
		}
	}()

	startTime := time.Now()
	select {
	case <-ctx.Done():
		return &CodeRunResult{Success: false, Error: "execution canceled by client", ExitCode: -1}, nil
	default:
	}

	var outputBin string
	if config.NeedsCompile {
		outputBin = filepath.Join(os.TempDir(), fmt.Sprintf("mcp-compiled-%d", time.Now().UnixNano()))
		cleanupFiles = append(cleanupFiles, outputBin)
		var compileArgs []string

		switch lang {
		case "java":
			compileArgs = []string{targetFile}
		case "c", "cpp":
			compileArgs = append(config.CompileArgs, outputBin, targetFile)
		case "rust":
			compileArgs = []string{targetFile, "-o", outputBin}
		}

		cmd := exec.CommandContext(ctx, config.Cmd, compileArgs...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return &CodeRunResult{
				Success:  false,
				Error:    fmt.Sprintf("Compilation failed: %s", string(out)),
				Command:  fmt.Sprintf("%s %s", config.Cmd, strings.Join(compileArgs, " ")),
				ExitCode: 1,
			}, nil
		}
	}

	var cmd *exec.Cmd
	var cmdStr string
	if config.RunCompiled && config.NeedsCompile {
		switch lang {
		case "java":
			className := strings.TrimSuffix(filepath.Base(targetFile), ".java")
			allArgs := append([]string{"-cp", workDir, className}, args...)
			cmd = exec.CommandContext(ctx, "java", allArgs...)
			cmdStr = fmt.Sprintf("java %s", strings.Join(allArgs, " "))
		default:
			allArgs := append([]string{outputBin}, args...)
			cmd = exec.CommandContext(ctx, outputBin, args...)
			cmdStr = fmt.Sprintf("%s %s", outputBin, strings.Join(allArgs, " "))
		}
	} else {
		allArgs := append(append(config.Args, targetFile), args...)
		cmd = exec.CommandContext(ctx, config.Cmd, allArgs...)
		cmdStr = fmt.Sprintf("%s %s", config.Cmd, strings.Join(allArgs, " "))
	}

	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	result := &CodeRunResult{
		Output:   string(output),
		Duration: duration.String(),
		Command:  cmdStr,
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.Success = false
		result.Error = fmt.Sprintf("Execution timed out after %v", timeout)
		result.ExitCode = -1
		return result, nil
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	} else {
		result.Success = true
	}

	return result, nil
}

func main() {
	s := server.NewMCPServer("code-runner", "1.0.1", server.WithToolCapabilities(true))

	runCodeTool := mcp.NewTool("run_code",
		mcp.WithDescription("Execute code in any supported programming language"),
		mcp.WithString("language", mcp.Required()),
		mcp.WithString("code"),
		mcp.WithString("path"),
		mcp.WithString("file"),
		mcp.WithArray("args"),
		mcp.WithNumber("timeout"),
		mcp.WithString("cwd"),
	)

	s.AddTool(runCodeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := runCode(req.Params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		outputText := fmt.Sprintf(
			"✓ Code execution completed\n\nCommand: %s\nSuccess: %v\nExit Code: %d\nDuration: %s\n\n--- Output ---\n%s",
			result.Command, result.Success, result.ExitCode, result.Duration, result.Output)

		if result.Error != "" {
			outputText += fmt.Sprintf("\n\n--- Error ---\n%s", result.Error)
		}

		return mcp.NewToolResultText(outputText), nil
	})

	os.Stdout.Sync()
	os.Stderr.Sync()

	if err := server.ServeStdio(s); err != nil {
		if strings.Contains(err.Error(), "broken pipe") {
			fmt.Fprintln(os.Stderr, "⚠️  Client disconnected (broken pipe) — exiting gracefully.")
			return
		}
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
