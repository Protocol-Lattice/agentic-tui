package src

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// runPrompt handles agent prompt execution modes inside the TUI.
// It covers step-build, orchestrator, inline UTCP, and default coding modes.
func (m *model) runPrompt(raw string) (tea.Model, tea.Cmd) {
	// Add user prompt to history
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// nothing to do — don't modify UI or start goroutines
		return m, nil
	}

	// now it's safe to append to output and start thinking
	m.output += m.style.accent.Render("You:") + "\n" + raw + "\n\n"
	m.output += m.style.thinking.Render("Lattice:") + "\n"
	m.renderOutput(true)
	m.textarea.Reset()

	m.isThinking = true
	m.prevMode = m.mode
	m.thinking = "thinking"

	// --- SHELL AGENT ---
	if m.selected.name == "shell" {
		cmd := func() tea.Msg {
			shellCmd := exec.Command("sh", "-c", raw)
			shellCmd.Dir = m.working
			output, err := shellCmd.CombinedOutput()
			if err != nil {
				return generateMsg{text: string(output), err: err}
			}
			return generateMsg{text: string(output), err: nil}
		}
		m.selected = plugin{}
		return m, tea.Batch(cmd, m.spinner.Tick) // ✅ Start spinner here
	}

	// --- INLINE UTCP ---
	if strings.HasPrefix(raw, "@utcp ") {
		cmd := func() tea.Msg {
			res, err := m.runUTCPInline(raw)
			if err != nil {
				return generateMsg{text: "", err: err}
			}
			m.saveCodeBlocks(res)
			return generateMsg{text: res, err: nil}
		}
		return m, tea.Batch(cmd, m.spinner.Tick) // ✅ Start spinner
	}

	// --- Step-builder / Refactor workflow ---
	cmd := func() tea.Msg {
		go m.runStepBuilder(raw)
		return m.spinner.Tick() // ✅ Spinner animation from start
	}

	return m, tea.Batch(cmd, m.spinner.Tick)
}

// runRefactorWorkflow reads the current workspace, sends the full context to the agent,
// and applies all returned file replacements.
func (m *model) runRefactorWorkflow(goal string) error {
	m.output = fmt.Sprintf("♻️ Refactoring codebase: %s\n\n", goal)
	m.renderOutput(true)

	files := collectWorkspaceFiles(m.working)
	if len(files) == 0 {
		return fmt.Errorf("no source files found in %s", m.working)
	}

	var ctx strings.Builder
	ctx.WriteString("### CODEBASE SNAPSHOT\n\n")
	for _, f := range files {
		if !allowedFile(f.Abs) {
			continue
		}
		data, err := os.ReadFile(f.Abs)
		if err != nil {
			continue
		}
		mime := mimeForPath(f.Rel)
		if strings.HasPrefix(mime, "text/") {
			ctx.WriteString(fmt.Sprintf("#### FILE: %s\n```%s\n%s\n```\n\n",
				f.Rel, fenceLangFromExt(filepath.Ext(f.Abs)), string(data)))
		}
	}

	fullPrompt := fmt.Sprintf(
		"You are Vibe, a refactoring agent inside Lattice Code.\n"+
			"Refactor the given CODEBASE SNAPSHOT according to this goal:\n\n%s\n\n"+
			"Return only full rewritten files in fenced code blocks, one per file.\n"+
			"Do not include diffs or explanations.\n\n%s", goal, ctx.String(),
	)

	ctxRef, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	defer cancel()

	resp, err := m.agent.Generate(ctxRef, "refactor", fullPrompt)
	if err != nil {
		return err
	}

	m.saveCodeBlocks(resp)
	m.Program.Send(stepBuildProgressMsg{log: "\n🎉 Refactor complete!\n"})
	return nil
}
