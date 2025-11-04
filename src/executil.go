// path: src/executil.go
package src

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// RunProject executes ./run.sh inside dir with a timeout, capturing combined stdout/stderr.
func RunProject(ctx context.Context, dir string, timeout time.Duration) (ok bool, out string, err error) {
	sh := filepath.Join(dir, "run.sh")
	if _, statErr := os.Stat(sh); statErr != nil {
		return false, "", fmt.Errorf("run.sh missing: %w", statErr)
	}
	// Defensive: ensure executable bit (macOS/Linux)
	_ = os.Chmod(sh, 0o755)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "run.sh")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CI=1") // keep tools quiet / deterministic

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err = cmd.Run()
	out = buf.String()
	ok = err == nil

	// If we hit the timeout, surface a helpful error.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && err != nil {
		err = fmt.Errorf("run timeout after %s: %w", timeout, err)
	}

	return ok, out, err
}

// TailBytes returns the last n bytes of a string (by bytes, not runes), safe for logs.
func TailBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	b := []byte(s)
	if len(b) <= n {
		return s
	}
	return string(b[len(b)-n:])
}
