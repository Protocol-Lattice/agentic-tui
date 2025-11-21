package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const Logo = `
██╗      █████╗ ████████╗████████╗██╗ ██████╗███████╗
██║     ██╔══██╗╚══██╔══╝╚══██╔══╝██║██╔════╝██╔════╝
██║     ███████║   ██║      ██║   ██║██║     █████╗  
██║     ██╔══██║   ██║      ██║   ██║██║     ██╔══╝  
███████╗██║  ██║   ██║      ██║   ██║╚██████╗███████╗
╚══════╝╚═╝  ╚═╝   ╚═╝      ╚═╝   ╚═╝ ╚═════╝╚══════╝
              C O D E  ·  W E A V I N G  I N T E L L I G E N C E
`

// Render generates the full UI string based on the provided state.
func Render(s State, styles Styles) string {
	header := renderHeader(styles)
	body := renderBody(s, styles)
	footer := renderFooter(s, styles)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func renderHeader(styles Styles) string {
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AD8CFF")).Bold(true).
		Background(lipgloss.Color("#000000")).UnsetBackground()
	subtitle := styles.Header.Render("Protocol Lattice")
	styledLogo := logoStyle.Render(Logo)

	return lipgloss.JoinVertical(lipgloss.Left, styledLogo, subtitle)
}

func renderFooter(s State, styles Styles) string {
	help := "ctrl+c: quit"
	if s.Mode == ModeDir {
		help += " | enter: select | ←/↑/↓/→: navigate"
	}
	return styles.Footer.Render(help)
}

func renderBody(s State, styles Styles) string {
	switch s.Mode {
	case ModeDir:
		return renderDir(s, styles)
	case ModeList:
		return renderList(s, styles)
	case ModeChat:
		return renderChat(s, styles)
	case ModeThinking:
		return renderThinking(s, styles)
	case ModeResult:
		return renderResult(s)
	case ModeSession:
		return renderSession(s, styles)
	case ModeSwarm:
		return renderSwarm(s, styles)
	default:
		return ""
	}
}

func renderDir(s State, styles Styles) string {
	pathHeader := styles.Subtitle.Render(fmt.Sprintf("Current: %s", s.WorkingDir))
	return lipgloss.JoinVertical(lipgloss.Left, pathHeader, s.DirList.View())
}

func renderList(s State, styles Styles) string {
	return styles.List.Render(s.List.View())
}

func renderChat(s State, styles Styles) string {
	var statusItems []string
	statusItems = append(statusItems, styles.Status.Render(fmt.Sprintf("SESSION: %s", s.SessionID)))
	if len(s.SharedSpaces) > 0 {
		statusItems = append(statusItems, styles.Status.Render(fmt.Sprintf("SWARM: %s", strings.Join(s.SharedSpaces, ", "))))
	}
	statusItems = append(statusItems, styles.StatusRight.Render(fmt.Sprintf("CTX: %d files (%s)", s.ContextFiles, humanSize(s.ContextBytes))))

	status := lipgloss.JoinHorizontal(lipgloss.Top, statusItems...)

	metaLines := []string{styles.Subtitle.Render(fmt.Sprintf("Working Directory: %s", s.WorkingDir))}
	if s.SelectedAgent != "" {
		metaLines = append(metaLines, styles.Subtle.Render(fmt.Sprintf("Agent: %s", s.SelectedAgent)))
	}
	if s.TranscriptPath != "" {
		rel := s.TranscriptPath
		if r, err := filepath.Rel(s.WorkingDir, s.TranscriptPath); err == nil {
			rel = r
		}
		metaLines = append(metaLines, styles.Subtle.Render(fmt.Sprintf("Shared chat log: %s", rel)))
	}
	chatView := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinVertical(lipgloss.Left, metaLines...),
		s.Viewport.View(),
		status,
		renderThinking(s, styles),
		s.TextArea.View(),
	)
	return styles.ChatContainer.Render(chatView)
}

func renderThinking(s State, styles Styles) string {
	if !s.IsThinking {
		return ""
	}
	return styles.Thinking.Render(fmt.Sprintf("Lattice %s %s", s.Spinner.View(), s.ThinkingText))
}

func renderResult(s State) string {
	return s.Output
}

func renderSession(s State, styles Styles) string {
	return lipgloss.JoinVertical(lipgloss.Left,
		styles.ListHeader.Render("Set Session ID"),
		styles.Subtle.Render("Changing the session ID isolates conversation history."),
		s.TextArea.View(),
		styles.Help.Render("enter: confirm | esc: cancel"),
	)
}

func renderSwarm(s State, styles Styles) string {
	return lipgloss.JoinVertical(lipgloss.Left,
		styles.ListHeader.Render("Set Swarm Spaces"),
		styles.Subtle.Render("Set comma-separated shared memory spaces for collaboration."),
		s.TextArea.View(),
		styles.Help.Render("enter: confirm | esc: cancel"),
	)
}

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
