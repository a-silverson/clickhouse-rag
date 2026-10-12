package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/a-silverson/clickhouse-rag/internal/chunk"
	"github.com/a-silverson/clickhouse-rag/internal/extract"
	"github.com/a-silverson/clickhouse-rag/internal/schema"
	"github.com/a-silverson/clickhouse-rag/internal/store"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type Pipeline struct {
	Store          store.Store
	Embed          Embedder
	Extract        extract.Extractor
	Tables         []string
	MinConfidence  float64
	ChunksPerTable int
	ChunkRunes     int
	ChunkOverlap   int
}

type Uploaded struct {
	File        store.File         `json:"file"`
	Extractions []store.Extraction `json:"extractions"`
}

func (p Pipeline) Ingest(ctx context.Context, filename, contentType string, body []byte) (Uploaded, error) {
	now := time.Now().UTC()
	file := store.File{
		ID:          uuid.NewString(),
		Filename:    filename,
		ContentType: contentType,
		Status:      "processing",
		ByteSize:    uint64(len(body)),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := p.Store.InsertFile(ctx, file); err != nil {
		return Uploaded{}, err
	}
	text := chunk.TextFromUpload(filename, contentType, body)
	parts := chunk.Split(text, p.ChunkRunes, p.ChunkOverlap)
	if len(parts) == 0 {
		file.Status = "failed"
		file.UpdatedAt = time.Now().UTC()
		_ = p.Store.MarkFile(ctx, file.ID, file.Status)
		return Uploaded{}, fmt.Errorf("no text could be read from %s", filename)
	}
	vectors, err := p.Embed.Embed(ctx, parts)
	if err != nil {
		_ = p.Store.MarkFile(ctx, file.ID, "failed")
		return Uploaded{}, err
	}
	chunks := make([]store.Chunk, len(parts))
	created := time.Now().UTC()
	for i, part := range parts {
		chunks[i] = store.Chunk{
			FileID:     file.ID,
			ChunkIndex: uint32(i),
			Content:    part,
			Embedding:  vectors[i],
			CreatedAt:  created,
		}
	}
	if err := p.Store.ReplaceChunks(ctx, file.ID, chunks); err != nil {
		_ = p.Store.MarkFile(ctx, file.ID, "failed")
		return Uploaded{}, err
	}
	var saved []store.Extraction
	for _, tableName := range p.Tables {
		if _, err := schema.Lookup(tableName); err != nil {
			_ = p.Store.MarkFile(ctx, file.ID, "failed")
			return Uploaded{}, err
		}
		sample := parts
		if p.ChunksPerTable > 0 && len(sample) > p.ChunksPerTable {
			sample = sample[:p.ChunksPerTable]
		}
		result, err := p.Extract.Extract(ctx, tableName, sample)
		if err != nil {
			_ = p.Store.MarkFile(ctx, file.ID, "failed")
			return Uploaded{}, err
		}
		item, err := p.saveExtraction(ctx, file.ID, tableName, result)
		if err != nil {
			_ = p.Store.MarkFile(ctx, file.ID, "failed")
			return Uploaded{}, err
		}
		saved = append(saved, item)
	}
	if err := p.Store.MarkFile(ctx, file.ID, "extracted"); err != nil {
		return Uploaded{}, err
	}
	file.Status = "extracted"
	file.UpdatedAt = time.Now().UTC()
	return Uploaded{File: file, Extractions: saved}, nil
}

func (p Pipeline) saveExtraction(ctx context.Context, fileID, tableName string, result extract.Result) (store.Extraction, error) {
	payload, err := json.Marshal(result.Rows)
	if err != nil {
		return store.Extraction{}, err
	}
	now := time.Now().UTC()
	status := "pending"
	if result.Confidence >= p.MinConfidence {
		status = "applied"
	}
	item := store.Extraction{
		ID:         uuid.NewString(),
		FileID:     fileID,
		TableName:  tableName,
		Status:     status,
		Payload:    string(payload),
		Confidence: float32(result.Confidence),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if status == "applied" {
		if err := applyRows(ctx, p.Store, tableName, result.Rows); err != nil {
			return store.Extraction{}, err
		}
	}
	if err := p.Store.UpsertExtraction(ctx, item); err != nil {
		return store.Extraction{}, err
	}
	return item, nil
}

func (p Pipeline) Approve(ctx context.Context, id string) (store.Extraction, error) {
	item, err := p.Store.GetExtraction(ctx, id)
	if err != nil {
		return store.Extraction{}, err
	}
	if item.Status != "pending" {
		return store.Extraction{}, fmt.Errorf("extraction is %s", item.Status)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(item.Payload), &rows); err != nil {
		return store.Extraction{}, err
	}
	if err := applyRows(ctx, p.Store, item.TableName, rows); err != nil {
		return store.Extraction{}, err
	}
	item.Status = "approved"
	item.UpdatedAt = time.Now().UTC()
	if err := p.Store.UpsertExtraction(ctx, item); err != nil {
		return store.Extraction{}, err
	}
	return item, nil
}

func (p Pipeline) Reject(ctx context.Context, id string) (store.Extraction, error) {
	item, err := p.Store.GetExtraction(ctx, id)
	if err != nil {
		return store.Extraction{}, err
	}
	if item.Status != "pending" {
		return store.Extraction{}, fmt.Errorf("extraction is %s", item.Status)
	}
	item.Status = "rejected"
	item.UpdatedAt = time.Now().UTC()
	if err := p.Store.UpsertExtraction(ctx, item); err != nil {
		return store.Extraction{}, err
	}
	return item, nil
}

func applyRows(ctx context.Context, db store.Store, tableName string, rows []map[string]any) error {
	table, err := schema.Lookup(tableName)
	if err != nil {
		return err
	}
	for _, row := range rows {
		id := uuid.NewString()
		if raw, ok := row[table.ID].(string); ok && raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", table.ID, err)
			}
			id = parsed.String()
		}
		delete(row, table.ID)
		if err := db.InsertEntity(ctx, tableName, id, row); err != nil {
			return err
		}
	}
	return nil
}
