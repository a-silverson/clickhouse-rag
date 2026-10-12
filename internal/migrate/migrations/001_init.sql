-- Query patterns, chosen before ORDER BY (immutable on MergeTree):
-- files: point read/update by file_id. The list endpoint scans a small catalog.
-- chunks: read and replace by file_id. Similarity search scans embeddings.
-- extractions: approve/reject by extraction_id.
-- country, developer, location, project, apartments: point read/update by entity id.
-- Status changes are new ReplacingMergeTree versions, not ALTER TABLE UPDATE.
-- No PARTITION BY: these tables have no retention lifecycle yet.
-- Vector index is omitted: embedding width comes from OLLAMA_EMBED_DIMENSIONS.

CREATE DATABASE IF NOT EXISTS rag;

CREATE TABLE IF NOT EXISTS rag.files (
    file_id UUID,
    filename String,
    content_type LowCardinality(String),
    status LowCardinality(String),
    byte_size UInt64,
    checksum String,
    deleted UInt8,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (file_id);

CREATE TABLE IF NOT EXISTS rag.chunks (
    file_id UUID,
    chunk_index UInt32,
    content String,
    embedding Array(Float32),
    deleted UInt8,
    created_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(created_at)
ORDER BY (file_id, chunk_index);

CREATE TABLE IF NOT EXISTS rag.extractions (
    extraction_id UUID,
    file_id UUID,
    table_name LowCardinality(String),
    status LowCardinality(String),
    payload String,
    confidence Float32,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (extraction_id);

CREATE TABLE IF NOT EXISTS rag.country (
    country_id UUID,
    name String,
    code LowCardinality(String),
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (country_id);

CREATE TABLE IF NOT EXISTS rag.developer (
    developer_id UUID,
    name String,
    country_id UUID,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (developer_id);

CREATE TABLE IF NOT EXISTS rag.location (
    location_id UUID,
    name String,
    country_id UUID,
    city String,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (location_id);

CREATE TABLE IF NOT EXISTS rag.project (
    project_id UUID,
    name String,
    developer_id UUID,
    location_id UUID,
    country_id UUID,
    description String,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (project_id);

CREATE TABLE IF NOT EXISTS rag.apartments (
    apartment_id UUID,
    project_id UUID,
    unit_code String,
    rooms UInt8,
    area_sqm Float32,
    price_minor UInt64,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (apartment_id);
