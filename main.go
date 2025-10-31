package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	utcp "github.com/universal-tool-calling-protocol/go-utcp"

	agent "github.com/Protocol-Lattice/go-agent"
	adk "github.com/Protocol-Lattice/go-agent/src/adk"
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
	utcp         *utcp.UtcpClientInterface
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

func newModel(ctx context.Context, a *agent.Agent, u *utcp.UtcpClientInterface, startDir string) *model {
	dirItems := loadDirs(startDir)
	dirDelegate := list.NewDefaultDelegate()
	dirList := list.New(dirItems, dirDelegate, 0, 0)
	dirList.Title = "Choose Working Directory"
	dirList.SetShowHelp(false)
	dirList.SetShowStatusBar(false)
	dirList.SetFilteringEnabled(false)

	items := []list.Item{
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
	ta.Placeholder = "Describe your task or goal..."
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

	// -------------------------------------------------------------------------
	// WINDOW RESIZE
	// -------------------------------------------------------------------------
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(m.width-10, m.height-12)
		m.dirlist.SetSize(m.width-10, m.height-12)
		m.textarea.SetWidth(m.width - 10)
		return m, nil

	// -------------------------------------------------------------------------
	// KEYBOARD INPUT
	// -------------------------------------------------------------------------
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "left", "esc":
			// ---- Navigation Back Logic ----
			if m.mode == modeUTCPArgs {
				m.mode = modeUTCP
				return m, nil
			}
			if m.mode == modeUTCP {
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems([]list.Item{
					plugin{"architect", "High-level design and refactoring"},
					plugin{"coder", "Feature implementation and tests"},
					plugin{"reviewer", "Code review and optimization"},
					plugin{"utcp", "Explore connected UTCP tools"},
				})
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
					m.list.SetItems([]list.Item{
						plugin{"architect", "High-level design and refactoring"},
						plugin{"coder", "Feature implementation and tests"},
						plugin{"reviewer", "Code review and optimization"},
						plugin{"utcp", "Explore connected UTCP tools"},
					})
				}
				m.textarea.Reset()
				return m, nil
			}
			if m.mode == modePrompt {
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems([]list.Item{
					plugin{"architect", "High-level design and refactoring"},
					plugin{"coder", "Feature implementation and tests"},
					plugin{"reviewer", "Code review and optimization"},
					plugin{"utcp", "Explore connected UTCP tools"},
				})
				m.textarea.Reset()
				return m, nil
			}

		case "enter":
			switch m.mode {

			// ---- Directory selection ----
			case modeDir:
				if i, ok := m.dirlist.SelectedItem().(dirItem); ok {
					info, err := os.Stat(i.path)
					if err == nil && info.IsDir() {
						m.history = append(m.history, i.path)
						m.working = i.path
						m.dirlist.SetItems(loadDirs(i.path))
						return m, nil
					}
				}
				m.mode = modeList
				return m, nil

			// ---- Agent list ----
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

			// ---- UTCP tools list ----
			case modeUTCP:
				if i, ok := m.list.SelectedItem().(utcpItem); ok {
					m.selectedUTCP = i
					m.prevMode = m.mode
					m.mode = modeUTCPArgs
					m.textarea.SetValue("{\n  \n}")
					m.textarea.Focus()
				}
				return m, nil

			// ---- UTCP args editor ----
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
						stream, err := (*m.utcp).CallToolStream(m.ctx, m.selectedUTCP.name, args)
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

					res, err := (*m.utcp).CallTool(m.ctx, m.selectedUTCP.name, args)
					if err != nil {
						return generateMsg{"", err}
					}
					return generateMsg{fmt.Sprintf("%v", res), nil}
				}
				return m, tea.Batch(cmd, thinkingTick())

			// ---- Inline prompt (new @utcp support) ----
			case modePrompt:
				prompt := strings.TrimSpace(m.textarea.Value())
				if prompt == "" {
					return m, nil
				}
				m.prevMode = m.mode
				m.mode = modeThinking
				m.output = ""
				m.thinking = "thinking"

				// Handle inline UTCP calls
				if strings.HasPrefix(prompt, "@utcp ") {
					cmd := func() tea.Msg {
						res, err := m.runUTCPInline(prompt)
						if err != nil {
							return generateMsg{"", err}
						}
						return generateMsg{res, nil}
					}
					return m, tea.Batch(cmd, thinkingTick())
				}

				// Default: normal agent request
				_, err := m.agent.Generate(context.Background(), "1", prompt)
				if err != nil {
					m.mode = modeResult
					m.output = m.style.error.Render(fmt.Sprintf("❌ %v", err))
					return m, nil
				}
				return m, tea.Batch(thinkingTick())

			// ---- Results view ----
			case modeResult, modeDone:
				switch m.prevMode {
				case modeUTCPArgs, modeUTCP:
					m.mode = modeUTCP
					m.list.Title = "UTCP Tools"
					m.list.SetItems(m.loadUTCPTools())
				default:
					m.mode = modeList
					m.list.Title = "Agents"
					m.list.SetItems([]list.Item{
						plugin{"architect", "High-level design and refactoring"},
						plugin{"coder", "Feature implementation and tests"},
						plugin{"reviewer", "Code review and optimization"},
						plugin{"utcp", "Explore connected UTCP tools"},
					})
				}
				return m, nil
			}
		}

	// -------------------------------------------------------------------------
	// ASYNC MESSAGES
	// -------------------------------------------------------------------------
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

	// -------------------------------------------------------------------------
	// CHILD COMPONENT UPDATES
	// -------------------------------------------------------------------------
	var cmd tea.Cmd
	switch m.mode {
	case modeDir:
		m.dirlist, cmd = m.dirlist.Update(msg)
	case modeList, modeUTCP:
		m.list, cmd = m.list.Update(msg)
	case modePrompt:
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.mode = modeList
			m.list.Title = "Agents"
			m.list.SetItems([]list.Item{
				plugin{"architect", "High-level design and refactoring"},
				plugin{"coder", "Feature implementation and tests"},
				plugin{"reviewer", "Code review and optimization"},
				plugin{"utcp", "Explore connected UTCP tools"},
			})
			m.textarea.Reset()
			return m, nil
		}
		m.textarea, cmd = m.textarea.Update(msg)
	case modeUTCPArgs:
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.mode = modeUTCP
			return m, nil
		}
		m.textarea, cmd = m.textarea.Update(msg)
	}
	return m, cmd
}

