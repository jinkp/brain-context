CREATE TABLE IF NOT EXISTS tenants (
    id uuid PRIMARY KEY,
    name text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS projects (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    repository_url text,
    default_branch text NOT NULL DEFAULT 'main',
    embed_model text NOT NULL,
    embed_dimensions int NOT NULL CHECK (embed_dimensions IN (768, 1024, 3072)),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS project_files (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path text NOT NULL,
    language text NOT NULL,
    content_hash char(64) NOT NULL,
    size_bytes bigint NOT NULL DEFAULT 0,
    indexed_at timestamptz,
    UNIQUE (project_id, path)
);

CREATE TABLE IF NOT EXISTS index_jobs (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('full', 'incremental')),
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'retry', 'failed', 'done', 'cancelled')),
    priority smallint NOT NULL DEFAULT 100,
    attempt int NOT NULL DEFAULT 0,
    max_attempts int NOT NULL DEFAULT 5,
    lease_expires_at timestamptz,
    error text,
    requested_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz
);

CREATE TABLE IF NOT EXISTS api_keys (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    project_id uuid,
    key_prefix text NOT NULL,
    secret_hash text NOT NULL,
    scope text NOT NULL CHECK (scope IN ('tenant', 'project', 'mcp_read')),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
