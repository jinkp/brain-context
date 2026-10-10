package okf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/indexer"
)

// GenerateDrafts reads the local snapshot file list for projectID and writes
// per-file draft OKF concept files to bundlePath.
//
// Idempotency: if the target file already exists with status: stable, it is
// skipped. Draft and missing files are always (re)written.
//
// Returns the count of files written (created or overwritten), or an error.
func GenerateDrafts(projectID, bundlePath string) (int, error) {
	snapshot, err := indexer.LoadSnapshot(projectID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("no snapshot found for project %q — run `brain update` first", projectID)
		}
		return 0, fmt.Errorf("load snapshot for %q: %w", projectID, err)
	}

	if err := os.MkdirAll(bundlePath, 0o755); err != nil {
		return 0, fmt.Errorf("create bundle dir: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	written := 0

	for filePath := range snapshot.Files {
		conceptID := sanitizeConceptID(filePath)
		targetFile := filepath.Join(bundlePath, conceptID+".md")

		// Idempotency check: if target exists with status: stable, skip it
		if shouldSkip(targetFile) {
			continue
		}

		content := buildDraftContent(filePath, now)
		if err := os.WriteFile(targetFile, []byte(content), 0o644); err != nil {
			return written, fmt.Errorf("write draft %q: %w", targetFile, err)
		}
		written++
	}

	return written, nil
}

// sanitizeConceptID converts a file path into a safe concept ID for use as a filename.
// Replaces path separators and dots with dashes, strips the file extension.
// Example: "internal/auth/handler.go" → "internal-auth-handler-go"
func sanitizeConceptID(filePath string) string {
	// Normalize separators
	id := strings.ReplaceAll(filePath, "\\", "/")
	// Remove extension
	ext := filepath.Ext(id)
	if ext != "" {
		id = id[:len(id)-len(ext)]
	}
	// Replace path separators and dots with dashes
	id = strings.NewReplacer(
		"/", "-",
		".", "-",
	).Replace(id)
	// Collapse consecutive dashes
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}
	// Trim leading/trailing dashes
	id = strings.Trim(id, "-")
	return id
}

// shouldSkip returns true if the target file exists and has status: stable.
func shouldSkip(targetFile string) bool {
	data, err := os.ReadFile(targetFile)
	if err != nil {
		// File doesn't exist or unreadable — don't skip
		return false
	}
	conceptID := strings.TrimSuffix(filepath.Base(targetFile), ".md")
	concept, err := ParseConcept(conceptID, string(data))
	if err != nil {
		return false // unparseable → overwrite
	}
	return concept.Status == "stable"
}

// buildDraftContent returns the YAML frontmatter + body for a new draft concept.
func buildDraftContent(filePath, generatedAt string) string {
	return fmt.Sprintf(`---
title: "%s"
type: FileSummary
status: draft
trust_tier: unverified
description: "Auto-generated draft from brain-context RAG index. Review and curate."
source_files:
  - %s
generated:
  by: brain-context/okf-generator
  at: "%s"
---

<!-- Auto-generated draft. Edit this file to add context, then set status: stable and add verified block. -->
`, filePath, filePath, generatedAt)
}
