package okf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Gentleman-Programming/brain-context/internal/indexer"
)

// writeConcept writes a minimal OKF .md file to dir.
func writeConcept(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write concept %s: %v", filename, err)
	}
}

func TestCheckStaleness_MatchesModified(t *testing.T) {
	bundleDir := t.TempDir()

	writeConcept(t, bundleDir, "auth-handler.md", `---
title: "Auth Handler"
type: FileSummary
status: stable
source_files:
  - internal/auth/handler.go
---
`)

	diff := indexer.DiffResult{
		ModifiedFiles: []string{"internal/auth/handler.go"},
	}

	staleIDs, err := CheckStaleness(bundleDir, diff)
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(staleIDs) != 1 || staleIDs[0] != "auth-handler" {
		t.Errorf("staleIDs = %v, want [auth-handler]", staleIDs)
	}
}

func TestCheckStaleness_MatchesDeleted(t *testing.T) {
	bundleDir := t.TempDir()

	writeConcept(t, bundleDir, "payment-processor.md", `---
title: "Payment Processor"
type: FileSummary
status: stable
source_files:
  - internal/payments/processor.go
---
`)

	diff := indexer.DiffResult{
		DeletedFiles: []string{"internal/payments/processor.go"},
	}

	staleIDs, err := CheckStaleness(bundleDir, diff)
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(staleIDs) != 1 || staleIDs[0] != "payment-processor" {
		t.Errorf("staleIDs = %v, want [payment-processor]", staleIDs)
	}
}

func TestCheckStaleness_NoMatch(t *testing.T) {
	bundleDir := t.TempDir()

	writeConcept(t, bundleDir, "auth-handler.md", `---
title: "Auth Handler"
type: FileSummary
status: stable
source_files:
  - internal/auth/handler.go
---
`)

	diff := indexer.DiffResult{
		ModifiedFiles: []string{"cmd/brain/main.go"},
	}

	staleIDs, err := CheckStaleness(bundleDir, diff)
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(staleIDs) != 0 {
		t.Errorf("staleIDs = %v, want empty (source file not in diff)", staleIDs)
	}
}

func TestCheckStaleness_NoSourceFiles(t *testing.T) {
	bundleDir := t.TempDir()

	// Concept without source_files — cannot be stale
	writeConcept(t, bundleDir, "general-requirement.md", `---
title: "General Requirement"
type: Requirement
status: stable
---
`)

	diff := indexer.DiffResult{
		ModifiedFiles: []string{"internal/auth/handler.go"},
	}

	staleIDs, err := CheckStaleness(bundleDir, diff)
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(staleIDs) != 0 {
		t.Errorf("staleIDs = %v, want empty (no source_files to match)", staleIDs)
	}
}

func TestCheckStaleness_DeprecatedSkipped(t *testing.T) {
	bundleDir := t.TempDir()

	writeConcept(t, bundleDir, "old-handler.md", `---
title: "Old Handler"
type: FileSummary
status: deprecated
source_files:
  - internal/auth/handler.go
---
`)

	diff := indexer.DiffResult{
		ModifiedFiles: []string{"internal/auth/handler.go"},
	}

	staleIDs, err := CheckStaleness(bundleDir, diff)
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(staleIDs) != 0 {
		t.Errorf("staleIDs = %v, want empty (deprecated concepts are skipped)", staleIDs)
	}
}

func TestCheckStaleness_EmptyDiff(t *testing.T) {
	bundleDir := t.TempDir()

	writeConcept(t, bundleDir, "auth.md", `---
title: "Auth"
type: FileSummary
status: stable
source_files:
  - internal/auth/handler.go
---
`)

	diff := indexer.DiffResult{} // completely empty

	staleIDs, err := CheckStaleness(bundleDir, diff)
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(staleIDs) != 0 {
		t.Errorf("staleIDs = %v, want empty (empty diff)", staleIDs)
	}
}

func TestCheckStaleness_MultipleConceptsPartialMatch(t *testing.T) {
	bundleDir := t.TempDir()

	writeConcept(t, bundleDir, "auth.md", `---
title: "Auth"
type: FileSummary
status: stable
source_files:
  - internal/auth/handler.go
---
`)
	writeConcept(t, bundleDir, "payments.md", `---
title: "Payments"
type: FileSummary
status: stable
source_files:
  - internal/payments/processor.go
---
`)

	// Only auth changed
	diff := indexer.DiffResult{
		ModifiedFiles: []string{"internal/auth/handler.go"},
	}

	staleIDs, err := CheckStaleness(bundleDir, diff)
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(staleIDs) != 1 {
		t.Errorf("staleIDs = %v, want [auth]", staleIDs)
	}
	if len(staleIDs) == 1 && staleIDs[0] != "auth" {
		t.Errorf("staleIDs[0] = %q, want %q", staleIDs[0], "auth")
	}
}
