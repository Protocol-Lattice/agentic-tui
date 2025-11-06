// path: src/planner.go
package src

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	agent "github.com/Protocol-Lattice/go-agent"
)

// PlanStep defines a single planner step with error propagation.
type PlanStep struct {
	Name           string `json:"name"`
	Goal           string `json:"goal"`
	PrevRuntimeErr string `json:"prev_runtime_err,omitempty"`
}

func safeSend(m *model, line string) {
	if m == nil || m.plannerQueue == nil {
		return
	}
	defer func() { _ = recover() }()
	select {
	case m.plannerQueue <- line:
	default:
	}
}

// findMainFile scans recursively for the most likely entrypoint across languages.
func findMainFile(root string) (string, string) {
	candidates := map[string][]string{
		"go":         {"main.go"},
		"python":     {"app.py", "main.py"},
		"javascript": {"index.js", "main.js"},
		"typescript": {"index.ts", "main.ts", "index.tsx"},
		"rust":       {"main.rs"},
		"java":       {"Main.java"},
		"c":          {"main.c"},
		"cpp":        {"main.cpp", "main.cc", "main.cxx"},
		"ruby":       {"main.rb", "app.rb"},
		"php":        {"index.php", "main.php"},
		"perl":       {"main.pl"},
		"r":          {"main.R", "script.R"},
		"lua":        {"main.lua", "app.lua"},
		"bash":       {"run.sh", "main.sh"},
		"shell":      {"run.sh", "main.sh"},
		"kotlin":     {"Main.kt", "main.kts"},
		"scala":      {"Main.scala", "App.scala"},
		"swift":      {"main.swift"},
		"dart":       {"main.dart"},
	}

	var foundPath, lang string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(filepath.Base(path))
		for l, patterns := range candidates {
			for _, p := range patterns {
				if name == p {
					rel, err := filepath.Rel(root, path)
					if err != nil {
						log.Printf("failed to make relative path: %v", err)
					}
					path = rel
					foundPath, lang = path, l
					return filepath.SkipDir
				}
			}
		}
		return nil
	})
	return foundPath, lang
}

