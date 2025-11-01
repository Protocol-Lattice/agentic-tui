package src

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

var (
	jsonRe              = regexp.MustCompile("(?is)```(?:json[c5]?|json5)?\\s*([{\\[].*?[}\\]])\\s*```")
	trailingArrayComma  = regexp.MustCompile(`,\s*\]`)
	trailingObjectComma = regexp.MustCompile(`,\s*\}`)
	backtickStringRe    = regexp.MustCompile("`([^`\\\\]*(?:\\\\.[^`\\\\]*)*)`")
)

// extractJSON finds the first JSON object or array in a string,
// handling optional markdown fences.
func extractJSON(raw string) ([]byte, error) {
	candidate := raw

	// First, try to find a fenced JSON block.
	if matches := jsonRe.FindStringSubmatch(raw); len(matches) > 1 {
		candidate = matches[1]
	} else {
		// If no fence is found, fall back to finding the first/last bracket.
		start := strings.IndexAny(raw, "[{")
		if start == -1 {
			return nil, errors.New("no JSON object or array found")
		}

		end := strings.LastIndexAny(raw, "}]")
		if end == -1 || end < start {
			return nil, errors.New("no JSON object or array found")
		}
		candidate = raw[start : end+1]
	}

	// At this point, 'candidate' should be our best guess at the JSON string.
	jsonStr := strings.TrimSpace(candidate)
	if jsonStr == "" {
		return nil, errors.New("empty JSON payload")
	}

	// Sanitize the JSON string to remove trailing commas.
	jsonStr = trailingArrayComma.ReplaceAllString(jsonStr, "]")
	jsonStr = trailingObjectComma.ReplaceAllString(jsonStr, "}")

	// Some providers occasionally use backticks instead of double quotes.
	if strings.Contains(jsonStr, "`") {
		jsonStr = backtickStringRe.ReplaceAllString(jsonStr, "\"$1\"")
	}

	// Ensure we still have what appears to be JSON.
	first := jsonStr[0]
	if first != '{' && first != '[' {
		return nil, errors.New("response did not contain JSON object or array")
	}

	return []byte(jsonStr), nil
}

func (m *model) runStepBuilder(userGoal string) {
	// Create a channel to stream progress back to the Update loop.
	progCh := make(chan stepBuildProgressMsg)

	// Goroutine to forward messages from our progress channel to the main program.
	go func() {
		// Ensure the channel is closed when the goroutine finishes
		defer close(progCh) // Ensure channel is closed.
		for msg := range progCh {
			m.Program.Send(msg)
		}
	}()

	var log strings.Builder
	fmt.Fprintf(&log, "🧩 Auto StepBuild for GOAL:\n%s\n\n", userGoal)

	subprompts, err := m.buildStepPrompts(userGoal) // This function now correctly sets context stats on the model.
	if err != nil {
		m.Program.Send(stepBuildCompleteMsg{err: fmt.Errorf("failed to split goal into sub-prompts: %v", err)})
		return
	}

	stepSummary := fmt.Sprintf("📋 %d step prompts generated:\n", len(subprompts))
	for i, s := range subprompts {
		stepSummary += fmt.Sprintf("  %d) %s\n", i+1, s)
	}
	progCh <- stepBuildProgressMsg{log: stepSummary + "\n"}

	var wg sync.WaitGroup
	for i, sub := range subprompts {
		wg.Add(1)
		go func(idx int, goal string) {
			defer wg.Done()
			if err := m.runStepBuilderPhase(progCh, goal, idx+1, len(subprompts)); err != nil {
				progCh <- stepBuildProgressMsg{log: fmt.Sprintf("⚠️ Step %d failed: %v\n", idx+1, err)}
			}
		}(i, sub)
	}
	wg.Wait()

	// Add a final tree view to show the result
	tree := buildTree(collectWorkspaceFiles(m.working))
	progCh <- stepBuildProgressMsg{log: fmt.Sprintf("\nFinal workspace structure:\n%s\n", tree)}
	progCh <- stepBuildProgressMsg{log: "\n🎉 Auto StepBuild complete!"}
	m.Program.Send(stepBuildCompleteMsg{}) // Signal completion
}

