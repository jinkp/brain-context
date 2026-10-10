package okf

import (
	"strings"
	"testing"
)

// ── ParseConcept ──────────────────────────────────────────────────────────────

func TestParseConcept(t *testing.T) {
	cases := []struct {
		name        string
		filename    string
		content     string
		wantErr     bool
		wantErrSub  string
		check       func(t *testing.T, c Concept)
	}{
		{
			name:     "valid full frontmatter",
			filename: "auth-handler",
			content: `---
title: "Auth Handler"
type: FileSummary
status: stable
trust_tier: human-reviewed
description: "Handles JWT validation"
tags:
  - auth
  - jwt
source_files:
  - internal/auth/handler.go
stale_after: "2030-01-01"
verified:
  by: human:techlead
  at: "2026-10-08T10:00:00Z"
---

This is the body content.
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if c.ConceptID != "auth-handler" {
					t.Errorf("ConceptID = %q, want %q", c.ConceptID, "auth-handler")
				}
				if c.Title != "Auth Handler" {
					t.Errorf("Title = %q, want %q", c.Title, "Auth Handler")
				}
				if c.Type != "FileSummary" {
					t.Errorf("Type = %q, want %q", c.Type, "FileSummary")
				}
				if c.Status != "stable" {
					t.Errorf("Status = %q, want %q", c.Status, "stable")
				}
				if c.TrustTier != "human-reviewed" {
					t.Errorf("TrustTier = %q, want %q", c.TrustTier, "human-reviewed")
				}
				if c.Description != "Handles JWT validation" {
					t.Errorf("Description = %q, want %q", c.Description, "Handles JWT validation")
				}
				if len(c.Tags) != 2 || c.Tags[0] != "auth" || c.Tags[1] != "jwt" {
					t.Errorf("Tags = %v, want [auth jwt]", c.Tags)
				}
				if len(c.SourceFiles) != 1 || c.SourceFiles[0] != "internal/auth/handler.go" {
					t.Errorf("SourceFiles = %v, want [internal/auth/handler.go]", c.SourceFiles)
				}
				if c.StaleAfter != "2030-01-01" {
					t.Errorf("StaleAfter = %q, want %q", c.StaleAfter, "2030-01-01")
				}
				if c.VerifiedBy != "human:techlead" {
					t.Errorf("VerifiedBy = %q, want %q", c.VerifiedBy, "human:techlead")
				}
				if !strings.Contains(c.Body, "This is the body content.") {
					t.Errorf("Body does not contain expected text, got: %q", c.Body)
				}
			},
		},
		{
			name:     "missing type field — parsed as empty string, not error",
			filename: "no-type",
			content: `---
title: "No Type"
status: draft
---
body here
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if c.Type != "" {
					t.Errorf("Type = %q, want empty", c.Type)
				}
				if c.Title != "No Type" {
					t.Errorf("Title = %q, want %q", c.Title, "No Type")
				}
			},
		},
		{
			name:     "missing title — parsed as empty string, not error",
			filename: "no-title",
			content: `---
type: Decision
status: stable
---
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if c.Title != "" {
					t.Errorf("Title = %q, want empty", c.Title)
				}
				if c.Type != "Decision" {
					t.Errorf("Type = %q, want %q", c.Type, "Decision")
				}
			},
		},
		{
			name:     "unknown status value — parsed as-is",
			filename: "unknown-status",
			content: `---
title: "Something"
type: Requirement
status: experimental
---
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if c.Status != "experimental" {
					t.Errorf("Status = %q, want %q", c.Status, "experimental")
				}
			},
		},
		{
			name:     "verified.by maps to VerifiedBy",
			filename: "with-verified",
			content: `---
title: "Verified Concept"
type: Architecture
status: stable
verified:
  by: human:techlead
  at: "2026-10-08T10:00:00Z"
---
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if c.VerifiedBy != "human:techlead" {
					t.Errorf("VerifiedBy = %q, want %q", c.VerifiedBy, "human:techlead")
				}
				// trust_tier not explicitly set → derived from verified.by
				if c.TrustTier != "human-reviewed" {
					t.Errorf("TrustTier = %q, want %q", c.TrustTier, "human-reviewed")
				}
			},
		},
		{
			name:     "explicit trust_tier overrides derived value",
			filename: "explicit-tier",
			content: `---
title: "Explicit Tier"
type: Architecture
status: stable
trust_tier: machine-confirmed
verified:
  by: human:techlead
  at: "2026-10-08T10:00:00Z"
---
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				// explicit trust_tier wins over derived
				if c.TrustTier != "machine-confirmed" {
					t.Errorf("TrustTier = %q, want %q", c.TrustTier, "machine-confirmed")
				}
			},
		},
		{
			name:     "tags parsed as []string",
			filename: "with-tags",
			content: `---
title: "Tagged"
type: Playbook
status: draft
tags: [go, testing, sqlite]
---
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if len(c.Tags) != 3 {
					t.Fatalf("Tags len = %d, want 3; got %v", len(c.Tags), c.Tags)
				}
				if c.Tags[0] != "go" || c.Tags[1] != "testing" || c.Tags[2] != "sqlite" {
					t.Errorf("Tags = %v, want [go testing sqlite]", c.Tags)
				}
			},
		},
		{
			name:     "source_files parsed as []string",
			filename: "with-sources",
			content: `---
title: "Multi Source"
type: FileSummary
status: stable
source_files:
  - internal/auth/handler.go
  - internal/auth/middleware.go
---
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if len(c.SourceFiles) != 2 {
					t.Fatalf("SourceFiles len = %d, want 2", len(c.SourceFiles))
				}
				if c.SourceFiles[0] != "internal/auth/handler.go" {
					t.Errorf("SourceFiles[0] = %q", c.SourceFiles[0])
				}
				if c.SourceFiles[1] != "internal/auth/middleware.go" {
					t.Errorf("SourceFiles[1] = %q", c.SourceFiles[1])
				}
			},
		},
		{
			name:     "body extracted after closing ---",
			filename: "body-test",
			content: `---
title: "Body Test"
type: Decision
status: stable
---

This is the markdown body.
It can span multiple lines.
`,
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if !strings.Contains(c.Body, "This is the markdown body.") {
					t.Errorf("Body = %q, want it to contain body text", c.Body)
				}
				if !strings.Contains(c.Body, "multiple lines") {
					t.Errorf("Body = %q, should contain 'multiple lines'", c.Body)
				}
			},
		},
		{
			name:     "no frontmatter — entire content is body",
			filename: "no-frontmatter",
			content:  "Just a plain markdown file without frontmatter.",
			check: func(t *testing.T, c Concept) {
				t.Helper()
				if c.Title != "" {
					t.Errorf("Title = %q, want empty (no frontmatter)", c.Title)
				}
				if !strings.Contains(c.Body, "plain markdown") {
					t.Errorf("Body = %q, should contain full content", c.Body)
				}
			},
		},
		{
			name:       "unclosed frontmatter returns error",
			filename:   "bad-fm",
			content:    "---\ntitle: broken\n",
			wantErr:    true,
			wantErrSub: "---",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseConcept(tc.filename, tc.content)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseConcept() expected error, got nil")
				}
				if tc.wantErrSub != "" && !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseConcept() unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, c)
			}
		})
	}
}

