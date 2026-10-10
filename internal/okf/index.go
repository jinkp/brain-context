package okf

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/config"

	// Register the modernc pure-Go SQLite driver under the name "sqlite".
	_ "modernc.org/sqlite"
)

// Index holds open SQLite *sql.DB connections for all registered project bundles.
// The zero value (nil pointer) is safe: all methods guard against nil receiver.
type Index struct {
	// projectID → slice of bundle DBs for that project
	bundles map[string][]*bundleDB
}

// bundleDB pairs a bundleID with its open database connection.
type bundleDB struct {
	bundleID string
	db       *sql.DB
}

// dbPath returns the canonical path for a bundle's SQLite database.
// ~/.brain/okf/{projectID}/{bundleID}.db
func dbPath(projectID, bundleID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".brain", "okf", projectID, bundleID+".db"), nil
}

// createSchema ensures both the FTS5 virtual table and the metadata table exist.
func createSchema(db *sql.DB) error {
	const ddl = `
CREATE VIRTUAL TABLE IF NOT EXISTS concepts USING fts5(
    concept_id,
    title,
    description,
    tags,
    concept_type,
    status,
    trust_tier,
    source_files,
    stale_after,
    verified_by,
    tokenize='porter ascii'
);

CREATE TABLE IF NOT EXISTS concept_meta (
    concept_id  TEXT PRIMARY KEY,
    status      TEXT NOT NULL,
    stale_after TEXT,
    updated_at  TEXT NOT NULL
);`

	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

// Close closes all open SQLite database connections held by the Index.
// Safe to call on a nil Index.
func (idx *Index) Close() {
	if idx == nil {
		return
	}
	for _, bundles := range idx.bundles {
		for _, bdb := range bundles {
			if bdb.db != nil {
				bdb.db.Close()
			}
		}
	}
}

// OpenIndex opens (or creates) SQLite DBs for every project that has OKFBundlePath set.
// Returns nil (not an error) when no projects have OKF configured.
func OpenIndex(projects map[string]config.ProjectConfig) (*Index, error) {
	idx := &Index{bundles: make(map[string][]*bundleDB)}
	any := false

	for projectName, proj := range projects {
		if proj.OKFBundlePath == "" || proj.OKFBundleID == "" {
			continue
		}

		bundleID := proj.OKFBundleID
		path, err := dbPath(proj.ProjectID, bundleID)
		if err != nil {
			return nil, fmt.Errorf("project %q: %w", projectName, err)
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("project %q: create okf dir: %w", projectName, err)
		}

		db, err := sql.Open("sqlite", path)
		if err != nil {
			return nil, fmt.Errorf("project %q: open okf db: %w", projectName, err)
		}

		if err := createSchema(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("project %q: %w", projectName, err)
		}

		// Key by projectID (the stable identifier used at query time).
		projectID := proj.ProjectID
		idx.bundles[projectID] = append(idx.bundles[projectID], &bundleDB{
			bundleID: bundleID,
			db:       db,
		})
		any = true
	}

	if !any {
		return nil, nil //nolint:nilnil // design contract: nil means "no OKF configured"
	}
	return idx, nil
}

// BuildIndex (re)builds the SQLite FTS5 index for one bundle from disk.
// Opens (or creates) the DB, ensures the schema, then walks all .md files in
// bundlePath and upserts them. Parse errors are collected and returned as a
// combined non-fatal warning (the caller decides whether to surface them).
func BuildIndex(projectID, bundleID, bundlePath string) error {
	path, err := dbPath(projectID, bundleID)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create okf dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := createSchema(db); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var parseErrors []string

	walkErr := filepath.Walk(bundlePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable dirs/files non-fatally
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: read: %v", path, err))
			return nil
		}

		// ConceptID = relative path from bundle root, without .md extension
		rel, err := filepath.Rel(bundlePath, path)
		if err != nil {
			rel = info.Name()
		}
		conceptID := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		concept, err := ParseConcept(conceptID, string(data))
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: parse: %v", path, err))
			return nil
		}

		if err := upsertConcept(db, concept, now); err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: upsert: %v", path, err))
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("walk bundle dir %q: %w", bundlePath, walkErr)
	}

	if len(parseErrors) > 0 {
		return fmt.Errorf("BuildIndex: %d file(s) had errors:\n  %s",
			len(parseErrors), strings.Join(parseErrors, "\n  "))
	}
	return nil
}

// upsertConcept deletes and re-inserts a concept in both FTS5 and metadata tables.
// FTS5 does not support UPDATE, so DELETE + INSERT is the correct idempotency pattern.
func upsertConcept(db *sql.DB, c Concept, now string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// FTS5: delete existing entry (no-op if not present)
	if _, err := tx.Exec(`DELETE FROM concepts WHERE concept_id = ?`, c.ConceptID); err != nil {
		return fmt.Errorf("delete fts5: %w", err)
	}

	// FTS5: insert
	_, err = tx.Exec(
		`INSERT INTO concepts(concept_id, title, description, tags, concept_type, status, trust_tier, source_files, stale_after, verified_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ConceptID,
		c.Title,
		c.Description,
		strings.Join(c.Tags, " "),       // space-separated for FTS tokenization
		c.Type,
		c.Status,
		c.TrustTier,
		strings.Join(c.SourceFiles, "\n"), // newline-separated for storage
		c.StaleAfter,
		c.VerifiedBy,
	)
	if err != nil {
		return fmt.Errorf("insert fts5: %w", err)
	}

	// Metadata: upsert
	_, err = tx.Exec(
		`INSERT INTO concept_meta(concept_id, status, stale_after, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(concept_id) DO UPDATE SET status=excluded.status, stale_after=excluded.stale_after, updated_at=excluded.updated_at`,
		c.ConceptID,
		c.Status,
		c.StaleAfter,
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert meta: %w", err)
	}

	return tx.Commit()
}
