// main.go
package main

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	// Protocol-Lattice / go-agent
	plagent "github.com/Protocol-Lattice/go-agent"
	"github.com/Protocol-Lattice/go-agent/src/adk"
	adkmodules "github.com/Protocol-Lattice/go-agent/src/adk/modules"
	"github.com/Protocol-Lattice/go-agent/src/memory"
	"github.com/Protocol-Lattice/go-agent/src/models"
	"github.com/Protocol-Lattice/go-agent/src/tools"
)

const appTitle = "✨ Vibe Coder (Multi-Agent TUI) — go-agent"

// ---------- messages ----------
type generateMsg struct {
	agentName string
	text      string
	err       error
}

// parseAgentDirective interprets the optional leading @agent selector in the
// prompt. It returns the resolved agent, the trimmed prompt text, and a flag
// indicating whether the caller explicitly selected an agent.
func parseAgentDirective(input, defaultAgent string, agents map[string]*plagent.Agent) (string, string, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return defaultAgent, "", false
	}
	if !strings.HasPrefix(trimmed, "@") {
		return defaultAgent, trimmed, false
	}

	body := []rune(trimmed[1:])
	if len(body) == 0 {
		return defaultAgent, "", false
	}

	var i int
	for i = 0; i < len(body); i++ {
		r := body[i]
		if unicode.IsSpace(r) || r == ':' {
			break
		}
		body[i] = unicode.ToLower(r)
	}

	agent := strings.ToLower(string(body[:i]))
	if agent == "" {
		return defaultAgent, trimmed, false
	}
	if _, ok := agents[agent]; !ok {
		return defaultAgent, trimmed, false
	}

	rest := strings.TrimLeftFunc(string(body[i:]), func(r rune) bool {
		return unicode.IsSpace(r) || r == ':'
	})
	return agent, strings.TrimSpace(rest), true
}

type resizeMsg struct {
	width, height int
}

// ---------- agents ----------
type agentSpec struct {
	name         string
	role         string
	systemPrompt string
}

var agentSpecs = []agentSpec{
	{
		name: "architect",
		role: "Software Architect",
		systemPrompt: `You are a Software Architect. Your role:
- Design system structure and module organization
- Make high-level technical decisions
- Review code for architectural consistency
- Suggest refactorings and improvements
When updating files, analyze git diff to understand current state.`,
	},
	{
		name: "coder",
		role: "Implementation Engineer",
		systemPrompt: `You are an Implementation Engineer. Your role:
- Write clean, efficient code
- Implement features and bug fixes
- Follow architectural guidelines
- Write tests for your code
When updating files, use git diff to see what changed and build upon it.`,
	},
	{
		name: "reviewer",
		role: "Code Reviewer",
		systemPrompt: `You are a Code Reviewer. Your role:
- Review code quality and style
- Suggest improvements and optimizations
- Check for bugs and edge cases
- Ensure best practices are followed
Use git diff to see recent changes and provide contextual feedback.`,
	},
}

// ---------- model ----------
type focusArea int

const (
	focusOutput focusArea = iota
	focusInput
)

type model struct {
	// UI
	ta        textarea.Model
	vp        viewport.Model
	fileVp    viewport.Model
	spin      spinner.Model
	thinking  bool
	showHelp  bool
	focus     focusArea
	width     int
	height    int
	leftWidth int // file tree pane width

	// Env
	ctx        context.Context
	cancel     context.CancelFunc
	workingDir string

	// Agents
	agents       map[string]*plagent.Agent
	agentMu      sync.RWMutex
	currentAgent string

	// Git
	gitEnabled bool

	// Output buffer
	log strings.Builder
}

