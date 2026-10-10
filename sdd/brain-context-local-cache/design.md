# Design: brain-context-local-cache

## Technical Approach

Implement a new local cache subsystem for read-path acceleration. The subsystem stores complete search responses keyed by project, normalized query, tool parameters, and remote freshness metadata. The cache is advisory only: whenever freshness cannot be proven, the client must fall back to the API.

The API remains the source of truth. The client gains three responsibilities:

1. Persist project freshness metadata locally
2. Reuse cached search responses when that metadata still matches
3. Expose operational CLI commands for cache inspection and cleanup

## Current State

The repo already has local persistence in two unrelated areas:

- `internal/indexer/hashing.go` stores indexing snapshots at `~/.brain/snapshots/<project_id>.json`
- `internal/embedder/cache.go` stores upload-time embedding cache at `~/.brain/cache/<project_id>/embeddings.json`

Search tools in `internal/mcp/tools.go` always execute remote HTTP requests for read operations such as:

- `search_project_context`
- `explain_flow`
- `get_file_summary`
- `get_related_files`
- `find_impact`
- `search_solution_context`

The MVP targets only query-style search responses first, because they are the most natural cache win and require the smallest correctness surface.

## Key Decisions

### 1. Separate read cache from indexing snapshot

Do not overload `~/.brain/snapshots`. That directory already means index-diff state. Introducing read snapshots there would create ambiguous semantics and future bugs.

Proposed new local storage:

- `~/.brain/context-cache.db` for cache metadata and cached responses
- `~/.brain/offline/<project_id>/...` reserved for future explicit offline snapshots

### 2. SQLite for MVP

Use SQLite for local cache metadata and response payloads.

Why:

- single-file storage
- no background daemon
- easy to version and migrate
- good fit for CLI-first workflows

Do not introduce a local vector database in the MVP.

### 3. Freshness is server-defined

Cache reuse is allowed only when the client can prove the project has not changed. That proof is based on remote project state, not local heuristics.

Minimum freshness fields:

- `project_id`
- `last_indexed`
- `embed_model`
- `embed_dimensions`

If any of these differ from what the cached entry was created with, the cache entry is stale.

### 4. Query cache before chunk cache

Store complete rendered search responses first. Chunk-level reuse is useful, but it adds more merging logic and partial invalidation rules. Query cache gives immediate latency improvement with less complexity.

## Storage Layout

### SQLite Database

`~/.brain/context-cache.db`

Tables:

#### `project_state`

- `project_id TEXT PRIMARY KEY`
- `project_name TEXT NOT NULL`
- `last_indexed TEXT`
- `embed_model TEXT NOT NULL`
- `embed_dimensions INTEGER NOT NULL`
- `api_endpoint TEXT NOT NULL`
- `updated_at TEXT NOT NULL`

#### `query_cache`

- `cache_key TEXT PRIMARY KEY`
- `project_id TEXT NOT NULL`
- `tool_name TEXT NOT NULL`
- `query_text TEXT NOT NULL`
- `query_hash TEXT NOT NULL`
- `params_json BLOB NOT NULL`
- `last_indexed TEXT`
- `embed_model TEXT NOT NULL`
- `embed_dimensions INTEGER NOT NULL`
- `response_schema_version INTEGER NOT NULL`
- `response_text BLOB NOT NULL`
- `created_at TEXT NOT NULL`
- `expires_at TEXT NOT NULL`

Future phases may add:

- `chunk_cache`
- `offline_snapshot`

## Cache Key

The cache key must uniquely identify a reusable response.

Suggested normalized key inputs:

- `project_id`
- `tool_name`
- normalized `query`
- normalized tool parameters such as `max_chunks` and `include_relationships`
- `last_indexed`
- `embed_model`
- `embed_dimensions`
- `response_schema_version`

Suggested implementation:

`sha256(serialized-key-components)`

## CLI Surface

### MVP commands

- `brain cache status --project <name>`
- `brain cache clear --project <name>`
- `brain cache prune`

### Deferred commands

- `brain cache warm --project <name>`
- `brain snapshot pull --project <name>`
- `brain snapshot status --project <name>`
- `brain snapshot delete --project <name>`

## Runtime Flow

### Search Query Flow

1. Resolve project config as today
2. Fetch or derive current remote project state
3. Build cache key from query + parameters + freshness state
4. If a non-expired cache entry exists, return it immediately
5. Otherwise call the API as today
6. Persist response into `query_cache`
7. Update `project_state`

### Freshness Source

Preferred option:

- API exposes a lightweight project state endpoint with `last_indexed`, `embed_model`, and `embed_dimensions`

Fallback option if adding a new endpoint is deferred:

- reuse freshness metadata already returned by an existing read endpoint if available

The MVP should prefer an explicit project state endpoint because correctness depends on it.

## Invalidation Rules

Invalidate cache entries for a project when any of the following changes:

- `last_indexed`
- `embed_model`
- `embed_dimensions`
- response schema version
- API endpoint for the configured project

Expiration also applies independently through TTL. TTL does not replace freshness validation; both must pass.

## Security Considerations

The MVP stores only rendered search responses locally, not the full remote corpus. This limits risk compared to full offline replication.

Future offline snapshots must be explicit because they weaken immediate revocation guarantees. That is why offline replication is deferred and kept out of default flows.

## Rollout Plan

### Phase 1

- SQLite cache store
- project freshness state store
- query cache integration for query-like tools
- cache status / clear / prune commands

### Phase 2

- chunk cache and warmup

### Phase 3

- explicit offline snapshots without vectors

### Phase 4

- optional local semantic search with vectors
