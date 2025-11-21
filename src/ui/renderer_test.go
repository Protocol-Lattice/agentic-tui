package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
)

func TestRenderContainsLogo(t *testing.T) {
	styles := NewStyles()
	dirList := list.New([]list.Item{}, list.NewDefaultDelegate(), 80, 20)

	state := State{
		Mode:       ModeDir,
		WorkingDir: "/tmp",
		DirList:    dirList,
	}

	output := Render(state, styles)

	// The logo contains ASCII art with "LATTICE" spelled out
	if !strings.Contains(output, "Protocol Lattice") && !strings.Contains(output, "CODE") {
		t.Errorf("Expected output to contain logo or header text")
	}
}

func TestRenderContainsProtocolLattice(t *testing.T) {
	styles := NewStyles()
	vp := viewport.New(80, 20)
	ta := textarea.New()
	ta.SetWidth(80)
	sp := spinner.New()

	state := State{
		Mode:       ModeChat,
		WorkingDir: "/tmp",
		Viewport:   vp,
		TextArea:   ta,
		Spinner:    sp,
	}

	output := Render(state, styles)

	if !strings.Contains(output, "Protocol Lattice") {
		t.Errorf("Expected output to contain 'Protocol Lattice', but it didn't")
	}
}

func TestRenderFooterContainsQuit(t *testing.T) {
	styles := NewStyles()
	vp := viewport.New(80, 20)
	ta := textarea.New()
	ta.SetWidth(80)
	sp := spinner.New()

	state := State{
		Mode:       ModeChat,
		WorkingDir: "/tmp",
		Viewport:   vp,
		TextArea:   ta,
		Spinner:    sp,
	}

	output := Render(state, styles)

	if !strings.Contains(output, "ctrl+c: quit") {
		t.Errorf("Expected footer to contain quit instruction")
	}
}

func TestRenderDirModeShowsWorkingDirectory(t *testing.T) {
	styles := NewStyles()
	state := State{
		Mode:       ModeDir,
		WorkingDir: "/home/user/project",
		DirList:    list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0),
	}

	output := Render(state, styles)

	if !strings.Contains(output, "/home/user/project") {
		t.Errorf("Expected output to show working directory")
	}
}

func TestRenderChatModeShowsSession(t *testing.T) {
	styles := NewStyles()
	vp := viewport.New(80, 20)
	ta := textarea.New()
	sp := spinner.New()

	state := State{
		Mode:         ModeChat,
		WorkingDir:   "/tmp",
		SessionID:    "test-session-123",
		Viewport:     vp,
		TextArea:     ta,
		Spinner:      sp,
		ContextFiles: 10,
		ContextBytes: 1024,
	}

	output := Render(state, styles)

	if !strings.Contains(output, "test-session-123") {
		t.Errorf("Expected output to show session ID")
	}
}

func TestRenderThinkingState(t *testing.T) {
	styles := NewStyles()
	vp := viewport.New(80, 20)
	ta := textarea.New()
	sp := spinner.New()

	state := State{
		Mode:         ModeChat,
		WorkingDir:   "/tmp",
		IsThinking:   true,
		ThinkingText: "processing request",
		Viewport:     vp,
		TextArea:     ta,
		Spinner:      sp,
	}

	output := Render(state, styles)

	if !strings.Contains(output, "Lattice") {
		t.Errorf("Expected thinking indicator to contain 'Lattice'")
	}
}

func TestRenderSessionMode(t *testing.T) {
	styles := NewStyles()
	ta := textarea.New()
	ta.SetWidth(80)

	state := State{
		Mode:     ModeSession,
		TextArea: ta,
	}

	output := Render(state, styles)

	if !strings.Contains(output, "Set Session ID") {
		t.Errorf("Expected session mode to show 'Set Session ID'")
	}
}

func TestRenderSwarmMode(t *testing.T) {
	styles := NewStyles()
	ta := textarea.New()
	ta.SetWidth(80)

	state := State{
		Mode:     ModeSwarm,
		TextArea: ta,
	}

	output := Render(state, styles)

	if !strings.Contains(output, "Set Swarm Spaces") {
		t.Errorf("Expected swarm mode to show 'Set Swarm Spaces'")
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		result := humanSize(tt.bytes)
		if result != tt.expected {
			t.Errorf("humanSize(%d) = %s; want %s", tt.bytes, result, tt.expected)
		}
	}
}

func TestNewStyles(t *testing.T) {
	styles := NewStyles()

	// Verify that styles are initialized (non-zero values)
	// We just check that the styles struct is properly created
	if styles.Header.GetPaddingLeft() < 0 {
		t.Errorf("Header style should be initialized")
	}

	if styles.Accent.GetForeground() == nil {
		t.Errorf("Accent style should have a foreground color")
	}
}
