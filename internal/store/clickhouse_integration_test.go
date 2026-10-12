package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClickHouseRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := Open(Credentials{Addr: "127.0.0.1:9000", User: "default"}, nil, nil)
	if err != nil {
		t.Skipf("clickhouse is not available: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	fileID := uuid.NewString()
	now := time.Now().UTC()
	file := File{
		ID: fileID, Filename: "integration.txt", ContentType: "text/plain",
		Status: "extracted", ByteSize: 12, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertFile(ctx, file); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceChunks(ctx, fileID, []Chunk{
		{FileID: fileID, ChunkIndex: 0, Content: "near", Embedding: []float32{1, 0, 0}, CreatedAt: now},
		{FileID: fileID, ChunkIndex: 1, Content: "far", Embedding: []float32{0, 1, 0}, CreatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := db.SearchChunks(ctx, []float32{1, 0, 0}, 50)
	if err != nil {
		t.Fatal(err)
	}
	var sawNear bool
	for _, hit := range hits {
		if hit.FileID == fileID && hit.Content == "near" && hit.Distance == 0 {
			sawNear = true
		}
	}
	if !sawNear {
		t.Fatalf("hits %#v", hits)
	}
	if err := db.DeleteFile(ctx, fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetFile(ctx, fileID); err != ErrNotFound {
		t.Fatalf("deleted file err %v", err)
	}
	hits, err = db.SearchChunks(ctx, []float32{1, 0, 0}, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		if hit.FileID == fileID {
			t.Fatalf("deleted file still searchable: %#v", hit)
		}
	}

	countryID := uuid.NewString()
	if err := db.InsertEntity(ctx, "country", countryID, map[string]any{"name": "Spain", "code": "ES"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateEntity(ctx, "country", countryID, map[string]any{"name": "Espana"}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryReadOnly(ctx, "SELECT name, code FROM rag.country WHERE country_id = '"+countryID+"' ORDER BY updated_at DESC LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["name"] != "Espana" || rows[0]["code"] != "ES" {
		t.Fatalf("country rows %#v", rows)
	}
	if _, err := db.QueryReadOnly(ctx, "DROP TABLE rag.country"); err == nil {
		t.Fatal("writer SQL was accepted on the reader path")
	}

	liveID := uuid.NewString()
	live := File{
		ID: liveID, Filename: "checksum.txt", ContentType: "text/plain",
		Status: "extracted", ByteSize: 4, Checksum: "abc", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertFile(ctx, live); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceChunks(ctx, liveID, []Chunk{
		{FileID: liveID, ChunkIndex: 0, Content: "old-spain", Embedding: []float32{1, 0, 0}, CreatedAt: now},
		{FileID: liveID, ChunkIndex: 1, Content: "old-extra", Embedding: []float32{0, 0, 1}, CreatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	byName, err := db.FindByFilename(ctx, "checksum.txt")
	if err != nil || byName.ID != liveID || byName.Checksum != "abc" {
		t.Fatalf("find %#v %v", byName, err)
	}
	if err := db.DeleteChunks(ctx, liveID); err != nil {
		t.Fatal(err)
	}
	listed, err := db.ListChunksByFile(ctx, liveID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("tombstoned chunks %#v", listed)
	}
	hits, err = db.SearchChunks(ctx, []float32{1, 0, 0}, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		if hit.FileID == liveID {
			t.Fatalf("deleted chunk still searchable: %#v", hit)
		}
	}
	rebuilt := now.Add(2 * time.Second)
	if err := db.ReplaceChunks(ctx, liveID, []Chunk{
		{FileID: liveID, ChunkIndex: 0, Content: "new-portugal", Embedding: []float32{0, 1, 0}, CreatedAt: rebuilt},
	}); err != nil {
		t.Fatal(err)
	}
	hits, err = db.SearchChunks(ctx, []float32{0, 1, 0}, 20)
	if err != nil {
		t.Fatal(err)
	}
	var sawNew bool
	for _, hit := range hits {
		if hit.FileID != liveID {
			continue
		}
		if hit.Content != "new-portugal" {
			t.Fatalf("old chunk survived rebuild: %#v", hit)
		}
		sawNew = true
	}
	if !sawNew {
		t.Fatalf("rebuilt chunk missing: %#v", hits)
	}
}
