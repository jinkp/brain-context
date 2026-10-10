package okf

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/indexer"
)

// CheckStaleness walks all .md files in bundlePath, parses their frontmatter,
// and returns concept IDs whose source_files overlap with files changed or
// deleted in diff (ModifiedFiles + DeletedFiles).
//
// Deprecated concepts are skipped — they are already flagged and irrelevant.
func CheckStaleness(bundlePath string, diff indexer.DiffResult) ([]string, error) {
	// Build a lookup set from the diff for O(1) membership tests
	changed := make(map[string]struct{}, len(diff.ModifiedFiles)+len(diff.DeletedFiles))
	for _, f := range diff.ModifiedFiles {
		changed[f] = struct{}{}
	}
	for _, f := range diff.DeletedFiles {
		changed[f] = struct{}{}
	}

	var staleIDs []string

	filepath.Walk(bundlePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(bundlePath, path)
		if err != nil {
			rel = info.Name()
		}
		conceptID := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		concept, err := ParseConcept(conceptID, string(data))
		if err != nil || concept.Status == "deprecated" {
			return nil
		}
		for _, srcFile := range concept.SourceFiles {
			if _, hit := changed[srcFile]; hit {
				staleIDs = append(staleIDs, concept.ConceptID)
				break
			}
		}
		return nil
	})

	return staleIDs, nil
}
