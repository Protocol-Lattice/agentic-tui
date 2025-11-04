// path: src/planner.go
package src

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agent "github.com/Protocol-Lattice/go-agent"
)

// PlanStep defines one sub-task in a plan.
type PlanStep struct {
	Name string `json:"name"`
	Goal string `json:"goal"`
}

// safeSend safely sends a log line into the plannerQueue channel.
// It automatically recovers from "send on closed channel" panics.
func safeSend(m *model, line string) {
	if m == nil || m.plannerQueue == nil {
		return
	}
	defer func() { _ = recover() }()
	select {
	case m.plannerQueue <- line:
	default:
		// avoid blocking if queue full
	}
}

func stripSystemPrompt(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), "system prompt") ||
			strings.Contains(strings.ToLower(l), "you are chatgpt") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// RunPlanner decomposes a user prompt into smaller sequential steps,
// executes each step, and streams logs to the chat view.
// path: src/planner.go
func RunPlanner(ctx context.Context, ag *agent.Agent, workspace, userPrompt string, m *model) error {
	start := time.Now()

	// --- Clean user prompt before sending ---
	userPrompt = stripSystemPrompt(userPrompt)

	metaPrompt := fmt.Sprintf(`
You are an expert software project planner.
Decompose the following goal into at most 3–5 small, ordered steps.

Rules:
- Respond ONLY with a pure JSON array of objects: [{"name":"Step 1","goal":"..."}].
- DO NOT use markdown, code fences, or explanations.
- Keep steps concise and logical.

User goal:
%s
`, userPrompt)

	resp, err := ag.Generate(ctx, "planner", metaPrompt)
	if err != nil {
		safeSend(m, fmt.Sprintf("❌ planner failed: %v\n", err))
		defer func() { recover() }()
		close(m.plannerQueue)
		return err
	}

	// --- Clean raw model response ---
	resp = stripSystemPrompt(resp)
	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```JSON")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var steps []PlanStep
	if err := json.Unmarshal([]byte(resp), &steps); err != nil || len(steps) == 0 {
		// fallback if JSON fails
		steps = heuristicSplit(resp)
	}
	if len(steps) == 0 {
		safeSend(m, "❌ no valid steps parsed\n")
		defer func() { recover() }()
		close(m.plannerQueue)
		return fmt.Errorf("no valid steps parsed")
	}

	// --- Sanitize step text ---
	for i := range steps {
		steps[i].Goal = strings.TrimSpace(steps[i].Goal)
		steps[i].Goal = strings.TrimPrefix(steps[i].Goal, "```json")
		steps[i].Goal = strings.TrimPrefix(steps[i].Goal, "```go")
		steps[i].Goal = strings.TrimPrefix(steps[i].Goal, "```")
		steps[i].Goal = strings.TrimSuffix(steps[i].Goal, "```")
		steps[i].Goal = strings.Trim(steps[i].Goal, "`")
		steps[i].Goal = strings.TrimSpace(steps[i].Goal)
	}

	// --- Optional cap for runaway plans ---
	if len(steps) > 8 {
		steps = steps[:8]
	}

	safeSend(m, fmt.Sprintf("🧭 Plan created with %d steps.\n", len(steps)))

	for i, step := range steps {
		safeSend(m, fmt.Sprintf("\n⚙️ Step %d/%d — %s\n", i+1, len(steps), step.Goal))

		headlessRes, err := RunHeadless(ctx, ag, workspace, step.Goal)
		if err != nil {
			safeSend(m, fmt.Sprintf("❌ Step %d failed: %v\n", i+1, err))
			continue
		}
		for _, act := range headlessRes.Actions {
			switch act.Action {
			case "saved":
				safeSend(m, fmt.Sprintf("💾 %s (%s)\n", act.Path, act.Message))
			case "deleted", "removed":
				safeSend(m, fmt.Sprintf("🧹 %s %s\n", strings.Title(act.Action), act.Path))
			case "error":
				safeSend(m, fmt.Sprintf("❌ %s: %s\n", act.Path, act.Message))
			default:
				safeSend(m, fmt.Sprintf("ℹ️ %s\n", act.Message))
			}
		}
	}

	safeSend(m, fmt.Sprintf("\n✅ Planner finished in %s\n", time.Since(start).Round(time.Second)))
	defer func() { recover() }()
	close(m.plannerQueue)
	return nil
}

// heuristicSplit fallback if planner output is not JSON.
func heuristicSplit(s string) []PlanStep {
	lines := strings.Split(s, "\n")
	var steps []PlanStep
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
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
