// path: main.go
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

type CodeRunResult struct {
	Success  bool   `json:"success"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exitCode"`
	Duration string `json:"duration"`
	Command  string `json:"command"`
}

func runCode(params mcp.CallToolParams) (*CodeRunResult, error) {
	argsMap := params.Arguments.(map[string]any)
	lang := argsMap["language"].(string)
	config, ok := languageConfigs[lang]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	timeout := 15 * time.Second
	if t, ok := argsMap["timeout"].(float64); ok {
		timeout = time.Duration(int(t)) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	code, _ := argsMap["code"].(string)
	path, _ := argsMap["path"].(string)
	file, _ := argsMap["file"].(string)
	cwd, _ := argsMap["cwd"].(string)
	argsAny, _ := argsMap["args"].([]any)

	runArgs := make([]string, len(argsAny))
	for i, v := range argsAny {
		runArgs[i] = fmt.Sprintf("%v", v)
	}

	var targetFile string
	if code != "" {
		tmpFile, err := os.CreateTemp("", fmt.Sprintf("mcp-code-*%s", config.Extension))
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmpFile.Name())
		tmpFile.WriteString(code)
		tmpFile.Close()
		targetFile = tmpFile.Name()
	} else if path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("invalid path: %v", err)
		}
		if info.IsDir() {
			if file == "" {
				return nil, fmt.Errorf("file required for directory path")
			}
			targetFile = filepath.Join(path, file)
		} else {
			targetFile = path
		}
	} else {
		return nil, fmt.Errorf("either code or path required")
	}

	if cwd != "" {
		os.Chdir(cwd)
	}

	start := time.Now()
	var cmd *exec.Cmd
	var cmdStr string
	var outputBin string

	if config.NeedsCompile {
		outputBin = filepath.Join(os.TempDir(), fmt.Sprintf("mcp-compiled-%d", time.Now().UnixNano()))
		defer os.Remove(outputBin)

		var compileArgs []string
		switch lang {
		case "c", "cpp":
			compileArgs = append(config.CompileArgs, outputBin, targetFile)
		case "rust":
			compileArgs = []string{targetFile, "-o", outputBin}
		case "java":
			compileArgs = []string{targetFile}
		}

		compileCmd := exec.CommandContext(ctx, config.Cmd, compileArgs...)
		compileOut, err := compileCmd.CombinedOutput()
		if err != nil {
			return &CodeRunResult{Success: false, Error: string(compileOut), ExitCode: 1}, nil
		}
	}

	if config.RunCompiled && config.NeedsCompile {
		if lang == "java" {
			className := strings.TrimSuffix(filepath.Base(targetFile), ".java")
			allArgs := append([]string{"-cp", filepath.Dir(targetFile), className}, runArgs...)
			cmd = exec.CommandContext(ctx, "java", allArgs...)
			cmdStr = fmt.Sprintf("java %s", strings.Join(allArgs, " "))
		} else {
			allArgs := append([]string{outputBin}, runArgs...)
			cmd = exec.CommandContext(ctx, outputBin, runArgs...)
			cmdStr = fmt.Sprintf("%s %s", outputBin, strings.Join(allArgs, " "))
		}
	} else {
		allArgs := append(append(config.Args, targetFile), runArgs...)
		cmd = exec.CommandContext(ctx, config.Cmd, allArgs...)
		cmdStr = fmt.Sprintf("%s %s", config.Cmd, strings.Join(allArgs, " "))
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return &CodeRunResult{Success: false, Error: err.Error(), ExitCode: 1}, nil
	}

	outputChan := make(chan string)
	go func() {
		out := new(strings.Builder)
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			out.WriteString(scanner.Text() + "\n")
		}
		scannerErr := bufio.NewScanner(stderrPipe)
		for scannerErr.Scan() {
			out.WriteString(scannerErr.Text() + "\n")
		}
		outputChan <- out.String()
	}()

	select {
	case <-time.After(2 * time.Second):
		// assume it’s a running server if still alive
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		duration := time.Since(start)
		return &CodeRunResult{
			Success:  true,
			Output:   "Server run successfully 🚀",
			Duration: duration.String(),
			Command:  cmdStr,
			ExitCode: 0,
		}, nil

	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return &CodeRunResult{
			Success:  false,
			Error:    fmt.Sprintf("Execution timed out after %v", timeout),
			Command:  cmdStr,
			ExitCode: -1,
		}, nil

	case out := <-outputChan:
		_ = cmd.Wait()
		duration := time.Since(start)
		result := &CodeRunResult{
			Output:   out,
			Duration: duration.String(),
			Command:  cmdStr,
		}

		if err := ctx.Err(); err == context.DeadlineExceeded {
			result.Success = false
			result.Error = fmt.Sprintf("Execution timed out after %v", timeout)
			result.ExitCode = -1
		} else {
			result.Success = true
		}

		return result, nil
	}
}

func main() {
	s := server.NewMCPServer("code-runner", "1.0.3", server.WithToolCapabilities(true))

	runTool := mcp.NewTool("run_code",
		mcp.WithDescription("Run code in various programming languages with timeout handling and server detection"),
		mcp.WithString("language", mcp.Required()),
		mcp.WithString("code"),
		mcp.WithString("path"),
		mcp.WithString("file"),
		mcp.WithArray("args"),
		mcp.WithNumber("timeout"),
		mcp.WithString("cwd"),
	)

	s.AddTool(runTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := runCode(req.Params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		text := fmt.Sprintf("✅ Command: %s\n⏱ Duration: %s\nSuccess: %v\n\n--- Output ---\n%s",
			result.Command, result.Duration, result.Success, result.Output)
		if result.Error != "" {
			text += fmt.Sprintf("\n\n--- Error ---\n%s", result.Error)
		}
		return mcp.NewToolResultText(text), nil
	})

	if err := server.ServeStdio(s); err != nil {
		if strings.Contains(err.Error(), "broken pipe") {
			fmt.Fprintln(os.Stderr, "⚠️ Client disconnected — exiting gracefully.")
			return
		}
		fmt.Fprintf(os.Stderr, "❌ Server error: %v\n", err)
		os.Exit(1)
	}
}
