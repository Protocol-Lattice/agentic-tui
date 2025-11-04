package src

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Protocol-Lattice/go-agent/src/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// codegenStatusMsg is sent from the locking mechanism to update the UI.
type codegenStatusMsg struct {
	msg string
	err error
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case transcriptTickMsg:
		var cmds []tea.Cmd
		if cmd := m.readTranscriptCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if next := m.scheduleTranscriptTick(); next != nil {
			cmds = append(cmds, next)
		}
		return m, tea.Batch(cmds...)

	case transcriptSyncMsg:
		if msg.err != nil {
			if errors.Is(msg.err, os.ErrNotExist) {
				m.persistTranscript()
			}
			return m, nil
		}
		if msg.checksum != m.lastTranscriptSig {
			m.mu.Lock()
			m.output = msg.content
			m.lastTranscriptSig = msg.checksum
			m.mu.Unlock()
			m.renderOutput(false)
		}
		return m, nil

	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(m.viewHeader())
		footerHeight := lipgloss.Height(m.viewFooter())
		chatContainerVPadding := m.style.chatContainer.GetVerticalPadding()
		chatContainerHPadding := m.style.chatContainer.GetHorizontalPadding()
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(m.width-chatContainerHPadding-2, m.height-headerHeight-footerHeight-chatContainerVPadding-2)
		m.dirlist.SetSize(m.width, m.height-headerHeight-footerHeight-2)                                             // No container padding
		m.textarea.SetWidth(m.width - chatContainerHPadding - 2)                                                     // -2 for border
		m.viewport.Width = m.width - chatContainerHPadding - 2                                                       // -2 for border
		m.viewport.Height = m.height - headerHeight - footerHeight - m.textarea.Height() - chatContainerVPadding - 4 // -4 for subtitle, status, thinking
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {

		case "ctrl+c":
			return m, tea.Quit

		case "ctrl+d": // New: shortcut to change directory
			m.mode = modeDir
			return m, nil

		case "ctrl+s": // New: set session ID
			m.prevMode = m.mode
			m.mode = modeSession
			m.textarea.Placeholder = "Enter new session ID..."
			m.textarea.SetValue(m.sessionID)
			m.textarea.Focus()
			return m, nil

		case "ctrl+w": // New: set sWarm spaces
			m.prevMode = m.mode
			m.mode = modeSwarm
			m.textarea.Placeholder = "Enter shared spaces (comma-separated)..."
			m.textarea.SetValue(strings.Join(m.sharedSpaces, ", "))
			m.textarea.Focus()
			return m, nil

		case "left":
			if m.mode == modeDir {
				parent := filepath.Dir(m.working)
				if parent != m.working { // This check is sufficient and correct
					m.working = parent
					items := loadDirs(m.working)
					m.dirlist.SetItems(items)
					m.dirlist.Select(0)
				}
				return m, nil
			}
			if m.mode == modeUTCPArgs {
				m.mode = modeUTCP
				return m, nil
			}
			if m.mode == modeUTCP {
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
				return m, nil
			}
			if m.mode == modeResult {
				switch m.prevMode {
				default:
					m.mode = modeList
					m.list.Title = "Agents"
					m.list.SetItems(defaultAgents())
				}
				m.textarea.Reset()
				return m, nil
			}
			if m.mode == modePrompt || m.mode == modeChat {
				m.mode = modeChat
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
				m.textarea.Reset()
				return m, nil
			}

		case "esc":
			switch m.mode {
			case modePrompt, modeResult, modeUTCPArgs, modeChat, modeSession, modeSwarm:
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
				m.textarea.Reset()
			case modeUTCP:
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
			}
			return m, nil

		case "enter":
			switch m.mode {

			case modeList:
				if i, ok := m.list.SelectedItem().(plugin); ok {
					m.selected = i
					m.prevMode = m.mode
					m.mode = modeChat
					m.refreshContext() // Refresh context on agent selection
					m.textarea.Focus()
				}
				return m, nil

			case modeDir:
				item, ok := m.dirlist.SelectedItem().(dirItem)
				if !ok {
					return m, nil
				}

				// --- Confirm current directory ---
				if strings.HasPrefix(item.name, "✅") {
					m.mode = modeChat // Go to chat after selecting dir
					m.list.Title = fmt.Sprintf("📁 %s", filepath.Base(m.working))
					m.list.SetItems(defaultAgents())
					m.refreshContext() // Refresh context after confirming directory
					return m, nil
				}

				// --- Go up one level ---
				if item.name == "⬆️ ../" {
					parent := filepath.Dir(m.working)
					if parent != m.working {
						m.working = parent
						items := loadDirs(m.working)
						m.dirlist.SetItems(items)
						m.dirlist.Select(0)
					}
					return m, nil
				}

				// --- Enter a subfolder ---
				info, err := os.Stat(item.path)
				if err == nil && info.IsDir() {
					m.working = item.path
					items := loadDirs(m.working)
					m.dirlist.SetItems(items)
					m.dirlist.Select(0)
					return m, nil
				}

			case modePrompt:
				raw := strings.TrimSpace(m.textarea.Value())
				if raw == "" {
					return m, nil
				}
				return m.runPrompt(raw)

			case modeChat:
				raw := strings.TrimSpace(m.textarea.Value())
				if raw == "" {
					return m, nil
				}

				// Handle direct UTCP calls from chat
				if strings.HasPrefix(raw, "@utcp ") {
					jsonStr := strings.TrimSpace(strings.TrimPrefix(raw, "@utcp "))
					if jsonStr == "" {
						m.output += m.style.error.Render("❌ UTCP call requires a JSON payload.\n")
						m.renderOutput(true)
						return m, nil
					}

					var payload struct {
						Tool   string         `json:"tool"`
						Args   map[string]any `json:"args"`
						Stream bool           `json:"stream"`
					}

					if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
						m.output += m.style.error.Render(fmt.Sprintf("❌ Invalid JSON for UTCP call: %v\n", err))
						m.renderOutput(true)
						return m, nil
					}

					if payload.Tool == "" {
						m.output += m.style.error.Render("❌ UTCP JSON payload must include a 'tool' name.\n")
						m.renderOutput(true)
						return m, nil
					}

					m.isThinking = true
					m.thinking = "calling UTCP tool"
					m.textarea.Reset()

					cmd := func() tea.Msg {
						if payload.Stream {
							return m.callUTCPStream(payload.Tool, payload.Args)
						}
						return m.callUTCP(payload.Tool, payload.Args)
					}
					return m, tea.Batch(cmd, m.spinner.Tick)
				} else {
					return m.runPrompt(raw)
				}

			case modeSession:
				newID := strings.TrimSpace(m.textarea.Value())
				if newID != "" {
					m.sessionID = newID
				}
				m.mode = modeChat
				m.textarea.Reset()
				m.textarea.Placeholder = "Describe your task or goal..."
				return m, nil

			case modeSwarm:
				spacesStr := strings.TrimSpace(m.textarea.Value())
				if spacesStr == "" {
					m.sharedSpaces = nil
				} else {
					m.sharedSpaces = strings.FieldsFunc(spacesStr, func(c rune) bool {
						return c == ',' || unicode.IsSpace(c)
					})
				}
				m.mode = modeChat
				m.textarea.Reset()
				m.textarea.Placeholder = "Describe your task or goal..."
				return m, nil
			}
		}

	// --- Handle final message from a generation task ---
	case generateMsg: // This is the final message from a generation task
		m.isThinking = false
		if msg.err != nil {
			m.output += m.style.error.Render(fmt.Sprintf("❌ %v\n", msg.err))
		} else {
			m.output += msg.text
			if msg.text != "" && !strings.HasSuffix(msg.text, "\n") {
				m.output += "\n"
			}
		}
		m.refreshContext() // Refresh context after generation and file I/O
		m.renderOutput(true)
		return m, nil

	// --- Handle real-time progress from step-builder ---
	case stepBuildProgressMsg:
		m.output += msg.log // Append new progress to the output.
		m.renderOutput(true)
		return m, nil

	case stepBuildCompleteMsg:
		m.isThinking = false
		m.thinking = ""
		// If finalLog is empty, it means the process finished but we don't want to overwrite the streamed output.
		if msg.err != nil {
			if !strings.HasSuffix(m.output, "\n") {
				m.output += "\n"
			}
			m.output += m.style.error.Render(fmt.Sprintf("❌ %v\n", msg.err))
		} else if msg.finalLog != "" {
			m.output = msg.finalLog
		}
		m.renderOutput(true)
		// No further tick needed, the process is complete.
		return m, nil

	case codegenStatusMsg:
		if msg.err != nil {
			m.output += m.style.error.Render(fmt.Sprintf("❌ %v\n", msg.err))
		} else if msg.msg != "" {
			m.output += m.style.subtle.Render(msg.msg + "\n")
		}
		m.renderOutput(true)
		// This is a status update, so we don't need to return a command.
		// The spinner is already ticking from the runPrompt command.
		return m, nil

	}
	var cmd tea.Cmd
	var newCmd tea.Cmd // Use a new variable for commands from the switch
	switch m.mode {
	case modeDir:
		m.dirlist, newCmd = m.dirlist.Update(msg)
	case modeList, modeUTCP:
		m.list, newCmd = m.list.Update(msg)
	case modePrompt, modeUTCPArgs, modeChat, modeSession, modeSwarm:
		var textareaCmd, viewportCmd tea.Cmd
		m.textarea, textareaCmd = m.textarea.Update(msg)
		m.viewport, viewportCmd = m.viewport.Update(msg)
		newCmd = tea.Batch(textareaCmd, viewportCmd)
	}
	cmd = tea.Batch(cmd, newCmd) // Batch commands from the switch with existing commands

	if m.isThinking {
		var spinnerCmd tea.Cmd
		m.spinner, spinnerCmd = m.spinner.Update(msg)
		cmd = tea.Batch(cmd, spinnerCmd)
	}
	return m, cmd
}

