package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	memory "github.com/Protocol-Lattice/go-agent/src/memory"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// App wires MemoryBank + SessionMemory + Spaces and registers MCP tools.
type App struct {
	bank   *memory.MemoryBank
	sm     *memory.SessionMemory
	engine *memory.Engine
	spaces *memory.SpaceRegistry
	shared map[string]*memory.SharedSession
	mu     sync.RWMutex
}

func newApp(ctx context.Context) (*App, error) {
	storeKind := strings.ToLower(env("MEMORY_STORE", "qdrant"))
	shortBuf := atoi(env("SHORT_TERM_SIZE", "500000"), 500000)
	spaceTTL := atoi(env("DEFAULT_SPACE_TTL_SEC", "86400"), 86400)

	var vs memory.VectorStore
	var err error
	switch storeKind {
	case "postgres", "pg":
		dsn := mustEnv("POSTGRES_DSN")
		vs, err = memory.NewPostgresStore(ctx, dsn)
		if err != nil {
			return nil, fmt.Errorf("failed to create postgres store: %w", err)
		}
	case "qdrant":
		base := mustEnv("QDRANT_URL")
		col := env("QDRANT_COLLECTION", "memories")
		api := env("QDRANT_API_KEY", "")
		vs = memory.NewQdrantStore(base, col, api)
	case "mongo":
		uri := mustEnv("MONGO_URI")
		database := mustEnv("MONGO_DATABASE")
		collection := env("MONGO_COLLECTION", "memories")
		vs, err = memory.NewMongoStore(ctx, uri, database, collection)
		if err != nil {
			return nil, fmt.Errorf("failed to create mongo store: %w", err)
		}
	default:
		vs = memory.NewInMemoryStore()
	}

	bank := memory.NewMemoryBankWithStore(vs)
	eng := memory.NewEngine(vs, memory.DefaultOptions()).WithEmbedder(memory.AutoEmbedder())
	sm := memory.NewSessionMemory(bank, shortBuf).WithEmbedder(memory.AutoEmbedder()).WithEngine(eng)
	spaces := memory.NewSpaceRegistry(time.Duration(spaceTTL) * time.Second)

	return &App{
		bank:   bank,
		sm:     sm,
		engine: eng,
		spaces: spaces,
		shared: make(map[string]*memory.SharedSession),
	}, nil
}

