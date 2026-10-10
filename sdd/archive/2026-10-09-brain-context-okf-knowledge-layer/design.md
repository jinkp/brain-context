# Design: brain-context-okf-knowledge-layer (V1 — Local Only)

**Change**: `brain-context-okf-knowledge-layer`
**Phase**: Design
**Date**: 2026-10-08
**Scope**: V1 local-only — no server sync, no API routes, no `okf_concepts` DB table

---

## Technical Approach

Add a local-first OKF knowledge layer that sits above the existing RAG path. V1 is entirely local: OKF bundles are directories on disk, indexed into SQLite FTS5 at `~/.brain/okf/{project_id}/{bundle_id}.db`. The MCP `Server` loads all registered OKF bundles at startup, queries them before RAG on every `search_project_context` / `explain_flow` call, and falls back to RAG on miss or low confidence. The `brain okf` CLI follows the existing `flag.NewFlagSet` + main-switch dispatch pattern (same as `runJoin`). The `brain update` pipeline gets a conditional post-hook that checks for stale concepts after `SaveSnapshot`. All new config fields are optional and backward-compatible.

---

## Architecture Decisions

### Decision 1 — SQLite FTS5 schema

| Option | Schema |
|--------|--------|
| **Chosen** | Single `concepts` FTS5 table per `.db` file (one DB per bundle) |
| Rejected A | Single DB, all bundles in one table with `bundle_id` column |
| Rejected B | Postgres `okf_concepts` table (V2; requires server — out of scope) |

**Rationale**: One DB per bundle matches the existing `~/.brain/snapshots/{project_id}.json` pattern (one file per entity). Simplifies re-indexing a single bundle without touching others. FTS5 is stdlib SQLite — no extra dependency.

```sql
-- ~/.brain/okf/{project_id}/{bundle_id}.db
CREATE VIRTUAL TABLE concepts USING fts5(
    concept_id,          -- filename without extension (stable ID)
    title,
    description,
    tags,                -- space-separated for FTS
    concept_type,        -- Requirement | Decision | Playbook | Architecture | FileSummary | APIEndpoint
    status,              -- draft | stable | deprecated
    trust_tier,          -- human-reviewed | machine-confirmed | unverified
    source_files,        -- newline-separated repo-relative paths
    stale_after,         -- ISO 8601 date string; empty = no expiry
    verified_by,         -- "human:X" or empty
    tokenize = 'porter ascii'
);

-- Metadata table (not FTS) for date comparisons
CREATE TABLE concept_meta (
    concept_id TEXT PRIMARY KEY,
    status     TEXT NOT NULL,
    stale_after TEXT,  -- ISO 8601 or ''
    updated_at  TEXT NOT NULL
);
```

### Decision 2 — MCP Server struct extension

| Option | Approach |
|--------|----------|
| **Chosen** | Add `okfIndex *okf.Index` field; `Index` is a thin struct holding `map[projectID][]BundleDB` opened at `New()` |
| Rejected A | Lazy-load per request — inconsistent with existing "load once at startup" pattern |
| Rejected B | Separate goroutine / watcher — unnecessary for V1 (no watch mode) |

**Rationale**: The existing `New()` function already reads all config at startup and populates `projectsByID`. OKF follows the same pattern: call `okf.OpenIndex(cfg)` inside `New()` before returning `srv`. No per-request state mutation.

```go
// internal/mcp/server.go — additions only
type Server struct {
    // ... existing fields unchanged ...
    okfIndex *okf.Index // nil if no OKF bundles registered
}
```

### Decision 3 — OKF-first orchestration in `handleSearchProjectContext`

| Option | Approach |
|--------|----------|
| **Chosen** | Wrap inside `handleSearchProjectContext` and `handleExplainFlow` only (2 tools) |
| Rejected A | Wrap all 5 tools (`get_file_summary`, `get_related_files`, `find_impact` too) |
| Rejected B | New MCP tool `search_okf_context` — user must call explicitly |

**Rationale**: Only the two search tools benefit from OKF-first (they accept a free-text query). File summary and related-files tools work at path-level and have no meaningful OKF equivalent. Wrapping them adds latency for no gain.

Interception is a 3-line guard before the existing `searchContext` call — the existing RAG path is untouched:

