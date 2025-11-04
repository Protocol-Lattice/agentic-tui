package src

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	utcp "github.com/universal-tool-calling-protocol/go-utcp"

	agent "github.com/Protocol-Lattice/go-agent"
)

type mode int

const (
	modeDir mode = iota
	modeList
	modePrompt
	modeThinking
	modeChat
	modeResult
	modeUTCP
	modeUTCPArgs
	modeStepBuild
	modeRefactor
	modeSession
	modeSwarm
)

const logo = `
██╗      █████╗ ████████╗████████╗██╗ ██████╗███████╗
██║     ██╔══██╗╚══██╔══╝╚══██╔══╝██║██╔════╝██╔════╝
██║     ███████║   ██║      ██║   ██║██║     █████╗  
██║     ██╔══██║   ██║      ██║   ██║██║     ██╔══╝  
███████╗██║  ██║   ██║      ██║   ██║╚██████╗███████╗
╚══════╝╚═╝  ╚═╝   ╚═╝      ╚═╝   ╚═╝ ╚═════╝╚══════╝
              C O D E  ·  W E A V I N G  I N T E L L I G E N C E
`

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

type generateMsg struct {
	text string
	err  error
}

// stepBuildProgressMsg is sent for each incremental update from the step-builder.
type stepBuildProgressMsg struct {
	log string
}

// stepBuildCompleteMsg is sent when the entire step-build process is finished.
type stepBuildCompleteMsg struct {
	finalLog string
	err      error
}

type model struct {
	ctx        context.Context
	agent      *agent.Agent
	utcp       utcp.UtcpClientInterface
	working    string
	history    []string
	mode       mode
	prevMode   mode
	selected   plugin
	isThinking bool
	list       list.Model
	dirlist    list.Model
	textarea   textarea.Model
	viewport   viewport.Model
	spinner    spinner.Model
	thinking   string
	output     string
	width      int
	height     int
	style      styles

	Program *tea.Program
	mu      sync.Mutex
	// Context snapshot stats (set on each run)
	contextFiles int
	contextBytes int64

	sessionID         string
	sharedSpaces      []string
	transcriptPath    string
	lastTranscriptSig string
	syncInterval      time.Duration
	lockDir           string
	plannerQueue      chan string // new: queued logs for planner output

}

type styles struct {
	header        lipgloss.Style
	subtitle      lipgloss.Style
	list          lipgloss.Style
	listHeader    lipgloss.Style
	listItem      lipgloss.Style
	listSelected  lipgloss.Style
	textarea      lipgloss.Style
	help          lipgloss.Style
	footer        lipgloss.Style
	accent        lipgloss.Style
	error         lipgloss.Style
	success       lipgloss.Style
	thinking      lipgloss.Style
	status        lipgloss.Style
	statusRight   lipgloss.Style
	chatContainer lipgloss.Style
	subtle        lipgloss.Style
	center        lipgloss.Style
}

func NewModel(ctx context.Context, a *agent.Agent, u utcp.UtcpClientInterface, startDir string) *model {
	dirItems := loadDirs(startDir)
	dirDelegate := list.NewDefaultDelegate()
	dirList := list.New(dirItems, dirDelegate, 0, 0)
	dirList.Title = "Choose Working Directory"
	dirList.SetShowHelp(false)
	dirList.SetShowStatusBar(false)
	dirList.SetFilteringEnabled(false)

	l := list.New(defaultAgents(), list.NewDefaultDelegate(), 0, 0)
	l.Title = "Agents"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	ta := textarea.New()
	ta.Placeholder = "Describe your task or goal..."
	ta.Focus()
	ta.SetHeight(3)

	st := newStyles()

	vp := viewport.New(0, 0)
	vp.SetContent("Welcome to Lattice Code! Describe your task to get started.\n")

	s := spinner.New()
	s.Spinner = spinner.Line
	s.Style = st.thinking

	// Generate a random session ID for this run.
	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes) // Best effort, ignore error.
	sessionID := hex.EncodeToString(randBytes)

	m := &model{
		ctx:          ctx,
		agent:        a,
		utcp:         u,
		working:      startDir,
		history:      []string{startDir},
		mode:         modeDir,
		list:         l,
		dirlist:      dirList,
		textarea:     ta,
		viewport:     vp,
		spinner:      s,
		style:        st,
		syncInterval: time.Second,
		sessionID:    sessionID,
		plannerQueue: make(chan string, 100), // <-- add this

	}

	return m
}

func (m *model) renderOutput(sync bool) {
	m.viewport.SetContent(m.output)
	m.viewport.GotoBottom()
	if sync {
		m.persistTranscript()
	}
}

func (m *model) persistTranscript() {
	if m.transcriptPath == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.WriteFile(m.transcriptPath, []byte(m.output), 0o644); err != nil {
		return
	}
	m.lastTranscriptSig = hashString(m.output)
}

func newStyles() styles {
	return styles{
		header: lipgloss.NewStyle(). // Less prominent header
						Foreground(lipgloss.Color("#555")).
						Faint(true).
						Padding(0, 1),

		subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999999")).
			Padding(0, 1),

		list: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#AD8CFF")),

		listHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AD8CFF")).
			Bold(true).
			Padding(0, 1),

		listItem: lipgloss.NewStyle().
			Padding(0, 1),

		listSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00E6B8")).
			Bold(true),

		textarea: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#AD8CFF")),

		help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#777777")),

		footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#777777")).
			Faint(true),

		accent: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AD8CFF")),

		error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5C5C")).
			Bold(true),

		success: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3DDC97")).
			Bold(true),

		thinking: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3DDC97")), // Changed to green

		status: lipgloss.NewStyle().
			Background(lipgloss.Color("#AD8CFF")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1),

		statusRight: lipgloss.NewStyle().
			Inherit(lipgloss.NewStyle().
				Background(lipgloss.Color("#AD8CFF")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Padding(0, 1)).Align(lipgloss.Right),

		chatContainer: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#AD8CFF")).Padding(0, 1),

		subtle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999999")),

		center: lipgloss.NewStyle().
			Align(lipgloss.Center),
	}
}

func defaultAgents() []list.Item {
	return []list.Item{
		plugin{"orchestrator", "Split into subtasks and execute sequentially"},
		plugin{"architect", "High-level design and refactoring"},
		plugin{"coder", "Feature implementation and tests"},
		plugin{"reviewer", "Code review and optimization"},
		plugin{"shell", "Execute terminal commands"},
		plugin{"utcp", "Explore connected UTCP tools"},
	}
}

func (m *model) Init() tea.Cmd {
	return m.scheduleTranscriptTick()
}
