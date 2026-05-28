package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/scanner"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func previewScanTool() mcpgo.Tool {
	return mcpgo.NewTool("preview_scan",
		mcpgo.WithDescription(
			"Preview which files would be scanned for a project WITHOUT actually indexing. "+
				"Shows the active filtering configuration (.brainignore, config exclude/include, "+
				"hardcoded ignores) and lists all files that would be processed. "+
				"Use this to validate scan configuration before running brain index."),
		mcpgo.WithString("project_id", mcpgo.Required(), mcpgo.Description("Project ID or project name")),
		mcpgo.WithString("stack", mcpgo.Description("Stack filter: go, dotnet, react, vue, angular, node, python, java, rust, ruby, php")),
	)
}

func (s *Server) handlePreviewScan(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	_, project, err := s.resolveProject(req)
	if err != nil {
		return toolError(err), nil
	}

	repoPath := strings.TrimSpace(project.RepoPath)
	if repoPath == "" {
		return toolError(fmt.Errorf("project has no repo_path configured")), nil
	}

	absRoot, err := filepath.Abs(repoPath)
	if err != nil {
		return toolError(fmt.Errorf("resolve repo path: %w", err)), nil
	}

	stack := strings.TrimSpace(req.GetString("stack", ""))

	// Load .brainignore for the report
	bi, _ := scanner.LoadBrainIgnore(absRoot)

	// Build scan options from all sources
	opts := scanner.ScanOptions{
		Stack:          stack,
		GlobalExclude:  s.globalExclude,
		GlobalInclude:  s.globalInclude,
		ProjectExclude: project.Exclude,
		ProjectInclude: project.Include,
	}

	// Run the scan (dry-run = same scan, just don't index)
	files, err := scanner.Scan(absRoot, opts)
	if err != nil {
		return toolError(fmt.Errorf("scan failed: %w", err)), nil
	}

	result := formatPreviewScan(absRoot, stack, s.globalExclude, s.globalInclude, project.Exclude, project.Include, bi, files)
	return textResult(result), nil
}

func formatPreviewScan(
	repoPath, stack string,
	globalExclude, globalInclude []string,
	projectExclude, projectInclude []string,
	bi scanner.BrainIgnore,
	files []scanner.ScannedFile,
) string {
	var b strings.Builder

	b.WriteString("## Scan Preview\n\n")
	b.WriteString(fmt.Sprintf("**Repo**: %s\n", repoPath))
	if stack != "" {
		b.WriteString(fmt.Sprintf("**Stack**: %s\n", stack))
	}
	b.WriteString(fmt.Sprintf("**Total files**: %d\n", len(files)))

	// ── Active configuration ─────────────────────────────────────────────
	b.WriteString("\n### Active Filtering Configuration\n\n")

	b.WriteString("**Hardcoded ignores** (always active):\n")
	b.WriteString("  Dirs: node_modules, .git, dist, build, vendor, bin, obj, coverage, __pycache__, .next, .nuxt, target, ...\n")
	b.WriteString("  Files: *.min.js, *.lock, images, binaries, fonts, media, databases\n\n")

	hasConfig := false

	if len(globalExclude) > 0 {
		b.WriteString(fmt.Sprintf("**Global config exclude**: %s\n", strings.Join(globalExclude, ", ")))
		hasConfig = true
	}
	if len(globalInclude) > 0 {
		b.WriteString(fmt.Sprintf("**Global config include**: %s\n", strings.Join(globalInclude, ", ")))
		hasConfig = true
	}
	if len(projectExclude) > 0 {
		b.WriteString(fmt.Sprintf("**Project config exclude**: %s\n", strings.Join(projectExclude, ", ")))
		hasConfig = true
	}
	if len(projectInclude) > 0 {
		b.WriteString(fmt.Sprintf("**Project config include**: %s\n", strings.Join(projectInclude, ", ")))
		hasConfig = true
	}

	if len(bi.ExcludeDirs) > 0 {
		b.WriteString(fmt.Sprintf("**.brainignore exclude**: %s\n", strings.Join(bi.ExcludeDirs, ", ")))
		hasConfig = true
	}
	if len(bi.IncludeExts) > 0 {
		b.WriteString(fmt.Sprintf("**.brainignore include**: %s\n", strings.Join(bi.IncludeExts, ", ")))
		hasConfig = true
	}

	if !hasConfig {
		b.WriteString("No custom exclude/include configured (only hardcoded ignores active).\n")
		b.WriteString("Tip: create a `.brainignore` file or add exclude/include to `~/.brain/config.yaml`.\n")
	}

	// ── Files by language ────────────────────────────────────────────────
	b.WriteString("\n### Files by Language\n\n")

	langCount := map[string]int{}
	for _, f := range files {
		langCount[f.Language]++
	}

	type langEntry struct {
		Name  string
		Count int
	}
	sorted := make([]langEntry, 0, len(langCount))
	for name, count := range langCount {
		sorted = append(sorted, langEntry{name, count})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })

	for _, entry := range sorted {
		b.WriteString(fmt.Sprintf("- %s: %d files\n", entry.Name, entry.Count))
	}

	// ── File list ────────────────────────────────────────────────────────
	b.WriteString("\n### File List\n\n")

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	sort.Strings(paths)

	// Group by top-level directory for readability
	groups := map[string][]string{}
	var rootFiles []string
	for _, p := range paths {
		dir := strings.SplitN(p, "/", 2)[0]
		if strings.Contains(p, "/") {
			groups[dir] = append(groups[dir], p)
		} else {
			rootFiles = append(rootFiles, p)
		}
	}

	if len(rootFiles) > 0 {
		b.WriteString("**Root**\n")
		for _, f := range rootFiles {
			b.WriteString(fmt.Sprintf("  %s\n", f))
		}
	}

	dirNames := make([]string, 0, len(groups))
	for d := range groups {
		dirNames = append(dirNames, d)
	}
	sort.Strings(dirNames)

	for _, dir := range dirNames {
		b.WriteString(fmt.Sprintf("\n**%s/** (%d files)\n", dir, len(groups[dir])))
		for _, f := range groups[dir] {
			b.WriteString(fmt.Sprintf("  %s\n", f))
		}
	}

	return b.String()
}
