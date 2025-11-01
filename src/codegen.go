package src

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func extractExplicitPath(code string) string {
	patterns := []string{
		`(?m)@path\s+([^\s]+)`,
		`(?m)//\s*path:\s*([^\s]+)`,
		`(?m)#\s*path:\s*([^\s]+)`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(code); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

var fenceRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_+\\.-]*)\\s*\\n(.*?)\\n```")

type fileMeta struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func (m *model) saveCodeBlocks(s string) {
	m.output += "\n---\n"
	matches := fenceRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		m.output += m.style.subtle.Render("No code blocks detected.\n")
		return
	}

	manifest := make(map[string][]fileMeta)

	for idx, mth := range matches {
		lang := strings.ToLower(strings.TrimSpace(mth[1]))
		code := strings.TrimSpace(mth[2])
		if lang == "" {
			lang = guessLanguageFromCode(code)
		}
		if lang == "" {
			lang = "txt"
		}

		// 1) Respect explicit @path if provided
		explicit := extractExplicitPath(code)
		var filename string
		var targetDir string
		pkgName := ""

		if explicit != "" {
			abs := filepath.Join(m.working, explicit)
			_ = os.MkdirAll(filepath.Dir(abs), 0o755)
			filename = abs
			targetDir = filepath.Dir(abs)
		} else {
			// 2) Entrypoint safeguard for Go: keep at root
			if lang == "go" && (strings.Contains(code, "package src") || strings.Contains(code, "func main(")) {
				targetDir = m.working
			} else {
				targetDir, pkgName = m.detectPackageDirectory(lang, code)
			}
			_ = os.MkdirAll(targetDir, 0o755)
			filename = filepath.Join(targetDir, m.guessFilename(lang, code, idx))
		}

		if err := os.WriteFile(filename, []byte(code+"\n"), 0o644); err != nil {
			m.output += m.style.error.Render(fmt.Sprintf("❌ failed to save %s: %v\n", filename, err))
			continue
		}

		key := lang
		if pkgName != "" {
			key = pkgName
		}
		manifest[key] = append(manifest[key], fileMeta{Name: filepath.Base(filename), Path: filename})
		m.output += m.style.success.Render(fmt.Sprintf("💾 saved %s\n", filename))
	}

	for _, files := range manifest {
		lang := guessPrimaryLang(files)
		m.addImports(lang, files)
	}
	if err := NormalizeImports(m.working); err != nil {
		m.output += m.style.subtle.Render(
			fmt.Sprintf("⚠ import normalize: %v\n", err),
		)
	}
}

// Universal package/module detector for ANY programming language
func (m *model) detectPackageDirectory(lang, code string) (string, string) {
	// Try universal patterns first
	if pkg := extractUniversalPackage(code); pkg != "" {
		pkgDir := filepath.Join(m.working, pkg)
		return pkgDir, pkg
	}

	// Language-specific detection
	switch lang {
	case "go":
		if pkg := extractGoPackage(code); pkg != "" && pkg != "main" {
			return filepath.Join(m.working, pkg), pkg
		}
		return m.working, ""

	case "python", "py":
		if pkg := extractPythonPackage(code); pkg != "" {
			return filepath.Join(m.working, pkg), pkg
		}
		return filepath.Join(m.working, lang), ""

	case "js", "javascript", "ts", "typescript":
		if pkg := extractJSPackage(code); pkg != "" {
			return filepath.Join(m.working, pkg), pkg
		}
		return filepath.Join(m.working, lang), ""

	case "rs", "rust":
		if mod := extractRustModule(code); mod != "" {
			return filepath.Join(m.working, "src", mod), mod
		}
		return filepath.Join(m.working, "src"), ""

	case "java":
		if pkg := extractJavaPackage(code); pkg != "" {
			pkgPath := strings.ReplaceAll(pkg, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "src", pkgPath), pkg
		}
		return filepath.Join(m.working, "src"), ""

	case "cs", "csharp", "c#":
		if ns := extractCSharpNamespace(code); ns != "" {
			nsPath := strings.ReplaceAll(ns, ".", string(os.PathSeparator))
			return filepath.Join(m.working, nsPath), ns
		}
		return filepath.Join(m.working, lang), ""

	case "cpp", "c++", "cc", "cxx":
		if ns := extractCppNamespace(code); ns != "" {
			return filepath.Join(m.working, "include", ns), ns
		}
		if strings.Contains(code, "#ifndef") || strings.Contains(code, "#pragma once") {
			return filepath.Join(m.working, "include"), ""
		}
		return filepath.Join(m.working, "src"), ""

	case "c":
		if strings.Contains(code, "#ifndef") || strings.Contains(code, "#pragma once") {
			return filepath.Join(m.working, "include"), ""
		}
		return filepath.Join(m.working, "src"), ""

	case "rb", "ruby":
		if mod := extractRubyModule(code); mod != "" {
			return filepath.Join(m.working, "lib", strings.ToLower(mod)), mod
		}
		return filepath.Join(m.working, "lib"), ""

	case "php":
		if ns := extractPHPNamespace(code); ns != "" {
			nsPath := strings.ReplaceAll(ns, "\\", string(os.PathSeparator))
			return filepath.Join(m.working, "src", nsPath), ns
		}
		return filepath.Join(m.working, "src"), ""

	case "kt", "kotlin":
		if pkg := extractKotlinPackage(code); pkg != "" {
			pkgPath := strings.ReplaceAll(pkg, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "src", pkgPath), pkg
		}
		return filepath.Join(m.working, "src"), ""

	case "swift":
		if mod := extractSwiftModule(code); mod != "" {
			return filepath.Join(m.working, "Sources", mod), mod
		}
		return filepath.Join(m.working, "Sources"), ""

	case "dart":
		if pkg := extractDartPackage(code); pkg != "" {
			return filepath.Join(m.working, "lib", pkg), pkg
		}
		return filepath.Join(m.working, "lib"), ""

	case "lua":
		if mod := extractLuaModule(code); mod != "" {
			return filepath.Join(m.working, mod), mod
		}
		return m.working, ""

	case "elixir", "ex":
		if mod := extractElixirModule(code); mod != "" {
			modPath := strings.ReplaceAll(mod, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "lib", strings.ToLower(modPath)), mod
		}
		return filepath.Join(m.working, "lib"), ""

	case "scala":
		if pkg := extractScalaPackage(code); pkg != "" {
			pkgPath := strings.ReplaceAll(pkg, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "src", "main", "scala", pkgPath), pkg
		}
		return filepath.Join(m.working, "src", "main", "scala"), ""

	case "clojure", "clj":
		if ns := extractClojureNamespace(code); ns != "" {
			nsPath := strings.ReplaceAll(ns, ".", string(os.PathSeparator))
			nsPath = strings.ReplaceAll(nsPath, "-", "_")
			return filepath.Join(m.working, "src", nsPath), ns
		}
		return filepath.Join(m.working, "src"), ""

	case "haskell", "hs":
		if mod := extractHaskellModule(code); mod != "" {
			modPath := strings.ReplaceAll(mod, ".", string(os.PathSeparator))
			return filepath.Join(m.working, "src", modPath), mod
		}
		return filepath.Join(m.working, "src"), ""

	case "r":
		if pkg := extractRPackage(code); pkg != "" {
			return filepath.Join(m.working, "R", pkg), pkg
		}
		return filepath.Join(m.working, "R"), ""

	case "julia", "jl":
		if mod := extractJuliaModule(code); mod != "" {
			return filepath.Join(m.working, "src", mod), mod
		}
		return filepath.Join(m.working, "src"), ""

	default:
		// Generic fallback: try to detect any module-like structure
		return filepath.Join(m.working, lang), ""
	}
}

// Universal package detector - works across multiple languages
func extractUniversalPackage(code string) string {
	patterns := []string{
		`@package\s+([a-zA-Z_][a-zA-Z0-9_.-]*)`,
		`@module\s+([a-zA-Z_][a-zA-Z0-9_.-]*)`,
		`#\s*package:\s*([a-zA-Z_][a-zA-Z0-9_.-]*)`,
		`//\s*package:\s*([a-zA-Z_][a-zA-Z0-9_.-]*)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if match := re.FindStringSubmatch(code); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

// Language-specific extractors
func extractGoPackage(code string) string {
	re := regexp.MustCompile(`(?m)^package\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractPythonPackage(code string) string {
	if strings.Contains(code, "__all__") || strings.Contains(code, "__init__") {
		if strings.Contains(code, "class ") {
			if name := extractAfter(code, "class "); name != "" {
				return strings.ToLower(name)
			}
		}
	}
	return ""
}

