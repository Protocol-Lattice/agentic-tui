package src

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
)

// NormalizeImports runs all language-specific fixers over the workspace.
func NormalizeImports(root string) error {
	_ = normalizeGo(root)
	_ = normalizePython(root)
	_ = normalizeJSLike(root)
	_ = normalizeJavaLike(root)
	_ = normalizeCppLike(root)
	_ = normalizePHP(root)
	return nil
}

func normalizeGo(root string) error {
	mod := goModulePath(root)
	if mod == "" {
		return nil
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		if strings.Contains(p, string(filepath.Separator)+"vendor"+string(filepath.Separator)) {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return nil
		}
		changed := false
		ast.Inspect(f, func(n ast.Node) bool {
			imp, ok := n.(*ast.ImportSpec)
			if !ok || imp.Path == nil {
				return true
			}
			path, _ := strconv.Unquote(imp.Path.Value)
			parts := strings.Split(path, "/")
			for i := 0; i < len(parts)-1; i++ {
				if parts[i] == "src" {
					newPath := mod + "/" + strings.Join(parts[i+1:], "/")
					if newPath != path {
						imp.Path.Value = strconv.Quote(newPath)
						changed = true
					}
					break
				}
			}
			return true
		})
		if !changed {
			return nil
		}
		var buf bytes.Buffer
		cfg := &printer.Config{Mode: printer.TabIndent | printer.UseSpaces, Tabwidth: 8}
		if err := cfg.Fprint(&buf, fset, f); err != nil {
			return nil
		}
		return os.WriteFile(p, buf.Bytes(), 0o644)
	})
}

