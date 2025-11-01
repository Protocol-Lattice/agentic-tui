package src

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Protocol-Lattice/go-agent/src/models"
)

type fileEntry struct {
	Rel  string
	Abs  string
	Size int64
}

func isIgnoredDir(name string) bool {
	ignored := map[string]struct{}{
		".git": {}, "node_modules": {}, "dist": {}, "build": {}, "out": {}, "target": {}, "vendor": {},
		".venv": {}, "__pycache__": {}, ".idea": {}, ".vscode": {}, ".DS_Store": {},
	}
	_, ok := ignored[name]
	return ok
}

func allowedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	allow := map[string]struct{}{
		".go": {},
		".md": {}, ".yaml": {}, ".yml": {}, ".json": {},
		".py": {}, ".js": {}, ".ts": {}, ".tsx": {}, ".jsx": {}, ".rs": {}, ".rb": {},
		".java": {}, ".c": {}, ".cpp": {}, ".h": {}, ".sh": {}, ".toml": {}, ".ini": {},
		".cfg": {}, ".txt": {},
	}
	_, ok := allow[ext]
	return ok
}

func buildTree(files []fileEntry) string {
	type node struct {
		name     string
		children map[string]*node
		file     bool
	}
	root := &node{name: "/", children: map[string]*node{}}

	for _, f := range files {
		parts := strings.Split(f.Rel, string(os.PathSeparator))
		cur := root
		for i, p := range parts {
			if cur.children == nil {
				cur.children = map[string]*node{}
			}
			if _, ok := cur.children[p]; !ok {
				cur.children[p] = &node{name: p, children: map[string]*node{}}
			}
			cur = cur.children[p]
			if i == len(parts)-1 {
				cur.file = true
			}
		}
	}

	var lines []string
	var walk func(prefix string, n *node)
	walk = func(prefix string, n *node) {
		keys := make([]string, 0, len(n.children))
		for k := range n.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := n.children[k]
			marker := "└─ "
			line := prefix + marker + child.name
			if !child.file {
				line += "/"
			}
			lines = append(lines, line)
			if len(child.children) > 0 {
				walk(prefix+"  ", child)
			}
		}
	}
	walk("", root)
	return strings.Join(lines, "\n")
}

func fenceLangFromExt(ext string) string {
	switch strings.TrimPrefix(strings.ToLower(ext), ".") {
	case "go":
		return "go"
	case "py":
		return "python"
	case "js":
		return "javascript"
	case "ts", "tsx":
		return "ts"
	case "jsx":
		return "jsx"
	case "rs":
		return "rust"
	case "rb":
		return "ruby"
	case "java":
		return "java"
	case "c":
		return "c"
	case "cpp", "hpp", "cc", "cxx":
		return "cpp"
	case "h":
		return "c"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "md":
		return "md"
	case "sh":
		return "bash"
	case "toml":
		return "toml"
	default:
		return ""
	}
}

