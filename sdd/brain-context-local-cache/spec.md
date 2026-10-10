# Specs: brain-context-local-cache

---

## Domain 1: local-query-cache (NEW)

### Requirement: Cached Search Responses Must Respect Remote Freshness

The system MUST reuse a locally cached search response only when the cached entry matches the current remote project freshness state. The freshness state MUST include the project identity and embedder identity, and SHOULD include the latest remote indexing timestamp.

#### Scenario: Reuse cached response when project state is unchanged

- **GIVEN** a project has a cached response for a query
- **AND** the current remote project state matches the cached entry's `project_id`, `last_indexed`, `embed_model`, and `embed_dimensions`
- **AND** the cached entry has not expired
- **WHEN** the same query is executed with the same tool parameters
- **THEN** the client MUST return the cached response without performing the full remote search request

#### Scenario: Bypass stale cached response when indexing changed

- **GIVEN** a project has a cached response for a query
- **AND** the remote `last_indexed` value differs from the value stored with the cache entry
- **WHEN** the same query is executed again
- **THEN** the client MUST ignore the cached response
- **AND** the client MUST fetch a fresh response from the API

#### Scenario: Bypass stale cached response when embedder identity changed

- **GIVEN** a project has a cached response for a query
- **AND** the remote `embed_model` or `embed_dimensions` differs from the value stored with the cache entry
- **WHEN** the same query is executed again
- **THEN** the client MUST ignore the cached response
- **AND** the client MUST fetch a fresh response from the API

### Requirement: Cache Key Must Include Tool Parameters

The system MUST treat two requests as cache-distinct when their tool parameters could change the result set or formatting.

#### Scenario: Different max_chunks values produce different cache entries

- **GIVEN** a cached response exists for query `"payment retry"` with `max_chunks=8`
- **WHEN** the same query is executed with `max_chunks=20`
- **THEN** the client MUST NOT reuse the cached `max_chunks=8` entry as if it were equivalent

#### Scenario: Different relationship flags produce different cache entries

- **GIVEN** a cached response exists for a query with `include_relationships=false`
- **WHEN** the same query is executed with `include_relationships=true`
- **THEN** the client MUST treat the requests as different cache entries

### Requirement: Query Cache Must Be Advisory Only

The system MUST fall back to the API whenever cache correctness cannot be proven.

#### Scenario: Missing project freshness state forces remote lookup

- **GIVEN** a project has a local cached response
- **AND** the client cannot obtain current remote project freshness state
- **WHEN** the query is executed
- **THEN** the client MUST NOT claim the cache is fresh by assumption
- **AND** the client MUST either fetch fresh data from the API or report that freshness could not be verified

---

## Domain 2: cache-management-cli (NEW)

### Requirement: Users Must Be Able To Inspect and Clear Cache State

The CLI MUST expose commands to inspect local cache state and clear it per project.

#### Scenario: Show project cache status

- **WHEN** the user runs `brain cache status --project sr-payments`
- **THEN** the CLI MUST report whether local query cache entries exist for that project
- **AND** it MUST show enough metadata to understand freshness, such as entry counts or latest cache timestamps

#### Scenario: Clear cache for a single project

- **GIVEN** a project has local cache entries
- **WHEN** the user runs `brain cache clear --project sr-payments`
- **THEN** the CLI MUST remove that project's local cache entries
- **AND** it MUST NOT delete unrelated projects' cache entries

### Requirement: Cache Storage Must Be Separate From Indexing Snapshot State

The system MUST keep read-cache storage distinct from existing local indexing snapshot files.

#### Scenario: Clearing query cache does not delete indexing snapshot

- **GIVEN** a project has a local indexing snapshot used for `brain update`
- **AND** the project also has query cache entries
- **WHEN** the user runs `brain cache clear --project sr-payments`
- **THEN** the system MUST delete only the read cache state
- **AND** it MUST preserve the indexing snapshot used by the diff pipeline

---

## Domain 3: offline-snapshot (DEFERRED)

### Requirement: Offline Snapshots Must Be Explicit

The system MUST NOT download a full offline context replica implicitly during `brain join`, `brain setup`, or normal query execution.

#### Scenario: Join configures access without downloading offline data

- **WHEN** a developer runs `brain join --code <invite>`
- **THEN** the CLI MUST store only the configuration and tokens required for future queries
- **AND** it MUST NOT automatically download a full offline context snapshot

#### Scenario: Offline snapshot requires explicit command

- **WHEN** the user wants a local offline copy of project context
- **THEN** the CLI MUST require a dedicated command such as `brain snapshot pull --project sr-payments`
- **AND** the user MUST be able to distinguish that artifact from the indexing snapshot used for diffing
