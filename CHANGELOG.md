# Changelog

All notable changes to brain-context are documented here.

---

## [v0.8.0] — 2026-10-14

### OKF Knowledge Layer (V1 — Local Only)

brain-context now supports OKF (Open Knowledge Format v0.2) knowledge bundles as a curated retrieval layer above the RAG code index.

**New commands:**
- `brain okf register --project <name> --bundle <path>` — register and index an OKF bundle
- `brain okf local --project <name> --bundle <path>` — create a local-only knowledge project (no server required)
- `brain okf generate --project <name>` — auto-generate draft concepts from the RAG index
- `brain okf list --project <name> [--status draft|stable|deprecated] [--stale]` — list concepts
- `brain okf validate --project <name> [--refs]` — validate frontmatter and source file references

**MCP behavior change:**
`search_project_context` and `explain_flow` now query registered OKF bundles first. When a concept's FTS5 relevance score meets the project's threshold (default 0.7), the curated concept is returned instead of RAG chunks — reducing token usage for knowledge-level queries. Falls back to RAG transparently when no match is found.

**V1 scope:** local-only bundles. Server sync and shared team bundles are V2.

**Migration:** no migration required. OKF is purely additive — existing configs and MCP behavior are unchanged when no bundles are registered.

### Local Query Cache (Phase 1)

brain-context now caches MCP search responses locally in `~/.brain/context-cache.db`.

**How it works:** Before calling the API, the MCP server fetches the project's remote freshness state (`last_indexed`, `embed_model`, `embed_dimensions`). If a matching non-expired entry exists in the local SQLite cache, it is returned immediately — zero API cost. Cache entries are automatically invalidated when the project is re-indexed.

**New commands:**
- `brain cache status --project <name>` — show local cache entries and freshness state
- `brain cache clear --project <name>` — delete cache entries for a project
- `brain cache prune` — delete all expired cache entries

**Default TTL:** 24 hours. Cache is advisory only — any network error during freshness validation bypasses the cache transparently.

### Local Telemetry

Every `search_project_context` and `explain_flow` call now records a query event locally, tracking which layer answered the query (OKF, cache, or API) and the end-to-end latency.

**New command:**
- `brain metrics local [--project <name>]` — show OKF/cache hit rates and latency breakdown

**Example output:**
```
Local telemetry — my-project

Total queries:     47
Source breakdown:
  OKF (local):     31  (66.0%)
  Cache (local):    8  (17.0%)
  API (remote):     8  (17.0%)
Local resolution:  83.0% (queries answered without API call)
Avg latency:       12ms
Avg OKF score:     0.8734
```

### Secure Invite Model

- `brain join` no longer stores `embed_api_key_enc` in the local config. Invited members receive only a scoped `mcp_read_key` (`brn_mcp_xxx`).
- `ProjectConfig.EmbedAPIKeyEnc` is deprecated — no new code writes it; existing configs are still read for backward compatibility.

### Server-side query embedding

`SearchContext` now generates the query embedding on the server using the project's stored (encrypted) provider key. Clients send a plain-text `query` string — no embed API key required on the client side.

### Bug Fixes

- Fixed `BuildIndex`, `CheckStaleness`, `runOKFList`, `runOKFValidate`, and `countMDFiles` to walk bundle directories recursively (`filepath.Walk`) instead of only reading the root level (`os.ReadDir`). Concepts in subdirectories were silently ignored.
- Fixed `brain okf register` to reuse the existing `bundleID` when called on an already-registered bundle, preventing orphaned SQLite DB files on every re-registration.

### Migration Notes

#### Clients using `query_embedding` (legacy path)

The API still accepts `query_embedding` alongside the new `query` field for backward compatibility. The new path takes precedence when `query` is present. Legacy clients can migrate at their own pace.

#### Clients with `embed_api_key_enc` in local config

If your `~/.brain/config.yaml` contains `embed_api_key_enc` (set by a pre-migration `brain join`), it will continue to work for local indexing flows. Run `brain join` again to get a fresh config without the deprecated field.

---

## Prior releases

History not tracked in this file. See git log for full history.