func extractJSPackage(code string) string {
	re := regexp.MustCompile(`[@/]\s*(?:package|module)\s+([a-zA-Z_][a-zA-Z0-9_-]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractRustModule(code string) string {
	re := regexp.MustCompile(`(?m)^(?:pub\s+)?mod\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractJavaPackage(code string) string {
	re := regexp.MustCompile(`(?m)^package\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractCSharpNamespace(code string) string {
	re := regexp.MustCompile(`(?m)^\s*namespace\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractCppNamespace(code string) string {
	re := regexp.MustCompile(`(?m)^\s*namespace\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractRubyModule(code string) string {
	re := regexp.MustCompile(`(?m)^\s*module\s+([A-Z][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractPHPNamespace(code string) string {
	re := regexp.MustCompile(`(?m)^\s*namespace\s+([a-zA-Z_][a-zA-Z0-9_\\]*)\s*;`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractKotlinPackage(code string) string {
	re := regexp.MustCompile(`(?m)^package\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractSwiftModule(code string) string {
	if strings.Contains(code, "public struct") || strings.Contains(code, "public class") {
		re := regexp.MustCompile(`public\s+(?:struct|class)\s+([A-Z][a-zA-Z0-9_]*)`)
		if match := re.FindStringSubmatch(code); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func extractDartPackage(code string) string {
	re := regexp.MustCompile(`(?m)^library\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractLuaModule(code string) string {
	re := regexp.MustCompile(`(?m)^local\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*\{\}`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		if strings.Contains(code, "return "+match[1]) {
			return match[1]
		}
	}
	return ""
}

func extractElixirModule(code string) string {
	re := regexp.MustCompile(`(?m)^\s*defmodule\s+([A-Z][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractScalaPackage(code string) string {
	re := regexp.MustCompile(`(?m)^package\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractClojureNamespace(code string) string {
	re := regexp.MustCompile(`(?m)^\s*\(\s*ns\s+([a-zA-Z_][a-zA-Z0-9_.-]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractHaskellModule(code string) string {
	re := regexp.MustCompile(`(?m)^module\s+([A-Z][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractRPackage(code string) string {
	re := regexp.MustCompile(`#'\s*@package\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractJuliaModule(code string) string {
	re := regexp.MustCompile(`(?m)^\s*module\s+([A-Z][a-zA-Z0-9_]*)`)
	if match := re.FindStringSubmatch(code); len(match) > 1 {
		return match[1]
	}
	return ""
}

func guessPrimaryLang(files []fileMeta) string {
	if len(files) == 0 {
		return "txt"
	}
	ext := filepath.Ext(files[0].Name)
	return strings.TrimPrefix(ext, ".")
}

func (m *model) dirForLanguage(lang string) string {
	switch lang {
	case "go":
		return m.working
	case "rs", "rust":
		return filepath.Join(m.working, "src")
	case "java", "kt", "kotlin", "scala":
		return filepath.Join(m.working, "src")
	case "rb", "ruby":
		return filepath.Join(m.working, "lib")
	case "php":
		return filepath.Join(m.working, "src")
	case "swift":
		return filepath.Join(m.working, "Sources")
	case "dart":
		return filepath.Join(m.working, "lib")
	case "elixir", "ex":
		return filepath.Join(m.working, "lib")
	case "clojure", "clj":
		return filepath.Join(m.working, "src")
	case "haskell", "hs":
		return filepath.Join(m.working, "src")
	case "r":
		return filepath.Join(m.working, "R")
	case "julia", "jl":
		return filepath.Join(m.working, "src")
	case "c", "cpp", "c++", "cc", "cxx":
		return filepath.Join(m.working, "src")
	default:
		return filepath.Join(m.working, lang)
	}
}

func (m *model) guessFilename(lang, code string, idx int) string {
	name := fmt.Sprintf("file_%d", idx+1)

	switch lang {
	case "python", "py":
		if strings.Contains(code, "class ") {
			name = strings.ToLower(sanitizeFilename(extractAfter(code, "class ")))
		} else if strings.Contains(code, "def ") {
			name = strings.ToLower(sanitizeFilename(extractAfter(code, "def ")))
		}
	case "js", "ts":
		if strings.Contains(code, "export default function") {
			name = sanitizeFilename(extractAfter(code, "export default function "))
		} else if strings.Contains(code, "function ") {
			name = sanitizeFilename(extractAfter(code, "function "))
		}
	case "rs":
		if strings.Contains(code, "pub mod ") {
			name = sanitizeFilename(extractAfter(code, "pub mod "))
		} else if strings.Contains(code, "mod ") {
			name = sanitizeFilename(extractAfter(code, "mod "))
		}
	case "cpp", "c":
		if strings.Contains(code, "class ") {
			name = sanitizeFilename(extractAfter(code, "class "))
		} else if strings.Contains(code, "struct ") {
			name = sanitizeFilename(extractAfter(code, "struct "))
		}
	case "java":
		if strings.Contains(code, "class ") {
			name = sanitizeFilename(extractAfter(code, "class "))
		}
	case "go":
		if strings.Contains(code, "package ") {
			name = sanitizeFilename(extractAfter(code, "package "))
		}
	case "swift":
		if strings.Contains(code, "struct ") {
			name = sanitizeFilename(extractAfter(code, "struct "))
		} else if strings.Contains(code, "class ") {
			name = sanitizeFilename(extractAfter(code, "class "))
		}
	}

	if name == "" {
		name = fmt.Sprintf("file_%d", idx+1)
	}

	dir := m.dirForLanguage(lang)
	ext := extFor(lang)
	return filepath.Join(dir, fmt.Sprintf("%s.%s", name, ext))
}

func extractAfter(code, key string) string {
	idx := strings.Index(code, key)
	if idx == -1 {
		return ""
	}
	line := strings.SplitN(code[idx+len(key):], "\n", 2)[0]
	return strings.Fields(line)[0]
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "(){};:")
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "'", "")
	return name
}

func extFor(lang string) string {
	mapping := map[string]string{
		"go": "go", "python": "py", "py": "py", "js": "js", "ts": "ts",
		"rs": "rs", "cpp": "cpp", "c": "c", "sh": "sh", "html": "html",
		"css": "css", "json": "json", "yml": "yml", "yaml": "yml",
		"md": "md", "txt": "txt",
	}
	if e, ok := mapping[lang]; ok {
		return e
	}
	return "txt"
}

func guessLanguageFromCode(code string) string {
	switch {
	case strings.Contains(code, "package src"):
		return "go"
	case strings.Contains(code, "def "):
		return "python"
	case strings.Contains(code, "import React"):
		return "js"
	case strings.Contains(code, "fn main"):
		return "rs"
	case strings.Contains(code, "#include"):
		return "cpp"
	default:
		return ""
	}
}

func (m *model) addImports(lang string, files []fileMeta) {
	// Go: ignore module scaffolding and imports entirely.
	if lang == "go" {
		return
	}

	// For other languages, create a lightweight aggregator file and append imports.
	main := filepath.Join(m.working, lang, fmt.Sprintf("main.%s", extFor(lang)))

	var imports []string
	for _, f := range files {
		if filepath.Base(f.Path) == filepath.Base(main) {
			continue
		}
		name := strings.TrimSuffix(f.Name, filepath.Ext(f.Name))
		switch lang {
		case "python", "py":
			imports = append(imports, fmt.Sprintf("from %s import *", name))
		case "js", "ts":
			imports = append(imports, fmt.Sprintf("import './%s.%s'", name, extFor(lang)))
		case "rs":
			imports = append(imports, fmt.Sprintf("mod %s;", name))
		case "cpp":
			imports = append(imports, fmt.Sprintf("#include \"%s.h\"", name))
		case "sh":
			imports = append(imports, fmt.Sprintf("source ./%s.sh", name))
		case "html":
			imports = append(imports, fmt.Sprintf("<script src=\"./%s.js\"></script>", name))
		default:
			imports = append(imports, fmt.Sprintf("// related: %s", name))
		}
	}

	if len(imports) == 0 {
		return
	}

	content := strings.Join(imports, "\n") + "\n\n"
	appendLine(main, content)
}

func appendLine(path, text string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(text)
}

func saveManifest(root, lang string, files any) {
	data, _ := json.MarshalIndent(files, "", "  ")
	_ = os.WriteFile(filepath.Join(root, lang, "manifest.json"), data, 0o644)
}
