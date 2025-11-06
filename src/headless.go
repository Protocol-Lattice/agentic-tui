// path: src/headless.go
package src

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	files, entries := collectAttachmentFiles(abs, 500, 10_000_000, 50_000, "")
	prompt := fmt.Sprintf(`File tree:
`+"```\n%s\n```"+`

My task:
%s

After generating the code, also generate a docker-compose.yml file to run the application.`, buildTree(entries), userPrompt)

	session := randomID()
	res, err := ag.GenerateWithFiles(ctx, session, prompt, files)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	actions, _ := WriteCodeBlocks(abs, res)

	return &HeadlessResult{Response: res, Actions: actions}, nil
}

func randomID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
