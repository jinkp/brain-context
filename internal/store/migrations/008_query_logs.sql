-- Query logs: one row per MCP search call
-- Used for usage metrics and impact analysis
CREATE TABLE IF NOT EXISTS query_logs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id      uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- key prefix of the token used (identifies the developer, without exposing the secret)
    actor_prefix    text,
    chunks_returned int  NOT NULL DEFAULT 0,
    top_score       real NOT NULL DEFAULT 0,
    avg_score       real NOT NULL DEFAULT 0,
    total_candidates int NOT NULL DEFAULT 0,
    tokens_returned  int NOT NULL DEFAULT 0,
    -- estimated tokens the LLM would have needed without brain-context
    -- computed as: total chunks in project × avg_chunk_tokens
    tokens_in_repo   int NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_query_logs_tenant_created  ON query_logs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_query_logs_project_created ON query_logs(project_id, created_at DESC);
