CREATE TABLE IF NOT EXISTS chunk_vectors_768 (
    chunk_id uuid PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    embedding vector(768) NOT NULL
);

CREATE TABLE IF NOT EXISTS chunk_vectors_1024 (
    chunk_id uuid PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    embedding vector(1024) NOT NULL
);

CREATE TABLE IF NOT EXISTS chunk_vectors_3072 (
    chunk_id uuid PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    embedding vector(3072) NOT NULL
);
