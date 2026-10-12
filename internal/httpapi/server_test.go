package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-silverson/clickhouse-rag/internal/extract"
	"github.com/a-silverson/clickhouse-rag/internal/pipeline"
	"github.com/a-silverson/clickhouse-rag/internal/store"
)

type fakeEmbed struct{}

func (fakeEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if strings.Contains(strings.ToLower(text), "spain") {
			out[i] = []float32{1, 0}
			continue
		}
		out[i] = []float32{0, 1}
	}
	return out, nil
}

type fakeChat struct{ confidence float64 }

func (f fakeChat) Chat(context.Context, string, string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"confidence": f.confidence,
		"rows":       []map[string]string{{"name": "Spain", "code": "ES"}},
	})
	return string(body), nil
}

func newTestServer(confidence float64) (http.Handler, *store.Memory) {
	mem := store.NewMemory()
	pipe := pipeline.Pipeline{
		Store:          mem,
		Embed:          fakeEmbed{},
		Extract:        extract.Extractor{Chat: fakeChat{confidence: confidence}},
		Tables:         []string{"country"},
		MinConfidence:  0.8,
		ChunksPerTable: 2,
		ChunkRunes:     800,
		ChunkOverlap:   50,
	}
	return Server{Store: mem, Pipeline: pipe}.Handler(), mem
}

