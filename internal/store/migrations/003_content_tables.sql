CREATE TABLE IF NOT EXISTS chunks (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_id uuid NOT NULL REFERENCES project_files(id) ON DELETE CASCADE,
    chunk_hash char(64) NOT NULL,
    symbol_name text,
    symbol_type text,
    start_line int,
    end_line int,
    content text NOT NULL,
    token_count int,
    tsv tsvector,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (file_id, chunk_hash)
);

CREATE TABLE IF NOT EXISTS symbols (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_id uuid NOT NULL REFERENCES project_files(id) ON DELETE CASCADE,
    fq_name text NOT NULL,
    kind text NOT NULL,
    signature text,
    entity_hash char(64) NOT NULL,
    UNIQUE (project_id, fq_name)
);

CREATE TABLE IF NOT EXISTS relationships (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    src_type text NOT NULL,
    src_id uuid NOT NULL,
    rel_type text NOT NULL,
    dst_type text NOT NULL,
    dst_id uuid NOT NULL,
    weight real NOT NULL DEFAULT 1.0,
    UNIQUE (project_id, src_id, rel_type, dst_id)
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    actor_id uuid,
    action text NOT NULL,
    resource_type text,
    resource_id uuid,
    request_id text,
    metadata jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