func newModel(agents map[string]*plagent.Agent, workingDir string, gitEnabled bool) model {
	ctx, cancel := context.WithCancel(context.Background())

	ta := textarea.New()
	ta.Placeholder = "Describe task. Use @agent to target specific agent (e.g., @coder implement login). Press Enter to send."
	ta.Focus()
	ta.Prompt = "› "
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.SetHeight(6)

	vp := viewport.New(80, 20)
	vp.SetContent(headerView(gitEnabled))

	fileVp := viewport.New(30, 20)
	fileVp.SetContent(renderFileTree(workingDir, 3, 200))

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return model{
		ta:           ta,
		vp:           vp,
		fileVp:       fileVp,
		spin:         sp,
		thinking:     false,
		showHelp:     false,
		focus:        focusInput,
		width:        0,
		height:       0,
		leftWidth:    34,
		ctx:          ctx,
		cancel:       cancel,
		workingDir:   workingDir,
		agents:       agents,
		currentAgent: "architect", // default
		gitEnabled:   gitEnabled,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, spinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.resize(msg.Width, msg.Height)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancel()
			return m, tea.Quit

		case "?":
			m.showHelp = !m.showHelp
			return m, nil

		case "tab":
			if m.focus == focusInput {
				m.focus = focusOutput
				m.ta.Blur()
			} else {
				m.focus = focusInput
				m.ta.Focus()
			}
			return m, nil

		case "1":
			m.currentAgent = "architect"
			return m.appendLine("🎯 Active agent → @architect"), nil
		case "2":
			m.currentAgent = "coder"
			return m.appendLine("🎯 Active agent → @coder"), nil
		case "3":
			m.currentAgent = "reviewer"
			return m.appendLine("🎯 Active agent → @reviewer"), nil

		case "pgup":
			m.vp.LineUp(10)
			return m, nil
		case "pgdown":
			m.vp.LineDown(10)
			return m, nil

		case "enter":
			// only send if input focused and not thinking
			if m.ta.Focused() && !m.thinking {
				raw := m.ta.Value()
				targetAgent, prompt, explicit := parseAgentDirective(raw, m.currentAgent, m.agents)
				prompt = strings.TrimSpace(prompt)
				if prompt == "" {
					return m.appendLine("⚠️  Please type something."), nil
				}

				if explicit && targetAgent != m.currentAgent {
					m.currentAgent = targetAgent
				}

				m.thinking = true
				m.ta.Reset()
				m.appendLine(lipgloss.NewStyle().Faint(true).Render(time.Now().Format("15:04:05")))
				m.appendLine(fmt.Sprintf("💤 You → @%s: %s", targetAgent, prompt))
				m.appendLine(fmt.Sprintf("💭 @%s thinking...", targetAgent))
				m.vp.SetContent(m.log.String())
				return m, m.generate(targetAgent, prompt)
			}
		}

	case generateMsg:
		m.thinking = false
		if msg.err != nil {
			m.appendLine(fmt.Sprintf("❌ @%s error: %v", msg.agentName, msg.err))
			m.vp.SetContent(m.log.String())
			return m, nil
		}
		m.appendLine(fmt.Sprintf("🤖 @%s:", msg.agentName))
		m.appendLine(msg.text)

		// Process code blocks with git-aware updates
		blocks := extractCodeBlocks(msg.text)
		if len(blocks) == 0 {
			m.appendLine("ℹ️  No code blocks found.")
		}
		for _, b := range blocks {
			path := b.Path
			if path == "" {
				path = guessPathFromContent(m.workingDir, b.Lang, b.Code)
			} else {
				path = filepath.Join(m.workingDir, filepath.Clean(path))
			}

			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				m.appendLine(fmt.Sprintf("❌ mkdir %s: %v", filepath.Dir(path), err))
				continue
			}

			fileExists := false
			if _, err := os.Stat(path); err == nil {
				fileExists = true
			}

			if err := os.WriteFile(path, []byte(b.Code+"\n"), 0o644); err != nil {
				m.appendLine(fmt.Sprintf("❌ write %s: %v", path, err))
				continue
			}

			relPath := relOrSame(m.workingDir, path)
			if fileExists {
				m.appendLine(fmt.Sprintf("✏️  updated %s", relPath))
				if m.gitEnabled {
					if diff := getGitDiff(m.workingDir, path); diff != "" {
						m.appendLine(fmt.Sprintf("📊 diff:\n%s", diff))
					}
				}
			} else {
				m.appendLine(fmt.Sprintf("💾 created %s", relPath))
			}
		}

		// Commit changes if git is enabled
		if m.gitEnabled && len(blocks) > 0 {
			if err := gitCommit(m.workingDir, fmt.Sprintf("@%s: %s", msg.agentName, msg.text[:min(50, len(msg.text))])); err == nil {
				m.appendLine("✅ changes committed to git")
			}
		}

		// Refresh file tree after writes
		m.fileVp.SetContent(renderFileTree(m.workingDir, 3, 200))
		m.vp.SetContent(m.log.String())
		return m, nil

	case spinner.TickMsg:
		if m.thinking {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// delegate to textarea
	var cmd tea.Cmd
	if m.focus == focusInput {
		m.ta, cmd = m.ta.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	title := lipgloss.NewStyle().Bold(true).Render(appTitle)
	subtitle := lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("Agents: %s", strings.Join([]string{"@architect", "@coder", "@reviewer"}, " • ")))
	help := lipgloss.NewStyle().Faint(true).Render("Enter = send • 1/2/3 = switch agent • Tab = focus • PgUp/PgDn = scroll • ? = help • Esc/Ctrl+C = quit")

	status := ""
	if m.thinking {
		status = " " + m.spin.View() + " thinking…"
	}

	// header & status bars
	header := lipgloss.JoinVertical(lipgloss.Top, title+status, subtitle, "")

	left := lipgloss.NewStyle().Width(m.leftWidth).Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(
		lipgloss.NewStyle().Bold(true).Render("Project") + "\n" + shortPath(m.workingDir) + "\n\n" + m.fileVp.View(),
	)

	right := lipgloss.NewStyle().Width(max(20, m.width-m.leftWidth-4)).Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(m.vp.View())

	editor := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(m.ta.View())

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	statusBar := lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf(
		"DIR: %s  •  AGENT: @%s  •  GIT: %s",
		shortPath(m.workingDir), m.currentAgent, onOff(m.gitEnabled),
	))

	if m.showHelp {
		return lipgloss.JoinVertical(
			lipgloss.Top,
			header,
			helpOverlay(),
			statusBar,
		)
	}

	return lipgloss.JoinVertical(
		lipgloss.Top,
		header,
		body,
		"",
		editor,
		"",
		help,
		statusBar,
	)
}

