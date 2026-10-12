package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/a-silverson/clickhouse-rag/internal/migrate"
	"github.com/a-silverson/clickhouse-rag/internal/schema"
	"github.com/a-silverson/clickhouse-rag/internal/sqlguard"
)

type ClickHouse struct {
	admin  clickhouse.Conn
	reader clickhouse.Conn
	writer clickhouse.Conn
}

type Credentials struct {
	Addr     string
	User     string
	Password string
}

func Open(admin Credentials, reader *Credentials, writer *Credentials) (*ClickHouse, error) {
	adminConn, err := connect(admin)
	if err != nil {
		return nil, err
	}
	ch := &ClickHouse{admin: adminConn, reader: adminConn, writer: adminConn}
	if reader != nil {
		conn, err := connect(*reader)
		if err != nil {
			_ = adminConn.Close()
			return nil, err
		}
		ch.reader = conn
	}
	if writer != nil {
		conn, err := connect(*writer)
		if err != nil {
			_ = adminConn.Close()
			return nil, err
		}
		ch.writer = conn
	}
	return ch, nil
}

func connect(c Credentials) (clickhouse.Conn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{c.Addr},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: c.User,
			Password: c.Password,
		},
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *ClickHouse) Close() error {
	if c.reader != c.admin {
		_ = c.reader.Close()
	}
	if c.writer != c.admin && c.writer != c.reader {
		_ = c.writer.Close()
	}
	return c.admin.Close()
}

func (c *ClickHouse) Migrate(ctx context.Context) error {
	return migrate.Apply(ctx, c.admin)
}

func (c *ClickHouse) InsertFile(ctx context.Context, file File) error {
	return c.insertFileVersion(ctx, file, 0)
}

func (c *ClickHouse) insertFileVersion(ctx context.Context, file File, deleted uint8) error {
	id, err := uuid.Parse(file.ID)
	if err != nil {
		return err
	}
	batch, err := c.admin.PrepareBatch(ctx, `INSERT INTO rag.files
		(file_id, filename, content_type, status, byte_size, deleted, created_at, updated_at)`)
	if err != nil {
		return err
	}
	if err := batch.Append(id, file.Filename, file.ContentType, file.Status, file.ByteSize, deleted, file.CreatedAt, file.UpdatedAt); err != nil {
		return err
	}
	return batch.Send()
}