func HumanSize(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func mimeForPath(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".md":
		return "text/markdown"
	case ".go", ".py", ".rs", ".rb", ".java", ".c", ".h", ".cpp", ".cc", ".cxx", ".sh", ".txt":
		return "text/plain"
	case ".js":
		return "application/javascript"
	case ".ts", ".tsx":
		return "application/typescript"
	case ".jsx":
		return "text/jsx"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".toml":
		return "application/toml"
	case ".ini", ".cfg":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func collectFiles(root, ext string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.EqualFold(filepath.Ext(p), ext) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func collectFilesMany(root string, exts []string) []string {
	extSet := map[string]struct{}{}
	for _, e := range exts {
		extSet[strings.ToLower(e)] = struct{}{}
	}

	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, ok := extSet[strings.ToLower(filepath.Ext(p))]; ok {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func relFromTo(fromDir, absTarget string) string {
	rel, err := filepath.Rel(fromDir, absTarget)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + filepath.ToSlash(rel)
	} else {
		rel = filepath.ToSlash(rel)
	}
	return rel
}

func isUnderRoot(root, target string) bool {
	abs := filepath.Join(root, filepath.FromSlash(target))
	_, err := os.Stat(abs)
	return err == nil
}

func allowedFileForLang(path, lang string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	langExts := map[string][]string{
		"go":         {".go"},
		"python":     {".py"},
		"py":         {".py"},
		"js":         {".js", ".jsx"},
		"ts":         {".ts", ".tsx"},
		"typescript": {".ts", ".tsx"},
		"rust":       {".rs"},
		"java":       {".java"},
		"cpp":        {".cpp", ".cc", ".cxx", ".h"},
		"c":          {".c", ".h"},
		"rb":         {".rb"},
		"ruby":       {".rb"},
		"php":        {".php"},
		"kotlin":     {".kt"},
		"swift":      {".swift"},
		"dart":       {".dart"},
		"lua":        {".lua"},
		"r":          {".r"},
		"scala":      {".scala"},
	}

	exts, ok := langExts[strings.ToLower(lang)]
	if !ok {
		return allowedFile(path)
	}

	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

func detectPromptLanguage(prompt string) string {
	prompt = strings.ToLower(prompt)

	switch {
	case strings.Contains(prompt, "golang") || strings.Contains(prompt, " in go") || strings.Contains(prompt, "use go"):
		return "go"
	case strings.Contains(prompt, "python"):
		return "python"
	case strings.Contains(prompt, "typescript") || strings.Contains(prompt, " ts ") || strings.Contains(prompt, " in ts"):
		return "ts"
	case strings.Contains(prompt, "javascript") || strings.Contains(prompt, " js ") || strings.Contains(prompt, "node"):
		return "js"
	case strings.Contains(prompt, "rust"):
		return "rust"
	case strings.Contains(prompt, "java"):
		return "java"
	case strings.Contains(prompt, "c++") || strings.Contains(prompt, "cpp"):
		return "cpp"
	case strings.Contains(prompt, "c#") || strings.Contains(prompt, "csharp"):
		return "cs"
	case strings.Contains(prompt, "ruby"):
		return "rb"
	case strings.Contains(prompt, "php"):
		return "php"
	case strings.Contains(prompt, "kotlin"):
		return "kotlin"
	case strings.Contains(prompt, "swift"):
		return "swift"
	case strings.Contains(prompt, "dart"):
		return "dart"
	case strings.Contains(prompt, "lua"):
		return "lua"
	case strings.Contains(prompt, "scala"):
		return "scala"
	case strings.Contains(prompt, "r "):
		return "r"
	case strings.Contains(prompt, "haskell"):
		return "hs"
	}

	re := regexp.MustCompile("```([a-zA-Z0-9_+.-]+)")
	if m := re.FindStringSubmatch(prompt); len(m) > 1 {
		return strings.ToLower(m[1])
	}

	return "go"
}

func buildCodebaseContext(root string, maxFiles int, maxTotalBytes, perFileLimit int64, langFilter string) (string, int, int64) {
	var entries []fileEntry
	var total int64

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if isIgnoredDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !allowedFileForLang(path, langFilter) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		entries = append(entries, fileEntry{Rel: rel, Abs: path, Size: info.Size()})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel })

	var included []fileEntry
	for _, e := range entries {
		if len(included) >= maxFiles {
			break
		}
		if total >= maxTotalBytes {
			break
		}
		included = append(included, e)
		capAdd := e.Size
		if capAdd > perFileLimit {
			capAdd = perFileLimit
		}
		total += capAdd
	}

	tree := buildTree(included)

	var filesSection strings.Builder
	for _, f := range included {
		content, _ := os.ReadFile(f.Abs)
		if int64(len(content)) > perFileLimit {
			content = content[:perFileLimit]
		}
		lang := fenceLangFromExt(filepath.Ext(f.Rel))
		filesSection.WriteString("\n### ")
		filesSection.WriteString(f.Rel)
		filesSection.WriteString("\n```")
		filesSection.WriteString(lang)
		filesSection.WriteString("\n")
		filesSection.Write(content)
		filesSection.WriteString("\n```\n")
	}

	var out strings.Builder
	out.WriteString("## CODEBASE SNAPSHOT\n")
	out.WriteString(fmt.Sprintf("- Root: `%s`\n", root))
	out.WriteString(fmt.Sprintf("- Files included: %d (limit %d)\n", len(included), maxFiles))
	out.WriteString(fmt.Sprintf("- Size included: %s (limit %s)\n", HumanSize(total), HumanSize(maxTotalBytes)))
	out.WriteString("\n### Tree\n```\n")
	out.WriteString(tree)
	out.WriteString("\n```\n")
	out.WriteString(filesSection.String())

	return out.String(), len(included), total
}

func collectAttachmentFiles(root string, maxFiles int, maxTotalBytes, perFileLimit int64, langFilter string) []models.File {
	var entries []fileEntry
	var total int64

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if isIgnoredDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !allowedFileForLang(path, langFilter) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		entries = append(entries, fileEntry{Rel: rel, Abs: path, Size: info.Size()})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel })

	var out []models.File
	for _, e := range entries {
		if len(out) >= maxFiles || total >= maxTotalBytes {
			break
		}
		b, err := os.ReadFile(e.Abs)
		if err != nil {
			continue
		}
		if int64(len(b)) > perFileLimit {
			b = b[:perFileLimit]
		}
		out = append(out, models.File{
			Name: e.Rel,
			MIME: mimeForPath(e.Rel),
			Data: b,
		})
		add := e.Size
		if add > perFileLimit {
			add = perFileLimit
		}
		total += add
	}
	return out
}

func collectWorkspaceFiles(root string) []fileEntry {
	var out []fileEntry
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if allowedFile(p) {
			rel, _ := filepath.Rel(root, p)
			st, _ := os.Stat(p)
			out = append(out, fileEntry{Rel: rel, Abs: p, Size: st.Size()})
		}
		return nil
	})
	return out
}