// responsive sizing
func (m model) resize(w, h int) (tea.Model, tea.Cmd) {
	if w <= 0 || h <= 0 {
		return m, nil
	}

	// Reserve space:
	// - header ~3 lines, status ~1, help ~1, editor height m.ta.Height()
	reserved := 3 + 1 + 1 + m.ta.Height() + 3 // padding
	avail := max(5, h-reserved)

	m.fileVp.Width = m.leftWidth - 4
	m.fileVp.Height = avail

	m.vp.Width = max(20, w-m.leftWidth-6)
	m.vp.Height = avail

	m.ta.SetWidth(max(20, w-6))
	return m, nil
}

// ---------- prompts & generation ----------
func (m model) generate(agentName, userPrompt string) tea.Cmd {
	return func() tea.Msg {
		m.agentMu.RLock()
		agent, ok := m.agents[agentName]
		m.agentMu.RUnlock()

		if !ok {
			return generateMsg{agentName, "", fmt.Errorf("agent not found: %s", agentName)}
		}

		contextPrompt := buildContextPrompt(m.workingDir, m.gitEnabled, userPrompt)

		resp, err := agent.Generate(m.ctx, fmt.Sprintf("%s-session", agentName), contextPrompt)
		if err != nil {
			return generateMsg{agentName, "", err}
		}
		text := fmt.Sprintf("%v", resp)
		return generateMsg{agentName, text, nil}
	}
}

// ---------- file tree helpers ----------
func renderFileTree(root string, maxDepth, maxEntries int) string {
	type node struct {
		name  string
		isDir bool
		depth int
		path  string
	}
	var lines []string
	seen := 0

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator))
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// skip noisy dirs
		if d.IsDir() {
			base := d.Name()
			switch base {
			case ".git", "node_modules", "vendor", ".idea", "dist", "build":
				return filepath.SkipDir
			}
		}
		prefix := strings.Repeat("  ", depth)
		marker := "📄"
		if d.IsDir() {
			marker = "📁"
		}
		lines = append(lines, fmt.Sprintf("%s%s %s", prefix, marker, d.Name()))
		seen++
		if seen >= maxEntries {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "(unable to read tree)"
	}
	if len(lines) == 0 {
		return "(empty project)"
	}
	return strings.Join(lines, "\n")
}

func listProjectFiles(dir string) string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".idea" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(dir, path); err == nil {
			files = append(files, rel)
		}
		return nil
	})

	if len(files) == 0 {
		return ""
	}
	if len(files) > 50 {
		files = files[:50]
		files = append(files, "... (truncated)")
	}
	return strings.Join(files, "\n")
}

