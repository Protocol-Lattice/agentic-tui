// @path main.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	utcp "github.com/universal-tool-calling-protocol/go-utcp"

	agent "github.com/Protocol-Lattice/go-agent"
	adk "github.com/Protocol-Lattice/go-agent/src/adk"
	"github.com/Protocol-Lattice/go-agent/src/adk/modules"
	adkmodules "github.com/Protocol-Lattice/go-agent/src/adk/modules"
	"github.com/Protocol-Lattice/go-agent/src/memory"
	"github.com/Protocol-Lattice/go-agent/src/models"
	"github.com/Protocol-Lattice/go-agent/src/tools"
)

// -----------------------------------------------------------------------------
// MODEL & TYPES
// -----------------------------------------------------------------------------

type mode int

const (
	modeDir mode = iota
	modeList
	modePrompt
	modeThinking
	modeDone
	modeResult
	modeUTCP
	modeUTCPArgs
	modeStepBuild
)

type plugin struct{ name, desc string }

func (p plugin) Title() string       { return p.name }
func (p plugin) Description() string { return p.desc }
func (p plugin) FilterValue() string { return p.name }

type dirItem struct {
	name string
	path string
}

func (d dirItem) Title() string       { return d.name }
func (d dirItem) Description() string { return d.path }
func (d dirItem) FilterValue() string { return d.name }

type utcpItem struct {
	name, provider, desc string
	stream               bool
}

func (u utcpItem) Title() string       { return u.name }
func (u utcpItem) Description() string { return fmt.Sprintf("[%s] %s", u.provider, u.desc) }
func (u utcpItem) FilterValue() string { return u.name }

type thinkingMsg string
type doneMsg struct{}
type generateMsg struct {
	text string
	err  error
}

type model struct {
	ctx          context.Context
	agent        *agent.Agent
	utcp         utcp.UtcpClientInterface
	working      string
	history      []string
	mode         mode
	prevMode     mode
	selected     plugin
	selectedUTCP utcpItem
	list         list.Model
	dirlist      list.Model
	textarea     textarea.Model
	thinking     string
	output       string
	width        int
	height       int
	style        styles

	// Context snapshot stats (set on each run)
	contextFiles int
	contextBytes int64
}

type styles struct {
	header  lipgloss.Style
	subtle  lipgloss.Style
	border  lipgloss.Style
	accent  lipgloss.Style
	error   lipgloss.Style
	success lipgloss.Style
	center  lipgloss.Style
	footer  lipgloss.Style
}

// -----------------------------------------------------------------------------
// INIT MODEL
// -----------------------------------------------------------------------------

func newModel(ctx context.Context, a *agent.Agent, u utcp.UtcpClientInterface, startDir string) *model {
	dirItems := loadDirs(startDir)
	dirDelegate := list.NewDefaultDelegate()
	dirList := list.New(dirItems, dirDelegate, 0, 0)
	dirList.Title = "Choose Working Directory"
	dirList.SetShowHelp(false)
	dirList.SetShowStatusBar(false)
	dirList.SetFilteringEnabled(false)

	items := []list.Item{
		plugin{"orchestrator", "Split into subtasks and execute sequentially"},
		plugin{"architect", "High-level design and refactoring"},
		plugin{"coder", "Feature implementation and tests"},
		plugin{"reviewer", "Code review and optimization"},
		plugin{"utcp", "Explore connected UTCP tools"},
	}
	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, 0, 0)
	l.Title = "Agents"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	ta := textarea.New()
	ta.Placeholder = "Describe your task or goal... (tip: start with `split:` to auto-orchestrate)"
	ta.SetHeight(7)

	st := styles{
		header:  lipgloss.NewStyle().Foreground(lipgloss.Color("#00E6B8")).Bold(true),
		subtle:  lipgloss.NewStyle().Foreground(lipgloss.Color("#999999")),
		border:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3),
		accent:  lipgloss.NewStyle().Foreground(lipgloss.Color("#AD8CFF")),
		error:   lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5C5C")).Bold(true),
		success: lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC97")).Bold(true),
		center:  lipgloss.NewStyle().Align(lipgloss.Center).Padding(2, 0),
		footer:  lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")).Faint(true).Align(lipgloss.Center),
	}

	return &model{
		ctx:      ctx,
		agent:    a,
		utcp:     u,
		working:  startDir,
		history:  []string{startDir},
		mode:     modeDir,
		list:     l,
		dirlist:  dirList,
		textarea: ta,
		style:    st,
	}
}

func (m *model) Init() tea.Cmd { return nil }

// -----------------------------------------------------------------------------
// UPDATE
// -----------------------------------------------------------------------------

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(m.width-10, m.height-12)
		m.dirlist.SetSize(m.width-10, m.height-12)
		m.textarea.SetWidth(m.width - 10)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {

		case "ctrl+c":
			return m, tea.Quit

		case "left":
			if m.mode == modeDir {
				parent := filepath.Dir(m.working)
				if parent != m.working { // This check is sufficient and correct
					m.working = parent
					items := loadDirs(m.working)
					m.dirlist.SetItems(items)
					m.dirlist.Select(0)
				}
				return m, nil
			}
			if m.mode == modeUTCPArgs {
				m.mode = modeUTCP
				return m, nil
			}
			if m.mode == modeUTCP {
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
				return m, nil
			}
			if m.mode == modeResult {
				switch m.prevMode {
				case modeUTCPArgs, modeUTCP:
					m.mode = modeUTCP
					m.list.Title = "UTCP Tools"
					m.list.SetItems(m.loadUTCPTools())
				default:
					m.mode = modeList
					m.list.Title = "Agents"
					m.list.SetItems(defaultAgents())
				}
				m.textarea.Reset()
				return m, nil
			}
			if m.mode == modePrompt {
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
				m.textarea.Reset()
				return m, nil
			}

		case "esc":
			switch m.mode {
			case modePrompt, modeResult, modeUTCPArgs:
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
				m.textarea.Reset()
			case modeUTCP:
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
			}
			return m, nil

		case "enter":
			switch m.mode {

			case modeDir:
				item, ok := m.dirlist.SelectedItem().(dirItem)
				if !ok {
					return m, nil
				}

				// --- Confirm current directory ---
				if strings.HasPrefix(item.name, "✅") {
					m.mode = modeList
					m.list.Title = fmt.Sprintf("📁 %s", filepath.Base(m.working))
					m.list.SetItems(defaultAgents())
					return m, nil
				}

				// --- Go up one level ---
				if item.name == "⬆️ ../" {
					parent := filepath.Dir(m.working)
					if parent != m.working {
						m.working = parent
						items := loadDirs(m.working)
						m.dirlist.SetItems(items)
						m.dirlist.Select(0)
					}
					return m, nil
				}

				// --- Enter a subfolder ---
				info, err := os.Stat(item.path)
				if err == nil && info.IsDir() {
					m.working = item.path
					items := loadDirs(m.working)
					m.dirlist.SetItems(items)
					m.dirlist.Select(0)
					return m, nil
				}

			case modeList:
				if i, ok := m.list.SelectedItem().(plugin); ok {
					if i.name == "utcp" {
						m.mode = modeUTCP
						m.list.SetItems(m.loadUTCPTools())
						m.list.Title = "UTCP Tools"
						return m, nil
					}
					m.selected = i
					m.mode = modePrompt
					m.textarea.Reset()
					m.textarea.Focus()
				}
				return m, nil

			case modeUTCP:
				if i, ok := m.list.SelectedItem().(utcpItem); ok {
					m.selectedUTCP = i
					m.prevMode = m.mode
					m.mode = modeUTCPArgs
					m.textarea.SetValue("{\n  \n}")
					m.textarea.Focus()
				}
				return m, nil

			case modeUTCPArgs:
				prompt := strings.TrimSpace(m.textarea.Value())
				if prompt == "" {
					return m, nil
				}

				var args map[string]any
				if err := json.Unmarshal([]byte(prompt), &args); err != nil {
					m.output = m.style.error.Render(fmt.Sprintf("Invalid JSON: %v", err))
					m.mode = modeResult
					return m, nil
				}

				m.prevMode = m.mode
				m.mode = modeThinking
				m.output = ""
				m.thinking = "thinking"

				cmd := func() tea.Msg {
					if m.selectedUTCP.stream {
						stream, err := m.utcp.CallToolStream(m.ctx, m.selectedUTCP.name, args)
						if err != nil {
							return generateMsg{"", err}
						}
						var out strings.Builder
						for {
							item, err := stream.Next()
							if err == io.EOF {
								break
							}
							if err != nil {
								return generateMsg{"", err}
							}
							out.WriteString(fmt.Sprintf("%v\n", item))
						}
						return generateMsg{out.String(), nil}
					}

					res, err := m.utcp.CallTool(m.ctx, m.selectedUTCP.name, args)
					if err != nil {
						return generateMsg{"", err}
					}
					return generateMsg{fmt.Sprintf("%v", res), nil}
				}
				return m, tea.Batch(cmd, thinkingTick())

			case modePrompt:
				raw := strings.TrimSpace(m.textarea.Value())
				if raw == "" {
					return m, nil
				}
				return m.runPrompt(raw)
			}
		}

	case thinkingMsg:
		if m.mode == modeThinking {
			m.thinking = string(msg)
			return m, thinkingTick()
		}

	case generateMsg:
		if msg.err != nil {
			m.mode = modeResult
			m.output = m.style.error.Render(fmt.Sprintf("❌ %v", msg.err))
			return m, nil
		}
		m.output = m.style.success.Render("✅ Done!") + "\n\n" + msg.text
		m.mode = modeDone
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return doneMsg{} })

	case doneMsg:
		m.mode = modeResult
		return m, nil
	}

	var cmd tea.Cmd
	switch m.mode {
	case modeDir:
		m.dirlist, cmd = m.dirlist.Update(msg)
	case modeList, modeUTCP:
		m.list, cmd = m.list.Update(msg)
	case modePrompt, modeUTCPArgs:
		m.textarea, cmd = m.textarea.Update(msg)
	}
	return m, cmd
}

