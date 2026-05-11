CREATE INDEX IF NOT EXISTS idx_chunk_vectors_768_embedding_hnsw
    ON chunk_vectors_768 USING hnsw (embedding vector_cosine_ops);

CREATE INDEX IF NOT EXISTS idx_chunk_vectors_1024_embedding_hnsw
    ON chunk_vectors_1024 USING hnsw (embedding vector_cosine_ops);

-- pgvector limits ANN indexes (HNSW/IVFFlat) to 2000 dimensions.
-- chunk_vectors_3072 (OpenAI text-embedding-3-large) exceeds this limit.
-- For MVP, 3072-dim queries use exact sequential scan — acceptable at small scale.
-- Production path: reduce to 1024 dims via OpenAI's dimensions parameter, or use Qdrant.

CREATE INDEX IF NOT EXISTS idx_chunks_tsv
    ON chunks USING gin (tsv);

CREATE INDEX IF NOT EXISTS idx_index_jobs_project_state_priority_created_at
    ON index_jobs (project_id, state, priority, created_at);

CREATE INDEX IF NOT EXISTS idx_project_files_project_content_hash
    ON project_files (project_id, content_hash);

CREATE INDEX IF NOT EXISTS idx_relationships_project_rel_type
    ON relationships (project_id, rel_type);

CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_created_at_desc
    ON audit_logs (tenant_id, created_at DESC);
