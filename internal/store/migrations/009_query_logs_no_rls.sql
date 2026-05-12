-- query_logs inserts are done by the system (goroutine with no tenant context)
-- RLS would block these inserts, so we explicitly disable it.
-- Reads are protected at the API layer (tenant filter in SQL).
ALTER TABLE query_logs DISABLE ROW LEVEL SECURITY;