func defaultAgents() []list.Item {
	return []list.Item{
		plugin{"orchestrator", "Split into subtasks and execute sequentially"},
		plugin{"architect", "High-level design and refactoring"},
		plugin{"coder", "Feature implementation and tests"},
		plugin{"reviewer", "Code review and optimization"},
		plugin{"utcp", "Explore connected UTCP tools"},
	}
}

// @package main
func extractExplicitPath(code string) string {
	patterns := []string{
		`(?m)@path\s+([^\s]+)`,
		`(?m)//\s*path:\s*([^\s]+)`,
		`(?m)#\s*path:\s*([^\s]+)`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(code); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// runPrompt handles agent prompt execution modes inside the TUI.
// It covers step-build, orchestrator, inline UTCP, and default coding modes.
func (m *model) runPrompt(raw string) (tea.Model, tea.Cmd) {
	m.prevMode = m.mode
	m.mode = modeThinking
	m.output = ""
	m.thinking = "thinking"

	// --- INLINE UTCP ---
	if strings.HasPrefix(raw, "@utcp ") {
		cmd := func() tea.Msg {
			res, err := m.runUTCPInline(raw)
			if err != nil {
				return generateMsg{text: "", err: err}
			}
			m.saveCodeBlocks(res)
			return generateMsg{text: res, err: nil}
		}
		return m, tea.Batch(cmd, thinkingTick())
	}

	// --- DEFAULT: STEP-BUILD WORKFLOW for all coding agents ---
	// This ensures that even simple prompts benefit from the robust,
	// context-aware, multi-step generation process.
	cmd := func() tea.Msg {
		out, err := m.runStepBuilder(raw)
		return generateMsg{text: out, err: err}
	}

	return m, tea.Batch(cmd, thinkingTick())
}

// -----------------------------------------------------------------------------
// VIEW
// -----------------------------------------------------------------------------

func (m *model) View() string {
	header := lipgloss.JoinVertical(lipgloss.Left,
		m.style.header.Render("Lattice Code — Protocol Lattice Labs"),
		m.style.subtle.Render(fmt.Sprintf("Workspace: %s", m.working)),
	)
	if m.contextFiles > 0 {
		header += "\n" + m.style.subtle.Render(
			fmt.Sprintf("Context: %d files / %s included", m.contextFiles, humanSize(m.contextBytes)),
		)
	}

	var body string
	switch m.mode {
	case modeDir:
		body = lipgloss.JoinVertical(lipgloss.Center,
			m.dirlist.View(),
			m.style.subtle.Render("Current: "+m.working),
			m.style.footer.Render("[↑↓] Move │ [←] Up │ [Enter] Open/Choose │ [Ctrl+C] Quit"),
		)
	case modeList:
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.list.View(),
			"",
			m.style.footer.Render("[↑↓] Select │ [Enter] Choose │ [Esc] Back"),
		)
	case modeUTCP:
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.style.accent.Render("Available UTCP Tools"),
			"",
			m.list.View(),
			"",
			m.style.footer.Render("[↑↓] Move │ [Enter] Choose │ [Esc] Back"),
		)
	case modeUTCPArgs:
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.style.accent.Render(fmt.Sprintf("Args for tool: %s", m.selectedUTCP.name)),
			"",
			m.textarea.View(),
			"",
			m.style.footer.Render("[Enter] Run │ [Esc] Back │ Write valid JSON"),
		)
	case modePrompt:
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.style.accent.Render(fmt.Sprintf("Active agent: %s", m.selected.name)),
			"",
			m.textarea.View(),
			"",
			m.style.footer.Render("[Enter] Run │ [Esc] Back"),
		)
	case modeThinking:
		dots := m.style.accent.Render(m.thinking)
		body = m.style.center.Width(m.width).Height(m.height / 2).Render(fmt.Sprintf("@%s %s", m.selected.name, dots))
	case modeDone:
		body = m.style.center.Width(m.width).Height(m.height / 2).Render(fmt.Sprintf("@%s ✅ Done", m.selected.name))
	case modeResult:
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.style.accent.Render("Result:"),
			"",
			m.output,
			"",
			m.style.footer.Render("[Enter/ Esc] Back │ [Ctrl+C] Quit"),
		)
	}

	container := lipgloss.NewStyle().
		Width(m.width - 2).
		Height(m.height - 2).
		Render(lipgloss.JoinVertical(lipgloss.Center, header, "", body))

	return m.style.border.Render(container)
}

// -----------------------------------------------------------------------------
// THINKING ANIMATION
// -----------------------------------------------------------------------------

func thinkingTick() tea.Cmd {
	states := []string{"thinking", "thinking.", "thinking..", "thinking..."}
	return tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg {
		i := (int(t.UnixMilli()/400) % len(states))
		return thinkingMsg(states[i])
	})
}

// -----------------------------------------------------------------------------
// UTCP TOOL LIST LOADER & INLINE EXEC
// -----------------------------------------------------------------------------

func (m *model) loadUTCPTools() []list.Item {
	if m.utcp == nil {
		return []list.Item{utcpItem{"(no UTCP client)", "none", "UTCP unavailable", false}}
	}

	tools, err := m.utcp.SearchTools("", 50)
	if err != nil {
		return []list.Item{utcpItem{"(error)", "none", err.Error(), false}}
	}

	items := make([]list.Item, 0, len(tools))
	for _, t := range tools {
		isStream := strings.Contains(strings.ToLower(t.Name), "stream")
		items = append(items, utcpItem{
			name:     t.Name,
			provider: "",
			desc:     t.Description,
			stream:   isStream,
		})
	}

	if len(items) == 0 {
		items = append(items, utcpItem{"(no tools found)", "none", "", false})
	}
	return items
}

func (m *model) runUTCPInline(prompt string) (string, error) {
	if !strings.HasPrefix(prompt, "@utcp ") {
		return "", fmt.Errorf("not a utcp command")
	}
	if m.utcp == nil {
		return "", fmt.Errorf("UTCP client unavailable")
	}

	cmd := strings.TrimSpace(strings.TrimPrefix(prompt, "@utcp "))
	parts := strings.SplitN(cmd, " ", 2)
	if len(parts) == 0 {
		return "", fmt.Errorf("usage: @utcp toolName {jsonArgs}")
	}
	toolName := parts[0]
	args := map[string]any{}

	if len(parts) == 2 {
		raw := strings.TrimSpace(parts[1])
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", fmt.Errorf("invalid JSON args: %v", err)
		}
	}

	isStream := strings.Contains(strings.ToLower(toolName), "stream")

	if isStream {
		stream, err := m.utcp.CallToolStream(m.ctx, toolName, args)
		if err != nil {
			return "", err
		}
		var out strings.Builder
		for {
			item, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			out.WriteString(fmt.Sprintf("%v\n", item))
		}
		return out.String(), nil
	}

	res, err := m.utcp.CallTool(m.ctx, toolName, args)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", res), nil
}

// -----------------------------------------------------------------------------
// FILE HELPERS
// -----------------------------------------------------------------------------

func loadDirs(path string) []list.Item {
	entries, err := os.ReadDir(path)
	if err != nil {
		return []list.Item{dirItem{name: "(error reading dir)", path: path}}
	}
	var items []list.Item

	// 1. Add confirmation item
	items = append(items, dirItem{name: fmt.Sprintf("✅ Use this directory (%s)", filepath.Base(path)), path: path})

	// 2. Add parent directory navigation
	if path != "/" {
		items = append(items, dirItem{name: "⬆️ ../", path: filepath.Dir(path)})
	}

	// 3. Add subdirectories
	for _, e := range entries { // Already sorted by ReadDir
		if e.IsDir() {
			items = append(items, dirItem{name: "📁 " + e.Name() + "/", path: filepath.Join(path, e.Name())})
		}
	}
	return items
}

// -----------------------------------------------------------------------------
// CODE SAVING HELPERS
// -----------------------------------------------------------------------------

var fenceRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_+\\.-]*)\\s*\\n(.*?)\\n```")

type fileMeta struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// @package main
func (m *model) saveCodeBlocks(s string) {
	m.output += "\n---\n"
	matches := fenceRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		m.output += m.style.subtle.Render("No code blocks detected.\n")
		return
	}

	manifest := make(map[string][]fileMeta)

	for idx, mth := range matches {
		lang := strings.ToLower(strings.TrimSpace(mth[1]))
		code := strings.TrimSpace(mth[2])
		if lang == "" {
			lang = guessLanguageFromCode(code)
		}
		if lang == "" {
			lang = "txt"
		}

		// 1) Respect explicit @path if provided
		explicit := extractExplicitPath(code)
		var filename string
		var targetDir string
		pkgName := ""

		if explicit != "" {
			abs := filepath.Join(m.working, explicit)
			_ = os.MkdirAll(filepath.Dir(abs), 0o755)
			filename = abs
			targetDir = filepath.Dir(abs)
		} else {
			// 2) Entrypoint safeguard for Go: keep at root
			if lang == "go" && (strings.Contains(code, "package main") || strings.Contains(code, "func main(")) {
				targetDir = m.working
			} else {
				targetDir, pkgName = m.detectPackageDirectory(lang, code)
			}
			_ = os.MkdirAll(targetDir, 0o755)
			filename = filepath.Join(targetDir, m.guessFilename(lang, code, idx))
		}

		if err := os.WriteFile(filename, []byte(code+"\n"), 0o644); err != nil {
			m.output += m.style.error.Render(fmt.Sprintf("❌ failed to save %s: %v\n", filename, err))
			continue
		}

		key := lang
		if pkgName != "" {
			key = pkgName
		}
		manifest[key] = append(manifest[key], fileMeta{Name: filepath.Base(filename), Path: filename})
		m.output += m.style.success.Render(fmt.Sprintf("💾 saved %s\n", filename))
	}

	for _, files := range manifest {
		lang := guessPrimaryLang(files)
		m.addImports(lang, files)
	}
	if err := NormalizeImports(m.working); err != nil {
		m.output += m.style.subtle.Render(
			fmt.Sprintf("⚠ import normalize: %v\n", err),
		)
	}
}

// Universal package/module detector for ANY programming language
func (m *model) detectPackageDirectory(lang, code string) (string, string) {
	// Try universal patterns first
	if pkg := extractUniversalPackage(code); pkg != "" {
		pkgDir := filepath.Join(m.working, pkg)
		return pkgDir, pkg
	}

	// Language-specific detection
	switch lang {
	case "go":
		if pkg := extractGoPackage(code); pkg != "" && pkg != "main" {
			return filepath.Join(m.working, pkg), pkg
		}
		return m.working, ""

	case "python", "py":
		if pkg := extractPythonPackage(code); pkg != "" {
			return filepath.Join(m.working, pkg), pkg
		}
		return filepath.Join(m.working, lang), ""

	case "js", "javascript", "ts", "typescript":
		if pkg := extractJSPackage(code); pkg != "" {
			return filepath.Join(m.working, pkg), pkg
		}
		return filepath.Join(m.working, lang), ""

	case "rs", "rust":
		if mod := extractRustModule(code); mod != "" {
			return filepath.Join(m.working, "src", mod), mod
		}
		return filepath.Join(m.working, "src"), ""

	case "java":
		if pkg := extractJavaPackage(code); pkg != "" {
			pkgPath := strings.ReplaceAll(pkg, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "src", pkgPath), pkg
		}
		return filepath.Join(m.working, "src"), ""

	case "cs", "csharp", "c#":
		if ns := extractCSharpNamespace(code); ns != "" {
			nsPath := strings.ReplaceAll(ns, ".", string(os.PathSeparator))
			return filepath.Join(m.working, nsPath), ns
		}
		return filepath.Join(m.working, lang), ""

	case "cpp", "c++", "cc", "cxx":
		if ns := extractCppNamespace(code); ns != "" {
			return filepath.Join(m.working, "include", ns), ns
		}
		if strings.Contains(code, "#ifndef") || strings.Contains(code, "#pragma once") {
			return filepath.Join(m.working, "include"), ""
		}
		return filepath.Join(m.working, "src"), ""

	case "c":
		if strings.Contains(code, "#ifndef") || strings.Contains(code, "#pragma once") {
			return filepath.Join(m.working, "include"), ""
		}
		return filepath.Join(m.working, "src"), ""

	case "rb", "ruby":
		if mod := extractRubyModule(code); mod != "" {
			return filepath.Join(m.working, "lib", strings.ToLower(mod)), mod
		}
		return filepath.Join(m.working, "lib"), ""

	case "php":
		if ns := extractPHPNamespace(code); ns != "" {
			nsPath := strings.ReplaceAll(ns, "\\", string(os.PathSeparator))
			return filepath.Join(m.working, "src", nsPath), ns
		}
		return filepath.Join(m.working, "src"), ""

	case "kt", "kotlin":
		if pkg := extractKotlinPackage(code); pkg != "" {
			pkgPath := strings.ReplaceAll(pkg, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "src", pkgPath), pkg
		}
		return filepath.Join(m.working, "src"), ""

	case "swift":
		if mod := extractSwiftModule(code); mod != "" {
			return filepath.Join(m.working, "Sources", mod), mod
		}
		return filepath.Join(m.working, "Sources"), ""

	case "dart":
		if pkg := extractDartPackage(code); pkg != "" {
			return filepath.Join(m.working, "lib", pkg), pkg
		}
		return filepath.Join(m.working, "lib"), ""

	case "lua":
		if mod := extractLuaModule(code); mod != "" {
			return filepath.Join(m.working, mod), mod
		}
		return m.working, ""

	case "elixir", "ex":
		if mod := extractElixirModule(code); mod != "" {
			modPath := strings.ReplaceAll(mod, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "lib", strings.ToLower(modPath)), mod
		}
		return filepath.Join(m.working, "lib"), ""

	case "scala":
		if pkg := extractScalaPackage(code); pkg != "" {
			pkgPath := strings.ReplaceAll(pkg, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "src", "main", "scala", pkgPath), pkg
		}
		return filepath.Join(m.working, "src", "main", "scala"), ""

	case "clojure", "clj":
		if ns := extractClojureNamespace(code); ns != "" {
			nsPath := strings.ReplaceAll(ns, ".", string(os.PathSeparator))
			nsPath = strings.ReplaceAll(nsPath, "-", "_")
			return filepath.Join(m.working, "src", nsPath), ns
		}
		return filepath.Join(m.working, "src"), ""

	case "haskell", "hs":
		if mod := extractHaskellModule(code); mod != "" {
			modPath := strings.ReplaceAll(mod, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "src", modPath), mod
		}
		return filepath.Join(m.working, "src"), ""

	case "r":
		if pkg := extractRPackage(code); pkg != "" {
			return filepath.Join(m.working, "R", pkg), pkg
		}
		return filepath.Join(m.working, "R"), ""

	case "julia", "jl":
		if mod := extractJuliaModule(code); mod != "" {
			return filepath.Join(m.working, "src", mod), mod
		}
		return filepath.Join(m.working, "src"), ""

	default:
		// Generic fallback: try to detect any module-like structure
		return filepath.Join(m.working, lang), ""
	}
}

// Universal package detector - works across multiple languages
func extractUniversalPackage(code string) string {
	patterns := []string{
		`@package\s+([a-zA-Z_][a-zA-Z0-9_.-]*)`,
		`@module\s+([a-zA-Z_][a-zA-Z0-9_.-]*)`,
		`#\s*package:\s*([a-zA-Z_][a-zA-Z0-9_.-]*)`,
		`//\s*package:\s*([a-zA-Z_][a-zA-Z0-9_.-]*)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if match := re.FindStringSubmatch(code); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

// Language-specific extractors
func extractGoPackage(code string) string {
	re := regexp.MustCompile(`(?m)^package\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractPythonPackage(code string) string {
	if strings.Contains(code, "__all__") || strings.Contains(code, "__init__") {
		if strings.Contains(code, "class ") {
			if name := extractAfter(code, "class "); name != "" {
				return strings.ToLower(name)
			}
		}
	}
	return ""
}

