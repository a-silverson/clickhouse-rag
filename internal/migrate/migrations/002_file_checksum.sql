-- Existing databases created before checksum deduplication.
-- ADD COLUMN keeps the original ORDER BY. This is not an UPDATE mutation.

ALTER TABLE rag.files ADD COLUMN IF NOT EXISTS checksum String DEFAULT '';

ALTER TABLE rag.chunks ADD COLUMN IF NOT EXISTS deleted UInt8 DEFAULT 0;
