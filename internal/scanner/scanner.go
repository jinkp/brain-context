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

// ScanOptions controls which files the scanner processes.
type ScanOptions struct {
	Stack      string   // predefined stack filter (go, dotnet, react, python, java, rust)
	Include    []string // extra extensions to include (e.g. ".proto", ".graphql")
	Exclude    []string // directories to exclude (e.g. "assets/", "Migrations/")
	Verbose    bool     // print each file as it's scanned
	OnFile     func(path string) // callback for progress reporting
}

// ── Stacks ────────────────────────────────────────────────────────────────────

var stacks = map[string][]string{
	"go":      {".go", ".mod", ".sum", ".sql", ".yaml", ".yml", ".json", ".md", ".toml"},
	"dotnet":  {".cs", ".csproj", ".sln", ".json", ".yaml", ".yml", ".sql", ".md", ".razor", ".cshtml", ".proto", ".xml"},
	"react":   {".ts", ".tsx", ".js", ".jsx", ".json", ".css", ".scss", ".md", ".html", ".svg"},
	"vue":     {".vue", ".ts", ".js", ".json", ".css", ".scss", ".md", ".html"},
	"angular": {".ts", ".html", ".css", ".scss", ".json", ".md"},
	"node":    {".ts", ".js", ".json", ".yaml", ".yml", ".sql", ".md", ".graphql", ".prisma"},
	"python":  {".py", ".pyi", ".sql", ".yaml", ".yml", ".json", ".md", ".toml", ".cfg"},
	"java":    {".java", ".xml", ".yaml", ".yml", ".sql", ".json", ".md", ".gradle", ".properties"},
	"rust":    {".rs", ".toml", ".sql", ".yaml", ".yml", ".json", ".md"},
	"ruby":    {".rb", ".rake", ".gemspec", ".yaml", ".yml", ".json", ".sql", ".md", ".erb"},
	"php":     {".php", ".blade.php", ".json", ".yaml", ".yml", ".sql", ".md", ".xml"},
}

// ListStacks returns all available stack names.
func ListStacks() []string {
	names := make([]string, 0, len(stacks))
	for k := range stacks {
		names = append(names, k)
	}
	return names
}

// ── Default ignore lists ──────────────────────────────────────────────────────

var ignoredDirs = map[string]struct{}{
	"node_modules":  {},
	".git":          {},
	".opencode":     {},
	".claude":       {},
	".cursor":       {},
	".vscode":       {},
	".idea":         {},
	".vs":           {},
	".atl":          {},
	".brain":        {},
	".engram":       {},
	"bin":           {},
	"obj":           {},
	"dist":          {},
	"build":         {},
	"vendor":        {},
	"coverage":      {},
	"__pycache__":   {},
	".next":         {},
	".nuxt":         {},
	".output":       {},
	"target":        {},
	"packages":      {},
}

var ignoredSuffixes = []string{
	".min.js",
	".lock",
	".png", ".jpg", ".jpeg", ".gif", ".ico", ".svg", ".webp",
	".zip", ".tar", ".gz", ".rar",
	".pdf",
	".exe", ".dll", ".so", ".dylib",
	".woff", ".woff2", ".ttf", ".eot",
	".mp3", ".mp4", ".avi", ".mov",
	".db", ".sqlite", ".sqlite3",
	".snap",
}

var languagesByExtension = map[string]string{
	".go":      "Go",
	".py":      "Python",
	".pyi":     "Python",
	".ts":      "TypeScript",
	".tsx":     "TypeScript",
	".js":      "JavaScript",
	".jsx":     "JavaScript",
	".java":    "Java",
	".cs":      "CSharp",
	".rs":      "Rust",
	".rb":      "Ruby",
	".php":     "PHP",
	".vue":     "Vue",
	".md":      "Markdown",
	".sql":     "SQL",
	".yaml":    "YAML",
	".yml":     "YAML",
	".json":    "JSON",
	".xml":     "XML",
	".html":    "HTML",
	".css":     "CSS",
	".scss":    "SCSS",
	".proto":   "Protobuf",
	".graphql": "GraphQL",
	".toml":    "TOML",
	".razor":   "Razor",
	".cshtml":  "Razor",
	".prisma":  "Prisma",
}

// ── Scanner ───────────────────────────────────────────────────────────────────

func Scan(root string, opts ...ScanOptions) ([]ScannedFile, error) {
	var opt ScanOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	absRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve scan root: %w", err)
	}

	// Build allowed extensions set from stack + includes
	allowedExts := buildAllowedExtensions(opt)

	// Build extra excluded dirs
	excludedDirs := buildExcludedDirs(opt)

	files := make([]ScannedFile, 0)
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip errors
		}

		if entry.IsDir() {
			name := strings.ToLower(entry.Name())
			if shouldIgnoreDir(name) || excludedDirs[name] {
				return filepath.SkipDir
			}
			// Also check relative path for nested excludes like "src/assets"
			if len(excludedDirs) > 0 {
				rel, _ := filepath.Rel(absRoot, path)
				if rel != "" {
					relSlash := strings.ToLower(filepath.ToSlash(rel))
					for dir := range excludedDirs {
						if relSlash == dir || strings.HasPrefix(relSlash, dir+"/") {
							return filepath.SkipDir
						}
					}
				}
			}
			return nil
		}

		if shouldIgnoreFile(entry.Name()) {
			return nil
		}

		// Skip irregular files (symlinks, devices, pipes)
		if !entry.Type().IsRegular() {
			return nil
		}

		// Stack filter: only process allowed extensions
		if len(allowedExts) > 0 {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if !allowedExts[ext] {
				return nil
			}
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}

		slashPath := filepath.ToSlash(relPath)

		// Progress callback
		if opt.OnFile != nil {
			opt.OnFile(slashPath)
		}

		files = append(files, ScannedFile{
			Path:        slashPath,
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

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildAllowedExtensions(opt ScanOptions) map[string]bool {
	if opt.Stack == "" && len(opt.Include) == 0 {
		return nil // no filter — scan everything
	}

	allowed := make(map[string]bool)

	// Add stack extensions
	if exts, ok := stacks[strings.ToLower(opt.Stack)]; ok {
		for _, ext := range exts {
			allowed[ext] = true
		}
	}

	// Add extra includes
	for _, ext := range opt.Include {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		allowed[strings.ToLower(ext)] = true
	}

	return allowed
}

func buildExcludedDirs(opt ScanOptions) map[string]bool {
	if len(opt.Exclude) == 0 {
		return nil
	}
	dirs := make(map[string]bool, len(opt.Exclude))
	for _, d := range opt.Exclude {
		d = strings.TrimSpace(d)
		d = strings.TrimSuffix(d, "/")
		d = strings.TrimSuffix(d, "\\")
		if d != "" {
			dirs[strings.ToLower(d)] = true
		}
	}
	return dirs
}

func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if language, ok := languagesByExtension[ext]; ok {
		return language
	}
	return "Unknown"
}

func shouldIgnoreDir(name string) bool {
	_, ignored := ignoredDirs[name]
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
