-- Query patterns, chosen before ORDER BY (immutable on MergeTree):
-- files: point read/update by file_id. The list endpoint scans a small catalog.
-- chunks: read and replace by file_id. Similarity search scans embeddings.
-- extractions: approve/reject by extraction_id.
-- microservice, api, pulsar_topic: point read/update by entity id.
-- api.microservice_id is not in ORDER BY. create/update filter by api_id, and that prefix must match the key.
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

CREATE TABLE IF NOT EXISTS rag.microservice (
    microservice_id UUID,
    name String,
    description String,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (microservice_id);

CREATE TABLE IF NOT EXISTS rag.api (
    api_id UUID,
    microservice_id UUID,
    name String,
    description String,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (api_id);

CREATE TABLE IF NOT EXISTS rag.pulsar_topic (
    topic_id UUID,
    name String,
    description String,
    created_at DateTime64(3, 'UTC'),
    updated_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (topic_id);
