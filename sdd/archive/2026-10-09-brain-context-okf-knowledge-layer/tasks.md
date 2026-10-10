# Tasks: brain-context-okf-knowledge-layer
<!-- V1 implementation complete: 2026-10-09 -->

## Phase 1: Foundation & Config

- [x] 1.1 Add `modernc.org/sqlite` dependency to `go.mod` via `go get modernc.org/sqlite`
- [x] 1.2 Create `internal/okf/bundle.go` — `Concept` struct with all frontmatter fields; YAML parser using `gopkg.in/yaml.v3`
- [x] 1.3 Create `internal/okf/` package — add file stubs: `index.go`, `search.go`, `staleness.go`, `generator.go`
- [x] 1.4 Extend `internal/config/config.go` — add 4 optional fields to `ProjectConfig`: `OKFBundlePath`, `OKFVisibility`, `OKFBundleID`, `OKFThreshold`
- [x] 1.5 Update `normalize()` in config — trim `OKFBundlePath`, `OKFVisibility`, `OKFBundleID`; validate `OKFThreshold` in [0.0, 1.0] if non-zero
- [x] 1.6 Update `Validate()` in config — test new fields in validation tests; ensure backward compatibility

## Phase 2: OKF Core Package

- [x] 2.1 Implement `internal/okf/index.go` — `Index` struct, `OpenIndex()` function; nil-safe design; open SQLite at `~/.brain/okf/{project_id}/{bundle_id}.db`
- [x] 2.2 Implement SQLite FTS5 schema in `index.go` — `concepts` virtual table + `concept_meta` metadata table per design
- [x] 2.3 Implement `internal/okf/index.go::BuildIndex()` — walk `.md` files, parse frontmatter, insert into FTS5; idempotent re-indexing
- [x] 2.4 Implement `internal/okf/search.go` — `Query()` function; FTS5 MATCH, staleness split (intent vs impl), BM25 threshold gate
- [x] 2.5 Implement `internal/okf/search.go::formatOKFResult()` — format matched concepts for MCP response; include `[OKF]` prefix + `[⚠️ stale]` warning if past `stale_after`
- [x] 2.6 Implement `internal/okf/staleness.go::CheckStaleness()` — cross-reference concept `source_files` vs `DiffResult` (modified + deleted files); return stale concept IDs
- [x] 2.7 Implement `internal/okf/generator.go::GenerateDrafts()` — read snapshot file list; write per-file draft `.md` files; idempotent (skip `status: stable` files)

## Phase 3: Integration & Wiring

- [x] 3.1 Modify `internal/mcp/server.go` — add `okfIndex *okf.Index` field to `Server` struct
- [x] 3.2 Modify `internal/mcp/server.go::New()` — call `okf.OpenIndex(cfg.Projects)` before return; handle nil gracefully
- [x] 3.3 Modify `internal/mcp/tools.go::handleSearchProjectContext()` — add OKF gate (3 lines): if `s.okfIndex != nil`, query OKF; return if score >= threshold
- [x] 3.4 Modify `internal/mcp/tools.go::handleExplainFlow()` — add same OKF gate as search
- [x] 3.5 Add `brain okf` subcommand in `cmd/brain/main.go` — switch case "okf" + dispatch to `runOKF()`
- [x] 3.6 Implement `cmd/brain/main.go::runOKFRegister()` — flags: `--bundle`, `--project`; validate path; write config; call `BuildIndex()`
- [x] 3.7 Implement `cmd/brain/main.go::runOKFGenerate()` — flags: `--project`, optional `--output`; load snapshot; generate drafts; print count
- [x] 3.8 Implement `cmd/brain/main.go::runOKFList()` — flags: `--project`, optional `--stale`; list concepts; filter by status
- [x] 3.9 Implement `cmd/brain/main.go::runOKFValidate()` — flags: `--project`, optional `--refs`; validate frontmatter + optional source_files existence
- [x] 3.10 Add post-hook in `cmd/brain/main.go::runIndexCommand()` — after `SaveSnapshot()`, if `project.OKFBundlePath != ""`, call `CheckStaleness()` + print stale count

## Phase 4: Unit Tests

- [x] 4.1 Create `internal/okf/bundle_test.go` — table-driven tests for Concept YAML parser; valid, missing `type`, missing `title`, unknown status
- [x] 4.2 Create `internal/okf/index_test.go` — `BuildIndex()` roundtrip with temp SQLite DB; verify FTS5 insert + retrieval
- [x] 4.3 Create `internal/okf/search_test.go` — `Query()` with fake index; test threshold gate, staleness split (intent vs impl), BM25 scoring
- [x] 4.4 Create `internal/okf/staleness_test.go` — `CheckStaleness()` with fake `DiffResult` + fake bundle; verify concept ID matching
- [x] 4.5 Create `internal/okf/generator_test.go` — `GenerateDrafts()` idempotency; run twice, verify stable concepts not overwritten
- [x] 4.6 Create `internal/config/okf_config_test.go` — test new fields normalize/validate; `OKFThreshold` out of range, whitespace trim, backward compat, omitempty
- [x] 4.7 Create `internal/mcp/okf_test.go` — test OKF gate in `handleSearchProjectContext`; real Index, RAG not called when OKF hits
- [x] 4.8 Create `internal/mcp/okf_test.go` — test nil guard: `Server{okfIndex: nil}` falls through to RAG without panic; also covers `handleExplainFlow`
- [x] 4.9 Create `cmd/brain/okf_test.go` — flag parsing tests for `runOKFRegister`, `runOKFGenerate`, `runOKFList`, `runOKFValidate`; missing flags, non-existent path, valid call
- [x] 4.10 Create `cmd/brain/integration_test.go` — lightweight routing: `runOKF` subcommand dispatch, missing flags, unknown commands

## Phase 5: Cleanup & Docs

- [x] 5.1 Update `internal/okf/` package comment — document nil-safety; all methods no-op when receiver is nil
- [x] 5.2 Add inline documentation to `OKF` gate in `tools.go` — explain OKF-first orchestration strategy
- [x] 5.3 Add CLI usage strings to `brain okf` subcommand — register, generate, list, validate commands (already present from Phase 3)
- [x] 5.4 Update `sdd/brain-context-okf-knowledge-layer/tasks.md` status — mark all tasks complete, update summary
- [x] 5.5 Create or update CHANGELOG entry — document OKF knowledge layer feature for V1 (local-only)
- [x] 5.6 Verify no regression — run existing MCP tests; confirm RAG path untouched when OKF is disabled