// ── IsImplementationType ──────────────────────────────────────────────────────

func TestIsImplementationType(t *testing.T) {
	cases := []struct {
		conceptType string
		want        bool
	}{
		{"FileSummary", true},
		{"APIEndpoint", true},
		{"Requirement", false},
		{"Decision", false},
		{"Playbook", false},
		{"Architecture", false},
		{"", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		t.Run(tc.conceptType, func(t *testing.T) {
			got := IsImplementationType(tc.conceptType)
			if got != tc.want {
				t.Errorf("IsImplementationType(%q) = %v, want %v", tc.conceptType, got, tc.want)
			}
		})
	}
}

// ── DeriveTrustTier ───────────────────────────────────────────────────────────

func TestDeriveTrustTier(t *testing.T) {
	cases := []struct {
		name       string
		verifiedBy string
		want       string
	}{
		{
			name:       "human prefix → human-reviewed",
			verifiedBy: "human:techlead",
			want:       "human-reviewed",
		},
		{
			name:       "human with different name → human-reviewed",
			verifiedBy: "human:alice",
			want:       "human-reviewed",
		},
		{
			name:       "non-human non-empty → machine-confirmed",
			verifiedBy: "bot:semantic-check",
			want:       "machine-confirmed",
		},
		{
			name:       "empty → unverified",
			verifiedBy: "",
			want:       "unverified",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Concept{VerifiedBy: tc.verifiedBy}
			got := DeriveTrustTier(c)
			if got != tc.want {
				t.Errorf("DeriveTrustTier(%q) = %q, want %q", tc.verifiedBy, got, tc.want)
			}
		})
	}
}
