package src

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type transcriptTickMsg struct{}

type transcriptSyncMsg struct {
	content  string
	checksum string
	err      error
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (m *model) scheduleTranscriptTick() tea.Cmd {
	if m.transcriptPath == "" || m.syncInterval <= 0 {
		return nil
	}
	interval := m.syncInterval
	return tea.Tick(interval, func(time.Time) tea.Msg {
		return transcriptTickMsg{}
	})
}

func (m *model) readTranscriptCmd() tea.Cmd {
	path := m.transcriptPath
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return transcriptSyncMsg{err: err}
			}
			return transcriptSyncMsg{err: err}
		}
		content := string(data)
		return transcriptSyncMsg{content: content, checksum: hashString(content)}
	}
}
