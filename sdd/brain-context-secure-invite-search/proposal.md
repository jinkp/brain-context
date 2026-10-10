# Proposal: brain-context-secure-invite-search

## Intent

Redesign the invite and MCP search flow so invited developers can query project context securely without ever receiving the embedder provider API key, the encrypted provider key, or the server encryption key.

## Problem

The current architecture mixes server-side secret storage with client-side query embedding:

- `brain join` returns `embed_api_key_enc`
- MCP tools such as `search_project_context`, `explain_flow`, and `search_solution_context` try to embed the query locally
- local embedding requires either `EmbedAPIKey` in plaintext or successful decryption of `EmbedAPIKeyEnc`
- decryption requires `BRAIN_ENCRYPTION_KEY`, which is supposed to stay server-side

This creates a broken trust model for invited developers:

- if the client does not have `BRAIN_ENCRYPTION_KEY`, search fails
- if the client does have `BRAIN_ENCRYPTION_KEY`, a server secret has leaked to the client
- if the client receives the plaintext provider key, the invite is no longer read-only in any meaningful sense

The current flow is therefore insecure or non-functional depending on which shortcut is chosen.

## Goals

- Allow invited developers to use MCP read tools successfully after `brain join`
- Keep `BRAIN_ENCRYPTION_KEY` server-only
- Keep embedder provider API keys server-only
- Preserve token-scoped read access via `mcp_read_key`
- Keep admin/indexing flows working during the migration

## Non-Goals

- Do not redesign chunk upload or indexing ownership in this change
- Do not make invited developers capable of indexing by default
- Do not introduce a new secret-sharing mechanism to clients
- Do not require local provider credentials for read-only MCP usage

## Scope

This change focuses on query-time search for MCP read tools:

1. Move query embedding for read-path search to the API
2. Stop returning `embed_api_key_enc` from `brain join`
3. Update MCP tools to send plain-text queries to the API
4. Keep compatibility long enough to migrate older clients safely

## Why This Approach

If an operation requires access to a secret, that operation belongs on the server. Query embedding requires access to the provider API key, so the embedding step must move server-side for invited read-only users. This restores a clean security boundary and makes invites behave like real read-only access instead of disguised secret distribution.
