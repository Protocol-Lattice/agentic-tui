// path: src/tracker.go
package src

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ChangeTracker tracks file contents between prompts and computes unified diffs.
type ChangeTracker struct {
	mu    sync.Mutex
	prev  map[string][]byte
	seqno uint64
}

var GlobalChanges = NewChangeTracker()

func NewChangeTracker() *ChangeTracker {
	return &ChangeTracker{prev: make(map[string][]byte)}
}

// BeginPrompt marks a new generation turn.
func (t *ChangeTracker) BeginPrompt() {
	t.mu.Lock()
	t.seqno++
	t.mu.Unlock()
}

// Snapshot returns the previous content of a file or reads it from disk.
func (t *ChangeTracker) Snapshot(root, rel string) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	rel = filepath.ToSlash(rel)
	if b, ok := t.prev[rel]; ok {
		cp := append([]byte(nil), b...)
		return cp
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if data, err := os.ReadFile(abs); err == nil {
		t.prev[rel] = append([]byte(nil), data...)
		return data
	}
	t.prev[rel] = nil
	return nil
}

// Record saves the current snapshot.
func (t *ChangeTracker) Record(rel string, data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if data == nil {
		delete(t.prev, rel)
		return
	}
	t.prev[filepath.ToSlash(rel)] = append([]byte(nil), data...)
}

// edit represents a single line change in a diff.
type edit struct {
	tag string // " " same, "+" add, "-" del
	txt string
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func splitLines(b []byte) []string {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	raw := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	for i := range raw {
		raw[i] = strings.TrimRight(raw[i], "\r")
	}
	return raw
}

const (
	colorReset = "\033[0m"
	colorRed   = "\033[31m"
	colorGreen = "\033[32m"
	colorCyan  = "\033[36m"
	colorGray  = "\033[90m"
	colorBold  = "\033[1m"
)

// DiffPretty prints a colorized git-style unified diff.
func (t *ChangeTracker) DiffPretty(rel string, oldB, newB []byte) string {
	if bytes.Equal(oldB, newB) {
		return ""
	}

	oldLines := splitLines(oldB)
	newLines := splitLines(newB)
	n, m := len(oldLines), len(newLines)

	// Build LCS table.
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	// Collect edits.
	var seq []edit
	i, j := 0, 0
	for i < n && j < m {
		if oldLines[i] == newLines[j] {
			seq = append(seq, edit{" ", oldLines[i]})
			i++
			j++
		} else if lcs[i+1][j] >= lcs[i][j+1] {
			seq = append(seq, edit{"-", oldLines[i]})
			i++
		} else {
			seq = append(seq, edit{"+", newLines[j]})
			j++
		}
	}
	for ; i < n; i++ {
		seq = append(seq, edit{"-", oldLines[i]})
	}
	for ; j < m; j++ {
		seq = append(seq, edit{"+", newLines[j]})
	}

	// Diff header like Git.
	oldHash := shortSHA(oldB)
	newHash := shortSHA(newB)
	var out strings.Builder

	// Write header with newlines
	out.WriteString(fmt.Sprintf("%sdiff --git a/%s b/%s%s\n", colorBold+colorCyan, rel, rel, colorReset))
	out.WriteString(fmt.Sprintf("index %s..%s 100644\n", oldHash, newHash))
	out.WriteString(fmt.Sprintf("%s--- a/%s%s\n", colorCyan, rel, colorReset))
	out.WriteString(fmt.Sprintf("%s+++ b/%s%s\n", colorCyan, rel, colorReset))

	// Context
	const ctx = 3
	var hunk []edit
	var startOld, startNew int
	countOld, countNew := 0, 0

	printHunk := func() {
		if len(hunk) == 0 {
			return
		}
		out.WriteString(fmt.Sprintf("%s@@ -%d,%d +%d,%d @@%s\n",
			colorCyan, startOld+1, countOld, startNew+1, countNew, colorReset))
		for _, e := range hunk {
			switch e.tag {
			case "+":
				out.WriteString(fmt.Sprintf("%s+%s%s\n", colorGreen, e.txt, colorReset))
			case "-":
				out.WriteString(fmt.Sprintf("%s-%s%s\n", colorRed, e.txt, colorReset))
			default:
				out.WriteString(fmt.Sprintf("%s %s%s\n", colorGray, e.txt, colorReset))
			}
		}
		hunk = hunk[:0]
	}

	inHunk := false
	for idx := range seq {
		e := seq[idx]
		if e.tag != " " {
			if !inHunk {
				inHunk = true
				startOld = max(0, idx-ctx)
				startNew = startOld
				hunk = append(hunk, seq[max(0, idx-ctx):idx]...)
				countOld, countNew = 0, 0
			}
			hunk = append(hunk, e)
			if e.tag != "+" {
				countOld++
			}
			if e.tag != "-" {
				countNew++
			}
		} else if inHunk {
			hunk = append(hunk, e)
			countOld++
			countNew++

			end := idx + ctx + 1
			if end > len(seq) {
				end = len(seq)
			}
			next := seq[idx+1 : end]
			if len(hunk) > 0 && !hasChangeAhead(next) {
				printHunk()
				inHunk = false
			}
		}
	}
	if inHunk {
		printHunk()
	}

	return out.String()
}

// shortSHA returns a short SHA1-like index label for diff headers.
func shortSHA(b []byte) string {
	h := sha1.Sum(b)
	return fmt.Sprintf("%x", h[:3]) // 6 hex chars, like Git short hash
}

// hasChangeAhead checks if the next few edits contain +/-
func hasChangeAhead(next []edit) bool {
	for _, e := range next {
		if e.tag == "+" || e.tag == "-" {
			return true
		}
	}
	return false
}
