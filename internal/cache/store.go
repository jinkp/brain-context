// Package cache provides a local SQLite-backed read-path cache for brain-context MCP queries.
// The cache is advisory only: it is bypassed whenever remote freshness cannot be proven.
// Storage layout: ~/.brain/context-cache.db
package cache

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

const (
	// DefaultTTL is how long a cache entry is considered valid before forced expiry.
	// Freshness invalidation (last_indexed change) overrides this independently.
	DefaultTTL = 24 * time.Hour

	// ResponseSchemaVersion must be incremented whenever the rendered response
	// format changes in a way that makes old cached text incorrect.
	ResponseSchemaVersion = 1

	dbFileName = "context-cache.db"
)

// ProjectState holds the remote freshness metadata for a project.
// A cached entry is only reused when ALL fields still match.
type ProjectState struct {
	ProjectID       string
	ProjectName     string
	LastIndexed     string // RFC3339 or empty
	EmbedModel      string
	EmbedDimensions int
	APIEndpoint     string
	UpdatedAt       time.Time
}

// CacheKey uniquely identifies a cacheable response.
type CacheKey struct {
	ProjectID       string
	ToolName        string
	QueryText       string
	ParamsJSON      string // canonical JSON of extra params (max_chunks, etc.)
	LastIndexed     string
	EmbedModel      string
	EmbedDimensions int
	SchemaVersion   int
}

// hash returns the sha256 hex digest of the key's canonical representation.
func (k CacheKey) hash() string {
	// Normalise query: lower-case, collapsed whitespace.
	normalised := strings.Join(strings.Fields(strings.ToLower(k.QueryText)), " ")

	raw := fmt.Sprintf(
		"v%d|%s|%s|%s|%s|%s|%s|%d",
		k.SchemaVersion,
		k.ProjectID,
		k.ToolName,
		normalised,
		k.ParamsJSON,
		k.LastIndexed,
		k.EmbedModel,
		k.EmbedDimensions,
	)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)
}

// Store is a local SQLite cache for MCP search responses.
// The zero value is not usable; create one with Open.
type Store struct {
	db *sql.DB
}

// DBPath returns the canonical path for the cache database.
// ~/.brain/context-cache.db
func DBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".brain", dbFileName), nil
}

// Open opens (or creates) the cache database at the canonical path.
func Open() (*Store, error) {
	path, err := DBPath()
	if err != nil {
		return nil, err
	}
	return OpenAt(path)
}

