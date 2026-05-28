package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBrainIgnore_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	bi, err := LoadBrainIgnore(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bi.Exists() {
		t.Error("expected Exists() == false when no file")
	}
	if bi.MatchesPath("anything.go") {
		t.Error("expected no matches when no file")
	}
}

func TestLoadBrainIgnore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".brainignore"), []byte(""), 0o644)

	bi, err := LoadBrainIgnore(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bi.Exists() {
		t.Error("expected Exists() == true for empty file")
	}
	if len(bi.Patterns()) != 0 {
		t.Errorf("expected 0 patterns, got %d", len(bi.Patterns()))
	}
}

func TestLoadBrainIgnore_GitignorePatterns(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		matchYes  []string // paths that SHOULD be ignored
		matchNo   []string // paths that should NOT be ignored
		wantLines int
	}{
		{
			name:      "glob extension *.css",
			content:   "*.css\n",
			matchYes:  []string{"style.css", "src/main.css", "deep/nested/file.css"},
			matchNo:   []string{"style.scss", "main.go", "css-file.txt"},
			wantLines: 1,
		},
		{
			name:      "directory with trailing slash",
			content:   "docs/\n",
			matchYes:  []string{"docs/readme.md", "docs/api/spec.yaml"},
			matchNo:   []string{"src/docs.go", "my-docs.txt"},
			wantLines: 1,
		},
		{
			name:      "double star glob",
			content:   "**/generated/**\n",
			matchYes:  []string{"generated/file.go", "src/generated/model.go", "a/b/generated/c/d.go"},
			matchNo:   []string{"src/generator.go", "main.go"},
			wantLines: 1,
		},
		{
			name:      "negation with !",
			content:   "*.log\n!important.log\n",
			matchYes:  []string{"debug.log", "error.log", "dir/app.log"},
			matchNo:   []string{"important.log", "main.go"},
			wantLines: 2,
		},
		{
			name:      "multiple patterns",
			content:   "# Build artifacts\ndist/\nbuild/\n\n# Test files\n*.test.js\n*.spec.ts\n",
			matchYes:  []string{"dist/bundle.js", "build/output.wasm", "app.test.js", "util.spec.ts"},
			matchNo:   []string{"src/app.js", "main.ts", "test-utils.js"},
			wantLines: 4,
		},
		{
			name:      "leading slash anchors to root",
			content:   "/config.yaml\n",
			matchYes:  []string{"config.yaml"},
			matchNo:   []string{"sub/config.yaml"},
			wantLines: 1,
		},
		{
			name:      "subdirectory pattern",
			content:   "src/assets/\n",
			matchYes:  []string{"src/assets/logo.png", "src/assets/fonts/inter.woff2"},
			matchNo:   []string{"assets/logo.png", "src/main.go"},
			wantLines: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, ".brainignore"), []byte(tt.content), 0o644)

			bi, err := LoadBrainIgnore(dir)
			if err != nil {
				t.Fatalf("LoadBrainIgnore error: %v", err)
			}
			if !bi.Exists() {
				t.Fatal("expected Exists() == true")
			}
			if len(bi.Patterns()) != tt.wantLines {
				t.Errorf("patterns count = %d, want %d", len(bi.Patterns()), tt.wantLines)
			}

			for _, path := range tt.matchYes {
				if !bi.MatchesPath(path) {
					t.Errorf("expected %q to be ignored", path)
				}
			}
			for _, path := range tt.matchNo {
				if bi.MatchesPath(path) {
					t.Errorf("expected %q to NOT be ignored", path)
				}
			}
		})
	}
}
