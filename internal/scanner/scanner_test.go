package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestScanMergesAllExcludeSources(t *testing.T) {
	root := t.TempDir()
	dirs := []string{"global_excluded", "project_excluded", "cli_excluded", "kept"}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(root, d), 0o755)
		os.WriteFile(filepath.Join(root, d, "file.go"), []byte("package "+d), 0o644)
	}
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)

	opts := ScanOptions{
		GlobalExclude:  []string{"global_excluded"},
		ProjectExclude: []string{"project_excluded"},
		Exclude:        []string{"cli_excluded"},
	}

	files, err := Scan(root, opts)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := filePaths(files)
	want := []string{"kept/file.go", "main.go"}
	if !slicesEqual(got, want) {
		t.Errorf("scanned files = %v, want %v", got, want)
	}
}

func TestScanMergesAllIncludeSources(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(root, "schema.proto"), []byte("syntax = \"proto3\";"), 0o644)
	os.WriteFile(filepath.Join(root, "query.graphql"), []byte("query {}"), 0o644)
	os.WriteFile(filepath.Join(root, "api.thrift"), []byte("service API {}"), 0o644)
	os.WriteFile(filepath.Join(root, "data.avro"), []byte("{}"), 0o644)

	opts := ScanOptions{
		Stack:          "go",
		GlobalInclude:  []string{".proto"},
		ProjectInclude: []string{".graphql"},
		Include:        []string{".thrift"},
	}

	files, err := Scan(root, opts)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := filePaths(files)
	want := []string{"api.thrift", "main.go", "query.graphql", "schema.proto"}
	if !slicesEqual(got, want) {
		t.Errorf("scanned files = %v, want %v", got, want)
	}
}

func TestScanHardcodedIgnoresAlwaysApplied(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)
	os.MkdirAll(filepath.Join(root, "node_modules"), 0o755)
	os.WriteFile(filepath.Join(root, "node_modules", "dep.js"), []byte("module.exports={}"), 0o644)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]"), 0o644)

	files, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := filePaths(files)
	if len(got) != 1 || got[0] != "main.go" {
		t.Errorf("scanned files = %v, want [main.go]", got)
	}
}

func TestScanBrainignoreGlobPatterns(t *testing.T) {
	root := t.TempDir()

	// Create file structure
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(root, "style.css"), []byte("body{}"), 0o644)
	os.WriteFile(filepath.Join(root, "app.test.js"), []byte("test()"), 0o644)
	os.WriteFile(filepath.Join(root, "app.js"), []byte("app()"), 0o644)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.WriteFile(filepath.Join(root, "docs", "readme.md"), []byte("# docs"), 0o644)
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "index.ts"), []byte("export {}"), 0o644)
	os.WriteFile(filepath.Join(root, "src", "deep.css"), []byte(".a{}"), 0o644)

	// .brainignore: exclude all CSS, test files, and docs/
	os.WriteFile(filepath.Join(root, ".brainignore"), []byte("*.css\n*.test.js\ndocs/\n"), 0o644)

	files, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := filePaths(files)
	want := []string{"app.js", "main.go", "src/index.ts"}
	if !slicesEqual(got, want) {
		t.Errorf("scanned files = %v, want %v", got, want)
	}
}

func TestScanBrainignoreNegation(t *testing.T) {
	root := t.TempDir()

	os.WriteFile(filepath.Join(root, "debug.log"), []byte("debug"), 0o644)
	os.WriteFile(filepath.Join(root, "error.log"), []byte("error"), 0o644)
	os.WriteFile(filepath.Join(root, "important.log"), []byte("important"), 0o644)
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)

	// Exclude all .log files EXCEPT important.log
	os.WriteFile(filepath.Join(root, ".brainignore"), []byte("*.log\n!important.log\n"), 0o644)

	files, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := filePaths(files)
	want := []string{"important.log", "main.go"}
	if !slicesEqual(got, want) {
		t.Errorf("scanned files = %v, want %v", got, want)
	}
}

func TestScanBrainignoreDoubleStarGlob(t *testing.T) {
	root := t.TempDir()

	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)
	os.MkdirAll(filepath.Join(root, "src", "generated"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "generated", "model.go"), []byte("package gen"), 0o644)
	os.MkdirAll(filepath.Join(root, "src", "real"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "real", "handler.go"), []byte("package real"), 0o644)

	// Exclude anything under any "generated" directory
	os.WriteFile(filepath.Join(root, ".brainignore"), []byte("**/generated/**\n"), 0o644)

	files, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := filePaths(files)
	want := []string{"main.go", "src/real/handler.go"}
	if !slicesEqual(got, want) {
		t.Errorf("scanned files = %v, want %v", got, want)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func filePaths(files []ScannedFile) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	sort.Strings(paths)
	return paths
}

func slicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