// ---------- git helpers ----------
func getGitDiff(workingDir, filePath string) string {
	cmd := exec.Command("git", "diff", "HEAD", filePath)
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	diff := string(out)
	lines := strings.Split(diff, "\n")
	if len(lines) > 20 {
		lines = lines[:20]
		lines = append(lines, "... (truncated)")
	}
	return strings.Join(lines, "\n")
}

func getRecentGitLog(workingDir string, n int) string {
	cmd := exec.Command("git", "log", fmt.Sprintf("-%d", n), "--oneline", "--decorate")
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func gitCommit(workingDir, message string) error {
	// Stage all changes
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = workingDir
	if err := cmd.Run(); err != nil {
		return err
	}
	// Commit
	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = workingDir
	return cmd.Run()
}

func initGitRepo(workingDir string) error {
	// Check if already a git repo
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = workingDir
	if err := cmd.Run(); err == nil {
		return nil // already initialized
	}
	// Initialize git repo
	cmd = exec.Command("git", "init")
	cmd.Dir = workingDir
	if err := cmd.Run(); err != nil {
		return err
	}
	// Create initial commit
	cmd = exec.Command("git", "commit", "--allow-empty", "-m", "Initial commit")
	cmd.Dir = workingDir
	return cmd.Run()
}

// ---------- code extraction & path guessing ----------
type codeBlock struct {
	Lang string
	Path string
	Code string
}

var fenceRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_+-]*)\\s*\\n(.*?)\\n```")
var pathHintLineRe = regexp.MustCompile(`(?i)^\s*(?://|#|;|--|/\*|\*)\s*(?:path|file)\s*:\s*([^\s*]+)`)

func extractCodeBlocks(s string) []codeBlock {
	matches := fenceRe.FindAllStringSubmatch(s, -1)
	var out []codeBlock
	for _, m := range matches {
		lang := strings.ToLower(strings.TrimSpace(m[1]))
		code := strings.TrimSpace(m[2])

		path := ""
		lines := strings.Split(code, "\n")
		max := len(lines)
		if max > 6 {
			max = 6
		}
		for i := 0; i < max; i++ {
			if p := findPathHint(lines[i]); p != "" {
				path = p
				break
			}
		}
		out = append(out, codeBlock{Lang: lang, Path: path, Code: code})
	}
	return out
}

func findPathHint(line string) string {
	if m := pathHintLineRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func guessPathFromContent(dir, lang, code string) string {
	switch lang {
	case "go", "golang":
		return guessGoPath(dir, code)
	case "ts", "tsx":
		base := guessByTopSymbol(code, ".ts")
		if strings.Contains(code, "from \"react\"") || strings.Contains(code, "from 'react'") || strings.Contains(code, "tsx") {
			return filepath.Join(dir, "web", strings.TrimSuffix(base, ".ts")+".tsx")
		}
		return filepath.Join(dir, "web", base)
	case "js", "jsx":
		return filepath.Join(dir, "web", guessByTopSymbol(code, ".js"))
	case "py":
		return filepath.Join(dir, "app", guessByTopSymbol(code, ".py"))
	case "rs":
		return filepath.Join(dir, "rust", guessByTopSymbol(code, ".rs"))
	case "html":
		return filepath.Join(dir, "web", "index.html")
	case "css":
		return filepath.Join(dir, "web", "styles.css")
	case "yaml", "yml":
		name := yamlRootName(code)
		if name == "" {
			name = "config"
		}
		return filepath.Join(dir, "config", name+".yaml")
	case "json":
		return filepath.Join(dir, "config", "config.json")
	case "md", "markdown":
		return filepath.Join(dir, "docs", "README.md")
	case "sh", "bash":
		return filepath.Join(dir, "scripts", "run.sh")
	default:
		h := sha1.Sum([]byte(code))
		return filepath.Join(dir, "snippets", "snippet_"+hex.EncodeToString(h[:6])+".txt")
	}
}

var (
	pkgRe       = regexp.MustCompile(`(?m)^\s*package\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*$`)
	mainFuncRe  = regexp.MustCompile(`(?m)^\s*func\s+main\s*\(`)
	typeRe      = regexp.MustCompile(`(?m)^\s*type\s+([A-Z][A-Za-z0-9_]*)\s+struct`)
	funcRe      = regexp.MustCompile(`(?m)^\s*func\s+([A-Z][A-Za-z0-9_]*)\s*\(`)
	importHTTP  = regexp.MustCompile(`(?m)^\s*\"net/http\"`)
	importGRPC  = regexp.MustCompile(`(?m)^\s*\"google.golang.org/grpc\"`)
	importCobra = regexp.MustCompile(`(?m)^\s*\"github.com/spf13/cobra\"`)
)

func guessGoPath(dir, code string) string {
	pkg := firstGroup(pkgRe, code)
	if pkg == "" {
		pkg = "main"
	}
	if pkg == "main" && mainFuncRe.MatchString(code) {
		return filepath.Join(dir, "cmd", "app", "main.go")
	}

	switch {
	case importHTTP.MatchString(code):
		return filepath.Join(dir, "internal", pkg, "http_server.go")
	case importGRPC.MatchString(code):
		return filepath.Join(dir, "internal", pkg, "grpc_server.go")
	case importCobra.MatchString(code):
		return filepath.Join(dir, "cmd", kebab(pkg), "main.go")
	}

	if t := firstGroup(typeRe, code); t != "" {
		return filepath.Join(dir, "internal", pkg, snake(t)+".go")
	}
	if f := firstGroup(funcRe, code); f != "" {
		return filepath.Join(dir, "internal", pkg, snake(f)+".go")
	}
	return filepath.Join(dir, "internal", pkg, "util.go")
}

func firstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func yamlRootName(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
		if strings.HasPrefix(line, "kind:") {
			return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "kind:")))
		}
	}
	return ""
}

