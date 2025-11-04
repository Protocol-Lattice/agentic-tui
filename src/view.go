// path: src/view.go
package src

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *model) View() string {
	header := m.viewHeader()
	body := m.viewBody()
	footer := m.viewFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *model) viewHeader() string {
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AD8CFF")).Bold(true).
		Background(lipgloss.Color("#000000")).UnsetBackground()
	subtitle := m.style.header.Render("Protocol Lattice")
	styledLogo := logoStyle.Render(logo)

	return lipgloss.JoinVertical(lipgloss.Left, styledLogo, subtitle)
}

func (m *model) viewBody() string {
	switch m.mode {
	case modeDir:
		return m.viewDir()
	case modeList:
		return m.viewList()
	case modeChat:
		return m.viewChat()
	case modeThinking:
		return m.viewThinking()
	case modeResult:
		return m.viewResult()
	case modeSession:
		return m.viewSession()
	case modeSwarm:
		return m.viewSwarm()
	default:
		return ""
	}
}

func (m *model) viewFooter() string {
	help := "ctrl+c: quit"
	if m.mode == modeDir {
		help += " | enter: select | ←/↑/↓/→: navigate"
	}
	return m.style.footer.Render(help)
}

func (m *model) viewDir() string {
	pathHeader := m.style.subtitle.Render(fmt.Sprintf("Current: %s", m.working))
	dirView := lipgloss.JoinVertical(lipgloss.Left, pathHeader, m.dirlist.View())
	return dirView
}

func (m *model) viewList() string {
	return m.style.list.Render(m.list.View())
}

func (m *model) viewChat() string {
	var statusItems []string
	statusItems = append(statusItems, m.style.status.Render(fmt.Sprintf("SESSION: %s", m.sessionID)))
	if len(m.sharedSpaces) > 0 {
		statusItems = append(statusItems, m.style.status.Render(fmt.Sprintf("SWARM: %s", strings.Join(m.sharedSpaces, ", "))))
	}
	statusItems = append(statusItems, m.style.statusRight.Render(fmt.Sprintf("CTX: %d files (%s)", m.contextFiles, HumanSize(m.contextBytes))))

	status := lipgloss.JoinHorizontal(lipgloss.Top, statusItems...)

	metaLines := []string{m.style.subtitle.Render(fmt.Sprintf("Working Directory: %s", m.working))}
	if m.selected.name != "" {
		metaLines = append(metaLines, m.style.subtle.Render(fmt.Sprintf("Agent: %s", m.selected.name)))
	}
	if m.transcriptPath != "" {
		rel := m.transcriptPath
		if r, err := filepath.Rel(m.working, m.transcriptPath); err == nil {
			rel = r
		}
		metaLines = append(metaLines, m.style.subtle.Render(fmt.Sprintf("Shared chat log: %s", rel)))
	}
	chatView := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinVertical(lipgloss.Left, metaLines...),
		m.viewport.View(),
		status,
		m.viewThinking(),
		m.textarea.View(),
	)
	return m.style.chatContainer.Render(chatView)
}

func (m *model) viewThinking() string {
	if !m.isThinking {
		return ""
	}
	return m.style.thinking.Render(fmt.Sprintf("Lattice %s %s", m.spinner.View(), m.thinking))
}

func (m *model) viewResult() string { return m.output }

func (m *model) viewSession() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		m.style.listHeader.Render("Set Session ID"),
		m.style.subtle.Render("Changing the session ID isolates conversation history."),
		m.textarea.View(),
		m.style.help.Render("enter: confirm | esc: cancel"),
	)
}

func (m *model) viewSwarm() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		m.style.listHeader.Render("Set Swarm Spaces"),
		m.style.subtle.Render("Set comma-separated shared memory spaces for collaboration."),
		m.textarea.View(),
		m.style.help.Render("enter: confirm | esc: cancel"),
	)
}