func (c *ClickHouse) ListFiles(ctx context.Context) ([]File, error) {
	rows, err := c.admin.Query(ctx, `
		SELECT
			file_id,
			argMax(filename, updated_at),
			argMax(content_type, updated_at),
			argMax(status, updated_at),
			argMax(byte_size, updated_at),
			argMax(deleted, updated_at),
			min(created_at),
			max(updated_at)
		FROM rag.files
		GROUP BY file_id
		HAVING argMax(deleted, updated_at) = 0
		ORDER BY max(updated_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		file, _, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

func (c *ClickHouse) GetFile(ctx context.Context, id string) (File, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return File{}, err
	}
	rows, err := c.admin.Query(ctx, `
		SELECT
			file_id,
			argMax(filename, updated_at),
			argMax(content_type, updated_at),
			argMax(status, updated_at),
			argMax(byte_size, updated_at),
			argMax(deleted, updated_at),
			min(created_at),
			max(updated_at)
		FROM rag.files
		WHERE file_id = ?
		GROUP BY file_id`, parsed)
	if err != nil {
		return File{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return File{}, ErrNotFound
	}
	file, deleted, err := scanFile(rows)
	if err != nil {
		return File{}, err
	}
	if deleted != 0 {
		return File{}, ErrNotFound
	}
	return file, rows.Err()
}

func scanFile(rows interface{ Scan(dest ...any) error }) (File, uint8, error) {
	var (
		file    File
		id      uuid.UUID
		deleted uint8
	)
	if err := rows.Scan(&id, &file.Filename, &file.ContentType, &file.Status, &file.ByteSize, &deleted, &file.CreatedAt, &file.UpdatedAt); err != nil {
		return File{}, 0, err
	}
	file.ID = id.String()
	return file, deleted, nil
}

func (c *ClickHouse) MarkFile(ctx context.Context, id, status string) error {
	file, err := c.GetFile(ctx, id)
	if err != nil {
		return err
	}
	file.Status = status
	file.UpdatedAt = time.Now().UTC()
	return c.insertFileVersion(ctx, file, 0)
}

func (c *ClickHouse) DeleteFile(ctx context.Context, id string) error {
	file, err := c.GetFile(ctx, id)
	if err != nil {
		return err
	}
	file.UpdatedAt = time.Now().UTC()
	return c.insertFileVersion(ctx, file, 1)
}

func (c *ClickHouse) ReplaceChunks(ctx context.Context, fileID string, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	batch, err := c.admin.PrepareBatch(ctx, `INSERT INTO rag.chunks
		(file_id, chunk_index, content, embedding, created_at)`)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		id, err := uuid.Parse(chunk.FileID)
		if err != nil {
			return err
		}
		if err := batch.Append(id, chunk.ChunkIndex, chunk.Content, chunk.Embedding, chunk.CreatedAt); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (c *ClickHouse) ListChunksByFile(ctx context.Context, fileID string, limit int) ([]Chunk, error) {
	id, err := uuid.Parse(fileID)
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 3
	}
	rows, err := c.admin.Query(ctx, `
		SELECT file_id, chunk_index, argMax(content, created_at), argMax(embedding, created_at), max(created_at)
		FROM rag.chunks
		WHERE file_id = ?
		GROUP BY file_id, chunk_index
		ORDER BY chunk_index
		LIMIT ?`, id, uint32(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var (
			chunk Chunk
			fid   uuid.UUID
		)
		if err := rows.Scan(&fid, &chunk.ChunkIndex, &chunk.Content, &chunk.Embedding, &chunk.CreatedAt); err != nil {
			return nil, err
		}
		chunk.FileID = fid.String()
		out = append(out, chunk)
	}
	return out, rows.Err()
}

func (c *ClickHouse) SearchChunks(ctx context.Context, embedding []float32, limit int) ([]Hit, error) {
	if limit < 1 {
		limit = 5
	}
	rows, err := c.reader.Query(ctx, `
		SELECT file_id, chunk_index, content, cosineDistance(embedding, ?) AS distance
		FROM rag.chunks
		WHERE (file_id, chunk_index, created_at) IN (
			SELECT file_id, chunk_index, max(created_at)
			FROM rag.chunks
			GROUP BY file_id, chunk_index
		)
		AND file_id IN (
			SELECT file_id FROM rag.files GROUP BY file_id HAVING argMax(deleted, updated_at) = 0
		)
		ORDER BY distance ASC
		LIMIT ?`, embedding, uint32(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var (
			hit      Hit
			id       uuid.UUID
			distance float32
		)
		if err := rows.Scan(&id, &hit.ChunkIndex, &hit.Content, &distance); err != nil {
			return nil, err
		}
		hit.FileID = id.String()
		hit.Distance = float64(distance)
		out = append(out, hit)
	}
	return out, rows.Err()
}

func (c *ClickHouse) UpsertExtraction(ctx context.Context, item Extraction) error {
	id, err := uuid.Parse(item.ID)
	if err != nil {
		return err
	}
	fileID, err := uuid.Parse(item.FileID)
	if err != nil {
		return err
	}
	batch, err := c.admin.PrepareBatch(ctx, `INSERT INTO rag.extractions
		(extraction_id, file_id, table_name, status, payload, confidence, created_at, updated_at)`)
	if err != nil {
		return err
	}
	if err := batch.Append(id, fileID, item.TableName, item.Status, item.Payload, item.Confidence, item.CreatedAt, item.UpdatedAt); err != nil {
		return err
	}
	return batch.Send()
}

func (c *ClickHouse) ListPendingExtractions(ctx context.Context) ([]Extraction, error) {
	rows, err := c.admin.Query(ctx, `
		SELECT
			extraction_id,
			argMax(file_id, updated_at),
			argMax(table_name, updated_at),
			argMax(status, updated_at) AS status,
			argMax(payload, updated_at),
			argMax(confidence, updated_at),
			min(created_at),
			max(updated_at)
		FROM rag.extractions
		GROUP BY extraction_id
		HAVING status = 'pending'
		ORDER BY min(created_at)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExtractions(rows)
}

func (c *ClickHouse) GetExtraction(ctx context.Context, id string) (Extraction, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Extraction{}, err
	}
	rows, err := c.admin.Query(ctx, `
		SELECT
			extraction_id,
			argMax(file_id, updated_at),
			argMax(table_name, updated_at),
			argMax(status, updated_at),
			argMax(payload, updated_at),
			argMax(confidence, updated_at),
			min(created_at),
			max(updated_at)
		FROM rag.extractions
		WHERE extraction_id = ?
		GROUP BY extraction_id`, parsed)
	if err != nil {
		return Extraction{}, err
	}
	defer rows.Close()
	items, err := scanExtractions(rows)
	if err != nil {
		return Extraction{}, err
	}
	if len(items) == 0 {
		return Extraction{}, ErrNotFound
	}
	return items[0], nil
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanExtractions(rows rowScanner) ([]Extraction, error) {
	var out []Extraction
	for rows.Next() {
		var (
			item   Extraction
			id     uuid.UUID
			fileID uuid.UUID
		)
		if err := rows.Scan(&id, &fileID, &item.TableName, &item.Status, &item.Payload, &item.Confidence, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ID = id.String()
		item.FileID = fileID.String()
		out = append(out, item)
	}
	return out, rows.Err()
}

func (c *ClickHouse) InsertEntity(ctx context.Context, table, id string, values map[string]any) error {
	return c.writeEntity(ctx, table, id, values, false)
}

func (c *ClickHouse) UpdateEntity(ctx context.Context, table, id string, values map[string]any) error {
	return c.writeEntity(ctx, table, id, values, true)
}

func (c *ClickHouse) writeEntity(ctx context.Context, tableName, id string, values map[string]any, update bool) error {
	table, err := schema.Lookup(tableName)
	if err != nil {
		return err
	}
	if err := sqlguard.CheckIdent(table.Name); err != nil {
		return err
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	created := now
	merged := map[string]any{}
	if update {
		current, err := c.readEntity(ctx, table, parsed)
		if err != nil {
			return err
		}
		for k, v := range current {
			merged[k] = v
		}
		if ts, ok := current["created_at"].(time.Time); ok {
			created = ts
		}
	}
	for k, v := range values {
		merged[k] = v
	}
	cols := []string{table.ID}
	args := []any{parsed}
	for _, col := range table.Columns {
		value, ok := merged[col.Name]
		if !ok || value == nil {
			if col.Required && !update {
				return fmt.Errorf("missing %s", col.Name)
			}
			value = zeroValue(col)
		}
		coerced, err := coerce(col, value)
		if err != nil {
			return fmt.Errorf("%s: %w", col.Name, err)
		}
		cols = append(cols, col.Name)
		args = append(args, coerced)
	}
	cols = append(cols, "created_at", "updated_at")
	args = append(args, created, now)
	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	query := fmt.Sprintf("INSERT INTO rag.%s (%s) VALUES (%s)", table.Name, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	batch, err := c.writer.PrepareBatch(ctx, query)
	if err != nil {
		return err
	}
	if err := batch.Append(args...); err != nil {
		return err
	}
	return batch.Send()
}

func (c *ClickHouse) readEntity(ctx context.Context, table schema.Table, id uuid.UUID) (map[string]any, error) {
	selects := []string{table.ID}
	for _, col := range table.Columns {
		selects = append(selects, fmt.Sprintf("argMax(%s, updated_at)", col.Name))
	}
	selects = append(selects, "min(created_at)", "max(updated_at)")
	query := fmt.Sprintf("SELECT %s FROM rag.%s WHERE %s = ? GROUP BY %s", strings.Join(selects, ", "), table.Name, table.ID, table.ID)
	rows, err := c.writer.Query(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	holders := make([]any, 1+len(table.Columns)+2)
	var idOut uuid.UUID
	holders[0] = &idOut
	for i, col := range table.Columns {
		holders[i+1] = newScanValue(col.Kind)
	}
	var created, updated time.Time
	holders[len(holders)-2] = &created
	holders[len(holders)-1] = &updated
	if err := rows.Scan(holders...); err != nil {
		return nil, err
	}
	out := map[string]any{
		table.ID:     idOut.String(),
		"created_at": created,
		"updated_at": updated,
	}
	for i, col := range table.Columns {
		out[col.Name] = unwrap(holders[i+1])
	}
	return out, rows.Err()
}

func newScanValue(kind string) any {
	switch kind {
	case "uuid":
		return new(uuid.UUID)
	case "uint8":
		return new(uint8)
	case "uint64":
		return new(uint64)
	case "float":
		return new(float32)
	case "time":
		return new(time.Time)
	default:
		return new(string)
	}
}

func (c *ClickHouse) QueryReadOnly(ctx context.Context, query string) ([]map[string]any, error) {
	if err := sqlguard.ValidateReadOnly(query); err != nil {
		return nil, err
	}
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_result_rows":      uint64(100),
		"result_overflow_mode": "break",
	}))
	rows, err := c.reader.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := rows.ColumnTypes()
	names := rows.Columns()
	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		for i, col := range cols {
			values[i] = newScanValue(scanKind(col.DatabaseTypeName()))
		}
		if err := rows.Scan(values...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(names))
		for i, name := range names {
			row[name] = unwrap(values[i])
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func scanKind(dbType string) string {
	switch {
	case strings.Contains(dbType, "UUID"):
		return "uuid"
	case strings.Contains(dbType, "Float"):
		return "float"
	case dbType == "UInt8" || dbType == "Bool":
		return "uint8"
	case strings.HasPrefix(dbType, "UInt") || strings.HasPrefix(dbType, "Int"):
		return "uint64"
	case strings.HasPrefix(dbType, "DateTime"):
		return "time"
	default:
		return "string"
	}
}

func zeroValue(col schema.Column) any {
	switch col.Kind {
	case "string":
		return ""
	case "uuid":
		return uuid.Nil.String()
	case "uint8", "uint64":
		return 0
	case "float":
		return float64(0)
	default:
		return ""
	}
}

func coerce(col schema.Column, value any) (any, error) {
	switch col.Kind {
	case "string":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string")
		}
		return text, nil
	case "uuid":
		switch v := value.(type) {
		case string:
			return uuid.Parse(v)
		case uuid.UUID:
			return v, nil
		default:
			return nil, fmt.Errorf("expected uuid")
		}
	case "uint8":
		n, err := asUint64(value)
		if err != nil {
			return nil, err
		}
		if n > 255 {
			return nil, fmt.Errorf("out of range")
		}
		return uint8(n), nil
	case "uint64":
		return asUint64(value)
	case "float":
		switch v := value.(type) {
		case float64:
			return float32(v), nil
		case float32:
			return v, nil
		case int:
			return float32(v), nil
		default:
			return nil, fmt.Errorf("expected number")
		}
	default:
		return nil, fmt.Errorf("unsupported kind %s", col.Kind)
	}
}

func asUint64(value any) (uint64, error) {
	switch v := value.(type) {
	case float64:
		if v < 0 {
			return 0, fmt.Errorf("negative")
		}
		return uint64(v), nil
	case int:
		if v < 0 {
			return 0, fmt.Errorf("negative")
		}
		return uint64(v), nil
	case uint64:
		return v, nil
	case uint8:
		return uint64(v), nil
	default:
		return 0, fmt.Errorf("expected number")
	}
}

func unwrap(value any) any {
	switch v := value.(type) {
	case *string:
		if v == nil {
			return ""
		}
		return *v
	case *uuid.UUID:
		if v == nil {
			return ""
		}
		return v.String()
	case uuid.UUID:
		return v.String()
	case *uint8:
		if v == nil {
			return uint8(0)
		}
		return *v
	case *uint64:
		if v == nil {
			return uint64(0)
		}
		return *v
	case *float32:
		if v == nil {
			return float32(0)
		}
		return *v
	case *time.Time:
		if v == nil {
			return time.Time{}
		}
		return *v
	default:
		return v
	}
}
