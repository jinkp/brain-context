// Package okf implements the OKF (Open Knowledge Format) knowledge layer for
// brain-context. It provides local-first, SQLite-backed indexing and search
// over OKF concept bundles, acting as the primary retrieval layer before the
// RAG code index.
//
// All exported methods on [Index] are nil-safe: a nil *Index is equivalent to
// "no OKF bundles registered" and returns the zero value without panicking.
//
// V1 is local-only: bundles are directories on disk, indexed into SQLite FTS5
// at ~/.brain/okf/{projectID}/{bundleID}.db. Server sync is deferred to V2.
package okf

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Concept holds the parsed content of a single OKF concept file (.md with YAML frontmatter).
type Concept struct {
	ConceptID   string   // filename without extension (stable ID)
	Title       string
	Description string
	Tags        []string
	Type        string   // Requirement | Decision | Playbook | Architecture | FileSummary | APIEndpoint
	Status      string   // draft | stable | deprecated
	TrustTier   string   // human-reviewed | machine-confirmed | unverified
	SourceFiles []string
	StaleAfter  string   // ISO 8601 date or ""
	VerifiedBy  string   // "human:X" or ""
	Body        string   // full markdown body after frontmatter
}

// frontmatterRaw is the internal YAML shape for unmarshal only.
// It maps nested keys (e.g. verified.by) that don't directly map to Concept fields.
type frontmatterRaw struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Type        string   `yaml:"type"`
	Status      string   `yaml:"status"`
	TrustTier   string   `yaml:"trust_tier"`
	SourceFiles []string `yaml:"source_files"`
	StaleAfter  string   `yaml:"stale_after"`
	Verified    struct {
		By string `yaml:"by"`
		At string `yaml:"at"`
	} `yaml:"verified"`
}

// ParseConcept parses a raw .md file content and returns a Concept.
// filename is the file path without the .md extension — used as the stable ConceptID.
func ParseConcept(filename, content string) (Concept, error) {
	fm, body, err := splitFrontmatter(content)
	if err != nil {
		return Concept{}, fmt.Errorf("parse concept %q: %w", filename, err)
	}

	var raw frontmatterRaw
	if err := yaml.Unmarshal([]byte(fm), &raw); err != nil {
		return Concept{}, fmt.Errorf("parse concept %q: unmarshal frontmatter: %w", filename, err)
	}

	c := Concept{
		ConceptID:   filename,
		Title:       strings.TrimSpace(raw.Title),
		Description: strings.TrimSpace(raw.Description),
		Tags:        raw.Tags,
		Type:        strings.TrimSpace(raw.Type),
		Status:      strings.TrimSpace(raw.Status),
		TrustTier:   strings.TrimSpace(raw.TrustTier),
		SourceFiles: raw.SourceFiles,
		StaleAfter:  strings.TrimSpace(raw.StaleAfter),
		VerifiedBy:  strings.TrimSpace(raw.Verified.By),
		Body:        body,
	}

	// Derive trust tier from verified.by if trust_tier is not explicitly set
	if c.TrustTier == "" {
		c.TrustTier = DeriveTrustTier(c)
	}

	return c, nil
}

// splitFrontmatter extracts the YAML frontmatter (between --- delimiters) and the remaining body.
// Returns (frontmatter, body, error). If no frontmatter is present, body is the whole content.
func splitFrontmatter(content string) (string, string, error) {
	const delimiter = "---"

	// Normalize line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")

	if !strings.HasPrefix(content, delimiter) {
		// No frontmatter — body is the whole file
		return "", content, nil
	}

	// Skip the opening ---
	rest := content[len(delimiter):]
	// Optional newline after opening ---
	if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}

	// Find closing ---
	end := strings.Index(rest, "\n"+delimiter)
	if end == -1 {
		return "", "", fmt.Errorf("frontmatter closing '---' not found")
	}

	fm := rest[:end]
	body := rest[end+1+len(delimiter):]
	// Trim leading newline from body
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}

	return fm, body, nil
}

// IsImplementationType returns true for concept types that get strictly filtered on staleness
// (FileSummary, APIEndpoint). Returns false for intent types (Requirement, Decision, Playbook,
// Architecture) which receive an advisory warning instead.
func IsImplementationType(conceptType string) bool {
	switch conceptType {
	case "FileSummary", "APIEndpoint":
		return true
	default:
		return false
	}
}

// DeriveTrustTier derives the trust tier from a Concept's VerifiedBy field:
//   - "human:*" prefix → "human-reviewed"
//   - non-empty, non-human verifier → "machine-confirmed"
//   - empty → "unverified"
func DeriveTrustTier(concept Concept) string {
	if strings.HasPrefix(concept.VerifiedBy, "human:") {
		return "human-reviewed"
	}
	if concept.VerifiedBy != "" {
		return "machine-confirmed"
	}
	return "unverified"
}