func extractJSPackage(code string) string {
	re := regexp.MustCompile(`[@/]\s*(?:package|module)\s+([a-zA-Z_][a-zA-Z0-9_-]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractRustModule(code string) string {
	re := regexp.MustCompile(`(?m)^(?:pub\s+)?mod\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractJavaPackage(code string) string {
	re := regexp.MustCompile(`(?m)^package\s+([a-zA-Z_][a-zA-Z0-9_.]*)\s*;`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractCSharpNamespace(code string) string {
	re := regexp.MustCompile(`(?m)^\s*namespace\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractCppNamespace(code string) string {
	re := regexp.MustCompile(`(?m)^\s*namespace\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractRubyModule(code string) string {
	re := regexp.MustCompile(`(?m)^\s*module\s+([A-Z][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractPHPNamespace(code string) string {
	re := regexp.MustCompile(`(?m)^\s*namespace\s+([a-zA-Z_][a-zA-Z0-9_\\]*)\s*;`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractKotlinPackage(code string) string {
	re := regexp.MustCompile(`(?m)^package\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractSwiftModule(code string) string {
	if strings.Contains(code, "public struct") || strings.Contains(code, "public class") {
		re := regexp.MustCompile(`public\s+(?:struct|class)\s+([A-Z][a-zA-Z0-9_]*)`)
		if match := re.FindStringSubmatch(code); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func extractDartPackage(code string) string {
	re := regexp.MustCompile(`(?m)^library\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractLuaModule(code string) string {
	re := regexp.MustCompile(`(?m)^local\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*\{\}`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		if strings.Contains(code, "return "+match[1]) {
			return match[1]
		}
	}
	return ""
}

func extractElixirModule(code string) string {
	re := regexp.MustCompile(`(?m)^\s*defmodule\s+([A-Z][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractScalaPackage(code string) string {
	re := regexp.MustCompile(`(?m)^package\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractClojureNamespace(code string) string {
	re := regexp.MustCompile(`(?m)^\s*\(\s*ns\s+([a-zA-Z_][a-zA-Z0-9_.-]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractHaskellModule(code string) string {
	re := regexp.MustCompile(`(?m)^module\s+([A-Z][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractRPackage(code string) string {
	re := regexp.MustCompile(`#'\s*@package\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractJuliaModule(code string) string {
	re := regexp.MustCompile(`(?m)^\s*module\s+([A-Z][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func guessPrimaryLang(files []fileMeta) string {
	if len(files) == 0 {
		return "txt"
	}
	ext := filepath.Ext(files[0].Name)
	return strings.TrimPrefix(ext, ".")
}

// Update dirForLanguage for universal language support
func (m *model) dirForLanguage(lang string) string {
	switch lang {
	case "go":
		return m.working
	case "rs", "rust":
		return filepath.Join(m.working, "src")
	case "java", "kt", "kotlin", "scala":
		return filepath.Join(m.working, "src")
	case "rb", "ruby":
		return filepath.Join(m.working, "lib")
	case "php":
		return filepath.Join(m.working, "src")
	case "swift":
		return filepath.Join(m.working, "Sources")
	case "dart":
		return filepath.Join(m.working, "lib")
	case "elixir", "ex":
		return filepath.Join(m.working, "lib")
	case "clojure", "clj":
		return filepath.Join(m.working, "src")
	case "haskell", "hs":
		return filepath.Join(m.working, "src")
	case "r":
		return filepath.Join(m.working, "R")
	case "julia", "jl":
		return filepath.Join(m.working, "src")
	case "c", "cpp", "c++", "cc", "cxx":
		return filepath.Join(m.working, "src")
	default:
		return filepath.Join(m.working, lang)
	}
}

func (m *model) guessFilename(lang, code string, index int) string {
	base := "snippet"
	switch lang {
	case "go":
		if strings.Contains(code, "package main") || strings.Contains(code, "func main(") {
			base = "main"
		} else if strings.Contains(code, "package ") {
			pkg := extractAfter(code, "package ")
			if pkg != "" {
				base = pkg
			}
		}
	case "py", "python":
		if strings.Contains(code, "def main(") {
			base = "main"
		} else if strings.Contains(code, "class ") {
			base = extractAfter(code, "class ")
		}
	case "yaml", "yml":
		base = "config"
	case "json":
		base = "data"
	case "sh":
		base = "script"
	}
	return fmt.Sprintf("%s.%s", sanitizeFilename(base), extFor(lang))
}

// --- Vibe System Prompt (multiline, safe for backticks) -----------------------

var vibeSystemPrompt = strings.Join([]string{
	"You are Vibe — a coding agent running inside a terminal TUI (Lattice Code).",
	"",
	"Your role:",
	"Write, refactor, and extend full software projects directly from user prompts.",
	"Your outputs are parsed automatically and saved to disk.",
	"Always return complete, runnable code inside proper fenced blocks.",
	"",
	"OPERATING RULES",
	"1) Response format",
	"   - Begin with a concise, high-level Plan (3–6 bullets).",
	"   - Then output one or more full files in fenced code blocks.",
	"   - Syntax: ```<language>\\n<full file content>\\n```",
	"   - Each fenced block represents one entire file to be written.",
	"   - No partials, no ellipses, no inline diffs.",
	"",
	"2) File hints",
	"   - You may include an inline comment like @package <name> or // package: <name> to guide file placement.",
	"   - Do not create top-level language folders (e.g., no /go/, /python/).",
	"   - When unsure, default to a logical package name based on file purpose.",
	"",
	"3) Context",
	"   - You will be given a CODEBASE SNAPSHOT (tree + file excerpts). Treat it as authoritative.",
	"   - Modify, extend, or refactor only relevant parts.",
	"   - If adding new functionality, integrate it coherently with existing structure.",
	"",
	"4) Language behavior",
	"   - Output idiomatic code per language (Go fmt/vet; Python PEP8; etc.).",
	"   - Prefer minimal runnable modules over heavy scaffolds.",
	"   - Do not emit dependency files (go.mod, package.json, etc.) unless explicitly requested.",
	"",
	"5) Quality",
	"   - Clarity, modularity, reliability. Handle errors where appropriate.",
	"   - Avoid hardcoded secrets; prefer env vars.",
	"   - Keep comments brief and purposeful.",
	"",
	"6) Tool usage",
	"   - If an external UTCP tool would help, suggest a one-liner the user can run:",
	"     @utcp lint.go {\"path\": \"./\"}",
	"   - Do not place UTCP commands inside code fences.",
	"",
	"7) Final section",
	"   - Optionally conclude with Next steps (2–5 bullets).",
	"",
	"OUTPUT CONTRACT SUMMARY",
	"Plan → Full files in fenced code blocks → (Optional) Next steps",
	"",
	"REMINDERS",
	"- One file per fence. Each fence = saved file.",
	"- The first token after ``` is the language.",
	"- Emit self-contained, executable code.",
	"- Avoid verbose commentary, placeholders, and diffs.",
}, "\n")

func extractAfter(code, key string) string {
	idx := strings.Index(code, key)
	if idx == -1 {
		return ""
	}
	line := strings.SplitN(code[idx+len(key):], "\n", 2)[0]
	return strings.Fields(line)[0]
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "(){};:")
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "'", "")
	return name
}

func extFor(lang string) string {
	mapping := map[string]string{
		"go": "go", "python": "py", "py": "py", "js": "js", "ts": "ts",
		"rs": "rs", "cpp": "cpp", "c": "c", "sh": "sh", "html": "html",
		"css": "css", "json": "json", "yml": "yml", "yaml": "yml",
		"md": "md", "txt": "txt",
	}
	if e, ok := mapping[lang]; ok {
		return e
	}
	return "txt"
}

func guessLanguageFromCode(code string) string {
	switch {
	case strings.Contains(code, "package main"):
		return "go"
	case strings.Contains(code, "def "):
		return "python"
	case strings.Contains(code, "import React"):
		return "js"
	case strings.Contains(code, "fn main"):
		return "rs"
	case strings.Contains(code, "#include"):
		return "cpp"
	default:
		return ""
	}
}

func (m *model) addImports(lang string, files []fileMeta) {
	// Go: ignore module scaffolding and imports entirely.
	if lang == "go" {
		return
	}

	// For other languages, create a lightweight aggregator file and append imports.
	main := filepath.Join(m.working, lang, fmt.Sprintf("main.%s", extFor(lang)))

	var imports []string
	for _, f := range files {
		if filepath.Base(f.Path) == filepath.Base(main) {
			continue
		}
		name := strings.TrimSuffix(f.Name, filepath.Ext(f.Name))
		switch lang {
		case "python", "py":
			imports = append(imports, fmt.Sprintf("from %s import *", name))
		case "js", "ts":
			imports = append(imports, fmt.Sprintf("import './%s.%s'", name, extFor(lang)))
		case "rs":
			imports = append(imports, fmt.Sprintf("mod %s;", name))
		case "cpp":
			imports = append(imports, fmt.Sprintf("#include \"%s.h\"", name))
		case "sh":
			imports = append(imports, fmt.Sprintf("source ./%s.sh", name))
		case "html":
			imports = append(imports, fmt.Sprintf("<script src=\"./%s.js\"></script>", name))
		default:
			imports = append(imports, fmt.Sprintf("// related: %s", name))
		}
	}

	if len(imports) == 0 {
		return
	}

	content := strings.Join(imports, "\n") + "\n\n"
	appendLine(main, content)
}

func appendLine(path, text string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(text)
}

func saveManifest(root, lang string, files any) {
	data, _ := json.MarshalIndent(files, "", "  ")
	_ = os.WriteFile(filepath.Join(root, lang, "manifest.json"), data, 0o644)
}

// -----------------------------------------------------------------------------
// CODEBASE CONTEXT SNAPSHOT
// -----------------------------------------------------------------------------

type fileEntry struct {
	Rel  string
	Abs  string
	Size int64
}

func isIgnoredDir(name string) bool {
	ignored := map[string]struct{}{
		".git": {}, "node_modules": {}, "dist": {}, "build": {}, "out": {}, "target": {}, "vendor": {},
		".venv": {}, "__pycache__": {}, ".idea": {}, ".vscode": {}, ".DS_Store": {},
	}
	_, ok := ignored[name]
	return ok
}

func allowedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	allow := map[string]struct{}{
		".go": {},
		".md": {}, ".yaml": {}, ".yml": {}, ".json": {},
		".py": {}, ".js": {}, ".ts": {}, ".tsx": {}, ".jsx": {}, ".rs": {}, ".rb": {},
		".java": {}, ".c": {}, ".cpp": {}, ".h": {}, ".sh": {}, ".toml": {}, ".ini": {},
		".cfg": {}, ".txt": {},
	}
	_, ok := allow[ext]
	return ok
}

func buildTree(files []fileEntry) string {
	type node struct {
		name     string
		children map[string]*node
		file     bool
	}
	root := &node{name: "/", children: map[string]*node{}}

	for _, f := range files {
		parts := strings.Split(f.Rel, string(os.PathSeparator))
		cur := root
		for i, p := range parts {
			if cur.children == nil {
				cur.children = map[string]*node{}
			}
			if _, ok := cur.children[p]; !ok {
				cur.children[p] = &node{name: p, children: map[string]*node{}}
			}
			cur = cur.children[p]
			if i == len(parts)-1 {
				cur.file = true
			}
		}
	}

	var lines []string
	var walk func(prefix string, n *node)
	walk = func(prefix string, n *node) {
		keys := make([]string, 0, len(n.children))
		for k := range n.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := n.children[k]
			marker := "└─ "
			line := prefix + marker + child.name
			if !child.file {
				line += "/"
			}
			lines = append(lines, line)
			if len(child.children) > 0 {
				walk(prefix+"  ", child)
			}
		}
	}
	walk("", root)
	return strings.Join(lines, "\n")
}

func fenceLangFromExt(ext string) string {
	switch strings.TrimPrefix(strings.ToLower(ext), ".") {
	case "go":
		return "go"
	case "py":
		return "python"
	case "js":
		return "javascript"
	case "ts", "tsx":
		return "ts"
	case "jsx":
		return "jsx"
	case "rs":
		return "rust"
	case "rb":
		return "ruby"
	case "java":
		return "java"
	case "c":
		return "c"
	case "cpp", "hpp", "cc", "cxx":
		return "cpp"
	case "h":
		return "c"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "md":
		return "md"
	case "sh":
		return "bash"
	case "toml":
		return "toml"
	case "ini", "cfg":
		return ""
	default:
		return ""
	}
}

func humanSize(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func mimeForPath(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".md":
		return "text/markdown"
	case ".go", ".py", ".rs", ".rb", ".java", ".c", ".h", ".cpp", ".cc", ".cxx", ".sh", ".txt":
		return "text/plain"
	case ".js":
		return "application/javascript"
	case ".ts", ".tsx":
		return "application/typescript"
	case ".jsx":
		return "text/jsx"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".toml":
		return "application/toml"
	case ".ini", ".cfg":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// -----------------------------------------------------------------------------
// ORCHESTRATOR: split → iterate subtasks → save files
// -----------------------------------------------------------------------------

type subtask struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}
type plan struct {
	Tasks []subtask `json:"tasks"`
}

func (m *model) runOrchestrator(userGoal string) (string, error) {
	var log strings.Builder
	fmt.Fprintf(&log, "🧭 Orchestrator: planning for goal:\n> %s\n\n", userGoal)

	// 1) Build current context for the planner
	const (
		maxFiles      = 300
		maxTotalBytes = int64(1_200_000)
		perFileLimit  = int64(80_000)
	)

	// Detect language dynamically from the current prompt or goal
	plannerPrompt := strings.Builder{}
	plannerPrompt.WriteString("You are a senior software planner. Split the user's GOAL into 3–8 ordered subtasks.\n")
	plannerPrompt.WriteString("Return JSON ONLY in this exact structure:\n")
	plannerPrompt.WriteString("{\"tasks\":[{\"title\":\"...\",\"detail\":\"...\"}]}\n")
	plannerPrompt.WriteString("Keep titles concise; put concrete instructions in detail. Do not include code.\n\n")
	plannerPrompt.WriteString("### [WORKSPACE ROOT]\n")
	plannerPrompt.WriteString(m.working + "\n\n")
	lang := detectPromptLanguage(plannerPrompt.String())

	ctxBlock, nFiles, nBytes := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
	m.contextFiles, m.contextBytes = nFiles, nBytes
	attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
	plannerPrompt.WriteString(ctxBlock)
	plannerPrompt.WriteString("\n\n---\nGOAL:\n")
	plannerPrompt.WriteString(userGoal)
	rawPlan, err := m.agent.GenerateWithFiles(m.ctx, "1", plannerPrompt.String(), attachments)
	if err != nil {
		// fallback to plain generate
		rawPlan, err = m.agent.Generate(m.ctx, "1", plannerPrompt.String())
		if err != nil {
			return "", fmt.Errorf("plan generation failed: %w", err)
		}
	}
	p, perr := parsePlan(rawPlan)
	if perr != nil || len(p.Tasks) == 0 {
		// Fallback: single task = original goal
		p = plan{Tasks: []subtask{{Title: "Apply requested changes", Detail: userGoal}}}
	}

	fmt.Fprintf(&log, "📝 Plan (%d tasks):\n", len(p.Tasks))
	for i, t := range p.Tasks {
		fmt.Fprintf(&log, "  %d) %s — %s\n", i+1, t.Title, trim(t.Detail, 140))
	}
	fmt.Fprintln(&log)

	// 3) Execute each subtask, rebuilding context each time
	for i, t := range p.Tasks {
		step := i + 1
		fmt.Fprintf(&log, "➡️  Task %d/%d: %s\n", step, len(p.Tasks), t.Title)

		// Refresh context & attachments to include previous steps' changes
		ctxBlock, nFiles, nBytes = buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
		m.contextFiles, m.contextBytes = nFiles, nBytes
		attachments = collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)

		sub := strings.Builder{}
		sub.WriteString("You are Vibe, the coding agent inside a TUI. Execute the current SUBTASK against the codebase.\n")
		sub.WriteString("Follow the OUTPUT CONTRACT: short Plan → full files in fenced code blocks → (optional) Next steps.\n")
		sub.WriteString("Do NOT emit diffs or partials. Use @path to place new/renamed files precisely. Avoid creating language folders.\n")
		sub.WriteString("\n### [WORKSPACE ROOT]\n")
		sub.WriteString(m.working + "\n\n")
		sub.WriteString(ctxBlock)
		sub.WriteString("\n---\nORIGINAL GOAL:\n")
		sub.WriteString(userGoal)
		sub.WriteString("\n---\nSUBTASK:\n")
		sub.WriteString(fmt.Sprintf("%s — %s\n", t.Title, t.Detail))

		resp, err := m.agent.GenerateWithFiles(m.ctx, "1", sub.String(), attachments)
		if err != nil {
			// graceful fallback
			resp, err = m.agent.Generate(m.ctx, "1", sub.String())
			if err != nil {
				fmt.Fprintf(&log, "   ❌ generation failed: %v\n", err)
				continue
			}
		}
		m.saveCodeBlocks(resp)
		fmt.Fprintf(&log, "   ✅ saved files for task %d\n\n", step)
	}

	fmt.Fprintln(&log, "🎉 Orchestration complete.")
	return log.String(), nil
}

func parsePlan(s string) (plan, error) {
	// 1) Prefer ```json blocks
	reFence := regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")
	if m := reFence.FindStringSubmatch(s); len(m) > 1 {
		var p plan
		if json.Unmarshal([]byte(m[1]), &p) == nil {
			return p, nil
		}
	}
	// 2) Try raw JSON
	var p plan
	if json.Unmarshal([]byte(strings.TrimSpace(s)), &p) == nil && len(p.Tasks) > 0 {
		return p, nil
	}
	// 3) Try extracting the tasks array and reconstruct object
	reTasks := regexp.MustCompile(`(?s)"tasks"\s*:\s*(\[[^\]]*\])`)
	if m := reTasks.FindStringSubmatch(s); len(m) > 1 {
		body := fmt.Sprintf(`{"tasks":%s}`, m[1])
		if json.Unmarshal([]byte(body), &p) == nil {
			return p, nil
		}
	}
	return plan{}, fmt.Errorf("no valid plan JSON found")
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// -----------------------------------------------------------------------------
// AGENT & UTCP BUILDERS
// -----------------------------------------------------------------------------

func buildAgent(ctx context.Context) (*agent.Agent, error) {
	qdrantURL := flag.String("qdrant-url", "http://localhost:6333", "Qdrant base URL")
	qdrantCollection := flag.String("qdrant-collection", "raezil", "Qdrant collection name")

	memOpts := memory.DefaultOptions()
	builder, err := adk.New(
		ctx,
		adk.WithDefaultSystemPrompt(vibeSystemPrompt),
		adk.WithModules(
			modules.InQdrantMemory(100000, *qdrantURL, *qdrantCollection, memory.AutoEmbedder(), &memOpts),

			adkmodules.NewModelModule("gemini", func(_ context.Context) (models.Agent, error) {
				return models.NewGeminiLLM(ctx, "gemini-2.5-pro", "Universal code generator")
			}),
			adkmodules.NewToolModule("essentials",
				adkmodules.StaticToolProvider([]agent.Tool{&tools.EchoTool{}}, nil),
			),
		),
	)
	if err != nil {
		return nil, err
	}
	return builder.BuildAgent(ctx)
}

func buildUTCP(ctx context.Context) (utcp.UtcpClientInterface, error) {
	cfg := &utcp.UtcpClientConfig{ProvidersFilePath: "provider.json"}
	client, err := utcp.NewUTCPClient(ctx, cfg, nil, nil)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func main() {
	startDir, _ := os.Getwd()
	ctx := context.Background()
	fmt.Println("🚀 Initializing Lattice Code Agent + UTCP...")

	a, err := buildAgent(ctx)
	if err != nil {
		fmt.Println("❌ Failed to build agent:", err)
		os.Exit(1)
	}

	u, err := buildUTCP(ctx)
	if err != nil {
		fmt.Println("⚠️ UTCP unavailable:", err)
	}

	p := tea.NewProgram(newModel(ctx, a, u, startDir), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
	}
}

// NormalizeImports runs all language-specific fixers over the workspace.
func NormalizeImports(root string) error {
	_ = normalizeGo(root)
	_ = normalizePython(root)
	_ = normalizeJSLike(root)
	_ = normalizeJavaLike(root)
	_ = normalizeCppLike(root)
	_ = normalizePHP(root)
	return nil
}

// --------------------------- GO ----------------------------------------------

func normalizeGo(root string) error {
	mod := goModulePath(root)
	if mod == "" {
		return nil
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		if strings.Contains(p, string(filepath.Separator)+"vendor"+string(filepath.Separator)) {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return nil
		}
		changed := false
		ast.Inspect(f, func(n ast.Node) bool {
			imp, ok := n.(*ast.ImportSpec)
			if !ok || imp.Path == nil {
				return true
			}
			path, _ := strconv.Unquote(imp.Path.Value)
			parts := strings.Split(path, "/")
			for i := 0; i < len(parts)-1; i++ {
				if parts[i] == "src" {
					newPath := mod + "/" + strings.Join(parts[i+1:], "/")
					if newPath != path {
						imp.Path.Value = strconv.Quote(newPath)
						changed = true
					}
					break
				}
			}
			return true
		})
		if !changed {
			return nil
		}
		var buf bytes.Buffer
		cfg := &printer.Config{Mode: printer.TabIndent | printer.UseSpaces, Tabwidth: 8}
		if err := cfg.Fprint(&buf, fset, f); err != nil {
			return nil
		}
		return os.WriteFile(p, buf.Bytes(), 0o644)
	})
}

func goModulePath(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// --------------------------- PYTHON ------------------------------------------

func normalizePython(root string) error {
	pyFiles := collectFiles(root, ".py")
	if len(pyFiles) == 0 {
		return nil
	}

	reFrom := regexp.MustCompile(`(?m)^\s*from\s+([A-Za-z0-9_\.]+)\s+import\s+`)
	reImp := regexp.MustCompile(`(?m)^\s*import\s+([A-Za-z0-9_\.]+)`)

	for _, p := range pyFiles {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false

		stripper := func(mod string) string {
			m := mod
			m = strings.TrimPrefix(m, "src.")
			m = strings.ReplaceAll(m, ".src.", ".")
			m = strings.TrimPrefix(m, moduleNameFromRoot(root)+".")
			m = strings.TrimPrefix(m, moduleNameFromRoot(root)+".src.")
			return m
		}

		txt = reFrom.ReplaceAllStringFunc(txt, func(line string) string {
			m := reFrom.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			newMod := stripper(m[1])
			if newMod != m[1] {
				changed = true
				return strings.Replace(line, "from "+m[1]+" ", "from "+newMod+" ", 1)
			}
			return line
		})
		txt = reImp.ReplaceAllStringFunc(txt, func(line string) string {
			m := reImp.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			newMod := stripper(m[1])
			if newMod != m[1] {
				changed = true
				return strings.Replace(line, "import "+m[1], "import "+newMod, 1)
			}
			return line
		})

		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}

	// Ensure __init__.py in packages (dirs with .py files)
	dirs := map[string]struct{}{}
	for _, p := range pyFiles {
		dirs[filepath.Dir(p)] = struct{}{}
	}
	for d := range dirs {
		init := filepath.Join(d, "__init__.py")
		if _, err := os.Stat(init); os.IsNotExist(err) {
			_ = os.WriteFile(init, []byte{}, 0o644)
		}
	}
	return nil
}

func moduleNameFromRoot(root string) string {
	return filepath.Base(root)
}

// --------------------------- JS / TS -----------------------------------------

func normalizeJSLike(root string) error {
	jsExts := []string{".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx"}
	files := collectFilesMany(root, jsExts)
	if len(files) == 0 {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*(?:import|export)\s+(?:[^'"]*?\s+from\s+)?["']([^"']+)["']`)
	for _, p := range files {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false

		txt = re.ReplaceAllStringFunc(txt, func(line string) string {
			m := re.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			target := m[1]
			if strings.HasPrefix(target, ".") || strings.HasPrefix(target, "@") {
				return line
			}
			if idx := strings.Index(target, "/src/"); idx >= 0 {
				suffix := target[idx+len("/src/"):]
				newRel := relFromTo(filepath.Dir(p), filepath.Join(root, "src", filepath.FromSlash(suffix)))
				if newRel != "" && newRel != target {
					changed = true
					return strings.Replace(line, `"`+target+`"`, `"`+newRel+`"`, 1)
				}
			}
			if strings.HasPrefix(target, "src/") {
				suffix := strings.TrimPrefix(target, "src/")
				newRel := relFromTo(filepath.Dir(p), filepath.Join(root, "src", filepath.FromSlash(suffix)))
				if newRel != "" && newRel != target {
					changed = true
					return strings.Replace(line, `"`+target+`"`, `"`+newRel+`"`, 1)
				}
			}
			if isUnderRoot(root, target) {
				abs := filepath.Join(root, filepath.FromSlash(target))
				newRel := relFromTo(filepath.Dir(p), abs)
				if newRel != "" && newRel != target {
					changed = true
					return strings.Replace(line, `"`+target+`"`, `"`+newRel+`"`, 1)
				}
			}
			return line
		})

		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}
	return nil
}

// --------------------------- JAVA / KOTLIN -----------------------------------

func normalizeJavaLike(root string) error {
	javaExts := []string{".java", ".kt"}
	files := collectFilesMany(root, javaExts)
	if len(files) == 0 {
		return nil
	}
	rePkg := regexp.MustCompile(`(?m)^(package\s+)([A-Za-z0-9_.]+)\s*;`)
	reImp := regexp.MustCompile(`(?m)^(import\s+)([A-Za-z0-9_.]+)\s*;`)
	for _, p := range files {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false
		fix := func(s string) (string, bool) {
			ns := strings.ReplaceAll(s, ".src.", ".")
			ns = strings.TrimPrefix(ns, "src.")
			ns = strings.ReplaceAll(ns, "..", ".")
			return ns, ns != s
		}
		txt = rePkg.ReplaceAllStringFunc(txt, func(line string) string {
			prefix, name := rePkg.FindStringSubmatch(line)[1], rePkg.FindStringSubmatch(line)[2]
			if nn, ok := fix(name); ok {
				changed = true
				return prefix + nn + ";"
			}
			return line
		})
		txt = reImp.ReplaceAllStringFunc(txt, func(line string) string {
			prefix, name := reImp.FindStringSubmatch(line)[1], reImp.FindStringSubmatch(line)[2]
			if nn, ok := fix(name); ok {
				changed = true
				return prefix + nn + ";"
			}
			return line
		})
		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}
	return nil
}

// --------------------------- C / C++ -----------------------------------------

func normalizeCppLike(root string) error {
	ccExts := []string{".c", ".h", ".hpp", ".hh", ".hxx", ".cpp", ".cc", ".cxx"}
	files := collectFilesMany(root, ccExts)
	if len(files) == 0 {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*#\s*include\s*[<"]([^">]+)[">]`)
	for _, p := range files {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false

		txt = re.ReplaceAllStringFunc(txt, func(line string) string {
			m := re.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			target := m[1]
			if strings.Contains(target, "/src/") {
				suffix := target[strings.Index(target, "/src/")+len("/src/"):]
				abs := filepath.Join(root, "src", filepath.FromSlash(suffix))
				if _, err := os.Stat(abs); err == nil {
					newRel := relFromTo(filepath.Dir(p), abs)
					if newRel != "" {
						changed = true
						return strings.Replace(line, target, newRel, 1)
					}
				}
			}
			if isUnderRoot(root, target) {
				abs := filepath.Join(root, filepath.FromSlash(target))
				if _, err := os.Stat(abs); err == nil {
					newRel := relFromTo(filepath.Dir(p), abs)
					if newRel != "" {
						changed = true
						return strings.Replace(line, target, newRel, 1)
					}
				}
			}
			return line
		})

		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}
	return nil
}

// --------------------------- PHP (PSR-4-ish) ---------------------------------

func normalizePHP(root string) error {
	files := collectFiles(root, ".php")
	if len(files) == 0 {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*use\s+([A-Za-z0-9_\\]+)\s*;`)
	for _, p := range files {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false
		txt = re.ReplaceAllStringFunc(txt, func(line string) string {
			m := re.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			name := m[1]
			nn := strings.ReplaceAll(name, `\Src\`, `\`)
			if nn != name {
				changed = true
				return strings.Replace(line, name, nn, 1)
			}
			return line
		})
		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}
	return nil
}

// --------------------------- Helpers -----------------------------------------

func collectFiles(root, ext string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.EqualFold(filepath.Ext(p), ext) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func collectFilesMany(root string, exts []string) []string {
	extSet := map[string]struct{}{}
	for _, e := range exts {
		extSet[strings.ToLower(e)] = struct{}{}
	}

	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, ok := extSet[strings.ToLower(filepath.Ext(p))]; ok {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func relFromTo(fromDir, absTarget string) string {
	rel, err := filepath.Rel(fromDir, absTarget)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + filepath.ToSlash(rel)
	} else {
		rel = filepath.ToSlash(rel)
	}
	return rel
}

func isUnderRoot(root, target string) bool {
	abs := filepath.Join(root, filepath.FromSlash(target))
	_, err := os.Stat(abs)
	return err == nil
}

func allowedFileForLang(path, lang string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	langExts := map[string][]string{
		"go":         {".go"},
		"python":     {".py"},
		"py":         {".py"},
		"js":         {".js", ".jsx"},
		"ts":         {".ts", ".tsx"},
		"typescript": {".ts", ".tsx"},
		"rust":       {".rs"},
		"java":       {".java"},
		"cpp":        {".cpp", ".cc", ".cxx", ".h"},
		"c":          {".c", ".h"},
		"rb":         {".rb"},
		"ruby":       {".rb"},
		"php":        {".php"},
		"kotlin":     {".kt"},
		"swift":      {".swift"},
		"dart":       {".dart"},
		"lua":        {".lua"},
		"r":          {".r"},
		"scala":      {".scala"},
	}

	exts, ok := langExts[strings.ToLower(lang)]
	if !ok {
		// fallback: accept only standard code files
		return allowedFile(path)
	}

	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// detectPromptLanguage tries to infer the programming language
// the user wants to work with based on their prompt text.
// It scans both explicit keywords (like “in Go” or “use TypeScript”)
// and implicit hints (like fenced code blocks).
func detectPromptLanguage(prompt string) string {
	prompt = strings.ToLower(prompt)

	// 1. Direct language keywords
	switch {
	case strings.Contains(prompt, "golang") || strings.Contains(prompt, " in go") || strings.Contains(prompt, "use go"):
		return "go"
	case strings.Contains(prompt, "python"):
		return "python"
	case strings.Contains(prompt, "typescript") || strings.Contains(prompt, " ts ") || strings.Contains(prompt, " in ts"):
		return "ts"
	case strings.Contains(prompt, "javascript") || strings.Contains(prompt, " js ") || strings.Contains(prompt, "node"):
		return "js"
	case strings.Contains(prompt, "rust"):
		return "rust"
	case strings.Contains(prompt, "java"):
		return "java"
	case strings.Contains(prompt, "c++") || strings.Contains(prompt, "cpp"):
		return "cpp"
	case strings.Contains(prompt, "c#") || strings.Contains(prompt, "csharp"):
		return "cs"
	case strings.Contains(prompt, "ruby"):
		return "rb"
	case strings.Contains(prompt, "php"):
		return "php"
	case strings.Contains(prompt, "kotlin"):
		return "kotlin"
	case strings.Contains(prompt, "swift"):
		return "swift"
	case strings.Contains(prompt, "dart"):
		return "dart"
	case strings.Contains(prompt, "lua"):
		return "lua"
	case strings.Contains(prompt, "scala"):
		return "scala"
	case strings.Contains(prompt, "r "):
		return "r"
	case strings.Contains(prompt, "haskell"):
		return "hs"
	}

	// 2. Fenced code hints (```go ... ```)
	re := regexp.MustCompile("```([a-zA-Z0-9_+.-]+)")
	if m := re.FindStringSubmatch(prompt); len(m) > 1 {
		return strings.ToLower(m[1])
	}

	// 3. Default fallback
	return "go" // sensible default for Lattice (Go-first environment)
}

// buildCodebaseContext walks the workspace, collects up to maxFiles under size limits,
// and returns a markdown-formatted CODEBASE SNAPSHOT containing only files matching langFilter.
func buildCodebaseContext(root string, maxFiles int, maxTotalBytes, perFileLimit int64, langFilter string) (string, int, int64) {
	var entries []fileEntry
	var total int64

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if isIgnoredDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		// filter by language
		if !allowedFileForLang(path, langFilter) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		entries = append(entries, fileEntry{Rel: rel, Abs: path, Size: info.Size()})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel })

	var included []fileEntry
	for _, e := range entries {
		if len(included) >= maxFiles {
			break
		}
		if total >= maxTotalBytes {
			break
		}
		included = append(included, e)
		capAdd := e.Size
		if capAdd > perFileLimit {
			capAdd = perFileLimit
		}
		total += capAdd
	}

	tree := buildTree(included)

	var filesSection strings.Builder
	for _, f := range included {
		content, _ := os.ReadFile(f.Abs)
		if int64(len(content)) > perFileLimit {
			content = content[:perFileLimit]
		}
		lang := fenceLangFromExt(filepath.Ext(f.Rel))
		filesSection.WriteString("\n### ")
		filesSection.WriteString(f.Rel)
		filesSection.WriteString("\n```")
		filesSection.WriteString(lang)
		filesSection.WriteString("\n")
		filesSection.Write(content)
		filesSection.WriteString("\n```\n")
	}

	var out strings.Builder
	out.WriteString("## CODEBASE SNAPSHOT\n")
	out.WriteString(fmt.Sprintf("- Root: `%s`\n", root))
	out.WriteString(fmt.Sprintf("- Files included: %d (limit %d)\n", len(included), maxFiles))
	out.WriteString(fmt.Sprintf("- Size included: %s (limit %s)\n", humanSize(total), humanSize(maxTotalBytes)))
	out.WriteString("\n### Tree\n```\n")
	out.WriteString(tree)
	out.WriteString("\n```\n")
	out.WriteString(filesSection.String())

	return out.String(), len(included), total
}

// collectAttachmentFiles gathers actual file contents (used in GenerateWithFiles)
// filtered by the selected programming language.
func collectAttachmentFiles(root string, maxFiles int, maxTotalBytes, perFileLimit int64, langFilter string) []models.File {
	var entries []fileEntry
	var total int64

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if isIgnoredDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !allowedFileForLang(path, langFilter) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		entries = append(entries, fileEntry{Rel: rel, Abs: path, Size: info.Size()})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel })

	var out []models.File
	for _, e := range entries {
		if len(out) >= maxFiles || total >= maxTotalBytes {
			break
		}
		b, err := os.ReadFile(e.Abs)
		if err != nil {
			continue
		}
		if int64(len(b)) > perFileLimit {
			b = b[:perFileLimit]
		}
		out = append(out, models.File{
			Name: e.Rel,
			MIME: mimeForPath(e.Rel),
			Data: b,
		})
		add := e.Size
		if add > perFileLimit {
			add = perFileLimit
		}
		total += add
	}
	return out
}

// planFile describes a planned file output
type planFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Lang string `json:"lang"`
	Goal string `json:"goal"`
}

// buildPlanningPrompt creates a plan describing the expected file structure
func (m *model) buildPlanningPrompt(userGoal string) ([]planFile, error) {
	const (
		maxFiles      = 300
		maxTotalBytes = int64(1_200_000)
		perFileLimit  = int64(80_000)
	)

	lang := detectPromptLanguage(userGoal)
	ctxBlock, nFiles, nBytes := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
	m.contextFiles, m.contextBytes = nFiles, nBytes
	attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)

	prompt := strings.Builder{}
	prompt.WriteString("You are a senior software planner. Generate a JSON array describing the files to be built step-by-step.\n")
	prompt.WriteString("Format: [{\"name\": \"file name\", \"path\": \"relative path\", \"lang\": \"language\", \"goal\": \"short purpose\"}]\n")
	prompt.WriteString("Do not include code, only planning metadata.\n\n")
	prompt.WriteString("### [WORKSPACE ROOT]\n")
	prompt.WriteString(m.working + "\n\n")
	prompt.WriteString(ctxBlock)
	prompt.WriteString("\n\n---\nGOAL:\n")
	prompt.WriteString(userGoal)

	raw, err := m.agent.GenerateWithFiles(m.ctx, "1", prompt.String(), attachments)
	if err != nil {
		raw, err = m.agent.Generate(m.ctx, "1", prompt.String())
		if err != nil {
			return nil, err
		}
	}

	// Extract JSON block
	re := regexp.MustCompile("(?s)```json\\s*(\\[.*?\\])\\s*```")
	matches := re.FindStringSubmatch(raw)
	var data []byte
	if len(matches) > 1 {
		data = []byte(matches[1])
	} else {
		data = []byte(strings.TrimSpace(raw))
	}

	var plan []planFile
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("invalid plan JSON: %v", err)
	}
	return plan, nil
}

// -----------------------------------------------------------------------------
// AUTO-SPLITTING STEPBUILDER
// -----------------------------------------------------------------------------

// buildStepPrompts breaks a large goal into several smaller sub-goals
func (m *model) buildStepPrompts(userGoal string) ([]string, error) {
	const (
		maxFiles      = 300
		maxTotalBytes = int64(1_200_000)
		perFileLimit  = int64(80_000)
	)
	lang := detectPromptLanguage(userGoal)
	ctxBlock, _, _ := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
	attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)

	prompt := strings.Builder{}
	prompt.WriteString("You are a senior software planner.\n")
	prompt.WriteString("Split the GOAL into 3–8 sequential sub-prompts, each focused on one major build area.\n")
	prompt.WriteString("Return JSON ONLY in this form:\n")
	prompt.WriteString("[\"sub-goal 1\", \"sub-goal 2\", ...]\n\n")
	prompt.WriteString("### [WORKSPACE ROOT]\n" + m.working + "\n\n")
	prompt.WriteString(ctxBlock)
	prompt.WriteString("\n---\nGOAL:\n" + userGoal)

	raw, err := m.agent.GenerateWithFiles(m.ctx, "1", prompt.String(), attachments)
	if err != nil {
		raw, err = m.agent.Generate(m.ctx, "1", prompt.String())
		if err != nil {
			return nil, err
		}
	}
	re := regexp.MustCompile("(?s)```json\\s*(\\[.*?\\])\\s*```")
	data := []byte(strings.TrimSpace(raw))
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		data = []byte(m[1])
	}
	var subs []string
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, fmt.Errorf("invalid stepbuild prompt JSON: %v", err)
	}
	return subs, nil
}

// runStepBuilderPhase runs one sub-prompt (a single phase) through the file-plan and build loop.
// It now generates files concurrently for maximum efficiency.
func (m *model) runStepBuilderPhase(subgoal string, stepIndex, total int) (string, error) {
	var log strings.Builder
	fmt.Fprintf(&log, "⚙️  Step %d/%d — %s\n", stepIndex, total, subgoal)

	// 1. Create a file plan for this subgoal.
	phase := stepPhase{Name: fmt.Sprintf("Step %d", stepIndex), Goal: subgoal}
	files, err := m.buildFilePlan(phase)
	if err != nil {
		return "", fmt.Errorf("failed to plan files for step %d: %v", stepIndex, err)
	}

	// 2. Set up concurrent generation.
	var wg sync.WaitGroup
	results := make(chan string, len(files))

	for j, fileToBuild := range files {
		wg.Add(1)
		go func(f planFile, fileIndex int) {
			defer wg.Done()

			index := fmt.Sprintf("%d.%d", stepIndex, fileIndex+1)
			m.thinking = fmt.Sprintf("building %s — %s", index, f.Name) // Note: race condition on m.thinking is ok for UI

			// Each goroutine gets its own tailored context window.
			const (
				maxFiles      = 300
				maxTotalBytes = int64(1_200_000)
				perFileLimit  = int64(80_000)
			)
			ctxBlock, _, _ := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, f.Lang)
			attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, f.Lang)

			sub := strings.Builder{}
			sub.WriteString("You are Vibe, the coding agent inside a TUI.\n")
			sub.WriteString(fmt.Sprintf("Generate ONLY ONE file for sub-goal '%s': %s\n\n", subgoal, f.Name))
			sub.WriteString("### [WORKSPACE ROOT]\n" + m.working + "\n\n")
			sub.WriteString(ctxBlock)
			sub.WriteString("\n---\nFILE SPEC:\n")
			sub.WriteString(fmt.Sprintf("%s — %s\n", f.Path, f.Goal))
			sub.WriteString("\nFollow OUTPUT CONTRACT: short plan → one fenced file block.")

			res, err := m.agent.GenerateWithFiles(m.ctx, "1", sub.String(), attachments)
			if err != nil {
				res, err = m.agent.Generate(m.ctx, "1", sub.String())
				if err != nil {
					results <- fmt.Sprintf("❌ failed to build %s: %v\n", f.Name, err)
					return
				}
			}

			m.saveCodeBlocks(res) // saveCodeBlocks is thread-safe enough for this use case
			results <- fmt.Sprintf("✅ %s\n", f.Path)
		}(fileToBuild, j)
	}

	// 3. Wait for all file generations to complete and collect results.
	wg.Wait()
	close(results)

	for res := range results {
		log.WriteString(res)
	}

	return log.String(), nil
}

// runStepBuilder now uses sub-prompts automatically
func (m *model) runStepBuilder(userGoal string) (string, error) {
	var log strings.Builder
	fmt.Fprintf(&log, "🧩 Auto StepBuild for GOAL:\n%s\n\n", userGoal)

	subprompts, err := m.buildStepPrompts(userGoal)
	if err != nil {
		return "", fmt.Errorf("failed to split goal into sub-prompts (falling back to single step): %v", err)
	}

	fmt.Fprintf(&log, "📋 %d step prompts generated:\n", len(subprompts))
	for i, s := range subprompts {
		fmt.Fprintf(&log, "  %d) %s\n", i+1, s)
	}
	fmt.Fprintln(&log)

	for i, sub := range subprompts {
		phaseLog, err := m.runStepBuilderPhase(sub, i+1, len(subprompts))
		if err != nil {
			fmt.Fprintf(&log, "⚠️  %v\n", err)
			continue
		}
		log.WriteString(phaseLog)
	}

	// Add a final tree view to show the result
	tree := buildTree(collectWorkspaceFiles(m.working))
	fmt.Fprintf(&log, "\nFinal workspace structure:\n%s\n", tree)

	fmt.Fprintln(&log, "🎉 Auto StepBuild complete!")
	return log.String(), nil
}

// stepPhase represents a high-level build phase (like a module or layer)
type stepPhase struct {
	Name  string     `json:"name"`
	Goal  string     `json:"goal"`
	Files []planFile `json:"files,omitempty"`
}

// buildFilePlan creates the list of files for a given build phase or subgoal
func (m *model) buildFilePlan(phase stepPhase) ([]planFile, error) {
	const (
		maxFiles      = 300
		maxTotalBytes = int64(1_200_000)
		perFileLimit  = int64(80_000)
	)

	lang := detectPromptLanguage(phase.Goal)
	ctxBlock, _, _ := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
	attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)

	prompt := strings.Builder{}
	prompt.WriteString("You are a senior code planner.\n")
	prompt.WriteString(fmt.Sprintf("For PHASE: %s — %s\n", phase.Name, phase.Goal))
	prompt.WriteString("Generate a JSON array describing the files to build.\n")
	prompt.WriteString("[{\"name\":\"...\",\"path\":\"...\",\"lang\":\"...\",\"goal\":\"...\"}]\n\n")
	prompt.WriteString("### [WORKSPACE ROOT]\n" + m.working + "\n\n")
	prompt.WriteString(ctxBlock)

	raw, err := m.agent.GenerateWithFiles(m.ctx, "1", prompt.String(), attachments)
	if err != nil {
		raw, err = m.agent.Generate(m.ctx, "1", prompt.String())
		if err != nil {
			return nil, err
		}
	}

	re := regexp.MustCompile("(?s)```json\\s*(\\[.*?\\])\\s*```")
	data := []byte(strings.TrimSpace(raw))
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		data = []byte(m[1])
	}

	var files []planFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("invalid file plan JSON: %v", err)
	}
	return files, nil
}

// collectWorkspaceFiles scans all allowed files for buildTree
func collectWorkspaceFiles(root string) []fileEntry {
	var out []fileEntry
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if allowedFile(p) {
			rel, _ := filepath.Rel(root, p)
			st, _ := os.Stat(p)
			out = append(out, fileEntry{Rel: rel, Abs: p, Size: st.Size()})
		}
		return nil
	})
	return out
}
