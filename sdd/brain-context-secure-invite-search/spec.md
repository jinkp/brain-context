# Specs: brain-context-secure-invite-search

---

## Domain 1: secure-invite-read-access (NEW)

### Requirement: Invited Developers Must Search Without Server Secrets

The system MUST allow a developer who joined via `brain join` to use read-only MCP search tools without possessing `BRAIN_ENCRYPTION_KEY` and without receiving the plaintext embedder provider API key.

#### Scenario: Joined developer searches project context without local decrypt secret

- **GIVEN** a developer joined a project via `brain join`
- **AND** their local environment does not contain `BRAIN_ENCRYPTION_KEY`
- **WHEN** they call `search_project_context`
- **THEN** the search MUST succeed using server-side query embedding
- **AND** the client MUST NOT need to decrypt `embed_api_key_enc`

#### Scenario: Joined developer uses explain_flow without provider key locally

- **GIVEN** a developer joined a project via `brain join`
- **AND** their local machine does not have the provider API key
- **WHEN** they call `explain_flow`
- **THEN** the request MUST succeed using server-side query embedding

### Requirement: Join Must Not Distribute Embedder Secrets

The system MUST NOT return encrypted or plaintext embedder provider credentials in invite redemption responses intended for read-only clients.

#### Scenario: Join returns only read-safe config

- **WHEN** a developer redeems an invite with `brain join --code <invite>`
- **THEN** the server MUST return the project identifiers and read token needed for future queries
- **AND** it MUST NOT include `embed_api_key_enc`
- **AND** it MUST NOT include the plaintext provider API key

---

## Domain 2: api-query-embedding (NEW)

### Requirement: Search API Must Accept Plain-Text Queries

The project context search API MUST accept a plain-text query and perform embedding server-side when authorized via a read token.

#### Scenario: API embeds plain-text query server-side

- **GIVEN** a valid `mcp_read_key` for a project
- **WHEN** the client sends `POST /api/projects/:id/context/search` with a non-empty `query`
- **THEN** the API MUST load the project's embedder configuration server-side
- **AND** it MUST generate the query embedding server-side
- **AND** it MUST return ranked context results

#### Scenario: API rejects requests missing both query and query_embedding

- **WHEN** the client sends a search request with neither `query` nor `query_embedding`
- **THEN** the API MUST return a validation error

### Requirement: API May Support Legacy Embedded Queries During Migration

The API SHOULD accept legacy `query_embedding` requests temporarily so older clients continue to function during migration.

#### Scenario: Legacy client still sends query_embedding

- **GIVEN** an older client sends `query_embedding`
- **WHEN** the request is otherwise valid
- **THEN** the API MUST continue to search successfully during the migration window

#### Scenario: New query field takes precedence over legacy embedding

- **GIVEN** a request contains both `query` and `query_embedding`
- **WHEN** the API processes the request
- **THEN** it MUST prefer the `query` field as the canonical input for server-side embedding

---

## Domain 3: mcp-client-simplification (NEW)

### Requirement: MCP Search Tools Must Stop Depending On Local Provider Secrets

The MCP client MUST send plain-text queries for search-style read tools instead of generating embeddings locally.

#### Scenario: search_project_context does not require local embed api key

- **GIVEN** a local config created via `brain join`
- **WHEN** `search_project_context` executes
- **THEN** the MCP client MUST send the query text to the API
- **AND** it MUST NOT fail just because local `EmbedAPIKey` is absent

#### Scenario: search_solution_context does not embed locally

- **GIVEN** a solution composed of multiple projects
- **WHEN** `search_solution_context` executes in smart or full mode
- **THEN** the MCP client MUST fan out plain-text query requests
- **AND** each project search MUST embed server-side

---

## Domain 4: secret-boundary (NEW)

### Requirement: Server Encryption Key Must Remain Server-Only

The system MUST NOT require developers to configure the server encryption key locally in order to perform read-only MCP searches.

#### Scenario: Read-only search does not require BRAIN_ENCRYPTION_KEY in client env

- **GIVEN** a developer machine with no `BRAIN_ENCRYPTION_KEY`
- **WHEN** the developer uses read-only MCP search tools after `brain join`
- **THEN** those tools MUST still function

#### Scenario: Revoking mcp_read_key removes access without client secret cleanup

- **GIVEN** a developer previously joined a project
- **AND** their `mcp_read_key` is revoked or renewed away
- **WHEN** they attempt another read-only search
- **THEN** the API MUST deny access
- **AND** no lingering local provider secret should allow bypassing revocation
