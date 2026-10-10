# Proposal: brain-context-okf-knowledge-layer

## Intent

Add an OKF (Open Knowledge Format v0.2) knowledge layer to brain-context that sits **above** the existing RAG code index. MCP tools query OKF first (fast, curated, structured intent), then fall back to RAG (code chunks) when needed. This separates INTENT (requirements, decisions, playbooks, objectives) from IMPLEMENTATION (code), reduces unnecessary token consumption on retrieval, and improves precision for queries that are better answered by curated knowledge than by raw code chunks.

## Problem

The current RAG index answers implementation-level questions well but cannot answer intent-level questions:
- "Why was this architecture decision made?" → no answer in code chunks
- "What is the requirement behind this feature?" → absent from embeddings
- "Which playbook handles incident X?" → not indexed at all

Tech Leads need a separate, curated layer of knowledge that complements code RAG rather than duplicating it. Without it, every semantic query pollutes context with low-signal code chunks when a single curated concept would suffice.

## Goals

- Allow MCP tools to answer intent-level queries from curated OKF bundles before falling back to code RAG
- Give Tech Leads a CLI (`brain okf`) to register bundles, generate drafts, and sync to the server
- Support two visibility tiers: server-shared bundles (team-accessible) and local-only bundles (private)
- Auto-generate OKF draft concepts from the existing RAG index to reduce cold-start friction

## Non-Goals

- Attested Computation support (OKF §10) — deferred
- Cross-bundle knowledge graph traversal — deferred
- Watch mode: auto-sync OKF bundle on code change — deferred
- Web UI for OKF curation — deferred
- OKF bundles embedded inside the code repo (they are separate, own lifecycle)

## Scope

### In Scope
- `internal/okf/` package: bundle reader, FTS indexer, search, draft generator
- New DB schema: `okf_concepts` table (separate from code chunks; tagged with bundle ID, trust tier, freshness)
- MCP orchestration: query OKF first → RAG fallback (configurable threshold)
- `brain okf` subcommand: `register`, `generate`, `sync`, `list`
- New API routes: `POST /api/okf/search`, `POST /api/okf/sync`, `GET /api/okf/bundles`
- Trust-tier filter on retrieval: `human-reviewed > machine-confirmed > unverified`; stale/deprecated concepts excluded by default
- Two visibility tiers: `shared` (uploaded to server, team-visible) vs `local` (on-disk only, private)

### Out of Scope
- OKF Attested Computation (§10)
- Cross-bundle graph traversal
- OKF watch mode (auto-sync on file change)
- Web UI for curation workflow

## Capabilities

### New Capabilities
- `okf-bundle-management`: Register, list, and manage OKF bundles (local and shared); CLI entry point `brain okf`
- `okf-indexer`: Read OKF bundles from disk, index concepts into Postgres (`okf_concepts` table), with trust tier + freshness metadata
- `okf-search`: FTS over title/description/tags with trust-tier and freshness filtering; returns ranked concepts
- `okf-generator`: Auto-draft OKF concepts from existing RAG index (`brain okf generate`); outputs `status: draft`, `generated.by: brain-context/...`
- `okf-sync`: Upload a local bundle to the server (`brain okf sync`) for team visibility
- `mcp-okf-orchestration`: Intercept MCP tool calls (search_project_context, explain_flow, get_file_summary, get_related_files, find_impact), query OKF first, fall back to RAG on miss or low-confidence

### Modified Capabilities
- None — existing RAG retrieval is wrapped, not replaced

## Approach

**Two-actor model:**
- **Developer**: benefits passively — MCP tools return OKF concepts + code chunks together, ranked by relevance. No new CLI required.
- **Tech Lead**: owns an OKF bundle (folder, optionally git-managed) outside the code repo. Uses `brain okf` CLI to register, generate drafts, curate, and sync.

