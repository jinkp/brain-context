package scanner

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const brainignoreFile = ".brainignore"

// BrainIgnore holds parsed patterns from a .brainignore file.
type BrainIgnore struct {
	ExcludeDirs []string // directory names or relative paths to exclude
	IncludeExts []string // file extensions to include (e.g. ".proto")
}

// LoadBrainIgnore reads a .brainignore file from the given root directory.
// Returns an empty BrainIgnore (no error) if the file does not exist.
func LoadBrainIgnore(root string) (BrainIgnore, error) {
	path := filepath.Join(root, brainignoreFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BrainIgnore{}, nil
		}
		return BrainIgnore{}, err
	}
	defer f.Close()
	return parseBrainIgnore(f)
}

// parseBrainIgnore parses a .brainignore file from an io.Reader.
//
// Syntax:
//
//	# comment lines (leading #)
//	[exclude]     — section header: lines below are dirs to exclude
//	[include]     — section header: lines below are extensions to include
//	assets/       — directory to exclude (trailing / optional)
//	Migrations    — directory to exclude
//	.proto        — extension to include (leading . required)
//
// Lines outside any section are treated as exclude directories by default.
func parseBrainIgnore(r *os.File) (BrainIgnore, error) {
	var bi BrainIgnore
	section := "exclude" // default section

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section headers
		lower := strings.ToLower(line)
		if lower == "[exclude]" {
			section = "exclude"
			continue
		}
		if lower == "[include]" {
			section = "include"
			continue
		}

		switch section {
		case "exclude":
			// Normalize: strip trailing slashes
			dir := strings.TrimSuffix(line, "/")
			dir = strings.TrimSuffix(dir, "\\")
			dir = strings.TrimSpace(dir)
			if dir != "" {
				bi.ExcludeDirs = append(bi.ExcludeDirs, dir)
			}
		case "include":
			ext := strings.TrimSpace(line)
			if ext == "" {
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			bi.IncludeExts = append(bi.IncludeExts, ext)
		}
	}
	if err := sc.Err(); err != nil {
		return BrainIgnore{}, err
	}
	return bi, nil
}
