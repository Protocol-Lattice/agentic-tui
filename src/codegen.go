// path: src/codegen.go
package src

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// checksum computes a SHA256 hash of the data for content comparison
func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return string(sum[:])
}

// CodeFence represents a parsed code block
type CodeFence struct {
	Lang string
	Code string
}

// parseFences scans s and returns (lang, code) fences in source order.
// It accepts ```<lang>\n...\n``` and ```\n...\n``` (no lang).
func parseFences(s string) []CodeFence {
	var out []CodeFence

	i := 0
	for {
		// Find start of code fence
		start := strings.Index(s[i:], "```")
		if start == -1 {
			break
		}
		start += i
		langLineStart := start + 3

		// Find end of language line
		nl := strings.IndexByte(s[langLineStart:], '\n')
		if nl == -1 {
			break // malformed fence - no newline after opening
		}
		nl += langLineStart

		// Extract language identifier
		lang := strings.TrimSpace(s[langLineStart:nl])

		// Find closing ```
		end := strings.Index(s[nl+1:], "```")
		if end == -1 {
			break // malformed fence - no closing marker
		}
		end += nl + 1

		// Extract code content
		code := s[nl+1 : end]

		out = append(out, CodeFence{
			Lang: lang,
			Code: code,
		})

		// Move past this fence
		i = end + 3
	}
	return out
}

// extFromLang maps language identifiers to file extensions
func extFromLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))

	switch lang {
	case "go", "golang":
		return ".go"
	case "python", "py":
		return ".py"
	case "javascript", "js", "node":
		return ".js"
	case "typescript", "ts":
		return ".ts"
	case "jsx":
		return ".jsx"
	case "tsx":
		return ".tsx"
	case "rust", "rs":
		return ".rs"
	case "java":
		return ".java"
	case "c":
		return ".c"
	case "cpp", "c++", "cc", "cxx":
		return ".cpp"
	case "h", "hpp", "hh", "hxx":
		return ".h"
	case "csharp", "c#", "cs":
		return ".cs"
	case "kotlin", "kt":
		return ".kt"
	case "swift":
		return ".swift"
	case "ruby", "rb":
		return ".rb"
	case "php":
		return ".php"
	case "scala":
		return ".scala"
	case "dart":
		return ".dart"
	case "lua":
		return ".lua"
	case "r":
		return ".r"
	case "shell", "bash", "sh", "zsh":
		return ".sh"
	case "sql":
		return ".sql"
	case "html", "xml", "svg":
		return "." + lang
	case "css", "scss", "sass", "less":
		return ".css"
	case "json":
		return ".json"
	case "yaml", "yml":
		return ".yaml"
	case "toml":
		return ".toml"
	case "md", "markdown":
		return ".md"
	case "dockerfile":
		return ".dockerfile"
	case "makefile":
		return "Makefile"
	case "":
		return ".txt"
	default:
		// Last resort: treat unknown tag as an extension if it looks valid
		if len(lang) <= 6 && regexp.MustCompile(`^[a-z0-9.+-]+$`).MatchString(lang) {
			if !strings.HasPrefix(lang, ".") {
				return "." + lang
			}
			return lang
		}
		return ".txt"
	}
}

// pathRe matches path comments in various formats:
// - // path: some/path.go
// - # path: some/path.py
// - -- path: some/path.sql
// - <!-- path: some/path.html -->
var pathRe = regexp.MustCompile(`(?m)^(?:(?://|#|--)\s*path:\s*([^\s]+)|<!--\s*path:\s*([^\s]+)\s*-->)\s*$`)

// extractPathFromCode looks for a path comment at the start of the code
// and returns the path and the remaining code with the comment removed
func extractPathFromCode(code string) (path string, remainingCode string) {
	// Trim leading whitespace but preserve structure
	trimmed := strings.TrimLeft(code, " \t")

	match := pathRe.FindStringSubmatchIndex(trimmed)
	if match == nil {
		return "", code
	}

	// Extract path from the appropriate capture group
	if match[2] != -1 && match[3] != -1 {
		// Single-line comment style (// # --)
		path = strings.TrimSpace(trimmed[match[2]:match[3]])
	} else if match[4] != -1 && match[5] != -1 {
		// HTML comment style (<!-- -->)
		path = strings.TrimSpace(trimmed[match[4]:match[5]])
	}

	// Remove the path comment line and any trailing newline
	before := code[:len(code)-len(trimmed)] // Preserve original indentation
	afterMatch := trimmed[match[1]:]
	afterMatch = strings.TrimPrefix(afterMatch, "\n")

	remainingCode = before + afterMatch
	return path, remainingCode
}

