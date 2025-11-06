// path: src/codeblocks.go
package src

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// WriteCodeBlocks writes fenced code blocks and prints per-prompt diffs.
func WriteCodeBlocks(root, response string) ([]FileAction, error) {
	GlobalChanges.BeginPrompt()
	var actions []FileAction

	blocks := extractCodeBlocks(response)
	if len(blocks) == 0 {
		return []FileAction{{Action: "info", Message: "No code blocks detected."}}, nil
	}

	for i, b := range blocks {
		path, body := extractPathAndStrip(b.lang, b.body)
		if path == "" {
			ext := strings.TrimPrefix(extFromLang(b.lang), ".")
			path = fmt.Sprintf("generated/file_%d.%s", i+1, ext)
		}
		abs := filepath.Join(root, filepath.FromSlash(path))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)

		newB := []byte(body)
		oldB := GlobalChanges.Snapshot(root, path)
		diff := GlobalChanges.DiffPretty(path, oldB, newB)
		status := "created"
		if oldB != nil {
			if bytes.Equal(oldB, newB) {
				status = "unchanged"
			} else {
				status = "updated"
			}
		}
		if status != "unchanged" {
			if err := os.WriteFile(abs, newB, 0o644); err != nil {
				actions = append(actions, FileAction{Path: path, Action: "error", Message: err.Error(), Err: err})
				continue
			}
		}
		GlobalChanges.Record(path, newB)

		actions = append(actions, FileAction{Path: path, Action: "saved", Message: status, Diff: diff})
	}

	return actions, nil
}

type codeBlock struct {
	lang string
	body string
}

var fenceRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_+\\.-]*)\\s*\\n(.*?)\\n```")

func extractCodeBlocks(s string) []codeBlock {
	var out []codeBlock
	for _, m := range fenceRe.FindAllStringSubmatch(s, -1) {
		out = append(out, codeBlock{lang: strings.ToLower(m[1]), body: m[2]})
	}
	return out
}

func extractPathAndStrip(lang, code string) (string, string) {
	lines := strings.Split(code, "\n")
	if len(lines) == 0 {
		return "", code
	}
	re := regexp.MustCompile(`(?i)^\s*(?:\/\/|#|--|;|@|<!--)\s*path:?\s*([^\s>]+)`)
	if m := re.FindStringSubmatch(lines[0]); len(m) > 1 {
		return filepath.ToSlash(strings.TrimSpace(m[1])), strings.Join(lines[1:], "\n")
	}
	return "", code
}
