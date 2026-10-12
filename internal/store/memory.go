package store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a-silverson/clickhouse-rag/internal/schema"
	"github.com/a-silverson/clickhouse-rag/internal/sqlguard"
)

type Memory struct {
	mu          sync.Mutex
	files       map[string]File
	deleted     map[string]bool
	chunks      map[string][]Chunk
	extractions map[string]Extraction
	rows        map[string]map[string]map[string]any
}

func NewMemory() *Memory {
	return &Memory{
		files:       map[string]File{},
		deleted:     map[string]bool{},
		chunks:      map[string][]Chunk{},
		extractions: map[string]Extraction{},
		rows:        map[string]map[string]map[string]any{},
	}
}

func (m *Memory) InsertFile(_ context.Context, file File) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[file.ID] = file
	delete(m.deleted, file.ID)
	return nil
}

func (m *Memory) ListFiles(_ context.Context) ([]File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []File
	for id, file := range m.files {
		if m.deleted[id] {
			continue
		}
		out = append(out, file)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) GetFile(_ context.Context, id string) (File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, ok := m.files[id]
	if !ok || m.deleted[id] {
		return File{}, ErrNotFound
	}
	return file, nil
}

func (m *Memory) FindByFilename(_ context.Context, filename string) (File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var found File
	var ok bool
	for id, file := range m.files {
		if m.deleted[id] || file.Filename != filename {
			continue
		}
		if !ok || file.UpdatedAt.After(found.UpdatedAt) {
			found = file
			ok = true
		}
	}
	if !ok {
		return File{}, ErrNotFound
	}
	return found, nil
}

func (m *Memory) MarkFile(_ context.Context, id, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, ok := m.files[id]
	if !ok || m.deleted[id] {
		return ErrNotFound
	}
	file.Status = status
	file.UpdatedAt = time.Now().UTC()
	m.files[id] = file
	return nil
}

func (m *Memory) DeleteFile(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[id]; !ok || m.deleted[id] {
		return ErrNotFound
	}
	m.deleted[id] = true
	return nil
}

func (m *Memory) DeleteChunks(_ context.Context, fileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.chunks, fileID)
	return nil
}

func (m *Memory) ReplaceChunks(_ context.Context, fileID string, chunks []Chunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := append([]Chunk(nil), chunks...)
	m.chunks[fileID] = copied
	return nil
}

func (m *Memory) ListChunksByFile(_ context.Context, fileID string, limit int) ([]Chunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	chunks := m.chunks[fileID]
	if limit > 0 && len(chunks) > limit {
		chunks = chunks[:limit]
	}
	return append([]Chunk(nil), chunks...), nil
}

func (m *Memory) SearchChunks(_ context.Context, embedding []float32, limit int) ([]Hit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit < 1 {
		limit = 5
	}
	var hits []Hit
	for fileID, chunks := range m.chunks {
		if m.deleted[fileID] {
			continue
		}
		for _, chunk := range chunks {
			hits = append(hits, Hit{
				FileID:     fileID,
				ChunkIndex: chunk.ChunkIndex,
				Content:    chunk.Content,
				Distance:   cosineDistance(embedding, chunk.Embedding),
			})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Distance < hits[j].Distance })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (m *Memory) UpsertExtraction(_ context.Context, item Extraction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extractions[item.ID] = item
	return nil
}

func (m *Memory) ListPendingExtractions(_ context.Context) ([]Extraction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Extraction
	for _, item := range m.extractions {
		if item.Status == "pending" {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) GetExtraction(_ context.Context, id string) (Extraction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.extractions[id]
	if !ok {
		return Extraction{}, ErrNotFound
	}
	return item, nil
}

func (m *Memory) InsertEntity(_ context.Context, table, id string, values map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeEntity(table, id, values, false)
}

func (m *Memory) UpdateEntity(_ context.Context, table, id string, values map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeEntity(table, id, values, true)
}

func (m *Memory) writeEntity(table, id string, values map[string]any, update bool) error {
	tableSpec, err := schema.Lookup(table)
	if err != nil {
		return err
	}
	if m.rows[table] == nil {
		m.rows[table] = map[string]map[string]any{}
	}
	current, exists := m.rows[table][id]
	if update && !exists {
		return ErrNotFound
	}
	if !update && exists {
		return fmt.Errorf("entity %s already exists", id)
	}
	next := map[string]any{}
	if update {
		for k, v := range current {
			next[k] = v
		}
	}
	next[tableSpec.ID] = id
	for _, col := range tableSpec.Columns {
		if v, ok := values[col.Name]; ok {
			next[col.Name] = v
		} else if col.Required && !update {
			return fmt.Errorf("missing %s", col.Name)
		}
	}
	now := time.Now().UTC()
	if _, ok := next["created_at"]; !ok {
		next["created_at"] = now
	}
	next["updated_at"] = now
	m.rows[table][id] = next
	return nil
}

func (m *Memory) QueryReadOnly(_ context.Context, query string) ([]map[string]any, error) {
	if err := sqlguard.ValidateReadOnly(query); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	upper := strings.ToUpper(query)
	for _, table := range schema.Tables {
		if strings.Contains(upper, strings.ToUpper(table.Name)) {
			var out []map[string]any
			for _, row := range m.rows[table.Name] {
				copied := map[string]any{}
				for k, v := range row {
					copied[k] = v
				}
				out = append(out, copied)
			}
			return out, nil
		}
	}
	return []map[string]any{}, nil
}

func (m *Memory) Row(table, id string) (map[string]any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[table][id]
	return row, ok
}

func cosineDistance(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 2
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 1
	}
	sim := dot / (math.Sqrt(na) * math.Sqrt(nb))
	return 1 - sim
}
