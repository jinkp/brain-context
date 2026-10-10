# brain-context

> CLI-first code context indexing and knowledge layer for AI-assisted development.

![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go) ![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)

## What is brain-context?

`brain-context` solves a common problem in AI-assisted coding: LLM tools need relevant context, but scanning entire repositories is slow, expensive, and often misses the **intent** behind the code. brain-context provides two complementary layers:

1. **RAG Code Index** — indexes repositories locally, generates embeddings, uploads only vectors and metadata to a cloud API, and exposes an MCP server for semantic code search.
2. **OKF Knowledge Layer** — a curated local knowledge layer over the code index. Requirements, architecture decisions, playbooks, and team documentation are indexed separately and queried first — reducing token cost and improving precision for intent-level questions.

Raw source code is **never uploaded**. Only vectors and metadata leave the developer machine.

---

## Architecture

### Full System (with OKF Knowledge Layer)

```text
┌──────────────────────────── Developer/Tech Lead Machine ──────────────────────────────┐
│                                                                                        │
│  Repos registered in brain-context:                                                    │
│  ┌─────────────────────────┐   ┌──────────────────────────────────────────────────┐   │
│  │  Code Repo (my-app)     │   │  OKF Bundle (my-knowledge/)                      │   │
│  │  /path/to/code          │   │  /path/to/knowledge  ← git repo or plain folder  │   │
│  │  ├─ src/                │   │  ├─ requirements/auth.md   [type: Requirement]    │   │
│  │  ├─ internal/           │   │  ├─ decisions/jwt-vs-session.md [type: Decision]  │   │
│  │  └─ ...                 │   │  ├─ playbooks/incident.md   [type: Playbook]      │   │
│  └─────────────────────────┘   │  └─ architecture/overview.md [type: Architecture] │   │
│           │                    └───────────────────┬──────────────────────────────┘   │
│           │ brain index/update                     │ brain okf register/generate      │
│           ▼                                        ▼                                  │
│  ┌─────────────────────────────────────────────────────────────────────────────────┐  │
│  │  brain CLI (embedded MCP server)                                                │  │
│  │                                                                                 │  │
│  │  ~/.brain/                                                                      │  │
│  │  ├─ config.yaml              ← project + OKF bundle registration                │  │
│  │  ├─ snapshots/{id}.json      ← local hash diff state (code indexing)            │  │
│  │  └─ okf/{project}/{bundle}.db ← SQLite FTS5 index (OKF knowledge layer)        │  │
│  │                                                                                 │  │
│  │  MCP query flow:                                                                │  │
│  │  1. Query OKF SQLite FTS5 index (local, fast, curated)                         │  │
│  │     → if score ≥ threshold → return OKF concept (skip RAG)                     │  │
│  │  2. Query RAG (backend API) — fallback when OKF misses                         │  │
│  └────────────────────┬────────────────────────────────────────────────────────────┘  │
│                       │ vectors + metadata only (code)                                │
│                       │ raw code NEVER leaves machine                                 │
└───────────────────────┼────────────────────────────────────────────────────────────────┘
                        ▼
┌──────────────────── Context API (Cloud Run) ─────────────────────────────────────────┐
│  Go + Echo                                                                            │
│  ├─ auth (tenant / project / mcp_read tokens)                                        │
│  ├─ server-side query embedding (embed key stored encrypted, never sent to clients)  │
│  ├─ retrieval: ANN search (HNSW) + BM25 rerank                                       │
│  └─ PostgreSQL + pgvector (multi-tenant RLS)                                         │
└──────────────────────────────────────────────────────────────────────────────────────┘
                        │
                        ▼
┌──────────────────── AI Clients ──────────────────────────────────────────────────────┐
│  Claude Code | Cursor | OpenCode | Gemini CLI | Windsurf                             │
│  MCP tools: search, explain, summaries, related files, impact                        │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

### Two-Actor Model

brain-context is designed for teams with two distinct roles:

| Role | Repos | Responsibilities |
|---|---|---|
| **Tech Lead** | Code repo + OKF bundle | Registers/curates the knowledge bundle. Validates that code matches requirements. Has full CLI access. |
| **Developer** | Code repo (via `brain join`) | Reads code context through MCP. Benefits passively from curated OKF content the Tech Lead publishes (V2). |

---

## Quick Start

### 1. Install

Linux/macOS:
```bash
curl -fsSL https://raw.githubusercontent.com/jinkp/brain-context/master/install.sh | bash
```

Windows PowerShell:
```powershell
irm https://raw.githubusercontent.com/jinkp/brain-context/master/install.ps1 | iex
```

### 2. Login

```bash
brain login --token brn_tenant_xxx --api https://api.brain-context.dev
```

### 3. Register and index your code repo

```bash
brain register \
  --project my-app \
  --repo /path/to/code \
  --embedder openai \
  --api-key <key> \
  --model text-embedding-3-large

