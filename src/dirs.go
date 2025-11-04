package src

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/bubbles/list"
)

func loadDirs(path string) []list.Item {
	entries, err := os.ReadDir(path)
	if path == "" {
		path, _ = os.Getwd()
	}

	if err != nil {
		return []list.Item{dirItem{name: "(error reading dir)", path: path}}
	}
	var items []list.Item

	// 1. Add confirmation item
	items = append(items, dirItem{name: fmt.Sprintf("✅ Use this directory (%s)", filepath.Base(path)), path: path})

	// 2. Add parent directory navigation
	if path != "/" {
		items = append(items, dirItem{name: "⬆️ ../", path: filepath.Dir(path)})
	}

	// 3. Add subdirectories
	for _, e := range entries { // Already sorted by ReadDir
		if e.IsDir() {
			items = append(items, dirItem{name: "📁 " + e.Name() + "/", path: filepath.Join(path, e.Name())})
		}
	}
	return items
}
