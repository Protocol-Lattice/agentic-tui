package src

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
				case modeUTCPArgs, modeUTCP:
					m.mode = modeUTCP
					m.list.Title = "UTCP Tools"
					m.list.SetItems(m.loadUTCPTools())
				default:
					m.mode = modeList
					m.list.Title = "Agents"
					m.list.SetItems(defaultAgents())
				}
				m.textarea.Reset()
				return m, nil
			}
			if m.mode == modePrompt || m.mode == modeChat {
				m.mode = modeList
				m.list.Title = "Agents"
				m.list.SetItems(defaultAgents())
				m.textarea.Reset()
				return m, nil
			}

		case "esc":
			switch m.mode {
			case modePrompt, modeResult, modeUTCPArgs, modeChat:
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

			case modeList:
				if i, ok := m.list.SelectedItem().(plugin); ok {
					if i.name == "utcp" {
						m.mode = modeUTCP
						m.list.SetItems(m.loadUTCPTools())
						m.list.Title = "UTCP Tools"
						return m, nil
					}
					m.selected = i
					m.mode = modeChat
				}
				return m, nil

			case modeUTCP:
				if i, ok := m.list.SelectedItem().(utcpItem); ok {
					m.selectedUTCP = i
					m.prevMode = m.mode
					m.mode = modeUTCPArgs
					m.textarea.SetValue("{\n  \n}")
					m.textarea.Focus()
				}
				return m, nil

			case modeUTCPArgs:
				prompt := strings.TrimSpace(m.textarea.Value())
				if prompt == "" {
					return m, nil
				}

				var args map[string]any
				if err := json.Unmarshal([]byte(prompt), &args); err != nil {
					m.output = m.style.error.Render(fmt.Sprintf("Invalid JSON: %v", err))
					m.renderOutput(true)
					m.mode = modeResult
					return m, nil
				}

				m.prevMode = m.mode
				m.isThinking = true
				m.output = ""
				m.thinking = "thinking"

				cmd := func() tea.Msg {
					if m.selectedUTCP.stream {
						stream, err := m.utcp.CallToolStream(m.ctx, m.selectedUTCP.name, args)
						if err != nil {
							return generateMsg{"", err}
						}
						var out strings.Builder
						for {
							item, err := stream.Next()
							if err == io.EOF {
								break
							}
							if err != nil {
								return generateMsg{"", err}
							}
							out.WriteString(fmt.Sprintf("%v\n", item))
						}
						return generateMsg{out.String(), nil}
					}

					res, err := m.utcp.CallTool(m.ctx, m.selectedUTCP.name, args)
					if err != nil {
						return generateMsg{"", err}
					}
					return generateMsg{fmt.Sprintf("%v", res), nil}
				}
				return m, tea.Batch(cmd, m.spinner.Tick)

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
				return m.runPrompt(raw)
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
	}
	var cmd tea.Cmd
	switch m.mode {
	case modeDir:
		m.dirlist, cmd = m.dirlist.Update(msg)
	case modeList, modeUTCP:
		m.list, cmd = m.list.Update(msg)
	case modePrompt, modeUTCPArgs, modeChat:
		m.textarea, cmd = m.textarea.Update(msg)
		if m.mode == modeChat {
			var vpCmd tea.Cmd
			m.viewport, vpCmd = m.viewport.Update(msg)
			cmd = tea.Batch(cmd, vpCmd)
		}
	}

	if m.isThinking {
		var spinnerCmd tea.Cmd
		m.spinner, spinnerCmd = m.spinner.Update(msg)
		cmd = tea.Batch(cmd, spinnerCmd)
	}
	return m, cmd
}