func goModulePath(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func normalizePython(root string) error {
	pyFiles := collectFiles(root, ".py")
	if len(pyFiles) == 0 {
		return nil
	}

	reFrom := regexp.MustCompile(`(?m)^\s*from\s+([A-Za-z0-9_\.]+)\s+import\s+`)
	reImp := regexp.MustCompile(`(?m)^\s*import\s+([A-Za-z0-9_\.]+)`)

	for _, p := range pyFiles {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false

		stripper := func(mod string) string {
			m := mod
			m = strings.TrimPrefix(m, "src.")
			m = strings.ReplaceAll(m, ".src.", ".")
			m = strings.TrimPrefix(m, moduleNameFromRoot(root)+".")
			m = strings.TrimPrefix(m, moduleNameFromRoot(root)+".src.")
			return m
		}

		txt = reFrom.ReplaceAllStringFunc(txt, func(line string) string {
			m := reFrom.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			newMod := stripper(m[1])
			if newMod != m[1] {
				changed = true
				return strings.Replace(line, "from "+m[1]+" ", "from "+newMod+" ", 1)
			}
			return line
		})
		txt = reImp.ReplaceAllStringFunc(txt, func(line string) string {
			m := reImp.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			newMod := stripper(m[1])
			if newMod != m[1] {
				changed = true
				return strings.Replace(line, "import "+m[1], "import "+newMod, 1)
			}
			return line
		})

		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}

	return nil
}

func moduleNameFromRoot(root string) string {
	return filepath.Base(root)
}

func normalizeJSLike(root string) error {
	jsExts := []string{".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx"}
	files := collectFilesMany(root, jsExts)
	if len(files) == 0 {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*(?:import|export)\s+(?:[^'"]*?\s+from\s+)?["']([^"']+)["']`)
	for _, p := range files {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false

		txt = re.ReplaceAllStringFunc(txt, func(line string) string {
			m := re.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			target := m[1]
			if strings.HasPrefix(target, ".") || strings.HasPrefix(target, "@") {
				return line
			}
			if idx := strings.Index(target, "/src/"); idx >= 0 {
				suffix := target[idx+len("/src/"):]
				newRel := relFromTo(filepath.Dir(p), filepath.Join(root, "src", filepath.FromSlash(suffix)))
				if newRel != "" && newRel != target {
					changed = true
					return strings.Replace(line, `"`+target+`"`, `"`+newRel+`"`, 1)
				}
			}
			if strings.HasPrefix(target, "src/") {
				suffix := strings.TrimPrefix(target, "src/")
				newRel := relFromTo(filepath.Dir(p), filepath.Join(root, "src", filepath.FromSlash(suffix)))
				if newRel != "" && newRel != target {
					changed = true
					return strings.Replace(line, `"`+target+`"`, `"`+newRel+`"`, 1)
				}
			}
			if isUnderRoot(root, target) {
				abs := filepath.Join(root, filepath.FromSlash(target))
				newRel := relFromTo(filepath.Dir(p), abs)
				if newRel != "" && newRel != target {
					changed = true
					return strings.Replace(line, `"`+target+`"`, `"`+newRel+`"`, 1)
				}
			}
			return line
		})

		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}
	return nil
}

func normalizeJavaLike(root string) error {
	javaExts := []string{".java", ".kt"}
	files := collectFilesMany(root, javaExts)
	if len(files) == 0 {
		return nil
	}
	rePkg := regexp.MustCompile(`(?m)^(package\s+)([A-Za-z0-9_.]+)\s*;`)
	reImp := regexp.MustCompile(`(?m)^(import\s+)([A-Za-z0-9_.]+)\s*;`)
	for _, p := range files {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false
		fix := func(s string) (string, bool) {
			ns := strings.ReplaceAll(s, ".src.", ".")
			ns = strings.TrimPrefix(ns, "src.")
			ns = strings.ReplaceAll(ns, "..", ".")
			return ns, ns != s
		}
		txt = rePkg.ReplaceAllStringFunc(txt, func(line string) string {
			prefix, name := rePkg.FindStringSubmatch(line)[1], rePkg.FindStringSubmatch(line)[2]
			if nn, ok := fix(name); ok {
				changed = true
				return prefix + nn + ";"
			}
			return line
		})
		txt = reImp.ReplaceAllStringFunc(txt, func(line string) string {
			prefix, name := reImp.FindStringSubmatch(line)[1], reImp.FindStringSubmatch(line)[2]
			if nn, ok := fix(name); ok {
				changed = true
				return prefix + nn + ";"
			}
			return line
		})
		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}
	return nil
}

func normalizeCppLike(root string) error {
	ccExts := []string{".c", ".h", ".hpp", ".hh", ".hxx", ".cpp", ".cc", ".cxx"}
	files := collectFilesMany(root, ccExts)
	if len(files) == 0 {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*#\s*include\s*[<"]([^">]+)[">]`)
	for _, p := range files {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false

		txt = re.ReplaceAllStringFunc(txt, func(line string) string {
			m := re.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			target := m[1]
			if strings.Contains(target, "/src/") {
				suffix := target[strings.Index(target, "/src/")+len("/src/"):]
				abs := filepath.Join(root, "src", filepath.FromSlash(suffix))
				if _, err := os.Stat(abs); err == nil {
					newRel := relFromTo(filepath.Dir(p), abs)
					if newRel != "" {
						changed = true
						return strings.Replace(line, target, newRel, 1)
					}
				}
			}
			if isUnderRoot(root, target) {
				abs := filepath.Join(root, filepath.FromSlash(target))
				if _, err := os.Stat(abs); err == nil {
					newRel := relFromTo(filepath.Dir(p), abs)
					if newRel != "" {
						changed = true
						return strings.Replace(line, target, newRel, 1)
					}
				}
			}
			return line
		})

		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}
	return nil
}

func normalizePHP(root string) error {
	files := collectFiles(root, ".php")
	if len(files) == 0 {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*use\s+([A-Za-z0-9_\\]+)\s*;`)
	for _, p := range files {
		orig, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		txt := string(orig)
		changed := false
		txt = re.ReplaceAllStringFunc(txt, func(line string) string {
			m := re.FindStringSubmatch(line)
			if len(m) < 2 {
				return line
			}
			name := m[1]
			nn := strings.ReplaceAll(name, `\Src\`, `\`)
			if nn != name {
				changed = true
				return strings.Replace(line, name, nn, 1)
			}
			return line
		})
		if changed {
			_ = os.WriteFile(p, []byte(txt), 0o644)
		}
	}
	return nil
}