func (m *model) callUTCP(toolName string, args map[string]any) tea.Msg {
	res, err := m.utcp.CallTool(m.ctx, toolName, args)
	if err != nil {
		return generateMsg{"", err}
	}
	return generateMsg{fmt.Sprintf("%v", res), nil}
}

func (m *model) callUTCPStream(toolName string, args map[string]any) tea.Msg {
	stream, err := m.utcp.CallToolStream(m.ctx, toolName, args)
	if err != nil {
		return generateMsg{"", err}
	}
	var out strings.Builder
	out.WriteString(m.style.accent.Render(fmt.Sprintf("UTCP Stream (%s):", toolName)) + "\n")
	for {
		item, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			out.WriteString("\n" + m.style.error.Render(fmt.Sprintf("❌ Stream error: %v", err)))
			break // Stop on stream error
		}
		// Render each item as it arrives
		// This part is tricky in a non-streaming UI update model.
		// For now, we buffer and return one message.
		// A more advanced implementation would use tea.Cmd to send progress messages.
		out.WriteString(fmt.Sprintf("%v\n", item))
	}
	return generateMsg{out.String(), nil}
}

func (m *model) runPrompt(raw string) (*model, tea.Cmd) {
	m.textarea.Reset()

	m.output += m.style.accent.Render("You: ") + raw + "\n\n"
	m.renderOutput(true)

	m.isThinking = true
	m.thinking = "thinking"

	cmd := func() tea.Msg {
		// Orchestrate, persist, run, and auto-repair if enabled
		_, tree := m.refreshContext()
		prompt := fmt.Sprintf("File tree:\n%s\n\nsubagent:%s %s", tree, m.selected.name, raw)
		result, err := RunHeadless(m.ctx, m.agent, m.working, prompt)
		if err != nil {
			return generateMsg{"", err}
		}

		var out strings.Builder
		out.WriteString(m.style.accent.Render(m.selected.name+":") + "\n")
		out.WriteString("\n---\n")

		// Report file actions
		for _, action := range result.Actions {
			switch action.Action {
			case "saved":
				out.WriteString(m.style.success.Render(fmt.Sprintf("💾 Saved %s\n", action.Path)))
				if strings.TrimSpace(action.Diff) != "" {
									out.WriteString(m.style.subtle.Render("```diff") + "\n")
									out.WriteString(action.Diff)
									out.WriteString(m.style.subtle.Render("```") + "\n")				}
			case "deleted", "removed":
				out.WriteString(m.style.subtle.Render(fmt.Sprintf("🧹 %s %s\n", strings.Title(action.Action), action.Path)))
			case "error":
				out.WriteString(m.style.error.Render(fmt.Sprintf("❌ %s\n", action.Message)))
			case "info":
				out.WriteString(m.style.subtle.Render(fmt.Sprintf("ℹ️ %s\n", action.Message)))
			}
		}

		// Friendly hint to run locally

		return generateMsg{out.String(), nil}
	}

	return m, tea.Batch(cmd, m.spinner.Tick)
}

func (m *model) refreshContext() ([]models.File, string) {
	// An empty string for the language filter will include all supported file types.
	lang := ""
	// Increase limits to include a much larger portion of the codebase.
	// maxFiles: 1000, maxTotalBytes: 10MB, perFileLimit: 100KB
	files, includedEntries := collectAttachmentFiles(m.working, 1000, 10000000, 100000, lang)
	var totalBytes int64
	for _, f := range files {
		totalBytes += int64(len(f.Data))
	}
	m.contextFiles = len(files)
	m.contextBytes = totalBytes

	tree := buildTree(includedEntries)
	return files, tree
}
