# Tasks: brain-context-local-cache

<!-- Status: NO INICIADO -->
<!-- Prioridad: BAJA — OKF V2 (server sync) tiene más impacto primero -->
<!-- Prerequisito: OKF V2 completado antes de evaluar si local-cache sigue siendo necesario -->

## Phase 1: Local Query Cache MVP

- [ ] 1.1 Add a dedicated local cache package for read-path persistence, separate from `internal/embedder` and `internal/indexer`
- [ ] 1.2 Define SQLite schema for `project_state` and `query_cache`
- [ ] 1.3 Add local cache path resolution under `~/.brain/context-cache.db`
- [ ] 1.4 Add cache-key builder that includes project, tool, normalized query, tool parameters, and freshness metadata
- [ ] 1.5 Add a lightweight remote project state read path that returns `last_indexed`, `embed_model`, and `embed_dimensions`
- [ ] 1.6 Integrate query cache lookup/write into `search_project_context`
- [ ] 1.7 Integrate query cache lookup/write into `explain_flow`
- [ ] 1.8 Add TTL handling and stale-entry bypass logic
- [ ] 1.9 Add `brain cache status --project <name>` CLI command
- [ ] 1.10 Add `brain cache clear --project <name>` CLI command
- [ ] 1.11 Add `brain cache prune` CLI command for expired entries
- [ ] 1.12 Add tests for cache hits, stale bypass, parameter-sensitive keys, and project-scoped clearing

## Phase 2: Warmup and Chunk Reuse

- [ ] 2.1 Add `chunk_cache` schema for reusable chunk-level content
- [ ] 2.2 Add chunk extraction/write logic from successful search responses
- [ ] 2.3 Add `brain cache warm --project <name>` command
- [ ] 2.4 Define warmup policy for recent/popular queries without downloading full corpus
- [ ] 2.5 Add tests for warmup idempotency and chunk cache invalidation boundaries

## Phase 3: Explicit Offline Snapshot Without Vectors

- [ ] 3.1 Define offline snapshot manifest format under `~/.brain/offline/<project_id>/`
- [ ] 3.2 Add dedicated CLI namespace: `brain snapshot pull|status|delete`
- [ ] 3.3 Add API contract for snapshot manifest and chunk export
- [ ] 3.4 Implement explicit stale marker when offline snapshot lags remote `last_indexed`
- [ ] 3.5 Ensure offline snapshot storage is distinct from indexing snapshots used by `brain update`
- [ ] 3.6 Add tests for explicit-only download behavior and stale snapshot reporting

## Phase 4: Optional Local Semantic Search

- [ ] 4.1 Evaluate embedded vector storage options suitable for a CLI-first product
- [ ] 4.2 Add feature-flagged local semantic search mode for offline snapshots
- [ ] 4.3 Benchmark local semantic search against server retrieval for realistic repo sizes
- [ ] 4.4 Add documentation describing tradeoffs around revocation, freshness, and disk footprint