```go
func (s *Server) handleSearchProjectContext(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
    projectID, project, err := s.resolveProject(req)
    // ... error handling unchanged ...
    query, _ := requiredTrimmedString(req, "query")
    maxChunks := clampToolNumber(req.GetFloat("max_chunks", 8), 8, 20)

    // OKF-first gate (new — skipped if okfIndex is nil)
    if s.okfIndex != nil {
        if result, ok := s.okfIndex.Query(ctx, projectID, query, project.OKFThreshold); ok {
            return textResult(result), nil
        }
    }

    // Existing RAG path — unchanged
    result, err := s.searchContext(ctx, projectID, project, query, maxChunks, false)
    // ...
}
```

### Decision 4 — `brain okf generate` strategy

| Option | Approach |
|--------|----------|
| **Chosen** | Read snapshot file list → per-file concept draft using file path as concept ID |
| Rejected A | Call API (`/context/search`) with broad queries to get clusters |
| Rejected B | Per-symbol granularity (too noisy; hundreds of files) |

**Rationale**: The local snapshot at `~/.brain/snapshots/{project_id}.json` already contains every indexed file path. Per-file drafts are predictable, idempotent (filename = concept ID), and require no API call — entirely offline. The Tech Lead curates them; granularity is appropriate for V1.

Idempotency rule: if `{bundle_path}/{concept_id}.md` already has `status: stable`, skip it. Only fill gaps.

### Decision 5 — Staleness check integration in `brain update`

