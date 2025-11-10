// path: src/headless.go
package src

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	agent "github.com/Protocol-Lattice/go-agent"
)

type FileAction struct {
	Path, Action, Message string
	Err                   error
	Diff                  string
}

type HeadlessResult struct {
	Response string
	Actions  []FileAction
}

// RunHeadless runs a prompt, writes code, and prints diffs in terminal.
func RunHeadless(ctx context.Context, ag *agent.Agent, workspace, userPrompt string) (*HeadlessResult, error) {
	if ag == nil {
		return nil, errors.New("agent is nil")
	}
	if strings.TrimSpace(userPrompt) == "" {
		return nil, errors.New("prompt cannot be empty")
	}

	abs, _ := filepath.Abs(workspace)
	_ = os.MkdirAll(abs, 0o755)

	if ag.UTCPClient != nil {
		res, err := runHeadlessWithUTCP(ctx, ag, abs, userPrompt)
		if err == nil {
			return res, nil
		}
		log.Printf("UTCP codegen failed, falling back to local generator: %v", err)
	}

	files, entries := collectAttachmentFiles(abs, 100, 1_000_000, 20_000, "")
	prompt := fmt.Sprintf("File tree:\n```\n%s\n```\n\nMy task:\n%s\n\nAfter generating the code, also generate a docker-compose.yml file to run the application.", buildTree(entries), userPrompt)

	session := randomID()
	res, err := ag.GenerateWithFiles(ctx, session, prompt, files)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	actions, _ := WriteCodeBlocks(abs, res)

	return &HeadlessResult{Response: res, Actions: actions}, nil
}

func runHeadlessWithUTCP(ctx context.Context, ag *agent.Agent, workspace, userPrompt string) (*HeadlessResult, error) {
	GlobalChanges.BeginPrompt()

	before, err := loadWorkspaceSnapshot(workspace)
	if err != nil {
		return nil, err
	}

	if _, err := ag.UTCPClient.CallTool(ctx, "codebase.store_tree", map[string]any{
		"path": workspace,
	}); err != nil {
		log.Printf("UTCP store_tree warning: %v", err)
	}

	session := randomID()
	args := map[string]any{
		"session_id": session,
		"path":       workspace,
	}
	if strings.TrimSpace(userPrompt) != "" {
		args["query"] = userPrompt
	}

	toolRes, err := ag.UTCPClient.CallTool(ctx, "codebase.refactor_codebase", args)
	if err != nil {
		return nil, fmt.Errorf("utcp refactor failed: %w", err)
	}

	after, err := loadWorkspaceSnapshot(workspace)
	if err != nil {
		return nil, err
	}

	actions := diffSnapshots(before, after)
	if len(actions) == 0 {
		actions = append(actions, FileAction{Action: "info", Message: "No file changes detected."})
	}

	return &HeadlessResult{Response: fmt.Sprintf("%v", toolRes), Actions: actions}, nil
}

func loadWorkspaceSnapshot(root string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if isIgnoredDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !allowedFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		files[rel] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func diffSnapshots(before, after map[string][]byte) []FileAction {
	var actions []FileAction
	processed := make(map[string]bool)

	for rel, oldData := range before {
		newData, ok := after[rel]
		if !ok {
			GlobalChanges.Record(rel, nil)
			actions = append(actions, FileAction{Path: rel, Action: "deleted"})
			continue
		}
		processed[rel] = true
		if !bytes.Equal(oldData, newData) {
			diff := GlobalChanges.DiffPretty(rel, oldData, newData)
			GlobalChanges.Record(rel, newData)
			actions = append(actions, FileAction{Path: rel, Action: "saved", Message: "updated", Diff: diff})
		} else {
			GlobalChanges.Record(rel, newData)
		}
	}

	for rel, newData := range after {
		if processed[rel] {
			continue
		}
		diff := GlobalChanges.DiffPretty(rel, nil, newData)
		GlobalChanges.Record(rel, newData)
		actions = append(actions, FileAction{Path: rel, Action: "saved", Message: "created", Diff: diff})
	}

	sort.SliceStable(actions, func(i, j int) bool {
		return actions[i].Path < actions[j].Path
	})
	return actions
}

func randomID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
