package src

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
)

func (m *model) loadUTCPTools() []list.Item {
	if m.utcp == nil {
		return []list.Item{utcpItem{"(no UTCP client)", "none", "UTCP unavailable", false}}
	}

	tools, err := m.utcp.SearchTools("", 50)
	if err != nil {
		return []list.Item{utcpItem{"(error)", "none", err.Error(), false}}
	}

	items := make([]list.Item, 0, len(tools))
	for _, t := range tools {
		isStream := strings.Contains(strings.ToLower(t.Name), "stream")
		items = append(items, utcpItem{
			name:     t.Name,
			provider: "",
			desc:     t.Description,
			stream:   isStream,
		})
	}

	if len(items) == 0 {
		items = append(items, utcpItem{"(no tools found)", "none", "", false})
	}
	return items
}

func (m *model) runUTCPInline(prompt string) (string, error) {
	if !strings.HasPrefix(prompt, "@utcp ") {
		return "", fmt.Errorf("not a utcp command")
	}
	if m.utcp == nil {
		return "", fmt.Errorf("UTCP client unavailable")
	}

	cmd := strings.TrimSpace(strings.TrimPrefix(prompt, "@utcp "))
	parts := strings.SplitN(cmd, " ", 2)
	if len(parts) == 0 {
		return "", fmt.Errorf("usage: @utcp toolName {jsonArgs}")
	}
	toolName := parts[0]
	args := map[string]any{}

	if len(parts) == 2 {
		raw := strings.TrimSpace(parts[1])
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", fmt.Errorf("invalid JSON args: %v", err)
		}
	}

	isStream := strings.Contains(strings.ToLower(toolName), "stream")

	if isStream {
		stream, err := m.utcp.CallToolStream(m.ctx, toolName, args)
		if err != nil {
			return "", err
		}
		var out strings.Builder
		for {
			item, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			out.WriteString(fmt.Sprintf("%v\n", item))
		}
		return out.String(), nil
	}

	res, err := m.utcp.CallTool(m.ctx, toolName, args)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", res), nil
}