// OpenAt opens (or creates) the cache database at the given path.
// Exported for testing with t.TempDir().
func OpenAt(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate cache schema: %w", err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// migrate applies the schema DDL idempotently.
func (s *Store) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS project_state (
    project_id       TEXT PRIMARY KEY,
    project_name     TEXT NOT NULL,
    last_indexed     TEXT,
    embed_model      TEXT NOT NULL,
    embed_dimensions INTEGER NOT NULL,
    api_endpoint     TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS query_cache (
    cache_key              TEXT PRIMARY KEY,
    project_id             TEXT NOT NULL,
    tool_name              TEXT NOT NULL,
    query_text             TEXT NOT NULL,
    query_hash             TEXT NOT NULL,
    params_json            TEXT NOT NULL,
    last_indexed           TEXT,
    embed_model            TEXT NOT NULL,
    embed_dimensions       INTEGER NOT NULL,
    response_schema_version INTEGER NOT NULL,
    response_text          TEXT NOT NULL,
    created_at             TEXT NOT NULL,
    expires_at             TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_query_cache_project ON query_cache(project_id);
CREATE INDEX IF NOT EXISTS idx_query_cache_expires ON query_cache(expires_at);
`
	if _, err := s.db.Exec(ddl); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// UpsertProjectState persists (or replaces) the freshness metadata for a project.
func (s *Store) UpsertProjectState(ps ProjectState) error {
	const q = `
INSERT INTO project_state (project_id, project_name, last_indexed, embed_model, embed_dimensions, api_endpoint, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id) DO UPDATE SET
    project_name     = excluded.project_name,
    last_indexed     = excluded.last_indexed,
    embed_model      = excluded.embed_model,
    embed_dimensions = excluded.embed_dimensions,
    api_endpoint     = excluded.api_endpoint,
    updated_at       = excluded.updated_at`
	_, err := s.db.Exec(q,
		ps.ProjectID,
		ps.ProjectName,
		ps.LastIndexed,
		ps.EmbedModel,
		ps.EmbedDimensions,
		ps.APIEndpoint,
		ps.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert project_state: %w", err)
	}
	return nil
}

// GetProjectState retrieves the stored freshness metadata for projectID.
// Returns nil, nil when no state has been stored yet.
func (s *Store) GetProjectState(projectID string) (*ProjectState, error) {
	const q = `SELECT project_id, project_name, last_indexed, embed_model, embed_dimensions, api_endpoint, updated_at
               FROM project_state WHERE project_id = ?`
	row := s.db.QueryRow(q, projectID)

	var ps ProjectState
	var updatedAt string
	err := row.Scan(
		&ps.ProjectID,
		&ps.ProjectName,
		&ps.LastIndexed,
		&ps.EmbedModel,
		&ps.EmbedDimensions,
		&ps.APIEndpoint,
		&updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project_state: %w", err)
	}
	ps.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &ps, nil
}

// Put stores a query response in the cache.
func (s *Store) Put(key CacheKey, responseText string, ttl time.Duration) error {
	if ttl == 0 {
		ttl = DefaultTTL
	}
	now := time.Now().UTC()
	cacheKey := key.hash()

	paramsJSON := key.ParamsJSON
	if paramsJSON == "" {
		paramsJSON = "{}"
	}

	const q = `
INSERT INTO query_cache
    (cache_key, project_id, tool_name, query_text, query_hash, params_json,
     last_indexed, embed_model, embed_dimensions, response_schema_version, response_text, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET
    response_text          = excluded.response_text,
    created_at             = excluded.created_at,
    expires_at             = excluded.expires_at`

	queryHash := fmt.Sprintf("%x", sha256.Sum256([]byte(key.QueryText)))

	_, err := s.db.Exec(q,
		cacheKey,
		key.ProjectID,
		key.ToolName,
		key.QueryText,
		queryHash,
		paramsJSON,
		key.LastIndexed,
		key.EmbedModel,
		key.EmbedDimensions,
		key.SchemaVersion,
		responseText,
		now.Format(time.RFC3339),
		now.Add(ttl).Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("cache put: %w", err)
	}
	return nil
}

// Get retrieves a cached response for key.
// Returns ("", false, nil) on a miss (expired or not found).
// Returns ("", false, err) on a storage error.
func (s *Store) Get(key CacheKey) (string, bool, error) {
	cacheKey := key.hash()
	now := time.Now().UTC().Format(time.RFC3339)

	const q = `SELECT response_text FROM query_cache
               WHERE cache_key = ? AND expires_at > ?`
	row := s.db.QueryRow(q, cacheKey, now)

	var text string
	err := row.Scan(&text)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("cache get: %w", err)
	}
	return text, true, nil
}

// CacheStats holds operational counters for a project's cache entries.
type CacheStats struct {
	ProjectID   string
	TotalCount  int
	ActiveCount int // not expired
	OldestEntry time.Time
	NewestEntry time.Time
}

// Stats returns cache entry counts for a given projectID.
func (s *Store) Stats(projectID string) (CacheStats, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	stats := CacheStats{ProjectID: projectID}

	const q = `
SELECT
    COUNT(*) AS total,
    SUM(CASE WHEN expires_at > ? THEN 1 ELSE 0 END) AS active,
    MIN(created_at),
    MAX(created_at)
FROM query_cache
WHERE project_id = ?`

	var oldest, newest sql.NullString
	err := s.db.QueryRow(q, now, projectID).Scan(
		&stats.TotalCount,
		&stats.ActiveCount,
		&oldest,
		&newest,
	)
	if err != nil && err != sql.ErrNoRows {
		return stats, fmt.Errorf("cache stats: %w", err)
	}
	if oldest.Valid {
		stats.OldestEntry, _ = time.Parse(time.RFC3339, oldest.String)
	}
	if newest.Valid {
		stats.NewestEntry, _ = time.Parse(time.RFC3339, newest.String)
	}
	return stats, nil
}

// Clear removes all cache entries for projectID.
// It does NOT affect other projects or the indexing snapshots.
func (s *Store) Clear(projectID string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM query_cache WHERE project_id = ?`, projectID)
	if err != nil {
		return 0, fmt.Errorf("cache clear: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Prune removes all expired cache entries across all projects.
func (s *Store) Prune() (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM query_cache WHERE expires_at <= ?`, now)
	if err != nil {
		return 0, fmt.Errorf("cache prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// BuildKey constructs a CacheKey from tool parameters.
// params is a map of additional tool parameters (e.g. {"max_chunks": 8}).
// Pass nil for tools that have no extra parameters.
func BuildKey(projectID, toolName, query string, params map[string]any, state ProjectState) (CacheKey, error) {
	paramsJSON := "{}"
	if len(params) > 0 {
		b, err := json.Marshal(params)
		if err != nil {
			return CacheKey{}, fmt.Errorf("marshal params: %w", err)
		}
		paramsJSON = string(b)
	}
	return CacheKey{
		ProjectID:       projectID,
		ToolName:        toolName,
		QueryText:       query,
		ParamsJSON:      paramsJSON,
		LastIndexed:     state.LastIndexed,
		EmbedModel:      state.EmbedModel,
		EmbedDimensions: state.EmbedDimensions,
		SchemaVersion:   ResponseSchemaVersion,
	}, nil
}