| Option | Location |
|--------|----------|
| **Chosen** | After `indexer.SaveSnapshot(...)` in `runIndexCommand`, conditional on `project.OKFBundlePath != ""` |
| Rejected A | Separate `brain okf check` command (TL must remember to run it) |
| Rejected B | Before SaveSnapshot (can't — diff not yet saved) |

**Rationale**: The exploration identified this exact integration point (exploration.md line 506). The `diff` variable is already computed and available at that line. Cost is a local file scan — no network, no blocking.

### Decision 6 — Staleness type split

| Concept Type | Stale Behavior |
|---|---|
| `Requirement`, `Decision`, `Playbook`, `Architecture` | Advisory — returned with `[⚠️ stale]` warning prefix |
| `FileSummary`, `APIEndpoint` | Filtered out after `stale_after`; falls to RAG |

**Rationale**: Intent types don't expire — a requirement from 2024 is still valid context. Implementation types (file summaries, endpoints) go stale when the code changes and returning them misleads the LLM.

---

## Data Flow

### Write path: `brain okf register + generate + validate`

```
brain okf register --bundle /path/to/bundle --project my-app
  └─ Load ~/.brain/config.yaml
  └─ Validate bundle path exists (os.Stat)
  └─ Warn if bundle inside repo (advisory only)
  └─ Write OKFBundlePath + OKFVisibility + OKFBundleID to config
  └─ Auto-index: okf.BuildIndex(projectID, bundleID, bundlePath)
       └─ Walk *.md files in bundle dir
       └─ Parse YAML frontmatter (gopkg.in/yaml.v3)
       └─ Insert into SQLite FTS5 at ~/.brain/okf/{pid}/{bid}.db
  └─ Print: "Registered + indexed N concepts"

brain okf generate --project my-app
  └─ Load config → resolve project + bundle path
  └─ indexer.LoadSnapshot(projectID) → file list
  └─ For each file in snapshot (not already in bundle as stable):
       └─ Write {bundle_path}/{sanitize(filepath)}.md with draft frontmatter
  └─ Print: "Generated N draft concepts → {bundle_path}"

brain okf validate [--refs]
  └─ Walk bundle *.md files
  └─ Validate frontmatter YAML (required: type, title, status)
  └─ If --refs: cross-check source_files against snapshot
  └─ Print: N valid, M warnings, K errors
```

### Read path: `search_project_context` with OKF-first

```
MCP client → search_project_context(project_id, query)
  └─ handleSearchProjectContext
       └─ resolveProject(req) → projectID, ProjectConfig
       └─ [OKF gate] s.okfIndex != nil?
            YES → okfIndex.Query(ctx, projectID, query, threshold)
                   └─ For each bundle DB registered for projectID:
                        └─ SQLite FTS5: MATCH query terms
                        └─ Filter: status='stable', not deprecated
                        └─ Staleness split:
                             intent types → include with [⚠️ stale] if past stale_after
                             impl types   → exclude if past stale_after
                        └─ Rank by: trust_tier desc, bm25 score desc
                   └─ Best score >= threshold?
                        YES → formatOKFResult → textResult (return, skip RAG)
                        NO  → fall through
            NO / miss → existing searchContext(RAG)
       └─ Return result
```

### `brain update` post-hook (staleness detection)

```
runIndexCommand
  └─ ... (existing scan → embed → upload pipeline) ...
  └─ indexer.SaveSnapshot(projectID, files, chunksByFile)   ← existing
  └─ [NEW] if project.OKFBundlePath != "":                  ← new post-hook
       └─ okf.CheckStaleness(bundlePath, diff)
            └─ Load concept source_files from SQLite meta
            └─ Intersect with diff.ModifiedFiles + diff.DeletedFiles
            └─ staleIDs = concepts whose source_files overlap
       └─ if len(staleIDs) > 0:
            fmt.Printf("⚠️  %d OKF concept(s) may be stale — run `brain okf list --stale`\n", ...)
```

---

## Interfaces / Contracts

### 1. Config extension (`internal/config/config.go`)

```go
type ProjectConfig struct {
    // ... all existing fields unchanged ...
    OKFBundlePath  string  `yaml:"okf_bundle_path,omitempty"`
    OKFVisibility  string  `yaml:"okf_visibility,omitempty"`  // "local" (default) | "shared" (V2)
    OKFBundleID    string  `yaml:"okf_bundle_id,omitempty"`   // stable UUID assigned at register
    OKFThreshold   float64 `yaml:"okf_threshold,omitempty"`   // 0 → runtime default 0.7
}
```

`normalize()` must trim `OKFBundlePath`, `OKFVisibility`, `OKFBundleID`. `OKFThreshold` validated: if non-zero, must be in `[0.0, 1.0]`.

### 2. `internal/okf` package public surface

```go
package okf

// Index holds open SQLite connections for all registered project bundles.
// Nil-safe: all methods are no-ops when the receiver is nil.
type Index struct { /* unexported */ }

// OpenIndex opens (or creates) SQLite DBs for every project that has OKFBundlePath set.
// Called once at MCP server startup. Returns nil (not error) if no bundles are registered.
func OpenIndex(projects map[string]config.ProjectConfig) (*Index, error)

// Query runs OKF-first search for a project. Returns (formatted result, true) when
// a concept's BM25 score >= threshold; (empty, false) on miss.
func (idx *Index) Query(ctx context.Context, projectID, query string, threshold float64) (string, bool)

// BuildIndex (re)builds the SQLite FTS5 index for one bundle from disk.
// Called by `brain okf register` and `brain okf reindex`.
func BuildIndex(projectID, bundleID, bundlePath string) error

// CheckStaleness returns concept IDs whose source_files overlap with changed/deleted files.
func CheckStaleness(bundlePath string, diff indexer.DiffResult) ([]string, error)

// --- OKF frontmatter types (read by BuildIndex) ---

type Concept struct {
    ConceptID   string   // filename without extension
    Title       string
    Description string
    Tags        []string
    Type        string   // Requirement | Decision | Playbook | Architecture | FileSummary | APIEndpoint
    Status      string   // draft | stable | deprecated
    TrustTier   string   // human-reviewed | machine-confirmed | unverified
    SourceFiles []string
    StaleAfter  string   // ISO 8601 date or ""
    VerifiedBy  string   // "human:X" or ""
}
```

### 3. OKF frontmatter fields read by brain-context (subset of full OKF spec)

```yaml
---
title: "Auth Handler — HTTP layer"
type: FileSummary           # required; drives staleness behavior
status: stable              # required: draft | stable | deprecated
trust_tier: human-reviewed  # optional; default: unverified
description: |              # optional; indexed for FTS
  Handles JWT validation and session creation.
tags: [auth, jwt, http]     # optional; indexed for FTS
source_files:               # optional; used for staleness check
  - internal/auth/handler.go
stale_after: "2027-03-01"   # optional ISO 8601; omit for no expiry
verified:                   # optional; social enforcement only
  by: human:techlead
  at: "2026-10-08T10:00:00Z"
---
```

Brain-context ignores all other OKF fields (attested computation, cross-bundle links, etc.).

### 4. `brain okf` CLI subcommand signature (main.go pattern)

```go
// In main() switch — same pattern as runJoin/runSolution
case "okf":
    err = runOKF(os.Args[2:])

func runOKF(args []string) error {
    if len(args) == 0 { /* print okf usage */ return nil }
    switch args[0] {
    case "register": return runOKFRegister(args[1:])
    case "generate": return runOKFGenerate(args[1:])
    case "list":     return runOKFList(args[1:])
    case "validate": return runOKFValidate(args[1:])
    default: /* print okf usage */ return nil
    }
}
```

---

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/config/config.go` | Modify | Add 4 optional fields to `ProjectConfig`; update `normalize()` and `Validate()` |
| `internal/okf/bundle.go` | Create | `Concept` struct, YAML frontmatter parser using `gopkg.in/yaml.v3` |
| `internal/okf/index.go` | Create | `Index` struct, `OpenIndex()`, `BuildIndex()` — SQLite FTS5 via `github.com/mattn/go-sqlite3` |
| `internal/okf/search.go` | Create | `Query()` — FTS5 MATCH, staleness split, BM25 threshold gate, `formatOKFResult()` |
| `internal/okf/staleness.go` | Create | `CheckStaleness()` — cross-reference concept source_files vs `DiffResult` |
| `internal/okf/generator.go` | Create | `GenerateDrafts()` — read snapshot file list, write per-file draft `.md` files |
| `internal/mcp/server.go` | Modify | Add `okfIndex *okf.Index` field; call `okf.OpenIndex(cfg.Projects)` in `New()` |
| `internal/mcp/tools.go` | Modify | Add OKF gate in `handleSearchProjectContext` and `handleExplainFlow` |
| `cmd/brain/main.go` | Modify | Add `case "okf"` in switch + `runOKF`, `runOKFRegister`, `runOKFGenerate`, `runOKFList`, `runOKFValidate`; add OKF post-hook at end of `runIndexCommand` |

---

## Testing Strategy

The project uses `package mcp` white-box tests (direct `Server{}` struct construction), `httptest.Server` for HTTP mocking, and table-driven cases. No interface injection — dependency substitution is done by constructing the struct directly with test values.

| Layer | What to Test | Approach |
|-------|-------------|----------|
| `internal/okf` unit | `BuildIndex` + `Query` roundtrip | `t.TempDir()` SQLite DB; table-driven cases for threshold, staleness split, trust tier ranking |
| `internal/okf` unit | `CheckStaleness` | Construct a fake `DiffResult` + fake bundle with known `source_files`; assert matching concept IDs returned |
| `internal/okf` unit | Frontmatter parser | Table-driven: valid YAML, missing `type`, missing `title`, unknown status values |
| `internal/okf` unit | `GenerateDrafts` idempotency | Run twice; assert stable concepts not overwritten; assert draft count = new files only |
| `internal/mcp` unit | OKF gate in `handleSearchProjectContext` | Construct `Server{okfIndex: fakeIndex}` (package-level `newOKFIndexFn` var for test injection, same pattern as `fakeSearchServer` in `solution_test.go`); assert OKF result returned when score >= threshold, RAG called when below |
| `internal/mcp` unit | `okfIndex == nil` guard | Construct `Server{okfIndex: nil}`; assert falls through to RAG without panic |
| `internal/config` unit | New fields normalize/validate | Table-driven: `OKFThreshold` out of range, fields with extra whitespace |
| CLI integration | `runOKFRegister` flag parsing | Test missing `--bundle`, missing `--project`, non-existent path; no actual SQLite needed (mock `BuildIndex` via package-level var) |

---

## Migration / Rollout

No migration required. All changes are additive:
- New `ProjectConfig` fields are `omitempty` — existing configs load without change.
- `okfIndex` is `nil` if no bundles are registered — all OKF code paths are guarded by `if s.okfIndex != nil`.
- New `internal/okf/` package is only imported by `cmd/brain/main.go` and `internal/mcp/server.go` — no circular deps.
- `brain update` post-hook is conditional on `project.OKFBundlePath != ""` — zero impact on existing usage.

---

## Open Questions

- None blocking V1. All key decisions were resolved in the exploration phase.
