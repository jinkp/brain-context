package okf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/indexer"
)

// writeTestSnapshot writes a fake snapshot JSON to ~/.brain/snapshots/{projectID}.json.
func writeTestSnapshot(t *testing.T, home, projectID string, files []string) {
	t.Helper()
	snapDir := filepath.Join(home, ".brain", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("create snapshots dir: %v", err)
	}

	snap := indexer.Snapshot{
		Files:     map[string]indexer.FileSnapshot{},
		UpdatedAt: time.Now().UTC(),
	}
	for _, f := range files {
		snap.Files[f] = indexer.FileSnapshot{ContentHash: "deadbeef"}
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	snapPath := filepath.Join(snapDir, projectID+".json")
	if err := os.WriteFile(snapPath, data, 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
}

func TestGenerateDrafts_CreatesFiles(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	projectID := "proj-gen"

	writeTestSnapshot(t, tmpHome, projectID, []string{
		"internal/auth/handler.go",
		"internal/payments/processor.go",
		"cmd/brain/main.go",
	})

	n, err := GenerateDrafts(projectID, bundleDir)
	if err != nil {
		t.Fatalf("GenerateDrafts: %v", err)
	}
	if n != 3 {
		t.Errorf("generated %d files, want 3", n)
	}

	// All 3 draft files should exist
	entries, _ := os.ReadDir(bundleDir)
	var mdFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			mdFiles = append(mdFiles, e.Name())
		}
	}
	if len(mdFiles) != 3 {
		t.Errorf("found %d .md files in bundle, want 3: %v", len(mdFiles), mdFiles)
	}
}

func TestGenerateDrafts_DraftContent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	projectID := "proj-content"

	writeTestSnapshot(t, tmpHome, projectID, []string{
		"internal/auth/handler.go",
	})

	if _, err := GenerateDrafts(projectID, bundleDir); err != nil {
		t.Fatalf("GenerateDrafts: %v", err)
	}

	// Find the generated file
	entries, _ := os.ReadDir(bundleDir)
	if len(entries) == 0 {
		t.Fatal("no files generated")
	}

	data, err := os.ReadFile(filepath.Join(bundleDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "status: draft") {
		t.Errorf("generated file missing 'status: draft': %q", content)
	}
	if !strings.Contains(content, "type: FileSummary") {
		t.Errorf("generated file missing 'type: FileSummary': %q", content)
	}
	if !strings.Contains(content, "internal/auth/handler.go") {
		t.Errorf("generated file missing source_files entry: %q", content)
	}
}

func TestGenerateDrafts_Idempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	projectID := "proj-idem-gen"

	writeTestSnapshot(t, tmpHome, projectID, []string{
		"internal/auth/handler.go",
		"internal/payments/processor.go",
	})

	n1, err := GenerateDrafts(projectID, bundleDir)
	if err != nil {
		t.Fatalf("GenerateDrafts first call: %v", err)
	}

	n2, err := GenerateDrafts(projectID, bundleDir)
	if err != nil {
		t.Fatalf("GenerateDrafts second call: %v", err)
	}

	// Both calls write the same count (draft files are overwritten — idempotent)
	if n1 != n2 {
		t.Errorf("first call wrote %d, second call wrote %d — counts differ (should be idempotent)", n1, n2)
	}
	if n1 != 2 {
		t.Errorf("expected 2 files generated, got %d", n1)
	}
}

func TestGenerateDrafts_StableNotOverwritten(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	projectID := "proj-stable"

	writeTestSnapshot(t, tmpHome, projectID, []string{
		"internal/auth/handler.go",
	})

	// Compute the expected concept ID for handler.go
	conceptID := sanitizeConceptID("internal/auth/handler.go")
	stableContent := "---\ntitle: \"Stable Auth\"\ntype: FileSummary\nstatus: stable\n---\n\nThis is curated content.\n"

	// Pre-create the concept file as stable
	stableFile := filepath.Join(bundleDir, conceptID+".md")
	if err := os.WriteFile(stableFile, []byte(stableContent), 0o644); err != nil {
		t.Fatalf("write stable file: %v", err)
	}

	n, err := GenerateDrafts(projectID, bundleDir)
	if err != nil {
		t.Fatalf("GenerateDrafts: %v", err)
	}

	// Stable file should not be overwritten → 0 files written
	if n != 0 {
		t.Errorf("GenerateDrafts wrote %d files, want 0 (stable file should be preserved)", n)
	}

	// Verify the stable content is still intact
	data, err := os.ReadFile(stableFile)
	if err != nil {
		t.Fatalf("read stable file: %v", err)
	}
	if !strings.Contains(string(data), "This is curated content.") {
		t.Errorf("stable file was overwritten! got: %q", string(data))
	}
}

func TestGenerateDrafts_NoSnapshot(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()

	_, err := GenerateDrafts("proj-nosnapshot", bundleDir)
	if err == nil {
		t.Error("GenerateDrafts should return error when snapshot does not exist")
	}
	if !strings.Contains(err.Error(), "snapshot") {
		t.Errorf("error should mention 'snapshot', got: %v", err)
	}
}

// ── sanitizeConceptID ─────────────────────────────────────────────────────────

func TestSanitizeConceptID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"internal/auth/handler.go", "internal-auth-handler"},
		{"cmd/brain/main.go", "cmd-brain-main"},
		{"simple.go", "simple"},
		{"path/with.dots/file.test.go", "path-with-dots-file-test"},
		{"internal/auth/handler.test.go", "internal-auth-handler-test"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeConceptID(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeConceptID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
