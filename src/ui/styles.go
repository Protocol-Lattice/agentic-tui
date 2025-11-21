package ui

import "github.com/charmbracelet/lipgloss"

type Styles struct {
	Header        lipgloss.Style
	Subtitle      lipgloss.Style
	List          lipgloss.Style
	ListHeader    lipgloss.Style
	ListItem      lipgloss.Style
	ListSelected  lipgloss.Style
	Textarea      lipgloss.Style
	Help          lipgloss.Style
	Footer        lipgloss.Style
	Accent        lipgloss.Style
	Error         lipgloss.Style
	Success       lipgloss.Style
	Thinking      lipgloss.Style
	Status        lipgloss.Style
	StatusRight   lipgloss.Style
	ChatContainer lipgloss.Style
	Subtle        lipgloss.Style
	Center        lipgloss.Style
}

func NewStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555")).
			Faint(true).
			Padding(0, 1),

		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999999")).
			Padding(0, 1),

		List: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#AD8CFF")),

		ListHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AD8CFF")).
			Bold(true).
			Padding(0, 1),

		ListItem: lipgloss.NewStyle().
			Padding(0, 1),

		ListSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00E6B8")).
			Bold(true),

		Textarea: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#AD8CFF")),

		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#777777")),

		Footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#777777")).
			Faint(true),

		Accent: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AD8CFF")),

		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5C5C")).
			Bold(true),

		Success: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3DDC97")).
			Bold(true),

		Thinking: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3DDC97")),

		Status: lipgloss.NewStyle().
			Background(lipgloss.Color("#AD8CFF")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1),

		StatusRight: lipgloss.NewStyle().
			Inherit(lipgloss.NewStyle().
				Background(lipgloss.Color("#AD8CFF")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Padding(0, 1)).Align(lipgloss.Right),

		ChatContainer: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#AD8CFF")).Padding(0, 1),

		Subtle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999999")),

		Center: lipgloss.NewStyle().
			Align(lipgloss.Center),
	}
}