// -----------------------------------------------------------------------------
// VIEW
// -----------------------------------------------------------------------------

func (m *model) View() string {
	header := lipgloss.JoinVertical(lipgloss.Left,
		m.style.header.Render("Lattice Code — Protocol Lattice Labs"),
		m.style.subtle.Render(fmt.Sprintf("Workspace: %s", m.working)),
	)

	var body string
	switch m.mode {
	case modeDir:
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.style.accent.Render("Navigate to your working directory"),
			"",
			m.dirlist.View(),
			"",
			m.style.footer.Render("[↑↓] Move │ [←] Up │ [Enter] Open │ [Ctrl+C] Quit"),
		)

	case modeList:
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.list.View(),
			"",
			m.style.footer.Render("[↑↓] Select │ [Enter] Choose │ [Esc] Quit"),
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
			m.style.footer.Render("[Enter/Esc] Back │ [Ctrl+C] Quit"),
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
// UTCP TOOL LIST LOADER
// -----------------------------------------------------------------------------

func (m *model) loadUTCPTools() []list.Item {
	if m.utcp == nil || *m.utcp == nil {
		return []list.Item{utcpItem{"(no UTCP client)", "none", "UTCP unavailable", false}}
	}

	tools, err := (*m.utcp).SearchTools("", 50)
	if err != nil {
		return []list.Item{utcpItem{"(error)", "none", err.Error(), false}}
	}

	items := make([]list.Item, 0, len(tools))
	for _, t := range tools {
		isStream := strings.Contains(strings.ToLower(t.Name), "stream")
		items = append(items, utcpItem{
			name:   t.Name,
			desc:   t.Description,
			stream: isStream,
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

	// Remove prefix and split into name + JSON
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

	if m.utcp == nil || *m.utcp == nil {
		return "", fmt.Errorf("UTCP client unavailable")
	}

	if isStream {
		stream, err := (*m.utcp).CallToolStream(m.ctx, toolName, args)
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

	res, err := (*m.utcp).CallTool(m.ctx, toolName, args)
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
	if path != "/" {
		items = append(items, dirItem{name: "../", path: filepath.Dir(path)})
	}
	for _, e := range entries {
		if e.IsDir() {
			items = append(items, dirItem{name: e.Name() + "/", path: filepath.Join(path, e.Name())})
		}
	}
	if len(items) == 0 {
		items = append(items, dirItem{name: "(empty)", path: path})
	}
	return items
}

// -----------------------------------------------------------------------------
// CODE SAVING HELPERS
// -----------------------------------------------------------------------------

var fenceRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_+\\.-]*)\\s*\\n(.*?)\\n```")

func (m *model) saveCodeBlocks(s string) {
	m.output += "\n---\n"
	matches := fenceRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		m.output += m.style.subtle.Render("No code blocks detected.\n")
		return
	}

	for idx, mth := range matches {
		lang := strings.TrimSpace(mth[1])
		code := strings.TrimSpace(mth[2])
		filename := m.guessFilename(lang, code, idx)
		_ = os.MkdirAll(filepath.Dir(filename), 0o755)
		if err := os.WriteFile(filename, []byte(code+"\n"), 0o644); err != nil {
			m.output += m.style.error.Render(fmt.Sprintf("❌ failed to save %s: %v\n", filename, err))
			continue
		}
		m.output += m.style.success.Render(fmt.Sprintf("💾 saved %s\n", filename))
	}
}

func (m *model) guessFilename(lang, code string, index int) string {
	exts := map[string]string{
		"go": "go", "py": "py", "rs": "rs", "cpp": "cpp",
		"js": "js", "ts": "ts", "sh": "sh", "html": "html",
		"css": "css", "json": "json", "yml": "yml", "md": "md", "txt": "txt",
	}
	ext := exts[strings.ToLower(lang)]
	if ext == "" {
		ext = "txt"
	}
	if strings.Contains(code, "func main(") || strings.Contains(code, "def main(") {
		return filepath.Join(m.working, "main."+ext)
	}
	sum := sha1.Sum([]byte(code))
	short := hex.EncodeToString(sum[:3])
	return filepath.Join(m.working, fmt.Sprintf("snippet_%s.%s", short, ext))
}

// -----------------------------------------------------------------------------
// AGENT & UTCP BUILDERS
// -----------------------------------------------------------------------------

func buildAgent(ctx context.Context) (*agent.Agent, error) {
	embedder := memory.AutoEmbedder()
	opts := memory.DefaultOptions()
	builder, err := adk.New(
		ctx,
		adk.WithDefaultSystemPrompt("You are a coding agent. Generate complete programs in any language."),
		adk.WithModules(
			adkmodules.InMemoryMemoryModule(512, embedder, &opts),
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

func buildUTCP(ctx context.Context) (*utcp.UtcpClientInterface, error) {
	cfg := &utcp.UtcpClientConfig{ProvidersFilePath: "provider.json"}
	client, err := utcp.NewUTCPClient(ctx, cfg, nil, nil)
	if err != nil {
		return nil, err
	}
	return &client, nil
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
