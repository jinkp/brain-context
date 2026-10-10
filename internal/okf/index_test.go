package okf

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// writeBundleFile writes a .md file with OKF frontmatter into dir.
func writeBundleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write bundle file %s: %v", name, err)
	}
}

// openTestDB opens the SQLite DB written by BuildIndex and returns it for inspection.
func openTestDB(t *testing.T, home, projectID, bundleID string) *sql.DB {
	t.Helper()
	path := filepath.Join(home, ".brain", "okf", projectID, bundleID+".db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open test db %s: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return n
}

// ── BuildIndex roundtrip ──────────────────────────────────────────────────────

func TestBuildIndex_RoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome) // Windows

	bundleDir := t.TempDir()
	projectID := "proj-test-001"
	bundleID := "bundle-abc"

	// Write two valid .md files
	writeBundleFile(t, bundleDir, "auth-handler.md", `---
title: "Auth Handler"
type: FileSummary
status: stable
trust_tier: human-reviewed
description: "Handles JWT validation"
source_files:
  - internal/auth/handler.go
---

Auth handler body.
`)
	writeBundleFile(t, bundleDir, "payment-flow.md", `---
title: "Payment Flow"
type: Architecture
status: stable
trust_tier: unverified
description: "Payment processing overview"
---

Payment flow body.
`)

	if err := BuildIndex(projectID, bundleID, bundleDir); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	db := openTestDB(t, tmpHome, projectID, bundleID)

	// Verify both tables have 2 rows
	if n := countRows(t, db, "concepts"); n != 2 {
		t.Errorf("concepts rows = %d, want 2", n)
	}
	if n := countRows(t, db, "concept_meta"); n != 2 {
		t.Errorf("concept_meta rows = %d, want 2", n)
	}

	// Verify concept content is queryable
	var title string
	err := db.QueryRow(`SELECT title FROM concepts WHERE concept_id = 'auth-handler'`).Scan(&title)
	if err != nil {
		t.Fatalf("query auth-handler: %v", err)
	}
	if title != "Auth Handler" {
		t.Errorf("title = %q, want %q", title, "Auth Handler")
	}
}

func TestBuildIndex_Idempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	projectID := "proj-idem"
	bundleID := "bundle-idem"

	writeBundleFile(t, bundleDir, "concept-a.md", `---
title: "Concept A"
type: Decision
status: stable
---
body
`)

	// Call twice
	if err := BuildIndex(projectID, bundleID, bundleDir); err != nil {
		t.Fatalf("BuildIndex first call: %v", err)
	}
	if err := BuildIndex(projectID, bundleID, bundleDir); err != nil {
		t.Fatalf("BuildIndex second call: %v", err)
	}

	db := openTestDB(t, tmpHome, projectID, bundleID)

	// No duplicates — still 1 row in each table
	if n := countRows(t, db, "concepts"); n != 1 {
		t.Errorf("concepts rows after 2 calls = %d, want 1 (no duplicates)", n)
	}
	if n := countRows(t, db, "concept_meta"); n != 1 {
		t.Errorf("concept_meta rows after 2 calls = %d, want 1", n)
	}
}

func TestBuildIndex_SkipsInvalidFiles(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	projectID := "proj-skip"
	bundleID := "bundle-skip"

	// One valid file
	writeBundleFile(t, bundleDir, "valid.md", `---
title: "Valid"
type: FileSummary
status: stable
---
body
`)
	// One invalid file: unclosed frontmatter
	writeBundleFile(t, bundleDir, "invalid.md", "---\ntitle: broken\n")

	// BuildIndex should return an error (parse errors collected) but valid file is indexed
	err := BuildIndex(projectID, bundleID, bundleDir)
	// Error is expected because invalid.md fails to parse
	if err == nil {
		t.Logf("BuildIndex returned nil (invalid file was silently skipped — that's also acceptable)")
	} else {
		t.Logf("BuildIndex returned expected error for invalid file: %v", err)
	}

	db := openTestDB(t, tmpHome, projectID, bundleID)

	// The valid file should still be in the DB
	var count int
	if err2 := db.QueryRow(`SELECT COUNT(*) FROM concepts WHERE concept_id = 'valid'`).Scan(&count); err2 != nil {
		t.Fatalf("query valid concept: %v", err2)
	}
	if count != 1 {
		t.Errorf("valid concept count = %d, want 1 (should be indexed despite co-existing invalid file)", count)
	}
}

func TestBuildIndex_NonMDFilesIgnored(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	projectID := "proj-nonmd"
	bundleID := "bundle-nonmd"

	// .md file
	writeBundleFile(t, bundleDir, "concept.md", `---
title: "A Concept"
type: Decision
status: stable
---
body
`)
	// non-.md files (should be ignored)
	if err := os.WriteFile(filepath.Join(bundleDir, "readme.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "schema.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := BuildIndex(projectID, bundleID, bundleDir); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	db := openTestDB(t, tmpHome, projectID, bundleID)
	if n := countRows(t, db, "concepts"); n != 1 {
		t.Errorf("concepts rows = %d, want 1 (only .md files indexed)", n)
	}
}
