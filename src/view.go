package src

import (
	"fmt"

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
		help += " | ctrl+d: change directory"
	}

	return m.style.footer.Render(help)
}

func (m *model) viewDir() string {
	return m.style.list.Render(m.dirlist.View())
}

func (m *model) viewList() string {
	return m.style.list.Render(m.list.View())
}

func (m *model) viewChat() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		m.style.subtitle.Render(fmt.Sprintf("Working Directory: %s", m.working)),
		m.viewport.View(),
		m.viewThinking(),
		m.textarea.View(),
	)
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