func snake(s string) string {
	var out []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out = append(out, '_', r+('a'-'A'))
		} else {
			out = append(out, rune(strings.ToLower(string(r))[0]))
		}
	}
	return string(out)
}

func kebab(s string) string {
	return strings.ReplaceAll(snake(s), "_", "-")
}

func relOrSame(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(r, "..") {
		return p
	}
	return r
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func shortPath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// ---------- build agent ----------
func buildAgent(ctx context.Context, spec agentSpec) (*plagent.Agent, error) {
	embedder := memory.AutoEmbedder()
	opts := memory.DefaultOptions()

	builder, err := adk.New(
		ctx,
		adk.WithDefaultSystemPrompt(spec.systemPrompt),
		adk.WithModules(
			adkmodules.InMemoryMemoryModule(
				512,
				embedder,
				&opts,
			),
			adkmodules.NewModelModule("gemini", func(_ context.Context) (models.Agent, error) {
				return models.NewGeminiLLM(ctx, "gemini-2.5-pro", spec.systemPrompt)
			}),
			adkmodules.NewToolModule("essentials",
				adkmodules.StaticToolProvider([]plagent.Tool{&tools.EchoTool{}}, nil),
			),
		),
	)
	if err != nil {
		return nil, err
	}
	return builder.BuildAgent(ctx)
}

// ---------- header & help ----------
func headerView(gitEnabled bool) string {
	mono := lipgloss.NewStyle().Faint(true)
	gitStatus := "enabled"
	if !gitEnabled {
		gitStatus = "disabled"
	}
	lines := []string{
		appTitle,
		fmt.Sprintf("Multi-agent collaborative coding system (git: %s)", gitStatus),
		"",
		mono.Render("Available Agents:"),
		mono.Render("  @architect - System design and architecture  (1)"),
		mono.Render("  @coder     - Feature implementation         (2)"),
		mono.Render("  @reviewer  - Code review and quality        (3)"),
		"",
	}
	return strings.Join(lines, "\n")
}

func helpOverlay() string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).Render(strings.Join([]string{
		"🆘 Help",
		"",
		"Enter  : send prompt",
		"1/2/3  : switch active agent (@architect/@coder/@reviewer)",
		"Tab    : toggle focus (editor ↔ output)",
		"PgUp/Dn: scroll output",
		"?      : toggle this help",
		"Esc    : quit",
	}, "\n"))
}

