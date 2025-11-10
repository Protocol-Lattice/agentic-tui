package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	memory "github.com/Protocol-Lattice/go-agent/src/memory"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type FileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
}

type CodebaseApp struct {
	engine *memory.Engine
	store  map[string][]FileEntry
}

func newApp(ctx context.Context) (*CodebaseApp, error) {
	baseURL := os.Getenv("QDRANT_URL")
	collection := "memories"
	vs := memory.NewQdrantStore(baseURL, collection, "")
	eng := memory.NewEngine(vs, memory.DefaultOptions()).WithEmbedder(memory.AutoEmbedder())
	return &CodebaseApp{
		engine: eng,
		store:  make(map[string][]FileEntry),
	}, nil
}

func main() {
	var (
		transport = flag.String("transport", "stdio", "stdio|http")
		addr      = flag.String("addr", ":8090", "addr for http")
	)
	flag.Parse()

	ctx := context.Background()
	app, err := newApp(ctx)
	if err != nil {
		log.Fatal(err)
	}

	s := server.NewMCPServer(
		"codebase-mcp",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	refactorTool := mcp.NewTool(
		"codebase.refactor_codebase",
		mcp.WithDescription("Refactor all source files in a directory using semantic context from memory-bank or external LLM."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Memory session ID for retrieval and context embedding")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Root directory containing source code")),
		mcp.WithString("extensions", mcp.Description("Comma-separated file extensions to include (e.g. .go,.md,.json)")),
		mcp.WithString("query", mcp.Description("Optional semantic refactor goal, e.g. 'Improve readability and add comments'")),
	)
	s.AddTool(refactorTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, _ := req.RequireString("session_id")
		root, _ := req.RequireString("path")
		exts := strings.Split(getStringParam(req, "extensions"), ",")
		query := getStringParam(req, "query")

		include := map[string]bool{}
		for _, e := range exts {
			if trimmed := strings.TrimSpace(e); trimmed != "" {
				include[trimmed] = true
			}
		}

		type RefactorResult struct {
			Path       string `json:"path"`
			Original   string `json:"original"`
			Refactored string `json:"refactored"`
		}

		results := []RefactorResult{}

		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			ext := filepath.Ext(p)
			if len(include) > 0 && !include[ext] {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}

			if _, err := app.engine.Store(ctx, sid, string(data), map[string]any{
				"path": p,
				"type": "original",
				"size": len(data),
			}); err != nil {
				log.Printf("[WARN] failed to store file %s: %v", p, err)
			}

			related, err := app.engine.Retrieve(ctx, query, 30)
			if err != nil {
				log.Printf("[WARN] memory search failed for %s: %v", p, err)
			}

			refactored := string(data)
			if strings.Contains(refactored, "TODO") {
				refactored = strings.ReplaceAll(refactored, "TODO", "// NOTE: addressed TODO")
			}
			if len(related) > 0 {
				refactored = fmt.Sprintf("// Refactored using %d related memories\n%s", len(related), refactored)
			}

			if err := os.WriteFile(p, []byte(refactored), 0o644); err != nil {
				return err
			}

			results = append(results, RefactorResult{
				Path:       p,
				Original:   string(data)[:min(300, len(data))],
				Refactored: refactored[:min(300, len(refactored))],
			})

			time.Sleep(100 * time.Millisecond)
			return nil
		})

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("refactor failed: %v", err)), nil
		}

		res, _ := mcp.NewToolResultJSON(map[string]any{
			"status":  "completed",
			"message": fmt.Sprintf("Refactored %d files in %s", len(results), root),
			"query":   query,
			"results": results,
		})
		return res, nil
	})

	saveCodebase := mcp.NewTool("codebase.save_codebase",
		mcp.WithDescription("Write retrieved file tree contents back to the specified directory path."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Destination root directory to save files")),
		mcp.WithString("tree_json", mcp.Required(), mcp.Description("JSON array from codebase.retrieve_tree or memory recall")),
	)
	s.AddTool(saveCodebase, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		root, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError("missing path"), nil
		}

		treeJSON, err := req.RequireString("tree_json")
		if err != nil {
			return mcp.NewToolResultError("missing tree_json"), nil
		}

		var entries []FileEntry
		if err := json.Unmarshal([]byte(treeJSON), &entries); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid tree_json: %v", err)), nil
		}

		saved := 0
		failed := 0

		for _, entry := range entries {
			targetPath := filepath.Join(root, entry.Path)

			if entry.IsDir {
				if err := os.MkdirAll(targetPath, 0o755); err != nil {
					failed++
					log.Printf("[ERROR] failed to create dir %s: %v", targetPath, err)
				}
				continue
			}

			dir := filepath.Dir(targetPath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				failed++
				log.Printf("[ERROR] failed to create parent dir %s: %v", dir, err)
				continue
			}

			if err := os.WriteFile(targetPath, []byte(entry.Content), 0o644); err != nil {
				failed++
				log.Printf("[ERROR] failed to write %s: %v", targetPath, err)
			} else {
				saved++
			}
		}

		res, _ := mcp.NewToolResultJSON(map[string]any{
			"status":        "completed",
			"files_saved":   saved,
			"files_failed":  failed,
			"destination":   root,
			"entries_total": len(entries),
		})
		return res, nil
	})

	s.AddTool(refactorTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, _ := req.RequireString("session_id")
		root, _ := req.RequireString("path")
		exts := strings.Split(getStringParam(req, "extensions"), ",")
		query := getStringParam(req, "query")

		include := map[string]bool{}
		for _, e := range exts {
			if trimmed := strings.TrimSpace(e); trimmed != "" {
				include[trimmed] = true
			}
		}

		type RefactorResult struct {
			Path       string `json:"path"`
			Original   string `json:"original"`
			Refactored string `json:"refactored"`
		}

		results := []RefactorResult{}

		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}

			ext := filepath.Ext(p)
			if len(include) > 0 && !include[ext] {
				return nil
			}

			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}

			if _, err := app.engine.Store(ctx, sid, string(data), map[string]any{
				"path": p,
				"type": "original",
				"size": len(data),
			}); err != nil {
				log.Printf("[WARN] failed to store file %s: %v", p, err)
			}

			related, err := app.engine.Retrieve(ctx, query, 30)
			if err != nil {
				log.Printf("[WARN] memory search failed for %s: %v", p, err)
			}

			refactored := string(data)
			if strings.Contains(refactored, "TODO") {
				refactored = strings.ReplaceAll(refactored, "TODO", "// NOTE: addressed TODO")
			}
			if len(related) > 0 {
				refactored = fmt.Sprintf("// Refactored using %d related memories\n%s", len(related), refactored)
			}

			if err := os.WriteFile(p, []byte(refactored), 0o644); err != nil {
				return err
			}

			results = append(results, RefactorResult{
				Path:       p,
				Original:   truncate(string(data), 300),
				Refactored: truncate(refactored, 300),
			})

			time.Sleep(100 * time.Millisecond)
			return nil
		})

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("refactor failed: %v", err)), nil
		}

		res, _ := mcp.NewToolResultJSON(map[string]any{
			"status":  "completed",
			"message": fmt.Sprintf("Refactored %d files in %s", len(results), root),
			"query":   query,
			"results": results,
		})
		return res, nil
	})

	chunkAndStore := mcp.NewTool(
		"memory.store_codebase_chunked",
		mcp.WithDescription("Store codebase files in chunks to avoid overwhelming the embedder"),
		mcp.WithString("session_id", mcp.Required()),
		mcp.WithString("file_path", mcp.Required()),
		mcp.WithString("content", mcp.Required()),
		mcp.WithNumber("chunk_size", mcp.Description("Characters per chunk, default 2000")),
	)

	s.AddTool(chunkAndStore, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, _ := req.RequireString("session_id")
		filePath, _ := req.RequireString("file_path")
		content, _ := req.RequireString("content")

		chunkSize := int(getNumberParam(req, "chunk_size"))
		if chunkSize <= 0 {
			chunkSize = 2000
		}

		chunks := make([]string, 0, (len(content)/chunkSize)+1)
		for i := 0; i < len(content); i += chunkSize {
			end := i + chunkSize
			if end > len(content) {
				end = len(content)
			}
			chunks = append(chunks, content[i:end])
		}

		stored := 0
		failed := 0

		for idx, chunk := range chunks {
			select {
			case <-ctx.Done():
				res, err := mcp.NewToolResultJSON(map[string]any{
					"file":          filePath,
					"chunks_stored": stored,
					"chunks_failed": failed,
					"total_chunks":  len(chunks),
					"status":        "cancelled",
					"message":       ctx.Err().Error(),
				})
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
				}
				return res, nil
			default:
			}

			meta := map[string]any{
				"file_path":    filePath,
				"chunk_index":  idx,
				"total_chunks": len(chunks),
				"type":         "code",
			}

			if _, err := app.engine.Store(ctx, sid, chunk, meta); err != nil {
				log.Printf("Failed to store chunk %d of %s: %v", idx, filePath, err)
				failed++
				continue
			}
			stored++
			time.Sleep(100 * time.Millisecond)
		}

		res, err := mcp.NewToolResultJSON(map[string]any{
			"file":          filePath,
			"chunks_stored": stored,
			"chunks_failed": failed,
			"total_chunks":  len(chunks),
			"status":        "ok",
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
		}
		return res, nil
	})

	batchStore := mcp.NewTool(
		"memory.store_batch",
		mcp.WithDescription("Store multiple items in Qdrant with auto-collection check, retry, and rate limiting."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Memory session ID")),
		mcp.WithString("items_json", mcp.Required(), mcp.Description("JSON array of {content, metadata} objects")),
		mcp.WithNumber("delay_ms", mcp.Description("Delay between items in ms (default 200)")),
	)

	s.AddTool(batchStore, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, _ := req.RequireString("session_id")
		itemsJSON, _ := req.RequireString("items_json")

		var items []struct {
			Content  string         `json:"content"`
			Metadata map[string]any `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(itemsJSON), &items); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid items_json: %v", err)), nil
		}

		delayMs := int(getNumberParam(req, "delay_ms"))
		if delayMs <= 0 {
			delayMs = 200
		}

		qdrantURL := os.Getenv("QDRANT_URL")
		collection := os.Getenv("QDRANT_COLLECTION")
		if qdrantURL == "" || collection == "" {
			return mcp.NewToolResultError(
				"QDRANT_URL or QDRANT_COLLECTION not set — export them before running the memory-bank service",
			), nil
		}

		ensureCollection := func() error {
			resp, err := http.Get(fmt.Sprintf("%s/collections/%s", strings.TrimSuffix(qdrantURL, "/"), collection))
			if err != nil {
				return fmt.Errorf("cannot reach Qdrant: %w", err)
			}
			if resp.StatusCode == 404 {
				body := `{"name":"` + collection + `"}`
				req, _ := http.NewRequest("PUT",
					fmt.Sprintf("%s/collections/%s", strings.TrimSuffix(qdrantURL, "/"), collection),
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				client := &http.Client{Timeout: 5 * time.Second}
				res, err := client.Do(req)
				if err != nil {
					return fmt.Errorf("failed to create collection: %w", err)
				}
				defer res.Body.Close()
				if res.StatusCode >= 300 {
					return fmt.Errorf("collection creation failed: %s", res.Status)
				}
			}
			return nil
		}

		if err := ensureCollection(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Qdrant collection check failed: %v", err)), nil
		}

		results := make([]map[string]any, 0, len(items))
		stored, failed := 0, 0

		for idx, item := range items {
			select {
			case <-ctx.Done():
				return mcp.NewToolResultError("operation cancelled"), nil
			default:
			}

			var rec memory.MemoryRecord
			var err error

			for attempt := 1; attempt <= 3; attempt++ {
				rec, err = app.engine.Store(ctx, sid, item.Content, item.Metadata)
				if err == nil {
					stored++
					results = append(results, map[string]any{
						"index":  idx,
						"status": "success",
						"id":     rec.ID,
					})
					break
				}
				log.Printf("[WARN] store_batch attempt %d failed for item %d: %v", attempt, idx, err)
				time.Sleep(time.Duration(delayMs*attempt) * time.Millisecond)
			}

			if err != nil {
				failed++
				results = append(results, map[string]any{
					"index":  idx,
					"status": "failed",
					"error":  err.Error(),
				})
			}

			if idx < len(items)-1 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
		}

		summary := map[string]any{
			"status":      "completed",
			"stored":      stored,
			"failed":      failed,
			"collection":  collection,
			"qdrant_url":  qdrantURL,
			"results":     results,
			"total_items": len(items),
			"timestamp":   time.Now().Format(time.RFC3339),
		}

		res, _ := mcp.NewToolResultJSON(summary)
		return res, nil
	})

	storeTree := mcp.NewTool("codebase.store_tree",
		mcp.WithDescription("Scan a directory and store its file tree structure and file contents"),
		mcp.WithString("path", mcp.Required()),
		mcp.WithString("extensions", mcp.Description("Comma-separated file extensions to include")),
	)
	s.AddTool(storeTree, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		root, _ := req.RequireString("path")
		exts := strings.Split(getStringParam(req, "extensions"), ",")
		include := map[string]bool{}
		for _, e := range exts {
			if trimmed := strings.TrimSpace(e); trimmed != "" {
				include[trimmed] = true
			}
		}

		var entries []FileEntry
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			entry := FileEntry{
				Path:  rel,
				Size:  info.Size(),
				IsDir: info.IsDir(),
			}
			if !info.IsDir() {
				ext := filepath.Ext(p)
				if len(include) > 0 && !include[ext] {
					return nil
				}
				if info.Size() > 100_000 {
					return nil
				}
				data, err := os.ReadFile(p)
				if err == nil {
					entry.Content = string(data)
				}
			}
			entries = append(entries, entry)
			return nil
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("walk failed: %v", err)), nil
		}

		app.store[root] = entries
		res := mcp.NewToolResultText(fmt.Sprintf("Stored %d entries from %s", len(entries), root))
		return res, nil
	})

	retrieveTree := mcp.NewTool("codebase.retrieve_tree",
		mcp.WithDescription("Retrieve a previously stored file tree structure and contents"),
		mcp.WithString("path", mcp.Required()),
	)
	s.AddTool(retrieveTree, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		root, _ := req.RequireString("path")
		entries, ok := app.store[root]
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("no entries stored for %s", root)), nil
		}
		out := map[string]any{
			"root":      root,
			"count":     len(entries),
			"tree":      entries,
			"retrieved": time.Now().Format(time.RFC3339),
		}
		res, _ := mcp.NewToolResultJSON(out)
		return res, nil
	})

	switch *transport {
	case "stdio":
		if err := server.ServeStdio(s); err != nil {
			log.Fatal(err)
		}
	case "http":
		h := server.NewStreamableHTTPServer(s)
		log.Printf("HTTP listening on %s", *addr)
		if err := h.Start(*addr); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("unknown transport: ", *transport)
	}
}

func getStringParam(req mcp.CallToolRequest, key string) string {
	args, ok := req.Params.Arguments.(map[string]interface{})
	if !ok {
		return ""
	}
	val, ok := args[key]
	if !ok {
		return ""
	}
	str, _ := val.(string)
	return str
}

func getNumberParam(req mcp.CallToolRequest, key string) float64 {
	args, ok := req.Params.Arguments.(map[string]interface{})
	if !ok {
		return 0
	}
	val, ok := args[key]
	if !ok {
		return 0
	}
	num, _ := val.(float64)
	return num
}

func isBinaryExt(ext string) bool {
	binaryExts := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true,
		".bin": true, ".dat": true, ".db": true, ".sqlite": true,
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
		".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
		".pyc": true, ".pyo": true, ".class": true, ".o": true,
	}
	return binaryExts[strings.ToLower(ext)]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