**OKF bundle as separate lifecycle:** bundles live outside the code repo (standalone folder or git repo). Changes rarely; curated intentionally. `brain okf register --bundle <path> --project <name>` binds a bundle to a project.

**Draft generation (`brain okf generate`):** reads the RAG index for the given project, clusters related chunks, and emits OKF concept files (`status: draft`, `generated.by: brain-context/rag-index`). Tech Lead curates drafts to promote to `verified`.

**MCP orchestration (OKF-first):**
1. Receive tool call with query
2. Query `okf_concepts` (FTS + trust tier filter + freshness filter)
3. If confidence ≥ threshold → return OKF result (+ optional supplemental RAG)
4. If miss or low confidence → fall back to full RAG (existing behavior)

**Trust tier order:** `human-reviewed` > `machine-confirmed` > `unverified`. Stale (`updated_at < 90d`) and `deprecated` concepts filtered out by default.

**Bundle visibility:**
- `local`: indexed only on-device; not synced to server; private to Tech Lead
- `shared`: synced via `brain okf sync`; stored server-side; accessible to team via MCP

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/okf/` | New | Bundle reader, indexer, FTS search, draft generator — new package |
| `internal/store/` | Modified | New `okf_concepts` table + migration; new store methods for OKF queries |
| `internal/api/` | Modified | New routes: `POST /okf/search`, `POST /okf/sync`, `GET /okf/bundles` |
| `internal/mcp/tools.go` | Modified | Orchestration layer: OKF-first query + RAG fallback per tool |
| `cmd/brain/main.go` | Modified | New `brain okf` subcommand with `register`, `generate`, `sync`, `list` |
| `sdd/brain-context-okf-knowledge-layer/` | New | SDD change folder |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| OKF adoption depends on Tech Lead curating bundles | Med | Auto-draft from RAG lowers cold-start cost; stale filter degrades gracefully to RAG |
| Auto-generated drafts no better than raw RAG if uncurated | Med | Drafts are clearly tagged `status: draft`; OKF layer only gains precision when promoted to `verified` |
| Bundle visibility model adds access control complexity | Med | Reuse existing `mcp_read_key` scoping; `shared` bundles go through same auth as project context |
| FTS over OKF concepts does not handle semantic miss | Low | RAG fallback is always available; OKF is additive, not a replacement |
| Schema migration on existing Postgres instances | Low | Additive migration (new table, no column changes to existing tables) |

## Rollback Plan

OKF layer is purely additive:
- If `internal/okf/` package is removed, MCP tools fall back to current RAG-only behavior automatically (orchestration wrapping is conditional on OKF being registered)
- `okf_concepts` table can be dropped without affecting `chunks` or any existing table
- New API routes can be disabled via feature flag or removed without side effects
- `brain okf` subcommand is a new cobra command; removing it does not affect existing commands
- No changes to existing embedding, indexing, or search pipeline

## Dependencies

- OKF v0.2 spec (open standard; no external service dependency — format is file-based)
- Postgres FTS (already available via `pg_trgm` or `tsvector`; no new extension required)
- Existing RAG index must be populated before `brain okf generate` produces useful drafts

## Success Criteria

- [ ] `brain okf register --bundle <path> --project <name>` indexes all concepts in the bundle into `okf_concepts`
- [ ] `brain okf generate --project <name>` produces draft OKF files from the RAG index with `status: draft`
- [ ] `brain okf sync --project <name>` uploads a local bundle to the server; team members can query it via MCP
- [ ] MCP tools return OKF concepts before RAG chunks when a matching, non-stale `human-reviewed` concept exists
- [ ] When no OKF match passes confidence threshold, tools fall back to RAG with no visible degradation
- [ ] Local-only bundles are NOT visible to team members (visibility tier enforced at API layer)
- [ ] Stale and deprecated concepts are excluded from results by default; opt-in flag to include them
- [ ] All existing MCP tool tests pass without modification (no regression on RAG path)
