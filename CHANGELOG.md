# Changelog

All notable changes to brain-context are documented here.

---

## [Unreleased]

### Bug Fixes (OKF — found during sandbox testing)

- Fixed `BuildIndex`, `CheckStaleness`, `runOKFList`, `runOKFValidate`, and `countMDFiles` to walk bundle directories recursively (`filepath.Walk`) instead of only reading the root level (`os.ReadDir`). Concepts in subdirectories were silently ignored.
- Fixed `brain okf register` to reuse the existing `bundleID` when called on an already-registered bundle, preventing orphaned SQLite DB files on every re-registration.

### OKF Knowledge Layer (V1 — Local Only)

brain-context now supports OKF (Open Knowledge Format v0.2) knowledge bundles as a curated retrieval layer above the RAG code index.

**New commands:**
- `brain okf register --project <name> --bundle <path>` — register and index an OKF bundle
- `brain okf generate --project <name>` — auto-generate draft concepts from the RAG index
- `brain okf list --project <name> [--status draft|stable|deprecated] [--stale]` — list concepts
- `brain okf validate --project <name> [--refs]` — validate frontmatter and source file references

**MCP behavior change:**
`search_project_context` and `explain_flow` now query registered OKF bundles first. When a concept's FTS5 relevance score meets the project's threshold (default 0.7), the curated concept is returned instead of RAG chunks — reducing token usage for knowledge-level queries. Falls back to RAG transparently when no match is found.

**V1 scope:** local-only bundles. Server sync and shared team bundles are V2.

**Migration:** no migration required. OKF is purely additive — existing configs and MCP behavior are unchanged when no bundles are registered.

### Added

- **Server-side query embedding**: `SearchContext` now generates the query embedding on the server using the project's stored (encrypted) provider key. Clients send a plain-text `query` string — no embed API key required on the client.
- **Secure invite model**: `brain join` no longer stores `embed_api_key_enc` in the local config. Invited members receive only a scoped `mcp_read_key` (`brn_mcp_xxx`) to access MCP read tools.
- **Team collaboration docs**: README updated with `brain invite` / `brain join` workflow and security boundary explanation.
- **Latency metrics**: `query_logs` table records per-stage latency (`embed_ms`, `vector_ms`, `rerank_ms`, `metrics_ms`, `total_ms`) exposed via `GET /api/metrics`.

### Changed

- `redeemInviteResponse` struct in the CLI no longer includes `embed_api_key_enc`; the field was removed from the API response in the prior release.
- `brain join` write path no longer sets `embed_api_key_enc` in `~/.brain/config.yaml`.
- `ProjectConfig.EmbedAPIKeyEnc` is **deprecated**: no new code writes it; existing configs are still read for backward compatibility.

### Migration Notes

#### Clients using `query_embedding` (legacy path)

The API still accepts `query_embedding` alongside the new `query` field for backward compatibility. The new path takes precedence when `query` is present. Legacy clients can migrate at their own pace.

#### Clients with `embed_api_key_enc` in local config

If your `~/.brain/config.yaml` contains `embed_api_key_enc` (set by a pre-migration `brain join`), it will continue to work for local indexing flows. Run `brain join` again to get a fresh config without the deprecated field.

#### Revocation of pre-migration invite configs

If you want to invalidate configs written before this change (which stored `embed_api_key_enc`), revoke the affected project tokens via `brain projects tokens` and re-issue fresh invites.

---

## Prior releases

History not tracked in this file. See git log for full history.
