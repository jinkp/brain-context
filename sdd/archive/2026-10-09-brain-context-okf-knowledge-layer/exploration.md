# Exploration: OKF Knowledge Layer — Operational Scenario Analysis

**Change**: `brain-context-okf-knowledge-layer`
**Date**: 2026-10-08
**Author**: sdd-explore agent
**Mode**: hybrid (Engram + filesystem)

---

## Current State

The `brain-context` CLI is a code-RAG indexing and search system.
Key structural facts derived from reading the actual codebase:

### Indexing Pipeline (`brain update`)
- `cmd/brain/main.go::runIndexCommand` drives the full pipeline
- Scan → Chunk → Diff against `~/.brain/snapshots/{project_id}.json` → Embed → Upload
- `internal/indexer/hashing.go`: `Snapshot` (per-file: `ContentHash` + `Chunks` set), `DiffResult` (NewFiles/Modified/Deleted at both file and chunk level)
- Snapshot is purely local (on the Tech Lead's machine), never uploaded
- No webhook, no CI trigger, no watch mode — `brain update` is ALWAYS manual

### Store (`internal/store/store.go`)
- PostgreSQL with `pgxpool`; multi-tenant via `SET app.tenant_id` RLS
- Tables: `tenants`, `projects`, `api_keys`, `project_files`, `chunks`, `vectors`, `index_jobs`, `query_logs`
- No OKF-related tables yet (purely additive opportunity)
- Transactions are first-class (`withTx` + `ContextWithTx`)

### MCP Server (`internal/mcp/`)
- `Server` struct holds `projectsByID`, `projectsByName`, `solutionsByName` — loaded once at startup from `~/.brain/config.yaml`
- `resolveProject`: input → try UUID first → fallback to name (case-insensitive)
- ALL tool calls go to `POST /api/projects/{id}/context/search` — pure HTTP to backend
- No local OKF query path exists today
- `searchContext` is the core function called by `handleSearchProjectContext` and `handleExplainFlow`
- Smart solution fan-out (2-pass probe + deep) exists in `solutionFanOut`

### Config (`internal/config/config.go`)
- Config at `~/.brain/config.yaml`
- `ProjectConfig` has: `ProjectID`, `ProjectToken`, `MCPReadKey`, `RepoPath`, `EmbedModel`, `EmbedAPIKey`, `EmbedDimensions`, `Exclude`, `Include`
- **No OKF bundle path field exists yet** — needs to be added
- Config is loaded synchronously; MCP server reads it once at process start

### CLI Commands (existing)
`login`, `register`, `index`, `update`, `scan`, `mcp`, `projects`, `tui`, `tokens`, `invite`, `join`, `metrics`, `solution`

**No `okf` subcommand exists yet.**

---

## Affected Areas

| Area | Status | Why |
|------|--------|-----|
| `internal/config/config.go` | Modified | Add `OKFBundlePath string` + `OKFVisibility string` to `ProjectConfig` |
| `internal/okf/` | New package | Bundle reader, FTS indexer, search, draft generator |
| `internal/store/` | Modified | New `okf_concepts` table + migration; OKF store methods |
| `internal/api/` | Modified | New routes: `POST /okf/search`, `POST /okf/sync`, `GET /okf/bundles` |
| `internal/mcp/tools.go` | Modified | Orchestration: OKF-first query wrapping `searchContext` |
| `internal/mcp/server.go` | Modified | Server struct gets OKF state; MCP startup reads registered bundles |
| `cmd/brain/main.go` | Modified | New `brain okf` subcommand (register, generate, sync, list) |
| `~/.brain/config.yaml` | Schema Extended | Per-project OKF config fields |
| `sdd/brain-context-okf-knowledge-layer/` | Change folder | SDD artifacts |

---

## Scenario Analysis

---

### Scenario 1: First-Time Setup (Tech Lead Registers a Bundle)

**Command**: `brain okf register --bundle /path/to/okf-bundle --project my-app`

#### Happy Path
1. CLI loads `~/.brain/config.yaml` → resolves project by name
2. Validates `--bundle` path exists and is a directory
3. Scans for existing `.md` files in folder; validates OKF frontmatter where present
4. Writes bundle path + visibility (`local` default) into `config.yaml` under project
5. If `--shared` flag: marks bundle as `shared` in config; sync required separately
6. Prints confirmation

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| Bundle folder doesn't exist | `os.Stat` | Error: "bundle path does not exist" |
| Project not registered in config | `cfg.Projects[name]` lookup | Error: "run `brain register` first" |
| Bundle already registered to a different project | Config check | Error or `--force` flag to re-bind |
| Existing `.md` files have invalid frontmatter | YAML parse during scan | Warn (not fail) — files are skipped with warning count |
| No `type` field in frontmatter | Frontmatter validator | Warn — concept is unindexable until fixed |
| `~/.brain/config.yaml` not writable (permissions) | `os.WriteFile` error | Error with path |

#### Edge Cases
- **Empty bundle folder**: valid — creates an "empty bundle" registration. `brain okf generate` fills it later.
- **Bundle folder is a git repo**: fully valid (and recommended). No special handling needed.
- **Bundle folder inside the code repo**: technically possible but violates the separation principle. Should **warn** but not block.
- **Re-registering with a different path**: updates the config (no server-side change needed for local bundles). For shared bundles, stale server content from the old path could persist → needs explicit re-sync.
- **Windows path separators**: `filepath.Abs` handles this; store as forward slashes in config for portability.

#### Open Questions
- **Q1.1**: Should registration auto-trigger an index pass (building the local FTS index), or is that a separate step (`brain okf index`)?
  - Recommendation: auto-index on register (similar to how register prompts to run `brain index`)
- **Q1.2**: Should `local` vs `shared` be set at register time or sync time?
  - Recommendation: set at register time, override-able at sync time

---

### Scenario 2: First Generation (`brain okf generate`)

**Command**: `brain okf generate --project my-app [--output /path/to/bundle]`

#### Happy Path
1. Loads project config → resolves project ID + API endpoint
2. Queries RAG index via API (`GET /api/projects/{id}/context/search` with a broad set of queries, or a dedicated generation endpoint)
3. Clusters related chunks by file/symbol/domain
4. For each cluster, emits a `.md` file with OKF frontmatter: `status: draft`, `generated.by: brain-context/rag-index`, no `verified` field
5. Writes files to the bundle output folder
6. Reports: N files created, 0 conflicts

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| RAG index is empty | API returns 0 chunks | Error: "no indexed content found — run `brain index` first" |
| RAG index is stale (old data) | No detection without metadata | Warn if snapshot `UpdatedAt` is > 30 days old |
| Output folder already has files | `os.Stat` per file | **Default: skip existing; `--overwrite` flag to replace** |
| Disk space exhaustion | `os.WriteFile` error | Error with "disk space" hint |
| API unreachable | `net.Error` in HTTP client | Error: "cannot connect to brain-context API" |
| Token expired | HTTP 401 | Error: "run `brain login` to refresh" |

#### Edge Cases
- **What concepts get generated?** This is the KEY design question:
  - Option A: **Per-file** — one concept per source file (simple, predictable, but coarse)
  - Option B: **Per-symbol** — one concept per function/class/module (fine-grained, may produce hundreds)
  - Option C: **Per-cluster** — group related symbols by domain/feature (best quality, most complex to implement)
  - **Recommendation**: Start with per-file drafts (Option A). Cluster-based can be v2. Per-symbol creates noise.
- **Concept naming**: auto-generated title should be the file path or a readable summary, not a UUID
- **Idempotency**: running `brain okf generate` twice should NOT create duplicates — use filename based on source file path as stable ID
- **Mixed bundle**: bundle already has some human-curated concepts + empty slots → generator should only fill gaps, not overwrite `status: stable` files

#### Open Questions
- **Q2.1**: Does `generate` call the existing `/context/search` endpoint multiple times, or does it need a new backend endpoint (`POST /api/okf/generate`) that returns structured summaries?
  - The current `search` endpoint returns chunks with scores, not concept-ready summaries. A dedicated endpoint is likely needed.
- **Q2.2**: What is the generation granularity — per file, per symbol cluster, or semantic domain?
- **Q2.3**: Should `generate` write directly to the bundle folder, or output a staging area for review first?

---

### Scenario 3: Tech Lead Curation and Approval

**Workflow**: TL opens bundle folder in editor → edits `.md` files → adds `verified` block → changes `status: stable`

#### Happy Path
1. TL opens `internal/auth/handler.md` in editor
2. Reads the draft content, makes corrections
3. Adds:
   ```yaml
   verified:
     by: human:techlead
     at: 2026-10-08T10:00:00Z
   ```
4. Changes `status: draft` → `status: stable`
5. Saves file
6. Runs `brain okf sync --project my-app` to upload

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| TL sets `verified` but forgets to change `status` | Validation at sync/index time | Warn: "concept X has verified but status is still draft — auto-promoting to stable" OR error |
| TL accidentally deletes the frontmatter `---` block | YAML parse failure | Error at sync: "malformed frontmatter in file X" with line number |
| TL deletes `type` field | Validation at sync | Error: "missing required field: type in file X" |
| Two TLs edit the same file simultaneously | File system race (git merge conflict) | Bundle is git-managed → merge conflict markers → sync fails → TL resolves manually |
| Incorrect `by` format (not `human:*`) | Validation regex | Warn or error depending on strictness |

#### Edge Cases
- **Who can approve?** The system has NO enforcement mechanism for `verified.by` — it's just YAML text. Anyone can write `verified: { by: human:techlead }`.
  - **Cannot be enforced at file level.** Enforcement requires either:
    - (a) A commit hook that checks git author matches the `by` field
    - (b) A server-side check on sync (but server has no identity for the TL)
    - (c) **Accept social enforcement** — if it's in the TL's bundle folder (which only they control), that IS the signal
  - **Recommendation**: Accept social enforcement. Document that `verified.by` is a label, not a cryptographic proof.
- **Partial approval**: TL approves 3/10 concepts in a bundle. The other 7 remain `draft`. This is fully valid — mixed bundles work.
- **Downgrading**: TL demotes a `stable` concept back to `draft` (e.g., they realized it was wrong). This is valid. Server side: the concept should be re-uploaded on next sync with new status.
- **Bulk approval**: TL writes a script to approve all drafts. This is a valid power-user workflow. No special handling needed.

#### Open Questions
- **Q3.1**: Should `brain okf sync` enforce that only `stable` concepts are uploaded to the server, or can drafts also be uploaded?
  - **Recommendation**: Default to `stable` only for sync. Add `--include-drafts` flag for explicit intent.
- **Q3.2**: Should there be a `brain okf validate` command that checks all frontmatter without syncing?
  - **Recommendation**: YES. This makes the CI/pre-commit story possible.

---

### Scenario 4: Sync to Server (Shared Visibility)

**Command**: `brain okf sync --project my-app`

#### Happy Path
1. Loads project config → bundle path, project ID, project token
2. Reads all `.md` files from bundle folder
3. Validates frontmatter for each
4. Filters to `status: stable` only (default)
5. Serializes concepts as JSON array
6. `POST /api/okf/sync` with project token + payload
7. Server upserts concepts into `okf_concepts` table
8. Returns: N created, M updated, K deleted (concepts no longer in bundle)
9. CLI prints summary

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| Authentication error (expired project token) | HTTP 401 | Error: "run `brain tokens renew --project X`" |
| Network failure mid-sync | HTTP error mid-request | Error with retry hint — partial state is dangerous |
| Draft concept gets uploaded accidentally | Filter logic bug | Mitigation: server-side validation rejects `status: draft` unless flag set |
| Deleted local file → server concept orphaned | Sync does NOT track deletes by default | See edge case below |
| Bundle folder empty | No files to sync | Warning: "nothing to sync" |
| Server-side schema validation failure | HTTP 422 | Error with field name from response |

#### Edge Cases
- **Incremental vs full sync**: Should sync compute a diff or always do a full replace?
  - Full replace is simpler and safer (no drift). For large bundles (100+ concepts), incremental is worth it.
  - **Recommendation**: V1 = full replace. V2 = hash-based incremental.
- **Server concepts deleted locally**: If TL deletes `internal/auth/handler.md` locally and re-syncs, should the server concept be deleted?
  - **YES** — local bundle is the source of truth. Full replace sync handles this correctly.
  - If incremental: must explicitly track deletes.
- **Conflict: local `updated_at` < server `updated_at`**: Could happen if two TLs sync the same bundle from different machines.
  - **Recommendation**: Last-write-wins at sync time, but warn when server version is newer.
- **Large binary files accidentally in bundle**: Non-`.md` files should be silently ignored by the sync command.
- **`local-only` bundle accidentally synced**: The sync command must check the bundle's visibility setting in config before uploading.

#### Open Questions
- **Q4.1**: What is the exact API shape for `POST /api/okf/sync`? Batch upsert? Per-concept endpoint? Stream?
  - **Recommendation**: Batch upsert (array of concepts in body). Atomicity is important.
- **Q4.2**: Should sync be transactional (all-or-nothing) or best-effort?
  - **Recommendation**: Transactional on the server side. Client retries on error.
- **Q4.3**: Does the server need a bundle registry (which project has which bundles), or just a flat `okf_concepts` table?

---

### Scenario 5: Developer Queries MCP

**Tool call**: `search_project_context(project_id: "my-app", query: "how does auth work")`

#### Happy Path
1. `handleSearchProjectContext` → `resolveProject` → gets project config
2. **OKF layer** (new): query `okf_concepts` for "how does auth work"
   - Filter: `status = stable`, `trust_tier = human-reviewed`, `not deprecated`, `updated_at > stale_threshold`
   - Returns matching concepts ranked by FTS score
3. If OKF confidence ≥ threshold (e.g., 0.7):
   - Return OKF concept(s) as primary answer
   - Optionally append top RAG chunks as supplemental
4. If OKF misses or score < threshold:
   - Fall through to current `searchContext` (existing RAG path)
5. Return formatted result to MCP client

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| OKF returns stale concept (past `stale_after`) that system missed | `stale_after` not enforced | Filter by `updated_at < now() - stale_threshold`; staleness check must be server-side |
| OKF returns `deprecated` concept | Filter gap | Explicit `status != deprecated` filter — must not forget this |
| OKF and RAG contradict each other | Both returned simultaneously | This is a **fundamental tension**: OKF is curated/intentional, RAG is current/automated. When they conflict, OKF is "correct" for intent but may be outdated for implementation. |
| Confidence threshold miscalibrated (too high) | OKF always misses → always falls to RAG | Threshold is configurable; default 0.7 is a guess, needs tuning |
| Confidence threshold miscalibrated (too low) | Stale OKF concept returned when RAG would be better | Same — tuning required |
| OKF table empty (no concepts indexed) | Query returns 0 rows | Graceful fallback to RAG — no error |
| Server OKF query timeout | HTTP timeout (30s) | Fall back to RAG; log warning |

#### Edge Cases
- **Human-reviewed concept past `stale_after`**: The concept was curated by a TL but the code has since changed.
  - **Critical**: Does the `human-reviewed` trust tier STILL beat RAG even if stale?
  - **Decision required**: Two schools of thought:
    - (a) YES — human intent doesn't expire. A curated architectural decision is still valid even if the code changed.
    - (b) NO — a stale concept is worse than fresh RAG because it's misleading.
  - **Recommendation**: Split by concept type:
    - `type: requirement` / `type: decision` / `type: playbook` → stale_after is advisory only; still returned with staleness warning
    - `type: implementation` / `type: file-summary` → strictly filtered after stale_after; falls to RAG
- **Local-only bundle**: OKF layer for local bundles is NOT queried via the server API. It's queried locally.
  - This means the MCP server needs a **dual query path**: server-side OKF for shared bundles, local FTS for local-only bundles.
  - **This is the most architecturally complex part of the system.**
- **Multiple projects in a solution**: The solution fan-out adds OKF complexity — does each project's OKF get queried individually, or aggregated?

#### Open Questions
- **Q5.1**: What is the FTS mechanism for local-only bundles?
  - Options: SQLite FTS5 locally, BleveSearch, simple title/description grep
  - **Recommendation**: SQLite FTS5 in `~/.brain/okf/{bundle_id}.db` — consistent with existing snapshot pattern
- **Q5.2**: Does the MCP server call the backend API for OKF search (like it does for RAG), or does it query locally?
  - For **shared** bundles: backend API call
  - For **local-only** bundles: local SQLite query
  - This dual-mode is a design commitment
- **Q5.3**: What does the response look like? How are OKF concepts formatted vs RAG chunks?
  - Recommendation: different header format: `## [OKF] Concept: Auth Architecture` vs `## Relevant Context`

---

### Scenario 6: Code Changes → Concept Regeneration

**Trigger**: Developer merges PR to Repo A. Tech Lead runs `brain update --project my-app`.

#### Happy Path
1. `brain update` runs the incremental indexer — computes `DiffResult` (new/modified/deleted files and chunks)
2. After indexing completes, a new step (not yet implemented): **OKF staleness check**
   - Compare `DiffResult.ModifiedFiles` against concept `source_files` references
   - For each matched concept: create new draft + mark old as `deprecated`
3. New draft file written to bundle folder with `status: draft`, `replaces: old-concept-id`
4. Tech Lead reviews the new draft and decides if the change is significant enough to update the concept

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| Old concept marked `deprecated` but TL hasn't seen new draft | No notification mechanism | **CRITICAL GAP** — see edge cases |
| Multiple code changes in short time → multiple draft versions accumulate | No deduplication | Bundle folder fills with draft files; TL confusion |
| `source_files` in concept frontmatter uses old paths (after file rename) | Path mismatch | No match found → concept NOT deprecated even though it should be; silent staleness |
| Concept doesn't have `source_files` field | No match possible | Concepts without `source_files` are immune to auto-deprecation |

#### Edge Cases
- **Notification gap**: When old concept is deprecated and new draft created, the Tech Lead has NO signal unless they:
  - Run `brain okf status` to see new drafts
  - See it in `brain okf list --status draft`
  - OR the system sends a notification (out of scope for v1)
  - **Recommendation**: `brain update` should print a summary: "3 OKF concepts may be stale — run `brain okf list --stale`"

- **Minor vs major change — should BOTH trigger regeneration?**
  - Rename a variable → `ContentHash` changes → file appears as `ModifiedFiles` → triggers staleness check
  - Major module restructure → multiple files change → triggers staleness check
  - The `DiffResult` doesn't distinguish "significance" — it's binary (changed/not changed)
  - **Decision needed**: Use file-level diff (current) or chunk-level diff for more granularity?
  - **Recommendation V1**: File-level. If a concept's source file is in `ModifiedFiles`, mark concept as potentially stale. The TL decides if the change is significant.
  - **Future**: Add a `significance_threshold` to ignore single-line changes

- **`brain update` does NOT currently hook into OKF at all** — the update pipeline ends at upload. OKF integration requires a new post-upload hook or a separate `brain okf check` command.
  - **Recommendation**: Add a post-index hook: if OKF bundles are registered for the project, run the staleness check automatically after indexing completes.

#### Open Questions
- **Q6.1**: Should `brain update` automatically run the OKF staleness check, or should it be a separate `brain okf check --project X` command?
  - Recommendation: auto-run if bundles are registered (zero-friction for TL)
- **Q6.2**: What is the format of the `source_files` field in OKF frontmatter?
  - Recommendation: array of relative paths from repo root
  ```yaml
  source_files:
    - internal/auth/handler.go
    - internal/auth/middleware.go
  ```
- **Q6.3**: At what granularity do auto-generated concepts reference source files? Per file? Per symbol?

---

### Scenario 7: Local-Only Bundle (Private, Not Synced)

**Use case**: TL has an organizational bundle with team management notes, sensitive decisions, or confidential playbooks.

#### Happy Path
1. Bundle registered with `--visibility local` (or default)
2. Bundle lives in TL's home directory or private git repo
3. MCP on TL's machine queries local bundle (local FTS)
4. Team members' MCP instances query only the server — local-only bundle is invisible to them
5. TL can use all `brain okf` commands locally: `register`, `generate`, `list` — but NOT `sync`

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| Local-only bundle accidentally synced | `brain okf sync` ignores visibility check | Must check `OKFVisibility != "local"` before uploading |
| TL runs `brain okf sync --all` without knowing local bundles are included | No filtering in `--all` mode | Must ALWAYS skip local bundles in sync, even with `--all` |
| Team member queries MCP and gets no OKF results (because all bundles are local-only) | Expected behavior, but confusing | Should print hint in MCP result: "No shared OKF bundles registered for this project" |
| Local bundle corrupted (disk failure, bad edit) | Local FTS index out of sync | `brain okf reindex --project X` re-reads files and rebuilds local FTS |

#### Edge Cases
- **TL is offline**: Local MCP still works (queries local SQLite). Team MCP queries server (no local content, as expected).
- **TL's machine changes (new laptop)**: Local-only bundle is NOT backed up to server → needs manual migration. This is a user education issue, not a system bug. Document clearly.
- **Partial local + shared**: TL has two bundles for the same project — one `local`, one `shared`. Both should be queried locally; only `shared` is synced to server. Team gets `shared` content via MCP.
- **Local bundle path changes** (TL moves the folder): Config update required; `brain okf register --bundle new-path` to re-bind.

#### Open Questions
- **Q7.1**: Where is the local OKF FTS index stored?
  - **Recommendation**: `~/.brain/okf/{project_id}/{bundle_id}.db` (SQLite FTS5)
- **Q7.2**: Should `local-only` bundles participate in `brain okf generate`?
  - **Recommendation**: YES. Drafts can be generated for any bundle; visibility only affects sync.

---

### Scenario 8: Multiple Bundles for the Same Project

**Setup**: Project `my-app` has:
- `tech-bundle` (shared): architecture, ADRs, playbooks
- `org-bundle` (local): team processes, HR-adjacent decisions

#### Happy Path
1. MCP receives query
2. Queries both bundles: server API for `tech-bundle`, local SQLite for `org-bundle`
3. Merges results, ranked by trust tier + FTS score
4. Returns combined result

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| Two bundles have conflicting concepts (same topic, different answers) | Semantic conflict — not detectable algorithmically | Both returned; LLM sees the conflict; unclear which to trust |
| One bundle stale, other fresh | Staleness filter | Stale bundle's concepts filtered out → only fresh one returned |
| Bundle ordering unclear | Non-deterministic merge | Sort by: trust_tier desc → fts_score desc → bundle_name asc |

#### Edge Cases
- **Priority between bundles**: When both match, which concept wins?
  - **Rule**: `human-reviewed` beats `machine-confirmed` beats `unverified`. Within same tier: FTS score wins. Within same score: **the bundle registered first** (deterministic tiebreaker).
  - This must be documented clearly.
- **Contradicting concepts, same trust tier**: Both concepts are `stable` + `human-reviewed` but disagree.
  - **No automatic resolution.** Return both with a warning header: `[Note: Multiple OKF concepts match this query — review both]`
  - This surfaces the conflict to the LLM and ultimately to the developer.
- **Solution (multi-project) + multiple bundles**: Fan-out means N projects × M bundles → N×M queries. Must be parallelized and time-bounded.

#### Open Questions
- **Q8.1**: Should there be a per-bundle `priority` field in config to resolve ties explicitly?
  - Recommendation: YES, as an optional `okf_bundle_priority: 1` config field (lower = higher priority)
- **Q8.2**: Should the response clearly indicate WHICH bundle each concept came from?
  - Recommendation: YES. Add `[Bundle: tech-bundle]` tag to concept header in MCP response.

---

### Scenario 9: OKF Concept References Code That No Longer Exists

**Setup**: Concept `auth-handler.md` has `source_files: [internal/auth/handler.go]`, but that file was deleted in a refactor.

#### Happy Path (broken reference detected)
1. `brain okf sync` runs pre-upload validation
2. Reads `source_files` from each concept
3. Checks if referenced files exist in current snapshot
4. Files not in snapshot → warns: "Concept X references deleted file internal/auth/handler.go"
5. Concept is still uploaded BUT flagged with `has_broken_refs: true`

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| Broken link in concept body (Markdown `[see](internal/auth/handler.go#L42)`) | Not validated | Link is broken; no detection without full Markdown parsing |
| Stale reference in frontmatter `source_files` | Sync validation (only if implemented) | Warning with filename + concept name |
| Misleading context returned (stale concept + deleted code) | OKF query returns concept; developer confused | Concept should be flagged as "potentially stale" in response |
| Nobody notices for months | No active monitoring | Risk: outdated architectural decisions silently misleading the team |

#### Edge Cases
- **File renamed (not deleted)**: `internal/auth/handler.go` → `internal/auth/http_handler.go`
  - The reference `source_files: [internal/auth/handler.go]` is now broken even though the code exists
  - `DiffResult.DeletedFiles` will include the old path → triggers staleness check
  - New draft should be generated with updated `source_files`
- **`brain okf sync` validation**: Should sync BLOCK on broken refs, or just warn?
  - **Recommendation**: Warn only (never block). TL may intentionally keep a historical concept that references deleted code.
- **Should `brain update` detect this?**: YES — when `DiffResult.DeletedFiles` contains a file referenced by an OKF concept, auto-deprecate the concept.

#### Open Questions
- **Q9.1**: Should `brain okf validate` check both frontmatter validity AND source_files existence?
  - **Recommendation**: YES. Two validation modes: `--frontmatter` and `--refs`.
- **Q9.2**: Should the sync command embed a file existence check against the latest snapshot?
  - **Recommendation**: YES. Cross-reference with `~/.brain/snapshots/{project_id}.json`.

---

### Scenario 10: Rollback / Disaster Recovery

**Scenario A: Bundle corrupted locally**
- `.md` files have invalid YAML
- Local FTS index out of sync with files

**Scenario B: Bundle accidentally deleted**

**Scenario C: Server `okf_concepts` table corrupted or accidentally wiped**

#### Happy Path (recovery)
- **Local corruption**: `brain okf reindex --project X` re-reads all `.md` files, rebuilds local FTS, skips malformed files with warnings
- **Local deletion**: If bundle is git-managed, `git checkout .` restores it. If not: no recovery (files gone). Server has the shared concepts as backup.
- **Server wipe**: `brain okf sync --project X` re-uploads all local stable concepts. Local bundle IS the source of truth.

#### Failure Modes
| Failure | Detection Point | Recovery |
|---------|----------------|----------|
| Re-registering bundle without history | `brain okf register` creates a fresh binding; old bundle_id unknown | Server concepts from old bundle_id become orphans |
| Server state diverges from local | Incremental sync skips re-upload of unchanged files | Force full sync: `brain okf sync --force` |
| Local bundle is git repo → force pushed → history lost | Git behavior, not brain's problem | Document: always use protected branches for OKF bundles |
| OKF index job fails mid-sync | Server-side incomplete state | Need idempotent upsert on server; re-syncing is safe |

#### Edge Cases
- **Source of truth is ALWAYS the local bundle**. The server is a mirror. This must be enforced architecturally.
  - The sync API must support full-replace semantics (not just incremental patch)
  - Server must never be the ONLY copy of OKF data
- **Re-registering after disaster**: If bundle folder + config are lost, the Tech Lead must:
  1. Restore bundle from git (or create new)
  2. `brain okf register --bundle <path> --project X`
  3. Server orphan concepts from the old bundle_id remain; need a `brain okf purge --project X --old-bundle` cleanup command

#### Open Questions
- **Q10.1**: Should concepts have a stable `concept_id` (content-hash or filename-based) that persists across re-registrations?
  - **Recommendation**: YES. Use the filename (without extension) as the stable concept ID. A concept named `auth-architecture.md` always has ID `auth-architecture`.
- **Q10.2**: What is the disaster recovery story for local-only bundles?
  - **Recommendation**: Strongly recommend TL uses git for local-only bundles too (private repo).

---

## Codebase Integration Analysis

### How `brain update` works today (critical for Scenario 6)

```
runUpdate → runIndexCommand(incremental=true)
  → Load config
  → Validate project config (RepoPath, ProjectToken, etc.)
  → scan.Walk(repoPath) → files
  → chunker.ChunkFiles(files) → chunks
  → indexer.LoadSnapshot(projectID) → previous state
  → indexer.Diff(snapshot, files, chunksByFile) → DiffResult
  → embed only changed chunks
  → upload to server via /api/projects/{id}/upload
  → indexer.SaveSnapshot(projectID, files, chunksByFile)
```

**OKF integration point**: After `SaveSnapshot`, if OKF bundles are registered, run:
```
if bundle, ok := cfg.Projects[projectName].OKFBundle; ok {
    okf.CheckStaleness(bundle, diff) → []staleConceptIDs
    okf.MarkDeprecated(staleConceptIDs) → []newDraftPaths
    if len(newDraftPaths) > 0 {
        fmt.Printf("⚠️  %d OKF concept(s) may be stale — run `brain okf list --stale`\n", len(newDraftPaths))
    }
}
```

### How `resolveProject` works in MCP (critical for Scenario 5)

```go
func (s *Server) resolveProject(req) (projectID, ProjectConfig, error) {
    input = req.GetString("project_id") || s.defaultProjectID
    // Try UUID → try name (case-insensitive)
    // Returns: (projectID string, ProjectConfig, error)
}
```

**OKF integration point**: After resolving the project, before calling `searchContext`:
```go
func (s *Server) handleSearchProjectContext(ctx, req) {
    projectID, project, err := s.resolveProject(req)
    
    // NEW: try OKF first
    if s.okfEnabled {
        okfResult, confidence := s.queryOKF(ctx, projectID, query)
        if confidence >= s.okfThreshold {
            return textResult(formatOKFResult(okfResult)), nil
        }
    }
    
    // Existing RAG path
    result, err := s.searchContext(ctx, projectID, project, query, maxChunks, false)
    ...
}
```

The `Server` struct needs new fields:
```go
type Server struct {
    // existing fields...
    okfEnabled   bool
    okfThreshold float64
    okfBundles   map[string][]OKFBundle // projectID → bundles
}
```

### Config Extension Required

```go
type ProjectConfig struct {
    // existing fields...
    OKFBundlePath   string `yaml:"okf_bundle_path,omitempty"`
    OKFVisibility   string `yaml:"okf_visibility,omitempty"` // "local" | "shared"
    OKFBundleID     string `yaml:"okf_bundle_id,omitempty"`  // server-assigned on first sync
}
```

---

## Risk Summary

| Risk | Severity | Likelihood | Notes |
|------|----------|------------|-------|
| **Dual query path** (local + server OKF) adds complexity to MCP server | HIGH | CERTAIN | Server struct needs dual-mode OKF query logic |
| **No notification for stale concepts** after code change | HIGH | HIGH | TL has no push signal; must actively run `brain okf list --stale` |
| **Verification can't be enforced** (YAML is editable by anyone) | MED | HIGH | Social enforcement only; document clearly |
| **Confidence threshold miscalibration** (OKF too aggressive or too quiet) | MED | HIGH | Needs empirical tuning; default 0.7 is a guess |
| **Concept-code drift** (source_files references become stale silently) | MED | MED | `brain update` post-hook mitigates; `brain okf validate --refs` for manual check |
| **Multiple drafts accumulate** without TL review | MED | MED | `brain okf list --status draft` shows backlog; UI/UX problem more than technical |
| **Local-only bundle recovery** without git backup | MED | LOW | User education; recommend git for all bundles |
| **OKF vs RAG contradiction** with no resolution strategy | MED | LOW | Conceptual (intent) vs implementation split helps; split by concept type |
| **Server source-of-truth creep** (incremental sync skips re-uploads) | LOW | LOW | Always support `--force` full-replace sync |
| **Bundle folder inside code repo** | LOW | LOW | Warn at register time; not fatal |

---

## Approaches

### Approach 1: Local-FTS + Server-API Dual Mode (Recommended)
- Local bundles: SQLite FTS5 at `~/.brain/okf/{project_id}/{bundle_id}.db`
- Shared bundles: server-side `okf_concepts` table + `POST /api/okf/search`
- MCP server queries local first, then server
- **Pros**: Full privacy for local bundles; consistent with existing `~/.brain/snapshots/` pattern; no server dependency for local queries
- **Cons**: MCP server must manage SQLite connection + HTTP; more complex than server-only
- **Effort**: High

### Approach 2: Server-Only (Simpler)
- All OKF bundles, including "local", are uploaded to server but with `visibility: private`
- Server enforces visibility at query time (tenant-scoped)
- Local-only = server-private
- **Pros**: Single query path; simpler MCP; easier ops
- **Cons**: "local-only" content still transits the network and touches the server; breaks the privacy model; requires auth to query even "local" concepts
- **Effort**: Medium

### Approach 3: Local-Only No Server (Simplest MVP)
- V1: ALL OKF bundles are local-only; no server sync
- MCP server queries local SQLite only
- No `okf sync`, no server changes
- **Pros**: Extremely simple; no backend work needed; proves the MCP orchestration value
- **Cons**: Team members can't share OKF context; limits value for team workflows
- **Effort**: Low (MVP)

### Recommendation
**Approach 3 for V1, Approach 1 for V2.**

Start with local-only to validate the OKF-first MCP orchestration pattern without the complexity of server sync. The local SQLite FTS5 pattern is sufficient to prove value. Server sync (Approach 1) is the natural V2 after validating the workflow.

---

## Open Questions Summary

| ID | Question | Priority | Recommended Answer |
|----|----------|----------|--------------------|
| Q2.1 | Generation: use existing search endpoint or new `/okf/generate`? | HIGH | New dedicated endpoint |
| Q2.2 | Generation granularity: per file, per symbol, per cluster? | HIGH | Per-file for V1 |
| Q5.1 | Local FTS mechanism? | HIGH | SQLite FTS5 |
| Q5.2 | Dual-mode MCP query (local + server)? | HIGH | Yes; start with local-only V1 |
| Q6.1 | OKF staleness check: auto on `brain update` or separate command? | HIGH | Auto on `brain update` if bundles registered |
| Q3.1 | Sync: only stable, or include drafts with flag? | MED | Stable only; `--include-drafts` flag |
| Q4.1 | Sync API shape: batch upsert or per-concept? | MED | Batch upsert (atomic) |
| Q5.3 | OKF vs RAG response format? | MED | Different header prefix `[OKF]` |
| Q6.2 | `source_files` frontmatter format? | MED | Array of repo-relative paths |
| Q1.1 | Auto-index on register? | LOW | Yes, like `brain index` after `brain register` |
| Q3.2 | `brain okf validate` command? | LOW | Yes, for CI/pre-commit |
| Q8.1 | Per-bundle priority field? | LOW | Optional; `okf_bundle_priority: 1` |
| Q10.1 | Stable concept ID based on filename? | LOW | Yes, filename without extension |

---

## Ready for Proposal

**YES** — with the following resolved decisions:
1. V1 is local-only (no server sync). Validates OKF-first MCP orchestration pattern with minimal risk.
2. Generation granularity: per-file drafts.
3. Local FTS: SQLite FTS5 at `~/.brain/okf/{project_id}/`.
4. Staleness check: post-`brain update` hook, prints stale count.
5. Verification enforcement: social only (YAML is trust-on-write).

---

## Affected Files Summary

- `cmd/brain/main.go` — Add `brain okf` subcommand (register, generate, list, validate, [sync for V2])
- `internal/config/config.go` — Add `OKFBundlePath`, `OKFVisibility`, `OKFBundleID` to `ProjectConfig`
- `internal/okf/` — New package: `bundle.go`, `frontmatter.go`, `index.go` (SQLite FTS5), `search.go`, `generator.go`, `staleness.go`
- `internal/mcp/server.go` — Add `okfBundles`, `okfEnabled`, `okfThreshold` to `Server` struct; load OKF state at startup
- `internal/mcp/tools.go` — Wrap `searchContext` with OKF-first logic; add `queryOKF` function
- `internal/store/` — (V2 only) `okf_concepts` table + migration + store methods
- `internal/api/` — (V2 only) `POST /api/okf/search`, `POST /api/okf/sync`, `GET /api/okf/bundles`
- `~/.brain/config.yaml` — Schema extended (backward-compatible: new optional fields)
- `~/.brain/okf/{project_id}/` — New local storage for OKF SQLite indexes