brain index --project my-app
```

### 4. Configure your AI client

```bash
brain setup opencode   # or: claude, cursor, gemini, windsurf, all
```

### 5. (Optional) Add the OKF knowledge layer

```bash
# Create your knowledge bundle folder (can be a git repo or plain folder)
mkdir /path/to/my-knowledge

# Register it as the OKF bundle for your project
brain okf register --project my-app --bundle /path/to/my-knowledge

# Auto-generate draft concepts from your indexed code
brain okf generate --project my-app

# Review and curate the drafts — add OKF frontmatter, set status: stable
# Then re-index so the MCP picks up the changes:
brain okf register --project my-app --bundle /path/to/my-knowledge
```

---

## Team Collaboration

### Inviting developers (Tech Lead → Developer)

```bash
# Tech Lead: create an invite code (single-use, 24h TTL by default)
brain invite --project my-app [--ttl 7d] [--max-uses 5]

# Developer: join the project
brain join --code brn_invite_xxx --api https://api.brain-context.dev

# Developer: configure their AI client
brain setup opencode
```

After `brain join`, the developer's local config contains only a scoped `mcp_read_key`. The embed API key is **never sent** to invited members — search queries are embedded server-side.

---

## OKF Knowledge Layer

### What is OKF?

OKF (Open Knowledge Format v0.2) is a minimal format for curated knowledge: a directory of Markdown files with YAML frontmatter. Each file is a **concept** — a requirement, architecture decision, playbook, or any structured knowledge that gives context to AI agents.

OKF complements the RAG code index:

| Layer | What it answers | Token cost | Precision |
|---|---|---|---|
| **OKF (knowledge)** | "Why was this designed this way?", "What are the requirements?" | Low (curated, compact) | High |
| **RAG (code)** | "How is this implemented?", "Where is this function?" | Higher (raw chunks) | Medium |

### MCP query flow

```
AI client → search_project_context(query)
  │
  ├─ 1. OKF FTS5 search (local SQLite)
  │     Filter: status=stable, not deprecated
  │     Rank by: trust tier → BM25 score
  │     Score ≥ threshold (default 0.7)?
  │         YES → return [OKF] concept (skip RAG entirely)
  │         NO  → fall through ↓
  │
  └─ 2. RAG search (backend API)
        Embed query server-side → ANN search → rerank → return chunks
```

### OKF concept format

```yaml
---
title: "Auth Flow — JWT Design"
type: Decision           # Requirement | Decision | Playbook | Architecture | FileSummary | APIEndpoint
status: stable           # draft | stable | deprecated
trust_tier: human-reviewed   # unverified | machine-confirmed | human-reviewed
description: "We use JWT over sessions because the API is stateless and deployed across multiple instances."
tags: [auth, jwt, architecture]
source_files:
  - internal/auth/handler.go
  - internal/auth/middleware.go
stale_after: "2027-06-01"    # optional — concept is advisory-stale after this date
verified:
  by: human:techlead
  at: "2026-10-09T10:00:00Z"
---

# Auth Flow — JWT Design

