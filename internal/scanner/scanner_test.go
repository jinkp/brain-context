package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestScanMergesAllExcludeSources(t *testing.T) {
	// Create a temp dir with structure:
	// root/
	//   main.go
	//   global_excluded/file.go     (excluded by GlobalExclude)
	//   project_excluded/file.go    (excluded by ProjectExclude)
	//   ignore_excluded/file.go     (excluded by .brainignore)
	//   cli_excluded/file.go        (excluded by CLI flag Exclude)
	//   kept/file.go                (should be kept)

	root := t.TempDir()
	dirs := []string{"global_excluded", "project_excluded", "ignore_excluded", "cli_excluded", "kept"}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(root, d), 0o755)
		os.WriteFile(filepath.Join(root, d, "file.go"), []byte("package "+d), 0o644)
	}
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)

	// Write .brainignore
	os.WriteFile(filepath.Join(root, ".brainignore"), []byte("[exclude]\nignore_excluded\n"), 0o644)

	opts := ScanOptions{
		GlobalExclude:  []string{"global_excluded"},
		ProjectExclude: []string{"project_excluded"},
		Exclude:        []string{"cli_excluded"},
	}

	files, err := Scan(root, opts)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := make([]string, 0, len(files))
	for _, f := range files {
		got = append(got, f.Path)
	}
	sort.Strings(got)

	want := []string{"kept/file.go", "main.go"}
	if !slicesEqual(got, want) {
		t.Errorf("scanned files = %v, want %v", got, want)
	}
}

func TestScanMergesAllIncludeSources(t *testing.T) {
	// When a stack is set, only matching extensions are scanned.
	// Include from all sources should also be allowed.

	root := t.TempDir()

	// Create files with different extensions
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(root, "schema.proto"), []byte("syntax = \"proto3\";"), 0o644)
	os.WriteFile(filepath.Join(root, "query.graphql"), []byte("query {}"), 0o644)
	os.WriteFile(filepath.Join(root, "api.thrift"), []byte("service API {}"), 0o644)
	os.WriteFile(filepath.Join(root, "data.avro"), []byte("{}"), 0o644)

	opts := ScanOptions{
		Stack:          "go", // only .go, .mod, .sum, .sql, .yaml, .yml, .json, .md, .toml
		GlobalInclude:  []string{".proto"},
		ProjectInclude: []string{".graphql"},
		Include:        []string{".thrift"},
		// .avro is NOT included anywhere — should be excluded
	}

	files, err := Scan(root, opts)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := make([]string, 0, len(files))
	for _, f := range files {
		got = append(got, f.Path)
	}
	sort.Strings(got)

	want := []string{"api.thrift", "main.go", "query.graphql", "schema.proto"}
	if !slicesEqual(got, want) {
		t.Errorf("scanned files = %v, want %v", got, want)
	}
}

func TestScanHardcodedIgnoresAlwaysApplied(t *testing.T) {
	// Hardcoded ignores (node_modules, .git, etc.) should always be skipped,
	// even if no exclude options are set.

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

	got := make([]string, 0, len(files))
	for _, f := range files {
		got = append(got, f.Path)
	}

	if len(got) != 1 || got[0] != "main.go" {
		t.Errorf("scanned files = %v, want [main.go]", got)
	}
}

func TestScanBrainignoreIncludeWithStack(t *testing.T) {
	// .brainignore [include] extensions should be added when a stack is set.

	root := t.TempDir()

	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(root, "custom.xyz"), []byte("custom data"), 0o644)

	// .brainignore adds .xyz as include
	os.WriteFile(filepath.Join(root, ".brainignore"), []byte("[include]\n.xyz\n"), 0o644)

	opts := ScanOptions{
		Stack: "go",
	}

	files, err := Scan(root, opts)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	got := make([]string, 0, len(files))
	for _, f := range files {
		got = append(got, f.Path)
	}
	sort.Strings(got)

	want := []string{"custom.xyz", "main.go"}
	if !slicesEqual(got, want) {
		t.Errorf("scanned files = %v, want %v", got, want)
	}
}
