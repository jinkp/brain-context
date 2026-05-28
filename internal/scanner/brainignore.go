package scanner

import (
	"os"
	"path/filepath"

	ignore "github.com/sabhiram/go-gitignore"
)

const brainignoreFile = ".brainignore"

// BrainIgnore wraps a compiled gitignore-compatible pattern matcher.
// When no .brainignore file exists, the matcher is nil and nothing is excluded.
//
// .brainignore uses full gitignore syntax:
//
//	# comments
//	*.css               — exclude all .css files
//	docs/               — exclude the docs directory
//	**/generated/**     — exclude any path containing a "generated" directory
//	!important.css      — re-include a previously excluded file
//	build/              — exclude build directory
//	*.test.js           — exclude test files
type BrainIgnore struct {
	matcher *ignore.GitIgnore
	lines   []string // raw non-comment, non-empty lines for reporting
}

// Exists returns true if a .brainignore file was found and loaded.
func (bi BrainIgnore) Exists() bool {
	return bi.matcher != nil
}

// MatchesPath returns true if the given path (relative, forward-slash) should be ignored.
func (bi BrainIgnore) MatchesPath(path string) bool {
	if bi.matcher == nil {
		return false
	}
	return bi.matcher.MatchesPath(path)
}

// Patterns returns the raw pattern lines for display/reporting purposes.
func (bi BrainIgnore) Patterns() []string {
	return bi.lines
}

// LoadBrainIgnore reads a .brainignore file from the given root directory.
// Returns a no-op BrainIgnore (Exists() == false) if the file does not exist.
func LoadBrainIgnore(root string) (BrainIgnore, error) {
	path := filepath.Join(root, brainignoreFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return BrainIgnore{}, nil
	}

	gi, err := ignore.CompileIgnoreFile(path)
	if err != nil {
		return BrainIgnore{}, err
	}

	lines, err := readPatternLines(path)
	if err != nil {
		// Non-fatal: matcher is valid, lines are just for display
		return BrainIgnore{matcher: gi}, nil
	}

	return BrainIgnore{matcher: gi, lines: lines}, nil
}

// readPatternLines extracts non-empty, non-comment lines for display.
func readPatternLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, raw := range splitLines(string(data)) {
		line := trimLine(raw)
		if line == "" || line[0] == '#' {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimLine(s string) string {
	// Trim \r and spaces
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