// buildPlanningPrompt creates a plan describing the expected file structure
func (m *model) buildPlanningPrompt(userGoal string) ([]planFile, error) {
	const (
		maxFiles      = 300
		maxTotalBytes = int64(1_200_000)
		perFileLimit  = int64(80_000)
	)

	lang := detectPromptLanguage(userGoal)
	ctxBlock, nFiles, nBytes := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
	m.contextFiles, m.contextBytes = nFiles, nBytes
	attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)

	prompt := strings.Builder{}
	prompt.WriteString("You are a senior software planner. Generate a JSON array describing the files to be built step-by-step.\n")
	prompt.WriteString("Respond with STRICT JSON only: an array of objects, each with keys name, path, lang, goal.\n")
	prompt.WriteString("No prose, comments, trailing commas, markdown fences, or extra keys. Values are plain strings.\n")
	prompt.WriteString("Format example: [{\"name\":\"server\",\"path\":\"src/server.go\",\"lang\":\"Go\",\"goal\":\"HTTP handlers\"}].\n")
	prompt.WriteString("Do not include code, only planning metadata.\n\n")
	prompt.WriteString("### [WORKSPACE ROOT]\n")
	prompt.WriteString(m.working + "\n\n")
	prompt.WriteString(ctxBlock)
	prompt.WriteString("\n\n---\nGOAL:\n")
	prompt.WriteString(userGoal)

	raw, err := m.agent.GenerateWithFiles(m.ctx, "1", prompt.String(), attachments)
	if err != nil {
		raw, err = m.agent.Generate(m.ctx, "1", prompt.String())
		if err != nil {
			return nil, err
		}
	}

	data, err := extractJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid plan JSON: %v", err)
	}

	var plan []planFile
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("invalid plan JSON: %v", err)
	}
	return plan, nil
}

// buildStepPrompts breaks a large goal into several smaller sub-goals
func (m *model) buildStepPrompts(userGoal string) ([]string, error) { // Note: This function is long, but its logic is self-contained.
	const (
		maxFiles      = 300
		maxTotalBytes = int64(1_200_000)
		perFileLimit  = int64(80_000)
	)
	lang := detectPromptLanguage(userGoal)
	ctxBlock, nFiles, nBytes := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
	m.contextFiles, m.contextBytes = nFiles, nBytes
	attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)

	prompt := strings.Builder{}
	prompt.WriteString("Split the GOAL into 3–8 sequential sub-prompts, each focused on one major build area.\n")
	prompt.WriteString("Respond with STRICT JSON only. Produce a single JSON array of strings representing the sub-goals.\n")
	prompt.WriteString("No prose, comments, trailing commas, markdown fences, or keys. Use straight double quotes.\n")
	prompt.WriteString("Example: [\"draft schema\", \"implement handlers\", \"write integration tests\"].\n\n")
	prompt.WriteString("### [WORKSPACE ROOT]\n" + m.working + "\n\n")
	prompt.WriteString(ctxBlock)
	prompt.WriteString("\n---\nGOAL:\n" + userGoal)

	raw, err := m.agent.GenerateWithFiles(m.ctx, "1", prompt.String(), attachments)
	if err != nil {
		raw, err = m.agent.Generate(m.ctx, "1", prompt.String())
		if err != nil {
			return nil, err
		}
	}
	data, err := extractJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid stepbuild prompt JSON: %v", err)
	}
	var subs []string
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, fmt.Errorf("invalid stepbuild prompt JSON: %v", err)
	}
	return subs, nil
}

