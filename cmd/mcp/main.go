// path: main.go
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// --- Language Configurations ---
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

// --- Server Detection ---
// --- Server Detection ---
var serverRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)listening`),
	regexp.MustCompile(`(?i)server\s+started`),
	regexp.MustCompile(`(?i)running\s+on\s+port`),
	regexp.MustCompile(`(?i)http://`),
	regexp.MustCompile(`(?i)tcp`),
	regexp.MustCompile(`(?i)port\s+\d+`),                      // "port 8080", "Port 3000"
	regexp.MustCompile(`(?i):\d{4,5}`),                        // ":8080", ":3000", ":8000"
	regexp.MustCompile(`(?i)localhost:\d+`),                   // "localhost:8080"
	regexp.MustCompile(`(?i)0\.0\.0\.0:\d+`),                  // "0.0.0.0:8080"
	regexp.MustCompile(`(?i)127\.0\.0\.1:\d+`),                // "127.0.0.1:8080"
	regexp.MustCompile(`(?i)\[\:\:\]:\d+`),                    // "[::]:8080"
	regexp.MustCompile(`(?i)starting\s+(server|application)`), // "starting server", "starting application"
}

func looksLikeServerOutput(s string) bool {
	for _, re := range serverRegexes {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// --- Result Struct ---
type CodeRunResult struct {
	Success  bool   `json:"success"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exitCode"`
	Duration string `json:"duration"`
	Command  string `json:"command"`
}

// --- Core Execution ---
func runCommandWithDetection(ctx context.Context, cmd *exec.Cmd, timeout time.Duration, cmdStr string) *CodeRunResult {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return &CodeRunResult{Success: false, Error: err.Error(), Command: cmdStr}
	}

	var buf bytes.Buffer
	serverDetected := make(chan struct{}, 1)
	done := make(chan error, 1)

	// Read from stdout and stderr in separate goroutines to avoid blocking
	bufMutex := &sync.Mutex{}
	readFromStream := func(stream io.Reader) {
		tmp := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := stream.Read(tmp)
			if n > 0 {
				chunk := string(tmp[:n])
				bufMutex.Lock()
				buf.WriteString(chunk)
				shouldCheck := looksLikeServerOutput(chunk)
				bufMutex.Unlock()

				if shouldCheck {
					select {
					case serverDetected <- struct{}{}:
					case <-ctx.Done():
						return
					default:
					}
				}
			}
			if err != nil {
				return
			}
		}
	}

	go readFromStream(stdout)
	go readFromStream(stderr)

	go func() {
		select {
		case done <- cmd.Wait():
		case <-ctx.Done():
			// Context cancelled, don't block on Wait()
			done <- fmt.Errorf("context cancelled")
		}
	}()

	select {
	case <-ctx.Done():
		// Context cancellation has highest priority
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		stdout.Close()
		stderr.Close()
		// Give a small timeout for goroutines to finish, but don't block indefinitely
		return &CodeRunResult{
			Success:  false,
			Error:    ctx.Err().Error(),
			Command:  cmdStr,
			Duration: time.Since(start).String(),
		}

	case <-serverDetected:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		stdout.Close()
		stderr.Close()
		// Don't use fmt.Println here - stdout is used for MCP protocol communication
		// The success message is already included in the result output
		return &CodeRunResult{
			Success:  true,
			Output:   "Server run successfully 🚀\n" + buf.String(),
			Command:  cmdStr,
			Duration: time.Since(start).String(),
		}

	case <-time.After(timeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		stdout.Close()
		stderr.Close()
		return &CodeRunResult{
			Success:  true,
			Output:   "Server run successfully 🚀 (timeout)\n" + buf.String(),
			Command:  cmdStr,
			Duration: time.Since(start).String(),
		}

	case err := <-done:
		stdout.Close()
		stderr.Close()
		duration := time.Since(start)
		out := buf.String()
		if err != nil {
			return &CodeRunResult{
				Success:  false,
				Error:    err.Error(),
				Output:   out,
				Command:  cmdStr,
				Duration: duration.String(),
			}
		}
		return &CodeRunResult{
			Success:  true,
			Output:   out,
			Command:  cmdStr,
			Duration: duration.String(),
		}
	}
}

func runCode(ctx context.Context, params mcp.CallToolParams) (*CodeRunResult, error) {
	args := params.Arguments.(map[string]any)
	lang := args["language"].(string)
	config, ok := languageConfigs[lang]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	timeout := 10 * time.Second
	if t, ok := args["timeout"].(float64); ok {
		timeout = time.Duration(int(t)) * time.Second
	}

	// Use the passed context, but ensure it has at least the timeout duration
	// If the context already has a shorter deadline, respect it
	ctxDeadline, hasDeadline := ctx.Deadline()
	if !hasDeadline || time.Until(ctxDeadline) > timeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	path, _ := args["path"].(string)
	file, _ := args["file"].(string)
	code, _ := args["code"].(string)

	var target string
	if code != "" {
		tmp, _ := os.CreateTemp("", fmt.Sprintf("mcp-%s-*%s", lang, config.Extension))
		defer os.Remove(tmp.Name())
		tmp.WriteString(code)
		tmp.Close()
		target = tmp.Name()
	} else {
		if path != "" && file != "" {
			target = filepath.Join(path, file)
		} else {
			target = path
		}
	}

	var cmd *exec.Cmd
	if config.NeedsCompile {
		bin := filepath.Join(os.TempDir(), fmt.Sprintf("mcp-bin-%d", time.Now().UnixNano()))
		defer os.Remove(bin)
		compile := exec.CommandContext(ctx, config.Cmd, append(config.CompileArgs, bin, target)...)
		if out, err := compile.CombinedOutput(); err != nil {
			return &CodeRunResult{Success: false, Error: string(out)}, nil
		}
		cmd = exec.CommandContext(ctx, bin)
	} else {
		cmd = exec.CommandContext(ctx, config.Cmd, append(config.Args, target)...)
	}
	return runCommandWithDetection(ctx, cmd, timeout, strings.Join(cmd.Args, " ")), nil
}

// --- MCP Server ---
func main() {
	s := server.NewMCPServer("code-runner", "1.4.1", server.WithToolCapabilities(true))

	runTool := mcp.NewTool("run_code",
		mcp.WithDescription("Runs code in multiple languages; detects servers and terminates safely."),
		mcp.WithString("language", mcp.Required()),
		mcp.WithString("path"),
		mcp.WithString("file"),
		mcp.WithString("code"),
		mcp.WithNumber("timeout"),
	)

	s.AddTool(runTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := runCode(ctx, req.Params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := fmt.Sprintf("✅ %s\n⏱ %s\nSuccess: %v\n\n--- Output ---\n%s",
			res.Command, res.Duration, res.Success, res.Output)
		if res.Error != "" {
			out += fmt.Sprintf("\n\n--- Error ---\n%s", res.Error)
		}
		return mcp.NewToolResultText(out), nil
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
