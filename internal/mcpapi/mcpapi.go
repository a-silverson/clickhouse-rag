package mcpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/a-silverson/clickhouse-rag/internal/pipeline"
	"github.com/a-silverson/clickhouse-rag/internal/schema"
	"github.com/a-silverson/clickhouse-rag/internal/sqlguard"
	"github.com/a-silverson/clickhouse-rag/internal/store"
)

type Server struct {
	DB    store.Store
	Embed pipeline.Embedder
}

func ServeStdio(in io.Reader, out io.Writer, s Server) error {
	reader := bufio.NewReader(in)
	for {
		length := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				_, _ = fmt.Sscanf(line, "Content-Length: %d", &length)
			}
		}
		if length == 0 {
			return fmt.Errorf("missing Content-Length")
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return err
		}
		var req request
		if err := json.Unmarshal(body, &req); err != nil {
			return err
		}
		payload, err := json.Marshal(s.handle(context.Background(), req))
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(payload), payload); err != nil {
			return err
		}
	}
}

func (s Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeRPC(w, response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}})
			return
		}
		writeRPC(w, s.handle(r.Context(), req))
	})
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s Server) handle(ctx context.Context, req request) response {
	switch req.Method {
	case "initialize":
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "rag-service", "version": "0.1.0"},
		}}
	case "notifications/initialized", "initialized":
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools()}}
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fail(req.ID, err)
		}
		text, err := s.call(ctx, params.Name, params.Arguments)
		if err != nil {
			return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
				"content": []map[string]string{{"type": "text", "text": err.Error()}},
				"isError": true,
			}}
		}
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
		}}
	default:
		return response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}}
	}
}

func (s Server) call(ctx context.Context, name string, args map[string]any) (string, error) {
	switch {
	case name == "search_knowledge_base":
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return "", fmt.Errorf("query is required")
		}
		limit := intArg(args["limit"], 5)
		vectors, err := s.Embed.Embed(ctx, []string{query})
		if err != nil {
			return "", err
		}
		hits, err := s.DB.SearchChunks(ctx, vectors[0], limit)
		if err != nil {
			return "", err
		}
		return encode(hits)
	case name == "search_projects":
		query, _ := args["sql"].(string)
		if err := sqlguard.ValidateReadOnly(query); err != nil {
			return "", err
		}
		rows, err := s.DB.QueryReadOnly(ctx, query)
		if err != nil {
			return "", err
		}
		return encode(rows)
	case strings.HasPrefix(name, "create_"):
		table, err := schema.Lookup(strings.TrimPrefix(name, "create_"))
		if err != nil {
			return "", err
		}
		id := uuid.NewString()
		if raw, ok := args[table.ID].(string); ok && raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				return "", err
			}
			id = parsed.String()
		}
		delete(args, table.ID)
		if err := s.DB.InsertEntity(ctx, table.Name, id, args); err != nil {
			return "", err
		}
		return encode(map[string]string{table.ID: id})
	case strings.HasPrefix(name, "update_"):
		table, err := schema.Lookup(strings.TrimPrefix(name, "update_"))
		if err != nil {
			return "", err
		}
		raw, _ := args[table.ID].(string)
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("%s is required", table.ID)
		}
		delete(args, table.ID)
		if err := s.DB.UpdateEntity(ctx, table.Name, parsed.String(), args); err != nil {
			return "", err
		}
		return encode(map[string]string{table.ID: parsed.String(), "status": "updated"})
	default:
		return "", fmt.Errorf("unknown tool %s", name)
	}
}

func tools() []map[string]any {
	list := []map[string]any{
		tool("search_knowledge_base", "Vector search over uploaded documents.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]string{"type": "string"},
				"limit": map[string]string{"type": "number"},
			},
			"required": []string{"query"},
		}),
		tool("search_projects", "Run one read-only SELECT against the real-estate tables.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sql": map[string]string{"type": "string"},
			},
			"required": []string{"sql"},
		}),
	}
	for _, table := range schema.Tables {
		props := map[string]any{}
		var required []string
		for _, col := range table.Columns {
			props[col.Name] = map[string]string{"type": jsonType(col.Kind)}
			if col.Required {
				required = append(required, col.Name)
			}
		}
		list = append(list, tool("create_"+table.Name, "Insert a "+table.Name+" row.", map[string]any{
			"type": "object", "properties": props, "required": required,
		}))
		updateProps := map[string]any{table.ID: map[string]string{"type": "string"}}
		for name, prop := range props {
			updateProps[name] = prop
		}
		list = append(list, tool("update_"+table.Name, "Update a "+table.Name+" row by inserting a new version.", map[string]any{
			"type": "object", "properties": updateProps, "required": []string{table.ID},
		}))
	}
	return list
}

func tool(name, description string, schema map[string]any) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": schema,
	}
}

func jsonType(kind string) string {
	switch kind {
	case "uint8", "uint64", "float":
		return "number"
	default:
		return "string"
	}
}

func intArg(value any, fallback int) int {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	}
	return fallback
}

func encode(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func fail(id any, err error) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32602, Message: err.Error()}}
}

func writeRPC(w http.ResponseWriter, body response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