[... markdown content ...]
```

### Trust tiers

| `verified.by` value | Trust tier | MCP behavior |
|---|---|---|
| `human:<id>` | **human-reviewed** | Highest priority — returned before RAG |
| `process:<id>` or other | **machine-confirmed** | Medium priority |
| *(absent)* | **unverified** | Lowest — still returned if score is high enough |

### Staleness behavior by concept type

| Type | Past `stale_after` |
|---|---|
| `Requirement`, `Decision`, `Playbook`, `Architecture` | Included with `[⚠️ stale]` warning (intent doesn't expire) |
| `FileSummary`, `APIEndpoint` | Excluded — falls to RAG (implementation types go stale) |

### OKF bundle structure

```
my-knowledge/           ← git repo OR plain folder
  requirements/
    auth.md             ← type: Requirement
    payments.md
  decisions/
    jwt-vs-session.md   ← type: Decision
    db-choice.md
  playbooks/
    incident-response.md ← type: Playbook
  architecture/
    overview.md         ← type: Architecture
  index.md              ← optional: bundle directory listing
```

### After curating concepts

When you edit `.md` files in the bundle (promote drafts to stable, add `verified`, update content), re-run register to rebuild the SQLite index so the MCP picks up the changes:

```bash
brain okf register --project my-app --bundle /path/to/my-knowledge
```

> **V1 limitation**: Each project supports one OKF bundle. Multiple-bundle support (e.g., shared technical docs + private organizational docs) is planned for V2.

### OKF Threshold

The MCP returns OKF results when the FTS5 relevance score meets the threshold (default `0.7`). For small bundles (< 20 concepts), you may need a lower threshold:

```yaml
# ~/.brain/config.yaml
projects:
  my-app:
    okf_threshold: 0.1   # lower = more aggressive OKF matching
```

---

## CLI Reference

### Code indexing

| Command | Description |
|---|---|
| `brain login --token <key> [--api <url>]` | Authenticate against a tenant API. |
| `brain register --project <name> --repo <path> --embedder <gemini\|openai\|ollama> --api-key <key> --model <model>` | Register a repository and embedder. |
| `brain index --project <name>` | Initial full index. |
| `brain update --project <name>` | Incremental re-index (hash diff). |
| `brain scan --project <name>` | Preview files that would be indexed (dry-run). |
| `brain projects` | List registered projects. |
| `brain projects delete --project <name>` | Delete a project. |
| `brain projects rename --project <name> --new-name <new>` | Rename a project. |

### Team collaboration

| Command | Description |
|---|---|
| `brain invite --project <name> [--ttl 24h] [--max-uses 1]` | Generate an invite code for a team member. |
| `brain join --code <brn_invite_xxx> [--api <url>]` | Join a project as a read-only member. |

### OKF knowledge layer

| Command | Description |
|---|---|
| `brain okf register --project <name> --bundle <path>` | Register an OKF bundle and build the local FTS5 index. Run again after editing concepts to re-index. |
| `brain okf generate --project <name>` | Auto-generate draft concepts from the code index (requires snapshot from `brain index`). |
| `brain okf list --project <name> [--status draft\|stable\|deprecated] [--stale]` | List concepts with status, trust tier, and staleness. |
| `brain okf validate --project <name> [--refs]` | Validate frontmatter. With `--refs`: cross-check `source_files` against the code snapshot. |

### AI client setup

| Command | Description |
|---|---|
| `brain setup` | Interactive Bubbletea setup wizard. |
| `brain setup <client>` | Configure: `opencode`, `claude`, `cursor`, `gemini`, `windsurf`, or `all`. |
| `brain mcp [--project <name>]` | Run the embedded MCP server. |
| `brain version` | Print CLI version. |

---

## MCP Tools

| Tool | Description |
|---|---|
| `search_project_context` | Query the OKF knowledge layer first, then RAG. Returns curated concepts or ranked code chunks. |
| `explain_flow` | Explain a feature flow (login, payment, etc.) — OKF-first, then RAG. |
| `get_file_summary` | Return an indexed summary of symbols in a file. |
| `get_related_files` | Find files related by imports, calls, or shared symbols. |
| `find_impact` | Identify files and symbols affected by changing an entity. |
| `preview_scan` | Preview which files would be scanned. |
| `search_solution_context` | Fan-out search across multiple projects in a solution (multi-repo). |

---

## Embedder Models

| Provider | Model | Dimensions |
|---|---|---:|
| Gemini | `gemini-embedding-001` | 768 |
| Gemini | `text-embedding-004` | 768 |
| OpenAI | `text-embedding-3-large` | 1024 |
| OpenAI | `text-embedding-3-small` | 1024 |
| Ollama | `nomic-embed-text` | 768 |
| Ollama | `bge-m3` | 1024 |

---

## Filtering: Exclude and Include

brain-context supports multiple sources for controlling which files are indexed (additive, evaluated in order):

1. **Hardcoded ignores**: `node_modules`, `.git`, `dist`, `build`, `vendor`, binaries, images, lock files.
2. **Global config** (`~/.brain/config.yaml` top-level): applies to all projects.
3. **Per-project config**: applies to one project.
4. **`.brainignore`** in repo root: gitignore syntax, version-controlled.
5. **CLI flags** `--exclude` / `--include`: one-time override.

```yaml
# ~/.brain/config.yaml
exclude: [Migrations, assets]   # global
include: [".proto"]