// RunPlanner executes each planned step sequentially,
// appending previous runtime errors to subsequent steps.
func RunPlanner(ctx context.Context, ag *agent.Agent, workspace, userPrompt string, m *model) error {
	start := time.Now()
	userPrompt = strings.TrimSpace(userPrompt)

	metaPrompt := fmt.Sprintf(`You are a software engineer. The user has a goal that requires code changes.

Break the goal into 1–5 concrete, immediately executable steps. 
Respond with ONLY a JSON array of {"name", "goal"} objects — no explanations, no planning meta-text.
The first step must be a **direct code modification or creation**, not "create a plan".

Example:
[{"name":"Step 1: Add config loader","goal":"Create config/config.go and implement a function LoadConfig() reading from .env."}]

User goal:
%s`, userPrompt)

	resp, err := ag.Generate(ctx, "planner", metaPrompt)
	if err != nil {
		safeSend(m, fmt.Sprintf("❌ planner failed: %v\n", err))
		close(m.plannerQueue)
		return err
	}

	// Strip markdown fences to get raw JSON, handling optional language tags.
	resp = strings.TrimSpace(resp)
	if strings.HasPrefix(resp, "```") && strings.HasSuffix(resp, "```") {
		resp = strings.TrimSuffix(resp, "```")
		resp = resp[strings.Index(resp, "\n")+1:] // Move past the first line (e.g., ```json)
	}
	var steps []PlanStep
	if err := json.Unmarshal([]byte(resp), &steps); err != nil || len(steps) == 0 {
		steps = heuristicSplit(resp)
	}
	if len(steps) == 0 {
		safeSend(m, "❌ no valid steps parsed\n")
		close(m.plannerQueue)
		return fmt.Errorf("no valid steps parsed")
	}

	// Enforce a maximum of 5 steps to prevent overly long plans.
	if len(steps) > 5 {
		steps = steps[:5]
	}

	safeSend(m, fmt.Sprintf("🧭 Plan created with %d steps.\n", len(steps)))

	for i := range steps {
		step := &steps[i]

		// Inject previous runtime error context into current step if exists
		if step.PrevRuntimeErr != "" {
			step.Goal += fmt.Sprintf("\n\n⚠️ Previous runtime error:\n%s\nPlease fix this issue in this step.", step.PrevRuntimeErr)
		}

		safeSend(m, fmt.Sprintf("\n⚙️ Step %d/%d — %s\n", i+1, len(steps), step.Goal))

		headlessRes, err := RunHeadless(ctx, ag, workspace, step.Goal)
		if err != nil {
			step.PrevRuntimeErr = fmt.Sprintf("❌ Step failed to generate: %v", err)
			safeSend(m, step.PrevRuntimeErr+"\n")
			continue
		}

		// Log the diffs from the headless run.
		logStepDiff(m, step.Name, headlessRes.Actions)

		entryPath, lang := findMainFile(workspace)
		if entryPath == "" {
			safeSend(m, fmt.Sprintf("ℹ️ No main file found for step %s\n", step.Name))
			step.PrevRuntimeErr = ""
			continue
		}
		args := map[string]any{
			"language": lang,      // not languageId — use the same key name your tool expects
			"path":     workspace, // file path to run
			"file":     entryPath, // optional: file content to run instead of path'
		}

		tools, err := m.utcp.SearchTools("", 5)
		if err != nil {
			msg := fmt.Sprintf("❌ Tool search error: %v", err)
			safeSend(m, msg+"\n")
			step.PrevRuntimeErr = msg
			continue
		}
		res, err := m.utcp.CallTool(ctx, tools[0].Name, args)
		if err != nil {
			msg := fmt.Sprintf("❌ Runtime error (%s): %v", filepath.Base(entryPath), err)
			safeSend(m, msg+"\n")
			step.PrevRuntimeErr = msg
		} else {
			out := fmt.Sprintf("🧪 Run result (%s):\n%s\n", filepath.Base(entryPath), res)
			safeSend(m, out)
			step.PrevRuntimeErr = ""
		}

		if i+1 < len(steps) {
			steps[i+1].PrevRuntimeErr = step.PrevRuntimeErr
		}
	}

	safeSend(m, fmt.Sprintf("\n✅ Planner finished in %s\n", time.Since(start).Round(time.Second)))
	close(m.plannerQueue)
	return nil
}

// path: src/planner.go
// Add this to the bottom of the file (below heuristicSplit)
func logStepDiff(m *model, stepName string, actions []FileAction) {
	if m == nil || len(actions) == 0 {
		return
	}

	safeSend(m, fmt.Sprintf("\n🔍 Changes in step: %s\n", stepName))
	for _, act := range actions {
		switch act.Action {
		case "saved":
			// Show diff if available
			if strings.TrimSpace(act.Diff) != "" {
				safeSend(m, fmt.Sprintf("💾 %s (%s)\n```diff\n%s\n```\n", act.Path, act.Message, act.Diff))
			} else {
				safeSend(m, fmt.Sprintf("💾 %s (%s, no diff)\n", act.Path, act.Message))
			}

		case "deleted", "removed":
			safeSend(m, fmt.Sprintf("🧹 %s %s\n", strings.Title(act.Action), act.Path))

		case "error":
			safeSend(m, fmt.Sprintf("❌ %s: %s\n", act.Path, act.Message))

		case "info":
			safeSend(m, fmt.Sprintf("ℹ️ %s\n", act.Message))

		default:
			safeSend(m, fmt.Sprintf("📄 %s: %s\n", act.Action, act.Path))
		}
	}
}

// heuristicSplit fallback for non-JSON planner output.
func heuristicSplit(s string) []PlanStep {
	lines := strings.Split(s, "\n")
	var steps []PlanStep
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			steps = append(steps, PlanStep{Name: strings.TrimSpace(parts[0]), Goal: strings.TrimSpace(parts[1])})
		} else {
			steps = append(steps, PlanStep{Goal: line})
		}
	}
	return steps
}