// runStepBuilderPhase runs one sub-prompt (a single phase) through the file-plan and build loop.
// It now generates files concurrently for maximum efficiency.
func (m *model) runStepBuilderPhase(progCh chan<- stepBuildProgressMsg, subgoal string, stepIndex, totalSteps int) error {
	progCh <- stepBuildProgressMsg{log: fmt.Sprintf("⚙️  Step %d/%d — %s\n", stepIndex, totalSteps, subgoal)}

	// 1. Create a file plan for this subgoal.
	phase := stepPhase{Name: fmt.Sprintf("Step %d", stepIndex), Goal: subgoal}
	files, err := m.buildFilePlan(phase)
	if err != nil {
		return fmt.Errorf("failed to plan files for step %d: %v", stepIndex, err)
	}

	// 2. Set up concurrent generation.
	var wg sync.WaitGroup
	results := make(chan string, len(files))

	for j, fileToBuild := range files {
		wg.Add(1)
		go func(f planFile, fileIndex int) {
			defer wg.Done()

			index := fmt.Sprintf("%d.%d", stepIndex, fileIndex+1)
			m.mu.Lock()
			m.thinking = fmt.Sprintf("building %s — %s", index, f.Name) // Note: race condition on m.thinking is ok for UI
			m.mu.Unlock()

			// Each goroutine gets its own tailored context window.
			const (
				maxFiles      = 300
				maxTotalBytes = int64(1_200_000)
				perFileLimit  = int64(80_000)
			)
			ctxBlock, _, _ := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, f.Lang)
			attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, f.Lang)

			sub := strings.Builder{}
			sub.WriteString("You are Vibe, the coding agent inside a TUI.\n")
			sub.WriteString(fmt.Sprintf("Generate ONLY ONE file for sub-goal '%s': %s\n\n", subgoal, f.Name))
			sub.WriteString("### [WORKSPACE ROOT]\n" + m.working + "\n\n")
			sub.WriteString(ctxBlock)
			sub.WriteString("\n---\nFILE SPEC:\n")
			sub.WriteString(fmt.Sprintf("%s — %s\n", f.Path, f.Goal))
			sub.WriteString("\nFollow OUTPUT CONTRACT: short plan → one fenced file block.")

			res, err := m.agent.GenerateWithFiles(m.ctx, "1", sub.String(), attachments)
			if err != nil {
				res, err = m.agent.Generate(m.ctx, "1", sub.String())
				if err != nil {
					results <- fmt.Sprintf("  ❌ failed to build %s: %v\n", f.Name, err)
					return
				}
			}

			m.saveCodeBlocks(res) // saveCodeBlocks is thread-safe enough for this use case
			results <- fmt.Sprintf("✅ %s\n", f.Path)
		}(fileToBuild, j)
	}

	// 3. Wait for all file generations to complete and collect results.
	wg.Wait()
	close(results)

	for res := range results {
		progCh <- stepBuildProgressMsg{log: "  " + res}
	}

	return nil
}

// stepPhase represents a high-level build phase (like a module or layer)
type stepPhase struct {
	Name  string     `json:"name"`
	Goal  string     `json:"goal"`
	Files []planFile `json:"files,omitempty"`
}

// planFile describes a planned file output
type planFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Lang string `json:"lang"`
	Goal string `json:"goal"`
}

// buildFilePlan creates the list of files for a given build phase or subgoal
func (m *model) buildFilePlan(phase stepPhase) ([]planFile, error) {
	const (
		maxFiles      = 300
		maxTotalBytes = int64(1_200_000)
		perFileLimit  = int64(80_000)
	)

	lang := detectPromptLanguage(phase.Goal)
	ctxBlock, _, _ := buildCodebaseContext(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)
	attachments := collectAttachmentFiles(m.working, maxFiles, maxTotalBytes, perFileLimit, lang)

	prompt := strings.Builder{}
	prompt.WriteString("You are a senior code planner.\n")
	prompt.WriteString(fmt.Sprintf("For PHASE: %s — %s\n", phase.Name, phase.Goal))
	prompt.WriteString("Generate a JSON array describing the files to build.\n")
	prompt.WriteString("[{\"name\":\"...\",\"path\":\"...\",\"lang\":\"...\",\"goal\":\"...\"}]\n\n")
	prompt.WriteString("### [WORKSPACE ROOT]\n" + m.working + "\n\n")
	prompt.WriteString(ctxBlock)

	raw, err := m.agent.GenerateWithFiles(m.ctx, "1", prompt.String(), attachments)
	if err != nil {
		raw, err = m.agent.Generate(m.ctx, "1", prompt.String())
		if err != nil {
			return nil, err
		}
	}

	data, err := extractJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid file plan JSON: %v", err)
	}
	var files []planFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("invalid file plan JSON: %v", err)
	}
	return files, nil
}