projects:
  my-repo:
    exclude: [Tests]
    include: [".graphql"]
```

---

## Security

- Raw source code is **never uploaded** — only vectors and metadata.
- PostgreSQL Row-Level Security scopes every query by `tenant_id`.
- API keys and tokens stored as **bcrypt hashes**, never plaintext.
- Token types are separated by responsibility:
  - `brn_tenant_xxx` — admin
  - `brn_proj_xxx` — upload/write only
  - `brn_mcp_xxx` — MCP read only
- **Invite security boundary**: embed API key is stored encrypted server-side and never included in invite responses. Invited members get only a scoped `brn_mcp_xxx` token.
- All tokens expire after 90 days.
- Public endpoints (register, login, invite redeem) are rate-limited.
- Install scripts verify SHA256 checksums before installing.
- Tenant self-registration: disable via `BRAIN_REGISTRATION=closed`.

---

## Local Development

### Prerequisites

- Go 1.26+
- Docker + Docker Compose
- WSL2 (for running the API on Windows against Docker PostgreSQL)

### Start PostgreSQL + pgvector

```bash
docker compose up -d
# postgres://brain:brain@127.0.0.1:5433/brain_context?sslmode=disable
```

### Run the API

```bash
# From WSL2 (required on Windows due to pgx auth issue with Docker)
go run ./cmd/api
```

### Project structure

```text
brain-context/
├── cmd/
│   ├── brain/main.go       ← CLI entry point
│   └── api/main.go         ← API entry point
├── internal/
│   ├── api/                ← HTTP handlers
│   ├── auth/               ← token validation
│   ├── chunker/            ← code chunking
│   ├── config/             ← ~/.brain/config.yaml
│   ├── embedder/           ← Gemini / OpenAI / Ollama
│   ├── indexer/            ← hash diff + snapshot
│   ├── jobs/               ← background jobs
│   ├── mcp/                ← MCP server + OKF-first orchestration
│   ├── okf/                ← OKF knowledge layer (SQLite FTS5)
│   ├── parser/             ← code symbol extraction
│   ├── retriever/          ← ANN search + rerank
│   ├── scanner/            ← file discovery
│   ├── store/              ← PostgreSQL store + migrations
│   ├── tui/                ← Bubbletea setup wizard
│   └── uploader/           ← batch upload to API
├── CHANGELOG.md
├── Dockerfile.api
├── docker-compose.yml
├── install.sh
├── install.ps1
└── Makefile
```

---

## Deployment

- **Context API**: Cloud Run (stateless Go binary)
- **PostgreSQL**: Neon with `pgvector` extension

---

## Agent Instructions (IMPORTANT)

Add this to your project's `AGENTS.md` so your AI agent uses brain-context automatically:

```markdown
## brain-context

When answering questions about this project's code, ALWAYS use brain-context MCP tools FIRST
before reading files directly:

- `search_project_context(project_id="<project-name>", query="...")` — find relevant context (OKF knowledge + code)
- `explain_flow(project_id="<project-name>", query="...")` — trace a feature end-to-end
- `get_file_summary(project_id="<project-name>", path="...")` — understand a file structure
- `find_impact(project_id="<project-name>", entity="...")` — find what a change affects

Only read files directly if brain-context doesn't return enough context.
```

---

## Contributing

Open an issue before submitting a PR: https://github.com/jinkp/brain-context/issues