func upload(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	part, err := form.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	form.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/files", &buf)
	req.Header.Set("Content-Type", form.FormDataContentType())
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func TestUploadAppliesHighConfidence(t *testing.T) {
	handler, mem := newTestServer(0.95)
	res := upload(t, handler, "Spain is a country with code ES.")
	if res.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", res.Code, res.Body.String())
	}
	var uploaded pipeline.Uploaded
	if err := json.Unmarshal(res.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.File.Status != "extracted" || uploaded.Extractions[0].Status != "applied" {
		t.Fatalf("%#v", uploaded)
	}
	pending := httptest.NewRecorder()
	handler.ServeHTTP(pending, httptest.NewRequest(http.MethodGet, "/api/extractions", nil))
	if !strings.Contains(pending.Body.String(), "[]") {
		t.Fatalf("pending %s", pending.Body.String())
	}
	if _, ok := mem.Row("country", ""); ok {
		t.Fatal("empty id should not exist")
	}
	files := httptest.NewRecorder()
	handler.ServeHTTP(files, httptest.NewRequest(http.MethodGet, "/api/files", nil))
	if !strings.Contains(files.Body.String(), "notes.txt") {
		t.Fatalf("files %s", files.Body.String())
	}
}

func TestUploadKeepsLowConfidencePending(t *testing.T) {
	handler, mem := newTestServer(0.2)
	res := upload(t, handler, "Spain is a country with code ES.")
	if res.Code != http.StatusCreated {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	var uploaded pipeline.Uploaded
	if err := json.Unmarshal(res.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Extractions[0].Status != "pending" {
		t.Fatalf("%#v", uploaded.Extractions)
	}
	approve := httptest.NewRecorder()
	handler.ServeHTTP(approve, httptest.NewRequest(http.MethodPost, "/api/extractions/"+uploaded.Extractions[0].ID+"/approve", nil))
	if approve.Code != http.StatusOK {
		t.Fatalf("approve %d %s", approve.Code, approve.Body.String())
	}
	found := false
	// The generated id is inside the store; scan via a second list of countries through search is not on HTTP.
	// Approve must have inserted exactly one country row. QueryReadOnly returns every row of the matched table.
	rows, err := mem.QueryReadOnly(context.Background(), "SELECT name, code FROM rag.country")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["code"] != "ES" {
		t.Fatalf("%#v", rows)
	}
	found = true
	if !found {
		t.Fatal("country was not inserted")
	}
	reject := httptest.NewRecorder()
	handler.ServeHTTP(reject, httptest.NewRequest(http.MethodPost, "/api/extractions/"+uploaded.Extractions[0].ID+"/reject", nil))
	if reject.Code != http.StatusBadRequest {
		t.Fatalf("second decision %d", reject.Code)
	}
}

type countingEmbed struct {
	calls int
}

func (e *countingEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	return fakeEmbed{}.Embed(context.Background(), texts)
}

func TestSameNameAndChecksumSkipsRewrite(t *testing.T) {
	mem := store.NewMemory()
	embedder := &countingEmbed{}
	handler := Server{Store: mem, Pipeline: pipeline.Pipeline{
		Store:          mem,
		Embed:          embedder,
		Extract:        extract.Extractor{Chat: fakeChat{confidence: 0.95}},
		Tables:         []string{"country"},
		MinConfidence:  0.8,
		ChunksPerTable: 2,
		ChunkRunes:     800,
		ChunkOverlap:   50,
	}}.Handler()
	body := "Spain is a country with code ES."
	first := upload(t, handler, body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first %d %s", first.Code, first.Body.String())
	}
	var created pipeline.Uploaded
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	second := upload(t, handler, body)
	if second.Code != http.StatusOK {
		t.Fatalf("second %d %s", second.Code, second.Body.String())
	}
	var again pipeline.Uploaded
	if err := json.Unmarshal(second.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if !again.Unchanged || again.Replaced || again.File.ID != created.File.ID || again.File.Checksum == "" {
		t.Fatalf("%#v", again)
	}
	if embedder.calls != 1 {
		t.Fatalf("embed calls %d", embedder.calls)
	}
	chunks, err := mem.ListChunksByFile(context.Background(), created.File.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks %#v", chunks)
	}
	files, err := mem.ListFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files %#v", files)
	}
}

func TestSameNameNewChecksumRebuildsChunks(t *testing.T) {
	mem := store.NewMemory()
	handler := Server{Store: mem, Pipeline: pipeline.Pipeline{
		Store:          mem,
		Embed:          fakeEmbed{},
		Extract:        extract.Extractor{Chat: fakeChat{confidence: 0.95}},
		Tables:         []string{"country"},
		MinConfidence:  0.8,
		ChunksPerTable: 2,
		ChunkRunes:     800,
		ChunkOverlap:   50,
	}}.Handler()
	first := upload(t, handler, "Spain is a country with code ES.")
	var created pipeline.Uploaded
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	second := upload(t, handler, "Portugal is a country with code PT.")
	if second.Code != http.StatusOK {
		t.Fatalf("second %d %s", second.Code, second.Body.String())
	}
	var replaced pipeline.Uploaded
	if err := json.Unmarshal(second.Body.Bytes(), &replaced); err != nil {
		t.Fatal(err)
	}
	if !replaced.Replaced || replaced.Unchanged || replaced.File.ID != created.File.ID {
		t.Fatalf("%#v", replaced)
	}
	if replaced.File.Checksum == created.File.Checksum {
		t.Fatal("checksum did not change")
	}
	chunks, err := mem.ListChunksByFile(context.Background(), created.File.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || !strings.Contains(chunks[0].Content, "Portugal") || strings.Contains(chunks[0].Content, "Spain") {
		t.Fatalf("chunks %#v", chunks)
	}
	hits, err := mem.SearchChunks(context.Background(), []float32{0, 1}, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		if strings.Contains(hit.Content, "Spain") {
			t.Fatalf("old chunk still searchable: %#v", hit)
		}
	}
}

func TestDeleteFile(t *testing.T) {
	handler, _ := newTestServer(0.95)
	res := upload(t, handler, "Spain is a country with code ES.")
	var uploaded pipeline.Uploaded
	if err := json.Unmarshal(res.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/api/files/"+uploaded.File.ID, nil))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete %d", deleted.Code)
	}
	files := httptest.NewRecorder()
	handler.ServeHTTP(files, httptest.NewRequest(http.MethodGet, "/api/files", nil))
	if strings.Contains(files.Body.String(), uploaded.File.ID) {
		t.Fatalf("deleted file still listed: %s", files.Body.String())
	}
}
