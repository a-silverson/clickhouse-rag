package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-silverson/clickhouse-rag/internal/store"
)

type embedder struct{}

func (embedder) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 0}}, nil
}

func TestSearchAndGuardedSQL(t *testing.T) {
	mem := store.NewMemory()
	if err := mem.ReplaceChunks(context.Background(), "file-1", []store.Chunk{{
		FileID: "file-1", ChunkIndex: 0, Content: "coastal project", Embedding: []float32{1, 0},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := mem.InsertFile(context.Background(), store.File{ID: "file-1", Filename: "a.txt", Status: "extracted"}); err != nil {
		t.Fatal(err)
	}
	handler := Server{DB: mem, Embed: embedder{}}.Handler()

	search := call(t, handler, "tools/call", map[string]any{
		"name":      "search_knowledge_base",
		"arguments": map[string]any{"query": "coast", "limit": 1},
	})
	if !strings.Contains(search, "coastal project") {
		t.Fatalf("search %s", search)
	}

	denied := call(t, handler, "tools/call", map[string]any{
		"name":      "search_projects",
		"arguments": map[string]any{"sql": "DELETE FROM rag.microservice"},
	})
	if !strings.Contains(denied, "isError") && !strings.Contains(denied, "not allowed") && !strings.Contains(denied, "only SELECT") {
		t.Fatalf("delete was not blocked: %s", denied)
	}

	created := call(t, handler, "tools/call", map[string]any{
		"name": "create_microservice",
		"arguments": map[string]any{
			"name":        "billing",
			"description": "charges customers",
		},
	})
	if !strings.Contains(created, "microservice_id") {
		t.Fatalf("create %s", created)
	}
	rows, err := mem.QueryReadOnly(context.Background(), "SELECT name FROM microservice")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %#v err %v", rows, err)
	}
}

func TestToolsListIncludesScopedWrites(t *testing.T) {
	handler := Server{DB: store.NewMemory(), Embed: embedder{}}.Handler()
	body := call(t, handler, "tools/list", map[string]any{})
	for _, name := range []string{"search_knowledge_base", "search_projects", "create_api", "update_pulsar_topic"} {
		if !strings.Contains(body, name) {
			t.Fatalf("missing %s in %s", name, body)
		}
	}
}

func call(t *testing.T, handler http.Handler, method string, params any) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(payload))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res.Body.String()
}
