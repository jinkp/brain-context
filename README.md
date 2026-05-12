# brain-context

> CLI-first code context indexing for AI-assisted development.

![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go) ![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)

## What is brain-context?

`brain-context` solves a common problem in AI-assisted coding: LLM tools need relevant code context, but scanning entire repositories is slow, expensive, and risky. `brain-context` indexes repositories locally, generates embeddings with Gemini, OpenAI, or Ollama, uploads only vectors and metadata to a cloud API, and exposes an MCP server so clients like Claude, Cursor, OpenCode, Gemini CLI, and Windsurf can retrieve focused context without ever sending raw source code off the developer machine.

## Architecture

```text
┌──────────────────────────────── Developer Machine ────────────────────────────────┐
│                                                                                   │
│  brain CLI                                                                        │
│  ├─ scanner                                                                       │
│  ├─ parser                                                                        │
│  ├─ chunker                                                                       │
│  ├─ embedder (Gemini | OpenAI | Ollama)                                           │
│  ├─ uploader                                                                      │
│  └─ embedded MCP server                                                           │
│                                                                                   │
│  Local repository ──> hash diff ──> chunks ──> embeddings                         │
│                                 │                                                 │
│                                 └── raw code NEVER leaves machine                 │
└──────────────────────────────────────┬────────────────────────────────────────────┘
                                       │ vectors + metadata only
                                       ▼
┌────────────────────────────────── Context API ────────────────────────────────────┐
│ Go + Echo                                                                         │
│ ├─ auth                                                                           │
│ ├─ project registry                                                               │
│ ├─ retrieval/search                                                               │
│ └─ PostgreSQL + pgvector                                                          │
│     └─ multi-tenant RLS                                                           │
└──────────────────────────────────────┬────────────────────────────────────────────┘
                                       │
                                       ▼
┌──────────────────────────────── AI Clients ───────────────────────────────────────┐
│ Claude Code | Cursor | OpenCode | Gemini CLI | Windsurf                          │
│ Query via MCP: search, summaries, related files, flow explanation, impact        │
└───────────────────────────────────────────────────────────────────────────────────┘
```

## Quick Start

### 1. Install

Linux/macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/<your-org>/brain-context/main/install.sh | bash
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/<your-org>/brain-context/main/install.ps1 | iex
```

### 2. 🔐 Login to your tenant

```bash
brain login --token brn_tenant_xxx --api http://localhost:8080
```

### 3. 📦 Register a project

```bash
brain register \
  --project my-repo \
  --repo /path/to/repo \
  --embedder gemini \
  --api-key <provider-api-key> \
  --model gemini-embedding-001
```

### 4. 🧠 Index the repository

```bash
brain index --project my-repo
```

### 5. 🔌 Configure your AI client

Interactive setup wizard:

```bash
brain setup
```

Direct setup mode:

```bash
brain setup opencode
# or
brain setup all
```

## CLI Reference

| Command | Description |
|---|---|
| `brain login --token <tenant-api-key> [--api http://localhost:8080]` | Authenticate the CLI against a tenant API. |
| `brain register --project <name> --repo <path> --embedder <gemini\|openai\|ollama> --api-key <key> --model <model>` | Register a repository and embedder configuration. |
| `brain index --project <name>` | Run the initial project indexing flow. |
| `brain update --project <name>` | Re-index incrementally using the 3-level hash diff pipeline. |
| `brain setup` | Launch the Bubbletea onboarding wizard with Catppuccin Mocha styling. |
| `brain setup <client>` | Configure a specific client: `opencode`, `claude`, `cursor`, `gemini`, `windsurf`, or `all`. |
| `brain mcp [--project <name>]` | Run the embedded MCP server, optionally pinned to one project. |
| `brain version` | Print the installed CLI version. |

## MCP Tools

| Tool | Description |
|---|---|
| `search_project_context` | Retrieve semantically and lexically relevant code context for a query. |
| `get_file_summary` | Return an indexed summary for a specific file. |
| `get_related_files` | Traverse indexed relationships to find nearby files and dependencies. |
| `explain_flow` | Explain an application flow, such as login or request handling. |
| `find_impact` | Identify files and symbols affected by changing an entity. |

## Embedder Models

| Provider | Model | Dimensions |
|---|---|---:|
| Gemini | `gemini-embedding-001` | 768 |
| Gemini | `text-embedding-004` | 768 |
| OpenAI | `text-embedding-3-large` | 1024 |
| OpenAI | `text-embedding-3-small` | 1024 |
| Ollama | `nomic-embed-text` | 768 |
| Ollama | `bge-m3` | 1024 |

## Local Development

### Prerequisites

- Go toolchain
- Docker and Docker Compose
- WSL2 for running the API locally on Windows

### Start PostgreSQL + pgvector

```bash
docker compose up -d
```

This project uses:

- Image: `pgvector/pgvector:pg16`
- Port: `5433`
- Database URL:

```bash
postgres://brain:brain@127.0.0.1:5433/brain_context?sslmode=disable
```

### Run the API from WSL2

The API should be run from WSL2 because of a local `pgx` authentication issue when Go runs on Windows against Docker-hosted PostgreSQL.

```bash
go run ./cmd/api
```

API entrypoint: `cmd/api/main.go`  
CLI entrypoint: `cmd/brain/main.go`

### Project Structure

```text
brain-context/
├── cmd/
│   ├── brain/main.go
│   └── api/main.go
├── internal/
│   ├── api/
│   ├── auth/
│   ├── chunker/
│   ├── config/
│   ├── embedder/
│   ├── indexer/
│   ├── jobs/
│   ├── mcp/
│   ├── parser/
│   ├── retriever/
│   ├── scanner/
│   ├── store/
│   │   └── migrations/
│   ├── tui/
│   └── uploader/
├── Dockerfile.api
├── docker-compose.yml
├── install.sh
├── install.ps1
├── Makefile
└── scripts/smoke_local.sh
```

## Deployment

The recommended deployment path is:

- **Context API** on **GCP Compute Engine**
- **PostgreSQL** on **Neon** with `pgvector`

This gives a simple operational model: run the stateless Go API in Compute Engine, keep vector search in managed PostgreSQL, and let each team manage its own embedding provider credentials per project.

## Security

- Raw source code is **never uploaded**; only vectors and metadata are sent to the API.
- PostgreSQL Row-Level Security scopes every query by `tenant_id`.
- API keys and tokens are stored as **bcrypt hashes**, never plaintext.
- Token types are separated by responsibility:
  - `brn_tenant_xxx` — tenant admin
  - `brn_proj_xxx` — project upload/write only
  - `brn_mcp_xxx` — MCP read only
- All tokens expire after 90 days and must be renewed manually.

## Contributing

Contributions are welcome. Please open an issue first to discuss bugs, features, or design changes:

- GitHub Issues: https://github.com/<your-org>/brain-context/issues

If you are working locally, review the CLI and API architecture above before adding features so changes remain aligned with the local-first, raw-code-never-leaves-machine model.