// ---------- main ----------
func main() {
	var workdir string
	var enableGit bool
	var askDir bool

	flag.StringVar(&workdir, "dir", ".", "Directory to save generated files")
	flag.BoolVar(&askDir, "ask-dir", false, "Interactively choose working directory before starting")
	flag.BoolVar(&enableGit, "git", true, "Enable git integration for tracking changes")
	flag.Parse()

	// Interactive choose-dir if requested
	if askDir {
		fmt.Printf("Choose working directory [%s]: ", workdir)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			workdir = line
		}
	}

	absDir, err := filepath.Abs(workdir)
	if err != nil {
		fmt.Println("resolve dir:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		fmt.Println("mkdir:", err)
		os.Exit(1)
	}

	// Initialize git if enabled
	if enableGit {
		if err := initGitRepo(absDir); err != nil {
			fmt.Printf("git init warning: %v (continuing without git)\n", err)
			enableGit = false
		}
	}

	ctx := context.Background()

	// Build all agents
	fmt.Println("Initializing agents...")
	agents := make(map[string]*plagent.Agent)
	for _, spec := range agentSpecs {
		fmt.Printf("  - %s (%s)\n", spec.name, spec.role)
		agent, err := buildAgent(ctx, spec)
		if err != nil {
			fmt.Printf("failed to build agent %s: %v\n", spec.name, err)
			fmt.Println("Ensure GEMINI/GOOGLE_API_KEY is set.")
			os.Exit(1)
		}
		agents[spec.name] = agent
	}
	fmt.Println("✅ All agents ready!")
	fmt.Println()

	if _, err := tea.NewProgram(newModel(agents, absDir, enableGit)).Run(); err != nil {
		fmt.Println("run:", err)
		os.Exit(1)
	}
}

// appendLine appends a line to the output log and refreshes the viewport.
// Value receiver returns an updated copy so callers can `return m.appendLine("..."), nil`.
func (m model) appendLine(s string) model {
	m.log.WriteString(s)
	if !strings.HasSuffix(s, "\n") {
		m.log.WriteString("\n")
	}
	m.vp.SetContent(m.log.String())
	return m
}

