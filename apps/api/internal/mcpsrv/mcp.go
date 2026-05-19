// Package mcpsrv exposes our REST surface as an MCP server.
//
// Transport: JSON-RPC 2.0 over HTTP POST and Server-Sent Events for streaming.
// Tools: search_notes, get_note, list_notes, capture_note. The tool handlers
// reuse the same vault + qdrant + embed clients as the REST handlers — same
// safety boundaries, same error semantics.
package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charlunn/knowlib/api/internal/handlers"
)

// MCP / JSON-RPC envelope =========================================================

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// === MCP capabilities ============================================================

const protocolVersion = "2024-11-05"

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func toolList() []Tool {
	return []Tool{
		{
			Name:        "search_notes",
			Description: "Semantic search over the user's knowLib. Returns top-K notes ranked by similarity, even when wording differs from note content.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"k":     map[string]any{"type": "integer", "default": 8},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_note",
			Description: "Fetch the full body + frontmatter of a single note by vault-relative path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_notes",
			Description: "List notes under a category prefix.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "capture_note",
			Description: "Quickly capture a note into the user's inbox. Use when the user says 'remember this' or 'save this'.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
					"source":  map[string]any{"type": "string", "default": "mcp"},
				},
				"required": []string{"content"},
			},
		},
	}
}

// === Handler =====================================================================

type Server struct {
	Deps *handlers.Deps
}

func New(d *handlers.Deps) *Server { return &Server{Deps: d} }

// ServeHTTP handles both JSON-RPC over HTTP POST and SSE for streaming responses.
// We keep the implementation small; clients that prefer streaming can GET to
// receive heartbeat events while POSTing requests separately.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.serveSSE(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	resp := s.handleOne(r.Context(), body)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// Periodic keep-alives so proxies don't drop the connection. Phase 1 doesn't
	// stream multi-event tool results — clients POST for those.
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	fmt.Fprintf(w, "event: ready\ndata: {\"protocolVersion\":\"%s\"}\n\n", protocolVersion)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleOne(ctx context.Context, raw []byte) rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "knowlib", "version": "0.1.0"},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolList()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params"}
			break
		}
		out, err := s.callTool(ctx, p.Name, p.Arguments)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
			break
		}
		resp.Result = map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": out},
			},
		}
	case "ping":
		resp.Result = map[string]any{}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

// callTool dispatches to handler logic, but reuses the vault/qdrant/embed
// clients directly — we don't want to round-trip through HTTP for in-process
// MCP calls.
func (s *Server) callTool(ctx context.Context, name string, argRaw json.RawMessage) (string, error) {
	d := s.Deps
	switch name {
	case "search_notes":
		var a struct {
			Query string `json:"query"`
			K     int    `json:"k"`
		}
		_ = json.Unmarshal(argRaw, &a)
		if strings.TrimSpace(a.Query) == "" {
			return "", fmt.Errorf("query is required")
		}
		if a.K <= 0 {
			a.K = 8
		}
		vec, err := d.Embed.Embed(ctx, a.Query)
		if err != nil {
			return "", err
		}
		hits, err := d.Qdrant.Search(ctx, vec, a.K)
		if err != nil {
			return "", err
		}
		out, _ := json.MarshalIndent(hits, "", "  ")
		return string(out), nil

	case "get_note":
		var a struct{ Path string `json:"path"` }
		_ = json.Unmarshal(argRaw, &a)
		body, err := d.Vault.Read(a.Path)
		if err != nil {
			return "", err
		}
		return string(body), nil

	case "list_notes":
		var a struct{ Category string `json:"category"` }
		_ = json.Unmarshal(argRaw, &a)
		base := d.Cfg.NotesDir
		if c := strings.Trim(a.Category, "/"); c != "" {
			base = base + "/" + c
		}
		listing, err := d.Vault.ListSubtree(base)
		if err != nil {
			return "", err
		}
		out, _ := json.MarshalIndent(listing, "", "  ")
		return string(out), nil

	case "capture_note":
		var a struct {
			Content string `json:"content"`
			Source  string `json:"source"`
		}
		_ = json.Unmarshal(argRaw, &a)
		if strings.TrimSpace(a.Content) == "" {
			return "", fmt.Errorf("content is required")
		}
		if a.Source == "" {
			a.Source = "mcp"
		}
		stamp := time.Now().UTC().Format("20060102T150405Z")
		rel := fmt.Sprintf("%s/%s-mcp.md", d.Cfg.InboxDir, stamp)
		body := fmt.Sprintf("---\ncaptured_at: %s\nsource: %s\n---\n\n%s\n",
			time.Now().UTC().Format(time.RFC3339), a.Source, a.Content)
		if err := d.Vault.WriteAtomic(rel, []byte(body)); err != nil {
			return "", err
		}
		return fmt.Sprintf("captured: %s", rel), nil
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}