// snapshotFiles creates a map of file paths to their checksums
func snapshotFiles(baseDir string) (map[string]string, error) {
	files := make(map[string]string)

	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip files with errors
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

		content, readErr := os.ReadFile(path)
		if readErr == nil {
			files[path] = checksum(content)
		}

		return nil
	})

	return files, err
}

// writeCodeFence writes a single code fence to disk
func writeCodeFence(baseDir string, index int, fence CodeFence, writtenFiles map[string]string) []FileAction {
	var actions []FileAction

	code := strings.TrimSpace(fence.Code)
	if code == "" {
		return actions
	}

	// Extract path from code comment or generate default
	path, body := extractPathFromCode(code)
	if path == "" {
		ext := extFromLang(fence.Lang)
		path = filepath.Join("generated", fmt.Sprintf("file_%d%s", index+1, ext))
	}

	// Normalize path separators and make absolute
	path = filepath.ToSlash(path)
	fullPath := filepath.Join(baseDir, filepath.FromSlash(path))

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return append(actions, FileAction{
			Path:    fullPath,
			Action:  "error",
			Message: fmt.Sprintf("Failed to create directory: %v", err),
			Err:     err,
		})
	}

	// Write file with trailing newline
	bodyBytes := []byte(body)
	if len(bodyBytes) > 0 && bodyBytes[len(bodyBytes)-1] != '\n' {
		bodyBytes = append(bodyBytes, '\n')
	}

	if err := os.WriteFile(fullPath, bodyBytes, 0o644); err != nil {
		return append(actions, FileAction{
			Path:    fullPath,
			Action:  "error",
			Message: fmt.Sprintf("Failed to write file: %v", err),
			Err:     err,
		})
	}

	// Track written file and its checksum
	writtenFiles[fullPath] = checksum(bodyBytes)

	return append(actions, FileAction{
		Path:   fullPath,
		Action: "saved",
	})
}

// removeStaleFiles deletes files that existed before but are no longer needed
func removeStaleFiles(initialFiles, writtenFiles map[string]string) []FileAction {
	var actions []FileAction

	// Build set of checksums from newly written files
	currentChecksums := make(map[string]bool)
	for _, chk := range writtenFiles {
		currentChecksums[chk] = true
	}

	for path, oldChecksum := range initialFiles {
		// Skip files that were just written
		if _, wasWritten := writtenFiles[path]; wasWritten {
			continue
		}

		// If this file's content no longer exists in any written file, delete it
		if !currentChecksums[oldChecksum] {
			if err := os.Remove(path); err == nil || errors.Is(err, fs.ErrNotExist) {
				actions = append(actions, FileAction{
					Path:   path,
					Action: "deleted",
				})
			}
		}
	}

	return actions
}

// deduplicateFiles removes duplicate files across different directories
func deduplicateFiles(baseDir string, writtenFiles map[string]string) ([]FileAction, error) {
	var actions []FileAction

	// Build map of checksums to file paths
	checksumToFiles := make(map[string][]string)

	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || errors.Is(walkErr, fs.ErrNotExist) {
			return nil
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

		content, readErr := os.ReadFile(path)
		if readErr == nil {
			sum := checksum(content)
			checksumToFiles[sum] = append(checksumToFiles[sum], path)
		}

		return nil
	})

	if err != nil {
		return actions, err
	}

	var multiErr error

	// Process each set of duplicate files
	for _, paths := range checksumToFiles {
		if len(paths) < 2 {
			continue // No duplicates
		}

		sort.Strings(paths) // Deterministic ordering

		// Determine which file to keep
		keep := selectFileToKeep(paths, writtenFiles)

		// Remove all duplicates except the one we're keeping
		for _, path := range paths {
			if path == keep {
				continue
			}

			// Don't remove duplicates within the same directory
			if filepath.Dir(path) == filepath.Dir(keep) {
				continue
			}

			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				multiErr = errors.Join(multiErr, err)
				actions = append(actions, FileAction{
					Path:    path,
					Action:  "error",
					Message: fmt.Sprintf("Failed to remove duplicate: %v", err),
					Err:     err,
				})
				continue
			}

			actions = append(actions, FileAction{
				Path:   path,
				Action: "removed",
			})
		}
	}

	return actions, multiErr
}

// selectFileToKeep determines which duplicate file to keep
func selectFileToKeep(paths []string, writtenFiles map[string]string) string {
	// Prefer files that were explicitly written in this run
	for _, p := range paths {
		if _, wasWritten := writtenFiles[p]; wasWritten {
			return p
		}
	}

	// Otherwise, keep the alphabetically first one (deterministic)
	return paths[0]
}
