CREATE INDEX IF NOT EXISTS idx_chunk_vectors_768_embedding_hnsw
    ON chunk_vectors_768 USING hnsw (embedding vector_cosine_ops);

CREATE INDEX IF NOT EXISTS idx_chunk_vectors_1024_embedding_hnsw
    ON chunk_vectors_1024 USING hnsw (embedding vector_cosine_ops);

CREATE INDEX IF NOT EXISTS idx_chunk_vectors_3072_embedding_hnsw
    ON chunk_vectors_3072 USING hnsw (embedding vector_cosine_ops);

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