func (a *App) sharedFor(principal string) *memory.SharedSession {
	a.mu.Lock()
	defer a.mu.Unlock()
	ss := a.shared[principal]
	if ss == nil {
		ss = memory.NewSharedSession(a.sm, principal)
		a.shared[principal] = ss
	}
	return ss
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

func main() {
	var (
		transport = flag.String("transport", "stdio", "stdio|http")
		addr      = flag.String("addr", ":8080", "addr for http")
	)
	flag.Parse()

	ctx := context.Background()
	app, err := newApp(ctx)
	if err != nil {
		log.Fatal(err)
	}

	s := server.NewMCPServer(
		"memory-bank-mcp",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	ping := mcp.NewTool("health.ping", mcp.WithDescription("Return pong and server time"))
	s.AddTool(ping, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, _ := mcp.NewToolResultJSON(map[string]any{"pong": true, "time": time.Now().Format(time.RFC3339)})
		return res, nil
	})

	embedTool := mcp.NewTool("memory.embed",
		mcp.WithDescription("Return embedding vector for a text using AutoEmbedder (ADK_EMBED_* env)"),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to embed")),
	)
	s.AddTool(embedTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, err := req.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing required parameter 'text': %v", err)), nil
		}
		e, err := app.sm.Embed(ctx, text)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, _ := mcp.NewToolResultJSON(map[string]any{
			"embedding": e,
		})
		return res, nil
	})

	addShort := mcp.NewTool("memory.add_short",
		mcp.WithDescription("Append short-term memory to a session buffer (flush later to long-term)"),
		mcp.WithString("session_id", mcp.Required()),
		mcp.WithString("content", mcp.Required()),
		mcp.WithString("metadata_json", mcp.Description("JSON object (string->string)")),
	)
	s.AddTool(addShort, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, err := req.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing session_id: %v", err)), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing content: %v", err)), nil
		}

		metaStr := getStringParam(req, "metadata_json")
		m := map[string]string{}
		if metaStr != "" {
			if err := json.Unmarshal([]byte(metaStr), &m); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid metadata_json: %v", err)), nil
			}
		}
		e, err := app.sm.Embed(ctx, content)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		app.sm.AddShortTerm(sid, content, stringMapToJSON(m), e)
		return mcp.NewToolResultText("ok"), nil
	})

	flush := mcp.NewTool("memory.flush",
		mcp.WithDescription("Promote short-term buffer to long-term vector store for the given session"),
		mcp.WithString("session_id", mcp.Required()),
	)
	s.AddTool(flush, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, err := req.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing session_id: %v", err)), nil
		}
		if err := app.sm.FlushToLongTerm(ctx, sid); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("flushed"), nil
	})

	storeLong := mcp.NewTool("memory.store_long",
		mcp.WithDescription("Embed, score and persist a long-term memory (uses Engine)"),
		mcp.WithString("session_id", mcp.Required()),
		mcp.WithString("content", mcp.Required()),
		mcp.WithString("metadata_json", mcp.Description("JSON object (any) e.g. {\"source\":\"chat\"}")),
	)
	s.AddTool(storeLong, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, err := req.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing session_id: %v", err)), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing content: %v", err)), nil
		}

		meta := map[string]any{}
		metaStr := getStringParam(req, "metadata_json")
		if metaStr != "" {
			if err := json.Unmarshal([]byte(metaStr), &meta); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid metadata_json: %v", err)), nil
			}
		}

		rec, err := app.engine.Store(ctx, sid, content, meta)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, _ := mcp.NewToolResultJSON(rec)
		return res, nil
	})

	retrieve := mcp.NewTool("memory.retrieve_context",
		mcp.WithDescription("Return top-k memories (short+long term) for session"),
		mcp.WithString("session_id", mcp.Required()),
		mcp.WithString("query", mcp.Required()),
		mcp.WithNumber("limit"),
	)
	s.AddTool(retrieve, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, err := req.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing session_id: %v", err)), nil
		}
		q, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing query: %v", err)), nil
		}

		limit := int(getNumberParam(req, "limit"))
		if limit <= 0 {
			limit = 8
		}

		recs, err := app.sm.RetrieveContext(ctx, sid, q, limit)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, _ := mcp.NewToolResultJSON(map[string]any{
			"records": recs,
		})
		return res, nil
	})

	chainPrompt := mcp.NewTool("memory.chain_prompt",
		mcp.WithDescription("Embed, store, flush, retrieve, and return a prompt with context memories"),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Memory session identifier")),
		mcp.WithString("content", mcp.Required(), mcp.Description("New content to store and embed")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Semantic query for retrieval")),
		mcp.WithNumber("limit", mcp.Description("Number of memories to retrieve")),
		mcp.WithString("include_contents", mcp.Description("Include raw file contents in prompt output")),
	)

	s.AddTool(chainPrompt, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sid, _ := req.RequireString("session_id")
		content, _ := req.RequireString("content")
		query, _ := req.RequireString("query")

		limit := int(getNumberParam(req, "limit"))
		if limit <= 0 {
			limit = 5
		}

		e, err := app.sm.Embed(ctx, content)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("embed failed: %v", err)), nil
		}

		app.sm.AddShortTerm(sid, content, "{}", e)

		if err := app.sm.FlushToLongTerm(ctx, sid); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("flush failed: %v", err)), nil
		}

		if _, err := app.sm.RetrieveContext(ctx, sid, query, limit); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("retrieve failed: %v", err)), nil
		}

		out := map[string]any{
			"status":   "Completed",
			"session":  sid,
			"query":    query,
			"embedded": len(e) > 0,
		}

		res, _ := mcp.NewToolResultJSON(out)
		return res, nil
	})

	spacesUpsert := mcp.NewTool("spaces.upsert",
		mcp.WithDescription("Create or update a space definition with TTL and ACL"),
		mcp.WithString("name", mcp.Required()),
		mcp.WithNumber("ttl_seconds"),
		mcp.WithString("acl_json", mcp.Description("JSON map principal->role (reader|writer|admin)")),
	)
	s.AddTool(spacesUpsert, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing name: %v", err)), nil
		}

		ttl := getNumberParam(req, "ttl_seconds")

		acl := map[string]string{}
		aclStr := getStringParam(req, "acl_json")
		if aclStr != "" {
			if err := json.Unmarshal([]byte(aclStr), &acl); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid acl_json: %v", err)), nil
			}
		}

		m := map[string]memory.SpaceRole{}
		for p, role := range acl {
			m[p] = parseRole(role)
		}
		app.spaces.Upsert(name, time.Duration(int(ttl))*time.Second, m)
		return mcp.NewToolResultText("ok"), nil
	})

	spacesGrant := mcp.NewTool("spaces.grant",
		mcp.WithDescription("Grant a role to a principal for a space"),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("principal", mcp.Required()),
		mcp.WithString("role", mcp.Required()),
		mcp.WithNumber("ttl_seconds"),
	)
	s.AddTool(spacesGrant, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing name: %v", err)), nil
		}
		principal, err := req.RequireString("principal")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing principal: %v", err)), nil
		}
		roleStr, err := req.RequireString("role")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing role: %v", err)), nil
		}

		ttl := int(getNumberParam(req, "ttl_seconds"))
		if ttl <= 0 {
			ttl = 3600
		}

		if err := app.spaces.Grant(name, principal, parseRole(roleStr), time.Duration(ttl)*time.Second); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("ok"), nil
	})

	spacesRevoke := mcp.NewTool("spaces.revoke",
		mcp.WithDescription("Revoke a principal from a space"),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("principal", mcp.Required()),
	)
	s.AddTool(spacesRevoke, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing name: %v", err)), nil
		}
		principal, err := req.RequireString("principal")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing principal: %v", err)), nil
		}
		app.spaces.Revoke(name, principal)
		return mcp.NewToolResultText("ok"), nil
	})

	spacesList := mcp.NewTool("spaces.list",
		mcp.WithDescription("List spaces visible to a principal"),
		mcp.WithString("principal", mcp.Required()),
	)
	s.AddTool(spacesList, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		principal, err := req.RequireString("principal")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing principal: %v", err)), nil
		}
		list := app.spaces.List(principal)
		res, _ := mcp.NewToolResultJSON(list)
		return res, nil
	})

	sharedJoin := mcp.NewTool("shared.join",
		mcp.WithDescription("Ensure a principal view exists and join a space"),
		mcp.WithString("principal", mcp.Required()),
		mcp.WithString("space", mcp.Required()),
	)
	s.AddTool(sharedJoin, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := req.RequireString("principal")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing principal: %v", err)), nil
		}
		space, err := req.RequireString("space")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing space: %v", err)), nil
		}
		ss := app.sharedFor(p)
		if err := ss.Join(space); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("ok"), nil
	})

	sharedLeave := mcp.NewTool("shared.leave",
		mcp.WithDescription("Leave a space in principal view"),
		mcp.WithString("principal", mcp.Required()),
		mcp.WithString("space", mcp.Required()),
	)
	s.AddTool(sharedLeave, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := req.RequireString("principal")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing principal: %v", err)), nil
		}
		space, err := req.RequireString("space")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing space: %v", err)), nil
		}
		app.sharedFor(p).Leave(space)
		return mcp.NewToolResultText("ok"), nil
	})

	sharedAdd := mcp.NewTool("shared.add_short_to",
		mcp.WithDescription("Add short-term memory directly to a shared space buffer"),
		mcp.WithString("principal", mcp.Required()),
		mcp.WithString("space", mcp.Required()),
		mcp.WithString("content", mcp.Required()),
		mcp.WithString("metadata_json"),
	)
	s.AddTool(sharedAdd, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := req.RequireString("principal")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing principal: %v", err)), nil
		}
		space, err := req.RequireString("space")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing space: %v", err)), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing content: %v", err)), nil
		}

		meta := map[string]string{}
		metaStr := getStringParam(req, "metadata_json")
		if metaStr != "" {
			if err := json.Unmarshal([]byte(metaStr), &meta); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid metadata_json: %v", err)), nil
			}
		}

		if err := app.sharedFor(p).AddShortTo(space, content, meta); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("ok"), nil
	})

	sharedRetrieve := mcp.NewTool("shared.retrieve",
		mcp.WithDescription("Retrieve merged (local+spaces) or only shared if only_shared=true"),
		mcp.WithString("principal", mcp.Required()),
		mcp.WithString("query", mcp.Required()),
		mcp.WithNumber("limit"),
		mcp.WithString("only_shared"),
	)
	s.AddTool(sharedRetrieve, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := req.RequireString("principal")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing principal: %v", err)), nil
		}
		q, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("missing query: %v", err)), nil
		}

		limit := int(getNumberParam(req, "limit"))
		if limit <= 0 {
			limit = 8
		}

		only := strings.ToLower(getStringParam(req, "only_shared")) == "true"

		var (
			recs []memory.MemoryRecord
			rerr error
		)
		if only {
			recs, rerr = app.sharedFor(p).RetrieveShared(ctx, q, limit)
		} else {
			recs, rerr = app.sharedFor(p).Retrieve(ctx, q, limit)
		}
		if rerr != nil {
			return mcp.NewToolResultError(rerr.Error()), nil
		}
		res, _ := mcp.NewToolResultJSON(recs)
		return res, nil
	})

	metrics := mcp.NewTool("engine.metrics", mcp.WithDescription("Return engine metrics snapshot"))
	s.AddTool(metrics, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, _ := mcp.NewToolResultJSON(app.engine.MetricsSnapshot())
		return res, nil
	})

	switch strings.ToLower(*transport) {
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

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic(fmt.Errorf("missing env %s", k))
	}
	return v
}
func atoi(s string, def int) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return i
}

func parseRole(s string) memory.SpaceRole {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "admin":
		return memory.SpaceRoleAdmin
	case "writer", "write":
		return memory.SpaceRoleWriter
	default:
		return memory.SpaceRoleReader
	}
}

func stringMapToJSON(m map[string]string) string {
	b, _ := json.Marshal(m)
	return string(b)
}