// guessByTopSymbol returns a sensible filename (with ext) based on the first
// prominent top-level symbol in the code (function/class/const/etc.).
// Falls back to a short content hash.
func guessByTopSymbol(code, ext string) string {
	trim := strings.TrimSpace
	var name string

	// JS/TS
	if m := regexp.MustCompile(`(?m)^\s*export\s+default\s+function\s+([A-Za-z_]\w*)`).FindStringSubmatch(code); m != nil {
		name = m[1]
	} else if m := regexp.MustCompile(`(?m)^\s*export\s+function\s+([A-Za-z_]\w*)`).FindStringSubmatch(code); m != nil {
		name = m[1]
	} else if m := regexp.MustCompile(`(?m)^\s*(?:export\s+)?class\s+([A-Za-z_]\w*)`).FindStringSubmatch(code); m != nil {
		name = m[1]
	} else if m := regexp.MustCompile(`(?m)^\s*(?:export\s+)?const\s+([A-Za-z_]\w*)\s*=\s*(?:async\s*)?(?:function\b|\()`).FindStringSubmatch(code); m != nil {
		name = m[1]
	} else if m := regexp.MustCompile(`(?m)^\s*(?:export\s+)?interface\s+([A-Za-z_]\w*)`).FindStringSubmatch(code); m != nil {
		name = m[1]
	}

	// Python
	if name == "" {
		if m := regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z_]\w*)\s*:`).FindStringSubmatch(code); m != nil {
			name = m[1]
		} else if m := regexp.MustCompile(`(?m)^\s*def\s+([A-Za-z_]\w*)\s*\(`).FindStringSubmatch(code); m != nil {
			name = m[1]
		}
	}

	// Rust
	if name == "" {
		if m := regexp.MustCompile(`(?m)^\s*(?:pub\s+)?struct\s+([A-Za-z_]\w*)`).FindStringSubmatch(code); m != nil {
			name = m[1]
		} else if m := regexp.MustCompile(`(?m)^\s*(?:pub\s+)?enum\s+([A-Za-z_]\w*)`).FindStringSubmatch(code); m != nil {
			name = m[1]
		} else if m := regexp.MustCompile(`(?m)^\s*(?:pub\s+)?fn\s+([A-Za-z_]\w*)\s*\(`).FindStringSubmatch(code); m != nil && trim(m[1]) != "main" {
			name = m[1]
		}
	}

	if name == "" {
		// fallback to short hash
		sum := sha1.Sum([]byte(code))
		return "snippet_" + hex.EncodeToString(sum[:3]) + ext
	}

	base := kebab(name) // reuse your helper to make filenames nice
	return base + ext
}
func buildContextPrompt(workingDir string, gitEnabled bool, userPrompt string) string {
	var context strings.Builder

	// --- Project structure summary ---
	context.WriteString("Current project structure:\n")
	if files := listProjectFiles(workingDir); files != "" {
		context.WriteString(files)
	} else {
		context.WriteString("(empty project)\n")
	}
	context.WriteString("\n")

	// --- Recent Git history ---
	if gitEnabled {
		if diff := getRecentGitLog(workingDir, 5); diff != "" {
			context.WriteString("Recent git history:\n")
			context.WriteString(diff)
			context.WriteString("\n")
		}
	}

	// --- Analyze Go imports in project ---
	imports := extractGoImports(workingDir)
	if len(imports) > 0 {
		context.WriteString("Relevant Go imports and documentation:\n")
		for _, imp := range imports {
			context.WriteString(fmt.Sprintf("\n📦 %s:\n", imp))

			// Verify repo + subpath existence via GitMCP tree
			verify := verifyGitMCPModulePath(imp)
			context.WriteString(verify + "\n")

			// Skip imports that don't exist in the repo
			if !strings.HasPrefix(verify, "✅") {
				context.WriteString("(skipped: module not found in GitMCP repository tree)\n")
				continue
			}

			// Fetch docs and API index only if module confirmed
			docs := fetchGitMCPDocs(imp)
			context.WriteString(docs)
			context.WriteString("\n")

			api := fetchGitMCPModuleIndex(imp)
			if api != "" {
				context.WriteString("📚 API Summary:\n")
				context.WriteString(api)
				context.WriteString("\n")
			}
		}
		context.WriteString("\n")
	}

	// --- Detect user-referenced GitHub packages in prompt ---
	pkgRe := regexp.MustCompile(`github\.com/[A-Za-z0-9_.\-]+/[A-Za-z0-9_.\-]+`)
	if match := pkgRe.FindString(userPrompt); match != "" {
		context.WriteString(fmt.Sprintf("User referenced package: %s\n", match))

		verify := verifyGitMCPModulePath(match)
		context.WriteString(verify + "\n")

		if strings.HasPrefix(verify, "✅") {
			docs := fetchGitMCPDocs(match)
			context.WriteString(docs)
			context.WriteString("\n")

			api := fetchGitMCPModuleIndex(match)
			if api != "" {
				context.WriteString("📚 API Summary:\n")
				context.WriteString(api)
				context.WriteString("\n")
			}
		} else {
			context.WriteString("(skipped: module not found in GitMCP repository tree)\n")
		}
		context.WriteString("\n\n")
	}

	// --- Task section ---
	context.WriteString("Task: ")
	context.WriteString(userPrompt)
	context.WriteString("\n\n")

	context.WriteString("Always use fenced code blocks with language and optional path hints:\n")
	context.WriteString("```go\n// path: internal/server/handler.go\n// code...\n```\n")

	return context.String()
}

// verifyGitMCPModulePath checks if a given module or subpath exists
// by querying gitmcp.io/<org>/<repo>/tree and scanning its contents.
func verifyGitMCPModulePath(repoURL string) string {
	repoURL = strings.TrimPrefix(repoURL, "https://")
	repoURL = strings.TrimPrefix(repoURL, "http://")
	repoURL = strings.TrimPrefix(repoURL, "www.")
	repoURL = strings.TrimPrefix(repoURL, "github.com/")
	repoURL = strings.TrimSuffix(repoURL, "/")
	if repoURL == "" {
		return "(invalid repo)"
	}

	parts := strings.SplitN(repoURL, "/", 3)
	if len(parts) < 2 {
		return "(invalid repo path)"
	}
	org, repo := parts[0], parts[1]
	subPath := ""
	if len(parts) == 3 {
		subPath = parts[2]
	}

	cacheDir := filepath.Join(".cache", "gitmcp")
	_ = os.MkdirAll(cacheDir, 0o755)
	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("%s_%s_tree.json", org, repo))

	var treeData []byte
	if data, err := os.ReadFile(cacheFile); err == nil && len(data) > 0 {
		treeData = data
	} else {
		treeURL := fmt.Sprintf("https://gitmcp.io/%s/%s/tree", org, repo)
		resp, err := http.Get(treeURL)
		if err != nil {
			return fmt.Sprintf("(⚠️ failed to fetch tree: %v)", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Sprintf("(⚠️ MCP tree returned %s)", resp.Status)
		}
		treeData, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = os.WriteFile(cacheFile, treeData, 0o644)
	}

	treeStr := string(treeData)
	if subPath == "" {
		return "✅ repo root exists"
	}

	// Check if the subpath (e.g. pkg/agent) exists anywhere in the tree JSON
	if strings.Contains(treeStr, "\""+subPath+"\"") {
		return fmt.Sprintf("✅ module '%s' found", subPath)
	}
	return fmt.Sprintf("⚠️ module '%s' not found", subPath)
}

// fetchGitMCPDocs fetches repository docs through gitmcp.io proxy.
// Example: repoURL = "github.com/Protocol-Lattice/go-agent"
func fetchGitMCPDocs(repoURL string) string {
	repoURL = strings.TrimPrefix(repoURL, "https://")
	repoURL = strings.TrimPrefix(repoURL, "http://")
	repoURL = strings.TrimPrefix(repoURL, "www.")
	repoURL = strings.TrimPrefix(repoURL, "github.com/")
	repoURL = strings.TrimSuffix(repoURL, "/")
	if repoURL == "" {
		return ""
	}

	cacheDir := filepath.Join(".cache", "gitmcp")
	_ = os.MkdirAll(cacheDir, 0o755)
	cacheFile := filepath.Join(cacheDir, strings.ReplaceAll(repoURL, "/", "_")+".txt")

	if data, err := os.ReadFile(cacheFile); err == nil && len(data) > 0 {
		return string(data)
	}

	mcpURL := fmt.Sprintf("https://gitmcp.io/%s/docs", repoURL)
	resp, err := http.Get(mcpURL)
	if err != nil {
		return fmt.Sprintf("(⚠️ failed to fetch docs: %v)\n", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("(⚠️ MCP returned %s)\n", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "(⚠️ read error while fetching docs)\n"
	}

	_ = os.WriteFile(cacheFile, body, 0o644)
	return string(body)
}

// fetchGitMCPModuleIndex tries to retrieve package and symbol listings (API index)
// for the given Go repo using the gitmcp.io introspection endpoint.
func fetchGitMCPModuleIndex(repoURL string) string {
	repoURL = strings.TrimPrefix(repoURL, "https://")
	repoURL = strings.TrimPrefix(repoURL, "http://")
	repoURL = strings.TrimPrefix(repoURL, "www.")
	repoURL = strings.TrimPrefix(repoURL, "github.com/")
	repoURL = strings.TrimSuffix(repoURL, "/")
	if repoURL == "" {
		return ""
	}

	cacheDir := filepath.Join(".cache", "gitmcp")
	_ = os.MkdirAll(cacheDir, 0o755)
	cacheFile := filepath.Join(cacheDir, strings.ReplaceAll(repoURL, "/", "_")+"_pkg.txt")

	if data, err := os.ReadFile(cacheFile); err == nil && len(data) > 0 {
		return string(data)
	}

	urls := []string{
		fmt.Sprintf("https://gitmcp.io/%s/pkg", repoURL),
		fmt.Sprintf("https://gitmcp.io/%s/docs", repoURL), // fallback
	}

	for _, u := range urls {
		resp, err := http.Get(u)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		_ = os.WriteFile(cacheFile, body, 0o644)
		return string(body)
	}

	return "(⚠️ failed to fetch module index)"
}

// extractGoImports walks through the working directory and extracts all Go import paths.
func extractGoImports(root string) []string {
	var imports = make(map[string]struct{})
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		// Simple regex to capture import paths.
		// Handles single and grouped imports.
		re := regexp.MustCompile(`(?m)^\s*import\s*(?:\(\s*([^)]*)\)|"([^"]+)")`)
		for _, m := range re.FindAllStringSubmatch(string(content), -1) {
			// Grouped imports
			if m[1] != "" {
				lines := strings.Split(m[1], "\n")
				for _, l := range lines {
					l = strings.Trim(strings.TrimSpace(l), `"`)
					if strings.HasPrefix(l, "github.com/") {
						imports[l] = struct{}{}
					}
				}
			}
			// Single import
			if m[2] != "" && strings.HasPrefix(m[2], "github.com/") {
				imports[m[2]] = struct{}{}
			}
		}
		return nil
	})

	out := make([]string, 0, len(imports))
	for imp := range imports {
		out = append(out, imp)
	}
	return out
}
