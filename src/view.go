package src

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
)

func (m *model) View() string {
	header := m.viewHeader()
	body := m.viewBody()
	footer := m.viewFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *model) viewHeader() string {
	// By setting and then unsetting the background, we make the spaces in the
	// logo string transparent, so only the characters themselves are colored.
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AD8CFF")).Bold(true).
		Background(lipgloss.Color("#000000")).UnsetBackground()
	subtitle := m.style.header.Render("Protocol Lattice Labs")
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
	default:
		return ""
	}
}

func (m *model) viewFooter() string {
	help := "ctrl+c: quit"
	if m.mode == modeChat {
		help += " | ctrl+d: change directory" // This is now handled by the modeDir case
	}
	if m.mode == modeDir {
		help += " | enter: select | ←/↑/↓/→: navigate"
	}

	return m.style.footer.Render(help)
}

func (m *model) viewDir() string {
	// Create a header for the directory view that shows the current path.
	pathHeader := m.style.subtitle.Render(fmt.Sprintf("Current: %s", m.working))
	dirView := lipgloss.JoinVertical(lipgloss.Left, pathHeader, m.dirlist.View())

	return dirView
}

func (m *model) viewList() string {
	return m.style.list.Render(m.list.View())
}

func (m *model) viewChat() string {
	// Status bar for the viewport
	status := lipgloss.JoinHorizontal(lipgloss.Top,
		m.style.status.Render(m.selected.name),
		m.style.statusRight.Render(fmt.Sprintf("CTX: %d files (%s)", m.contextFiles, HumanSize(m.contextBytes))),
	)

	// Main chat view with a border
	metaLines := []string{m.style.subtitle.Render(fmt.Sprintf("Working Directory: %s", m.working))}
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

func (m *model) viewResult() string {
	// This view is now effectively deprecated in favor of the unified chat view.
	// We keep it for any legacy paths that might still use it.
	return m.output
}
