# Tasks: brain-context-secure-invite-search

## Phase 1: API Server-Side Query Embedding

- [x] 1.1 Extend `internal/api/search_handler.go` request model to accept `query` alongside legacy `query_embedding`
- [x] 1.2 Add server-side embedder resolution for project search using stored encrypted provider keys
- [x] 1.3 When `query` is present, generate the embedding server-side and reuse the existing retrieval/rerank pipeline
- [x] 1.4 Keep legacy `query_embedding` path temporarily for backward compatibility
- [x] 1.5 Add validation errors for requests missing both `query` and `query_embedding`
- [x] 1.6 Add API tests for plain-text query search, legacy embedding search, and mixed-field precedence

## Phase 2: MCP Client Migration

- [x] 2.1 Update `internal/mcp/tools.go` so `search_project_context` sends `query` instead of local `query_embedding`
- [x] 2.2 Update `internal/mcp/tools.go` so `explain_flow` sends `query` instead of local `query_embedding`
- [x] 2.3 Update `search_solution_context` probe and deep fan-out requests to send plain-text `query`
- [x] 2.4 Remove read-path dependence on local plaintext embedder keys for MCP search tools
- [x] 2.5 Add tests covering joined-member search flows without local embedder secrets

## Phase 3: Secure Join Response

- [x] 3.1 Remove `embed_api_key_enc` from invite redeem API responses intended for clients
- [x] 3.2 Update `brain join` response parsing and local config write path accordingly
      <!-- cmd/brain/main.go — EmbedAPIKeyEnc removed from redeemInviteResponse and runJoin write path -->
- [x] 3.3 Ensure older local configs with `embed_api_key_enc` continue to load without breaking
- [x] 3.4 Add tests verifying that `brain join` persists only read-safe configuration for invited members
      <!-- cmd/brain/join_test.go — TestRunJoin_DoesNotPersistEmbedAPIKeyEnc + TestRunJoin_ServerReturnsEmbedKeyEnc_FieldIsIgnored -->

## Phase 4: Cleanup and Hardening

- [x] 4.1 Remove or deprecate client-side read-path decrypt logic once migration is complete
      <!-- config.go — EmbedAPIKeyEnc marked deprecated; resolveEmbedAPIKey — path marked deprecated, kept for backward compat -->
- [x] 4.2 Update README and onboarding docs to describe the new invite security boundary
      <!-- README.md — added Team Collaboration section + security bullet for invite boundary + brain invite/join in CLI reference -->
- [x] 4.3 Add release notes / migration notes for older clients still using `query_embedding`
      <!-- CHANGELOG.md created with migration notes for legacy query_embedding and embed_api_key_enc -->
- [x] 4.4 Evaluate rate limiting or caching strategies for increased server-side embedding load
      <!-- internal/api/server.go — documented evaluation: token auth is current mitigation; embed_ms in query_logs is the signal to watch; concrete threshold before adding per-project rate limiter -->
