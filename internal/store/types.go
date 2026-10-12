package store

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type File struct {
	ID          string    `json:"file_id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Status      string    `json:"status"`
	ByteSize    uint64    `json:"byte_size"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Chunk struct {
	FileID     string
	ChunkIndex uint32
	Content    string
	Embedding  []float32
	CreatedAt  time.Time
}

type Hit struct {
	FileID     string  `json:"file_id"`
	ChunkIndex uint32  `json:"chunk_index"`
	Content    string  `json:"content"`
	Distance   float64 `json:"distance"`
}

type Extraction struct {
	ID         string    `json:"id"`
	FileID     string    `json:"file_id"`
	TableName  string    `json:"table_name"`
	Status     string    `json:"status"`
	Payload    string    `json:"payload"`
	Confidence float32   `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Store interface {
	InsertFile(ctx context.Context, file File) error
	ListFiles(ctx context.Context) ([]File, error)
	GetFile(ctx context.Context, id string) (File, error)
	MarkFile(ctx context.Context, id, status string) error
	DeleteFile(ctx context.Context, id string) error

	ReplaceChunks(ctx context.Context, fileID string, chunks []Chunk) error
	ListChunksByFile(ctx context.Context, fileID string, limit int) ([]Chunk, error)
	SearchChunks(ctx context.Context, embedding []float32, limit int) ([]Hit, error)

	UpsertExtraction(ctx context.Context, item Extraction) error
	ListPendingExtractions(ctx context.Context) ([]Extraction, error)
	GetExtraction(ctx context.Context, id string) (Extraction, error)

	InsertEntity(ctx context.Context, table string, id string, values map[string]any) error
	UpdateEntity(ctx context.Context, table string, id string, values map[string]any) error
	QueryReadOnly(ctx context.Context, query string) ([]map[string]any, error)
}
