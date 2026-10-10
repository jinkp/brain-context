package okf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/brain-context/internal/config"
)

// minQueryThreshold is used in tests to accept any FTS5 match.
// FTS5 BM25 scores in single-document test indexes are very small (~0.000002),
// so we use a value just above zero.
const minQueryThreshold = 0.000001

// buildTestIndex creates a temporary bundle, populates it with the given md
// files, calls BuildIndex, and returns an *Index wired to projectID.
// HOME/USERPROFILE are redirected so the SQLite DB lands in an isolated location.
// The returned Index is automatically closed when the test ends.
func buildTestIndex(t *testing.T, projectID, bundleID string, files map[string]string) *Index {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(bundleDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write bundle file %s: %v", name, err)
		}
	}

	if err := BuildIndex(projectID, bundleID, bundleDir); err != nil {
		// Non-fatal: some test bundles intentionally have invalid files
		t.Logf("BuildIndex warning: %v", err)
	}

	idx, err := OpenIndex(map[string]config.ProjectConfig{
		"test-project": {
			ProjectID:     projectID,
			OKFBundlePath: bundleDir,
			OKFBundleID:   bundleID,
		},
	})
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	if idx == nil {
		t.Fatal("OpenIndex returned nil (no OKF bundles registered)")
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

// ── concept fixtures ──────────────────────────────────────────────────────────

const stableAuthConcept = `---
title: "Auth Handler"
type: FileSummary
status: stable
trust_tier: human-reviewed
description: "Handles JWT authentication and session creation for the application."
tags: [auth, jwt, handler]
source_files:
  - internal/auth/handler.go
---

Detailed description of the auth handler.
`

const stablePaymentConcept = `---
title: "Payment Processing"
type: Architecture
status: stable
trust_tier: unverified
description: "Describes the payment processing flow and retry logic."
tags: [payment, retry]
source_files:
  - internal/payments/processor.go
---
`

const deprecatedConcept = `---
title: "Old Auth"
type: FileSummary
status: deprecated
trust_tier: unverified
description: "Deprecated auth handler — do not use."
tags: [auth, deprecated]
---
`

const draftConcept = `---
title: "Draft Feature"
type: FileSummary
status: draft
trust_tier: unverified
description: "Work in progress feature."
tags: [draft, wip]
---
`

const staleImplConcept = `---
title: "Stale File Summary"
type: FileSummary
status: stable
trust_tier: unverified
description: "This file summary is past its stale date."
tags: [stale, filesummary]
stale_after: "2020-01-01"
source_files:
  - internal/old/handler.go
---
`

const staleIntentConcept = `---
title: "Old Requirement"
type: Requirement
status: stable
trust_tier: unverified
description: "This requirement was set long ago but is still valid context."
tags: [requirement, old]
stale_after: "2020-01-01"
---
`

const humanReviewedConcept = `---
title: "Auth Architecture"
type: Architecture
status: stable
trust_tier: human-reviewed
description: "Handles JWT authentication and session creation — human reviewed."
tags: [auth, jwt]
---
`

const unverifiedConcept = `---
title: "Auth Architecture Unverified"
type: Architecture
status: stable
trust_tier: unverified
description: "Handles JWT authentication — unverified copy."
tags: [auth, jwt]
---
`

// ── Query nil/empty guard tests ───────────────────────────────────────────────

func TestQuery_NilIndex(t *testing.T) {
	var idx *Index
	result, ok := idx.Query(context.Background(), "any-project", "auth", 0, false)
	if ok {
		t.Errorf("nil Index.Query() returned ok=true, want false")
	}
	if result != "" {
		t.Errorf("nil Index.Query() returned non-empty result: %q", result)
	}
}

func TestQuery_NoProjectBundles(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	// Index an empty bundle for "proj-other" (different project ID)
	_ = BuildIndex("proj-other", "bundle-other", bundleDir)

	idx, err := OpenIndex(map[string]config.ProjectConfig{
		"other": {
			ProjectID:     "proj-other",
			OKFBundlePath: bundleDir,
			OKFBundleID:   "bundle-other",
		},
	})
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	if idx == nil {
		t.Skip("no OKF bundles registered (empty bundle dir), skipping")
	}
	t.Cleanup(func() { idx.Close() })

	// Query with a projectID that has no bundles registered
	result, ok := idx.Query(context.Background(), "nonexistent-project-id", "auth", 0, false)
	if ok {
		t.Errorf("Query with unknown projectID returned ok=true")
	}
	if result != "" {
		t.Errorf("Query with unknown projectID returned result: %q", result)
	}
}

// ── Query threshold tests ─────────────────────────────────────────────────────

func TestQuery_HitAboveThreshold(t *testing.T) {
	idx := buildTestIndex(t, "proj-hit", "bundle-hit", map[string]string{
		"auth-handler.md": stableAuthConcept,
	})

	// Use a very low threshold so any FTS5 match passes
	result, ok := idx.Query(context.Background(), "proj-hit", "auth jwt handler", minQueryThreshold, false)
	if !ok {
		t.Fatalf("Query() returned ok=false, expected a hit for query 'auth jwt handler'")
	}
	if !strings.Contains(result, "[OKF]") {
		t.Errorf("result does not contain '[OKF]' prefix: %q", result)
	}
	if !strings.Contains(result, "Auth Handler") {
		t.Errorf("result does not contain concept title: %q", result)
	}
}

func TestQuery_MissBelowThreshold(t *testing.T) {
	idx := buildTestIndex(t, "proj-miss", "bundle-miss", map[string]string{
		"auth-handler.md": stableAuthConcept,
	})

	// Use threshold=1.0 — FTS5 BM25 scores (negated) are never that high
	result, ok := idx.Query(context.Background(), "proj-miss", "auth", 1.0, false)
	if ok {
		t.Errorf("Query() returned ok=true with threshold=1.0 (should fail threshold gate): result=%q", result)
	}
}

// ── Status filtering ──────────────────────────────────────────────────────────

func TestQuery_DeprecatedExcluded(t *testing.T) {
	idx := buildTestIndex(t, "proj-depr", "bundle-depr", map[string]string{
		"deprecated.md": deprecatedConcept,
	})

	result, ok := idx.Query(context.Background(), "proj-depr", "auth deprecated old", minQueryThreshold, false)
	if ok {
		t.Errorf("Query() returned ok=true for deprecated concept: %q", result)
	}
}

func TestQuery_DraftExcluded(t *testing.T) {
	idx := buildTestIndex(t, "proj-draft", "bundle-draft", map[string]string{
		"draft.md": draftConcept,
	})

	result, ok := idx.Query(context.Background(), "proj-draft", "draft feature wip", minQueryThreshold, false)
	if ok {
		t.Errorf("Query() returned ok=true for draft concept: %q", result)
	}
}

// ── Staleness split (Decision 6) ─────────────────────────────────────────────

func TestQuery_StaleImplType_Excluded(t *testing.T) {
	// FileSummary past stale_after → filtered out (fall to RAG)
	idx := buildTestIndex(t, "proj-stale-impl", "bundle-stale-impl", map[string]string{
		"stale-impl.md": staleImplConcept,
	})

	result, ok := idx.Query(context.Background(), "proj-stale-impl", "stale filesummary handler", minQueryThreshold, false)
	if ok {
		t.Errorf("Query() returned ok=true for stale impl-type concept (FileSummary past stale_after must be excluded): %q", result)
	}
}

func TestQuery_StaleIntentType_IncludedWithWarning(t *testing.T) {
	// Requirement past stale_after → included with [⚠️ stale] warning
	idx := buildTestIndex(t, "proj-stale-intent", "bundle-stale-intent", map[string]string{
		"stale-intent.md": staleIntentConcept,
	})

	result, ok := idx.Query(context.Background(), "proj-stale-intent", "requirement old", minQueryThreshold, false)
	if !ok {
		t.Fatalf("Query() returned ok=false for stale intent-type concept (should be included with advisory warning)")
	}
	if !strings.Contains(result, "⚠️") {
		t.Errorf("result should contain staleness warning '⚠️', got: %q", result)
	}
	if !strings.Contains(result, "[OKF]") {
		t.Errorf("result should have [OKF] prefix, got: %q", result)
	}
}

// ── Trust tier ranking ────────────────────────────────────────────────────────

func TestQuery_TrustTierRanking(t *testing.T) {
	// Both concepts match "auth jwt" — human-reviewed must beat unverified
	idx := buildTestIndex(t, "proj-tier", "bundle-tier", map[string]string{
		"human-reviewed.md": humanReviewedConcept,
		"unverified.md":     unverifiedConcept,
	})

	result, ok := idx.Query(context.Background(), "proj-tier", "auth jwt", minQueryThreshold, false)
	if !ok {
		t.Fatalf("Query() returned ok=false, expected a hit")
	}
	// The human-reviewed concept should appear in the result
	if !strings.Contains(result, "human-reviewed") {
		t.Errorf("expected human-reviewed concept to win tier ranking, result: %q", result)
	}
}

// ── sanitizeFTS5Query ─────────────────────────────────────────────────────────

func TestSanitizeFTS5Query(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"auth", `"auth"`},
		{"auth handler", `"auth" "handler"`},
		{"", ""},
		{`auth "quoted"`, `"auth" """quoted"""`},
	}
	for _, tc := range cases {
		got := sanitizeFTS5Query(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeFTS5Query(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

