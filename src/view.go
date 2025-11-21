// path: src/view.go
package src

import (
	"github.com/Protocol-Lattice/lattice-code/src/ui"
)

// View delegates to the ui package renderer
func (m *model) View() string {
	state := ui.State{
		Mode:           m.mode,
		WorkingDir:     m.working,
		SessionID:      m.sessionID,
		SharedSpaces:   m.sharedSpaces,
		ContextFiles:   m.contextFiles,
		ContextBytes:   m.contextBytes,
		TranscriptPath: m.transcriptPath,
		IsThinking:     m.isThinking,
		ThinkingText:   m.thinking,
		Output:         m.output,
		SelectedAgent:  m.selected.name,
		List:           m.list,
		DirList:        m.dirlist,
		TextArea:       m.textarea,
		Viewport:       m.viewport,
		Spinner:        m.spinner,
	}

	return ui.Render(state, m.style)
}
