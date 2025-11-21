package ui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
)

// Mode represents the current UI state
type Mode int

const (
	ModeDir Mode = iota
	ModeList
	ModePrompt
	ModeThinking
	ModeChat
	ModeResult
	ModeUTCP
	ModeUTCPArgs
	ModeStepBuild
	ModeRefactor
	ModeSession
	ModeSwarm
)

// State contains all the data required to render the UI.
// This decouples the renderer from the main application logic.
type State struct {
	Mode           Mode
	WorkingDir     string
	SessionID      string
	SharedSpaces   []string
	ContextFiles   int
	ContextBytes   int64
	TranscriptPath string
	IsThinking     bool
	ThinkingText   string
	Output         string
	SelectedAgent  string

	// Bubble Tea models
	List     list.Model
	DirList  list.Model
	TextArea textarea.Model
	Viewport viewport.Model
	Spinner  spinner.Model
}
