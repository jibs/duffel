package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"

	"duffel/src/backend/internal/search"
	"duffel/src/backend/internal/storage"
)

type mcpRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      any          `json:"id,omitempty"`
	Result  any          `json:"result,omitempty"`
	Error   *mcpRPCError `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func handleMCP(store *storage.Store, getSearcher func() *search.Searcher, onContentChanged func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("mcp: request method=%s path=%s remote=%s", r.Method, r.URL.Path, r.RemoteAddr)
		if r.Method == http.MethodGet {
			log.Printf("mcp: status probe remote=%s", r.RemoteAddr)
			writeJSON(w, http.StatusOK, map[string]any{
				"status":    "ok",
				"endpoint":  "/mcp",
				"transport": "streamable-http",
			})
			return
		}
		if r.Method != http.MethodPost {
			log.Printf("mcp: method not allowed method=%s remote=%s", r.Method, r.RemoteAddr)
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req mcpRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("mcp: parse error remote=%s err=%v", r.RemoteAddr, err)
			writeMCPError(w, nil, -32700, "parse error")
			return
		}
		if strings.TrimSpace(req.Method) == "" {
			log.Printf("mcp: invalid request missing method remote=%s", r.RemoteAddr)
			writeMCPError(w, decodeMCPID(req.ID), -32600, "invalid request")
			return
		}
		log.Printf("mcp: rpc method=%s remote=%s", req.Method, r.RemoteAddr)

		switch req.Method {
		case "initialize":
			log.Printf("mcp: initialize ok remote=%s", r.RemoteAddr)
			writeMCPResult(w, decodeMCPID(req.ID), map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "duffel",
					"version": "1.0.0",
				},
			})
			return
		case "notifications/initialized":
			log.Printf("mcp: initialized notification remote=%s", r.RemoteAddr)
			w.WriteHeader(http.StatusNoContent)
			return
		case "tools/list":
			log.Printf("mcp: tools/list count=%d remote=%s", len(mcpTools()), r.RemoteAddr)
			writeMCPResult(w, decodeMCPID(req.ID), map[string]any{"tools": mcpTools()})
			return
		case "tools/call":
			var params mcpToolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				log.Printf("mcp: tools/call invalid params remote=%s err=%v", r.RemoteAddr, err)
				writeMCPError(w, decodeMCPID(req.ID), -32602, "invalid params")
				return
			}
			log.Printf("mcp: tools/call name=%s arg_keys=%s remote=%s", params.Name, mcpArgKeys(params.Arguments), r.RemoteAddr)
			result, err := runMCPTool(store, getSearcher, onContentChanged, params)
			if err != nil {
				log.Printf("mcp: tools/call error name=%s remote=%s err=%v", params.Name, r.RemoteAddr, err)
				writeMCPResult(w, decodeMCPID(req.ID), mcpToolError(err.Error()))
				return
			}
			log.Printf("mcp: tools/call ok name=%s remote=%s", params.Name, r.RemoteAddr)
			writeMCPResult(w, decodeMCPID(req.ID), mcpToolSuccess(result))
			return
		default:
			log.Printf("mcp: method not found rpc_method=%s remote=%s", req.Method, r.RemoteAddr)
			writeMCPError(w, decodeMCPID(req.ID), -32601, "method not found")
			return
		}
	}
}

func mcpArgKeys(args map[string]any) string {
	if len(args) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func writeMCPResult(w http.ResponseWriter, id any, result any) {
	writeJSON(w, http.StatusOK, mcpRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeMCPError(w http.ResponseWriter, id any, code int, message string) {
	writeJSON(w, http.StatusOK, mcpRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcpRPCError{Code: code, Message: message},
	})
}

func decodeMCPID(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var id any
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil
	}
	return id
}

func mcpToolSuccess(result any) map[string]any {
	payload, _ := json.Marshal(result)
	return map[string]any{
		"isError": false,
		"content": []map[string]any{
			{"type": "text", "text": string(payload)},
		},
		"structuredContent": mcpStructuredContent(result),
	}
}

func mcpStructuredContent(result any) map[string]any {
	payload, err := json.Marshal(result)
	if err != nil {
		return map[string]any{}
	}

	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return map[string]any{}
	}

	switch v := decoded.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return v
	case []any:
		return map[string]any{
			"results": v,
			"total":   len(v),
		}
	default:
		return map[string]any{"value": v}
	}
}

func mcpToolError(message string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{{"type": "text", "text": message}},
	}
}

func mcpTools() []map[string]any {
	full := []map[string]any{
		toolDef("duffel_list", "List directory contents", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":     map[string]any{"type": "string"},
				"archived": map[string]any{"type": "boolean"},
			},
		}),
		toolDef("duffel_read", "Read file content", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}}, "path")),
		toolDef("duffel_write", "Write file content", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "path", "content")),
		toolDef("duffel_delete", "Delete file or empty directory", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}}, "path")),
		toolDef("duffel_mkdir", "Create directory", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}}, "path")),
		toolDef("duffel_move", "Move file or directory", objectWithRequired(map[string]any{"source": map[string]any{"type": "string"}, "destination": map[string]any{"type": "string"}}, "source", "destination")),
		toolDef("duffel_archive", "Archive file", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}}, "path")),
		toolDef("duffel_unarchive", "Unarchive file", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}}, "path")),
		toolDef("duffel_journal_create", "Create journal", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "path")),
		toolDef("duffel_journal_append", "Append journal entry", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "path", "content")),
		toolDef("duffel_search", "Search notes", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":           map[string]any{"type": "string"},
				"limit":           map[string]any{"type": "integer"},
				"offset":          map[string]any{"type": "integer"},
				"intent":          map[string]any{"type": "string"},
				"candidate_limit": map[string]any{"type": "integer"},
				"min_score":       map[string]any{"type": "number"},
				"explain":         map[string]any{"type": "boolean"},
				"fields":          map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		}),
		toolDef("search", "Deep research alias for note search", objectWithRequired(map[string]any{"query": map[string]any{"type": "string"}}, "query")),
		toolDef("fetch", "Deep research alias for file read", objectWithRequired(map[string]any{"path": map[string]any{"type": "string"}}, "path")),
	}
	return full
}

func toolDef(name, description string, inputSchema map[string]any) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": inputSchema,
	}
}

func objectWithRequired(properties map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func runMCPTool(store *storage.Store, getSearcher func() *search.Searcher, onContentChanged func(), params mcpToolCallParams) (any, error) {
	name := strings.TrimSpace(params.Name)
	args := params.Arguments
	if args == nil {
		args = map[string]any{}
	}

	switch name {
	case "duffel_list":
		path := argString(args, "path", "/")
		archived := argBool(args, "archived", false)
		dir, err := store.List(path, archived)
		if err != nil {
			return nil, mcpStorageError(path, err)
		}
		if archived {
			archivedDir, err := store.ListArchived(path)
			if err == nil && archivedDir != nil && len(archivedDir.Entries) > 0 {
				dir.Entries = append(dir.Entries, archivedDir.Entries...)
			}
		}
		return dir, nil

	case "duffel_read", "fetch":
		path := argString(args, "path", "")
		if path == "" {
			return nil, fmt.Errorf("path is required")
		}
		file, err := store.Read(path)
		if err != nil {
			return nil, mcpStorageError(path, err)
		}
		recommended := recommendForFile(store, getSearcher, file)
		if recommended == nil {
			recommended = []search.Result{}
		}
		return fileGetResponse{
			Path:        file.Path,
			Content:     file.Content,
			Size:        file.Size,
			ModTime:     file.ModTime,
			IsJournal:   file.IsJournal,
			Kind:        file.Kind,
			Recommended: recommended,
		}, nil

	case "duffel_write":
		path := argString(args, "path", "")
		content := argString(args, "content", "")
		if path == "" {
			return nil, fmt.Errorf("path is required")
		}
		if err := store.Write(path, []byte(content)); err != nil {
			return nil, mcpStorageError(path, err)
		}
		triggerContentChanged(onContentChanged)
		file, err := store.Read(path)
		if err != nil {
			return nil, err
		}
		return file, nil

	case "duffel_delete":
		path := argString(args, "path", "")
		if path == "" {
			return nil, fmt.Errorf("path is required")
		}
		if err := store.Delete(path); err != nil {
			return nil, mcpStorageError(path, err)
		}
		triggerContentChanged(onContentChanged)
		return map[string]string{"status": "deleted", "path": path}, nil

	case "duffel_mkdir":
		path := argString(args, "path", "")
		if path == "" {
			return nil, fmt.Errorf("path is required")
		}
		if err := store.CreateDir(path); err != nil {
			return nil, mcpStorageError(path, err)
		}
		return map[string]string{"status": "created", "path": path}, nil

	case "duffel_move":
		src := argString(args, "source", "")
		dst := argString(args, "destination", "")
		if src == "" || dst == "" {
			return nil, fmt.Errorf("source and destination are required")
		}
		if err := store.Move(src, dst); err != nil {
			return nil, mcpStorageError(src, err)
		}
		triggerContentChanged(onContentChanged)
		return map[string]string{"status": "moved", "from": src, "to": dst}, nil

	case "duffel_archive":
		path := argString(args, "path", "")
		if path == "" {
			return nil, fmt.Errorf("path is required")
		}
		if err := store.Archive(path); err != nil {
			return nil, mcpStorageError(path, err)
		}
		triggerContentChanged(onContentChanged)
		return map[string]string{"status": "archived", "path": path}, nil

	case "duffel_unarchive":
		path := argString(args, "path", "")
		if path == "" {
			return nil, fmt.Errorf("path is required")
		}
		if err := store.Unarchive(path); err != nil {
			return nil, mcpStorageError(path, err)
		}
		triggerContentChanged(onContentChanged)
		return map[string]string{"status": "unarchived", "path": path}, nil

	case "duffel_journal_create":
		path := argString(args, "path", "")
		content := argString(args, "content", "")
		if path == "" {
			return nil, fmt.Errorf("path is required")
		}
		if err := store.CreateJournal(path, content); err != nil {
			return nil, mcpStorageError(path, err)
		}
		triggerContentChanged(onContentChanged)
		file, err := store.Read(path)
		if err != nil {
			return nil, err
		}
		return file, nil

	case "duffel_journal_append":
		path := argString(args, "path", "")
		content := argString(args, "content", "")
		if path == "" || content == "" {
			return nil, fmt.Errorf("path and content are required")
		}
		if err := store.AppendJournal(path, content); err != nil {
			return nil, mcpStorageError(path, err)
		}
		triggerContentChanged(onContentChanged)
		file, err := store.Read(path)
		if err != nil {
			return nil, err
		}
		return file, nil

	case "duffel_search", "search":
		query := argString(args, "query", "")
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		searcher := getSearcher()
		if searcher == nil {
			return nil, fmt.Errorf("search is unavailable — qmd has not indexed yet")
		}
		fieldsRaw := argString(args, "fields", "")
		fields, err := parseSearchFields(fieldsRaw)
		if err != nil {
			return nil, err
		}

		opts := search.SearchOptions{
			Query:          query,
			Collection:     "duffel",
			Limit:          argInt(args, "limit", 20),
			Offset:         argInt(args, "offset", 0),
			Intent:         argString(args, "intent", ""),
			CandidateLimit: argInt(args, "candidate_limit", 0),
			MinScore:       argFloat(args, "min_score", 0),
			Explain:        argBool(args, "explain", false),
		}
		if opts.Limit <= 0 {
			opts.Limit = 20
		}
		if opts.Offset < 0 {
			opts.Offset = 0
		}
		if opts.CandidateLimit < 0 {
			opts.CandidateLimit = 0
		}
		if opts.MinScore < 0 {
			opts.MinScore = 0
		}

		results, err := searcher.Search(opts)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}
		results = search.MapPaths(results, store.Root())
		if len(fields) == 0 {
			return results, nil
		}
		return projectSearchResults(results, fields), nil

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func argString(args map[string]any, key string, fallback string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}

func argBool(args map[string]any, key string, fallback bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

func argInt(args map[string]any, key string, fallback int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fallback
		}
		return int(x)
	case int:
		return x
	default:
		return fallback
	}
}

func argFloat(args map[string]any, key string, fallback float64) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fallback
		}
		return x
	case int:
		return float64(x)
	default:
		return fallback
	}
}

func mcpStorageError(path string, err error) error {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return fmt.Errorf("not found: %s", path)
	case errors.Is(err, storage.ErrNotEmpty):
		return fmt.Errorf("directory is not empty: %s", path)
	case errors.Is(err, storage.ErrPathTraversal):
		return fmt.Errorf("path traversal not allowed: %s", path)
	case errors.Is(err, storage.ErrAlreadyExists):
		return fmt.Errorf("already exists: %s", path)
	default:
		return err
	}
}
