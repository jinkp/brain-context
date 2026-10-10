# Proposal: brain-context-local-cache

## Intent

Add a client-side local context acceleration layer to `brain-context` while keeping the API as the single source of truth. The first implementation step is a local query cache invalidated by remote project freshness metadata. Later phases may add chunk warmup and explicit offline snapshots.

## Problem

`brain-context` already keeps indexing local and retrieval remote, which is the correct trust boundary for collaboration and token-based access control. However, repeated MCP searches still pay the full round trip to the API even when:

- the same query was already asked recently
- the indexed project has not changed since the previous answer
- the developer only needs read acceleration, not a full offline replica

The repo already stores two different local artifacts:

- indexing snapshots in `~/.brain/snapshots/<project_id>.json` for hash-diff indexing
- embedding cache in `~/.brain/cache/<project_id>/embeddings.json` for upload-side deduplication

Neither solves read-path latency for MCP queries.

## Goals

- Reduce latency for repeated read queries without changing the server-centered architecture.
- Keep cache correctness tied to remote freshness metadata such as `last_indexed` and embedder identity.
- Add explicit CLI controls so users can inspect and clear local cache state.
- Reserve a clear path for optional offline support later, without making it the default behavior.

## Non-Goals

- Do not download the full semantic index by default.
- Do not make local storage the source of truth.
- Do not implement full offline vector search in the first phase.
- Do not reuse the existing indexing snapshot format for read acceleration.

## Scope

This change is phased:

1. MVP: local query cache + project freshness state + cache management CLI
2. Follow-up: chunk cache and warmup helpers
3. Follow-up: explicit offline snapshots without vectors
4. Follow-up: optional local semantic search with vectors

## Why This Approach

The expensive part to optimize first is not storage of vectors; it is repeated API work for identical or near-identical read queries. A query cache invalidated by server freshness metadata provides the best value-to-complexity ratio while preserving collaboration, revocation, and multi-device consistency.
