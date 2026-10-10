# Design: brain-context-secure-invite-search

## Technical Approach

Move read-path query embedding from the client MCP process into the API. MCP clients will send a natural-language `query` string to the API, authenticated with `mcp_read_key`. The API will load the encrypted provider key from secure storage, decrypt it server-side, generate the embedding, perform retrieval, and return ranked context results.

This removes the need for joined clients to store or decrypt embedder secrets.

## Current State

Today the read-path is split incorrectly across trust boundaries:

1. `brain join` stores `embed_api_key_enc` in local config
2. MCP tools in `internal/mcp/tools.go` resolve a local provider key
3. The client embeds the query locally
4. The client sends `query_embedding` to `POST /api/projects/:id/context/search`
5. The API validates vector dimensions and performs retrieval

The API endpoint in `internal/api/search_handler.go` currently requires `query_embedding` and rejects requests that do not provide it.

This design is incompatible with secure invites because invited clients should not need decryption capability for server-managed secrets.

## Key Decisions

### 1. Query embedding moves server-side for read search

All read-only MCP search flows must submit plain-text queries, not client-generated embeddings.

This applies to:

- `search_project_context`
- `explain_flow`
- `search_solution_context`

`get_file_summary`, `get_related_files`, and `find_impact` do not need embedding changes because they already operate directly against indexed metadata/relations.

### 2. `embed_api_key_enc` must not leave the server in join responses

`brain join` should provide only the minimum local config needed for read access:

- `project_id`
- `project_name`
- `embed_model`
- `embed_dimensions`
- `mcp_read_key`

It must no longer return `embed_api_key_enc` to invited clients.

### 3. Compatibility must be explicit and temporary

The API should support a transition period where it can accept either:

- legacy `query_embedding` requests from older clients, or
- new `query` requests from migrated clients

However, new client code must prefer `query` immediately.

### 4. Secret handling remains server-only

Server responsibilities:

- load project metadata
- load encrypted provider key
- decrypt with `BRAIN_ENCRYPTION_KEY`
- instantiate provider embedder
- embed query
- execute retrieval and rerank

Client responsibilities:

- send query text
- authenticate with `mcp_read_key`
- render returned results

## API Contract Changes

### Project Search

Current request:

```json
{
  "query_embedding": [0.1, 0.2],
  "query_tsv": "payment retry",
  "max_chunks": 8,
  "include_relationships": false
}
```

New request:

```json
{
  "query": "payment retry",
  "max_chunks": 8,
  "include_relationships": false
}
```

Compatibility request during migration:

```json
{
  "query": "payment retry",
  "query_embedding": [0.1, 0.2],
  "query_tsv": "payment retry",
  "max_chunks": 8,
  "include_relationships": false
}
```

The server should prefer `query` when present and only fall back to `query_embedding` for older clients.

### Solution Search

Today `search_solution_context` embeds on the client using the first project's embedder config. After this change, the MCP server should forward plain-text `query` to each project search endpoint and let each API request embed server-side.

This preserves the smart probe/deep routing logic while removing local access to provider secrets.

## Server Implementation Shape

### Search Request Model

Extend `internal/api/search_handler.go` request model to include:

- `Query string \`json:"query,omitempty"\``
- keep `QueryEmbedding []float32` temporarily for backward compatibility

Validation rules:

- if `query` is present and non-empty, server-side embedding path is used
- else if `query_embedding` is present, legacy path is used
- else return validation error

### Provider Key Resolution

The API must resolve provider secrets server-side using stored encrypted keys. That logic should stay entirely inside API/store layers.

Preferred shape:

- add a helper near the API layer to load project embedder configuration and decrypt `embed_api_key_enc`
- return a clear service error when the project lacks a stored provider key

### Reranking

The reranker currently already uses `QueryTSV`. For the new path, `query` itself becomes the rerank text source.

## Client Implementation Shape

### MCP Search Tools

Update `internal/mcp/tools.go` so search-style tools stop creating local embedders and instead send plain-text queries to the API.

Effects:

- remove read-path dependence on `EmbedAPIKey`
- remove read-path dependence on `EmbedAPIKeyEnc`
- remove read-path dependence on `BRAIN_ENCRYPTION_KEY`

### Join Flow

Update `brain join` response handling to stop expecting `embed_api_key_enc`.

Local config may keep the field for backward compatibility, but new joins should not populate it.

## Security Properties After Change

- invited clients cannot recover provider API keys
- invited clients do not need `BRAIN_ENCRYPTION_KEY`
- revoking `mcp_read_key` removes read access immediately
- provider keys remain centralized and rotatable on the server

## Migration Strategy

### Phase 1

- API accepts `query` in search requests
- MCP clients switch to sending `query`
- join responses continue to include old fields only if needed for compatibility

### Phase 2

- `brain join` stops returning `embed_api_key_enc`
- MCP no longer attempts local decrypt for read tools

### Phase 3

- remove legacy `query_embedding` requirement from read-path clients
- keep API fallback only as long as older clients are still supported

## Risks and Mitigations

### Increased server cost

Server-side embedding increases per-query backend cost.

Mitigation:

- add per-token rate limits later if needed
- combine with local query cache work to cut repeated requests

### API latency increase

Embedding moves into the request path.

Mitigation:

- correctness and security take priority
- local cache can offset repeated-query latency in a later change

### Backward compatibility drift

Old clients may still send embeddings.

Mitigation:

- keep temporary dual-path request parsing
- emit clear deprecation messaging in docs/release notes
