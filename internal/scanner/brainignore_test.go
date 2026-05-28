package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBrainIgnore(t *testing.T) {
	tests := []struct {
		name        string
		content     string // if empty, file is not created
		wantExclude []string
		wantInclude []string
		wantErr     bool
	}{
		{
			name:        "file does not exist",
			content:     "",
			wantExclude: nil,
			wantInclude: nil,
		},
		{
			name: "exclude only with default section",
			content: `# Directories to exclude
assets
Migrations/
Tests\
`,
			wantExclude: []string{"assets", "Migrations", "Tests"},
			wantInclude: nil,
		},
		{
			name: "include only section",
			content: `[include]
.proto
.graphql
sql
`,
			wantExclude: nil,
			wantInclude: []string{".proto", ".graphql", ".sql"},
		},
		{
			name: "both sections",
			content: `[exclude]
assets
docs/

[include]
.proto
`,
			wantExclude: []string{"assets", "docs"},
			wantInclude: []string{".proto"},
		},
		{
			name: "comments and blank lines ignored",
			content: `# This is a comment

assets
# Another comment

[include]
# Include proto files
.proto
`,
			wantExclude: []string{"assets"},
			wantInclude: []string{".proto"},
		},
		{
			name: "section headers are case insensitive",
			content: `[EXCLUDE]
assets
[INCLUDE]
.proto
`,
			wantExclude: []string{"assets"},
			wantInclude: []string{".proto"},
		},
		{
			name: "mixed sections switch back and forth",
			content: `[exclude]
assets

[include]
.proto

[exclude]
Migrations

[include]
.graphql
`,
			wantExclude: []string{"assets", "Migrations"},
			wantInclude: []string{".proto", ".graphql"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			if tt.content != "" {
				err := os.WriteFile(filepath.Join(dir, ".brainignore"), []byte(tt.content), 0o644)
				if err != nil {
					t.Fatalf("write .brainignore: %v", err)
				}
			}

			bi, err := LoadBrainIgnore(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}

			if !slicesEqual(bi.ExcludeDirs, tt.wantExclude) {
				t.Errorf("ExcludeDirs = %v, want %v", bi.ExcludeDirs, tt.wantExclude)
			}
			if !slicesEqual(bi.IncludeExts, tt.wantInclude) {
				t.Errorf("IncludeExts = %v, want %v", bi.IncludeExts, tt.wantInclude)
			}
		})
	}
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
