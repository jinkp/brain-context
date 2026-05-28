package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/brain-context/internal/scanner"
)

func TestFormatPreviewScan_NoConfig(t *testing.T) {
	files := []scanner.ScannedFile{
		{Path: "main.go", Language: "Go", SizeBytes: 100},
		{Path: "cmd/app.go", Language: "Go", SizeBytes: 200},
	}

	bi := scanner.BrainIgnore{} // no file = Exists() == false
	result := formatPreviewScan("/tmp/repo", "", nil, nil, nil, nil, bi, files)

	if !strings.Contains(result, "Total files**: 2") {
		t.Errorf("expected total files 2, got:\n%s", result)
	}
	if !strings.Contains(result, "No custom exclude/include configured") {
		t.Errorf("expected 'no custom' message, got:\n%s", result)
	}
}

func TestFormatPreviewScan_WithAllSources(t *testing.T) {
	files := []scanner.ScannedFile{
		{Path: "main.go", Language: "Go"},
		{Path: "schema.proto", Language: "Protobuf"},
		{Path: "src/app.ts", Language: "TypeScript"},
	}

	// Create a real .brainignore to get a valid BrainIgnore with Exists()==true
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, ".brainignore"), []byte("*.css\ndocs/\n"), 0o644)
	bi, _ := scanner.LoadBrainIgnore(tmpDir)

	result := formatPreviewScan(
		"/home/user/repo", "go",
		[]string{"Migrations"}, []string{".proto"},
		[]string{"Tests"}, []string{".graphql"},
		bi, files,
	)

	checks := []string{
		"Global config exclude**: Migrations",
		"Global config include**: .proto",
		"Project config exclude**: Tests",
		"Project config include**: .graphql",
		".brainignore** (2 patterns)",
		"Go: ",
		"Protobuf: ",
		"TypeScript: ",
		"Total files**: 3",
		"Stack**: go",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("expected %q in output, got:\n%s", check, result)
		}
	}
}

func TestFormatPreviewScan_GroupsByDirectory(t *testing.T) {
	files := []scanner.ScannedFile{
		{Path: "main.go", Language: "Go"},
		{Path: "internal/api/server.go", Language: "Go"},
		{Path: "internal/api/handler.go", Language: "Go"},
		{Path: "cmd/app.go", Language: "Go"},
	}

	result := formatPreviewScan("/repo", "", nil, nil, nil, nil, scanner.BrainIgnore{}, files)

	if !strings.Contains(result, "**Root**") {
		t.Errorf("expected Root group, got:\n%s", result)
	}
	if !strings.Contains(result, "**internal/**") {
		t.Errorf("expected internal/ group, got:\n%s", result)
	}
	if !strings.Contains(result, "**cmd/**") {
		t.Errorf("expected cmd/ group, got:\n%s", result)
	}
}
