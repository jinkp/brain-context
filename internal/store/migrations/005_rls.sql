ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE index_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE chunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE symbols ENABLE ROW LEVEL SECURITY;
ALTER TABLE relationships ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE chunk_vectors_768 ENABLE ROW LEVEL SECURITY;
ALTER TABLE chunk_vectors_1024 ENABLE ROW LEVEL SECURITY;
ALTER TABLE chunk_vectors_3072 ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON projects;
CREATE POLICY tenant_isolation ON projects
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON project_files;
CREATE POLICY tenant_isolation ON project_files
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON index_jobs;
CREATE POLICY tenant_isolation ON index_jobs
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON api_keys;
CREATE POLICY tenant_isolation ON api_keys
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON chunks;
CREATE POLICY tenant_isolation ON chunks
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON symbols;
CREATE POLICY tenant_isolation ON symbols
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON relationships;
CREATE POLICY tenant_isolation ON relationships
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON audit_logs;
CREATE POLICY tenant_isolation ON audit_logs
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON chunk_vectors_768;
CREATE POLICY tenant_isolation ON chunk_vectors_768
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON chunk_vectors_1024;
CREATE POLICY tenant_isolation ON chunk_vectors_1024
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON chunk_vectors_3072;
CREATE POLICY tenant_isolation ON chunk_vectors_3072
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
