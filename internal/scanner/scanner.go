package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type ScannedFile struct {
	Path        string
	Language    string
	SizeBytes   int64
	ContentHash string
}

var ignoredDirs = map[string]struct{}{
	"node_modules": {},
	".git":         {},
	"bin":          {},
	"obj":          {},
	"dist":         {},
	"build":        {},
	"vendor":       {},
	"coverage":     {},
}

var ignoredSuffixes = []string{
	".min.js",
	".lock",
	".png",
	".jpg",
	".jpeg",
	".gif",
	".zip",
	".pdf",
	".exe",
	".dll",
	".so",
}

var languagesByExtension = map[string]string{
	".go":   "Go",
	".py":   "Python",
	".ts":   "TypeScript",
	".tsx":  "TypeScript",
	".js":   "JavaScript",
	".jsx":  "JavaScript",
	".java": "Java",
	".cs":   "CSharp",
	".rs":   "Rust",
	".rb":   "Ruby",
	".php":  "PHP",
	".md":   "Markdown",
	".sql":  "SQL",
	".yaml": "YAML",
	".yml":  "YAML",
	".json": "JSON",
}

func Scan(root string) ([]ScannedFile, error) {
	absRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve scan root: %w", err)
	}

	files := make([]ScannedFile, 0)
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if shouldIgnoreDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldIgnoreFile(entry.Name()) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read file info %s: %w", path, err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			return fmt.Errorf("resolve relative path %s: %w", path, err)
		}

		files = append(files, ScannedFile{
			Path:        filepath.ToSlash(relPath),
			Language:    DetectLanguage(path),
			SizeBytes:   info.Size(),
			ContentHash: hashBytes(data),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory tree: %w", err)
	}

	return files, nil
}

func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if language, ok := languagesByExtension[ext]; ok {
		return language
	}
	return "Unknown"
}

func shouldIgnoreDir(name string) bool {
	_, ignored := ignoredDirs[strings.ToLower(strings.TrimSpace(name))]
	return ignored
}

func shouldIgnoreFile(name string) bool {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	for _, suffix := range ignoredSuffixes {
		if strings.HasSuffix(lowerName, suffix) {
			return true
		}
	}
	return false
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
