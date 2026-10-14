package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	brainconfig "github.com/Gentleman-Programming/brain-context/internal/config"
	"github.com/Gentleman-Programming/brain-context/internal/cache"
	"github.com/Gentleman-Programming/brain-context/internal/retriever"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

const (
	defaultSmartThreshold = 0.50
	probeCap              = 3
)

var (
	errLoginRequired  = errors.New("Run 'brain login' first")
	errUnauthorized   = errors.New("Invalid or expired token. Run 'brain login' to refresh.")
	errAPIUnreachable = errors.New("Cannot connect to brain-context API. Check your connection.")
)

type skippedProject struct {
	Name       string
	ProbeScore float64
}

// probeMaxScore returns the highest Score in chunks, or 0.0 if empty.
func probeMaxScore(chunks []searchChunkResult) float64 {
	max := 0.0
	for _, c := range chunks {
		if c.Score > max {
			max = c.Score
		}
	}
	return max
}

type projectNotRegisteredError struct {
	name string
}

func (e projectNotRegisteredError) Error() string {
	if strings.TrimSpace(e.name) == "" {
		return "Project not registered. Run 'brain register --project <name>'"
	}
	return fmt.Sprintf("Project not registered. Run 'brain register --project %s'", e.name)
}

type searchRequest struct {
	Query                string    `json:"query,omitempty"`
	QueryEmbedding       []float32 `json:"query_embedding,omitempty"`
	QueryTSV             string    `json:"query_tsv,omitempty"`
	MaxChunks            int       `json:"max_chunks"`
	IncludeRelationships bool      `json:"include_relationships"`
}

type searchResponse struct {
	ProjectID       string              `json:"project_id"`
	Chunks          []searchChunkResult `json:"chunks"`
	TotalCandidates int                 `json:"total_candidates"`
}

type searchChunkResult struct {
	SymbolName    string                   `json:"symbol_name"`
	SymbolType    string                   `json:"symbol_type"`
	FilePath      string                   `json:"file_path"`
	StartLine     int                      `json:"start_line"`
	EndLine       int                      `json:"end_line"`
	Language      string                   `json:"language"`
	Score         float64                  `json:"score"`
	Relationships []retriever.Relationship `json:"relationships,omitempty"`
	ProjectName   string                   `json:"project_name,omitempty"`
}

type fileSummaryResponse struct {
	FilePath   string              `json:"file_path"`
	Language   string              `json:"language"`
	Symbols    []fileSummarySymbol `json:"symbols"`
	ChunkCount int                 `json:"chunk_count"`
	IndexedAt  *time.Time          `json:"indexed_at,omitempty"`
}

type fileSummarySymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type relatedFilesResponse struct {
	FilePath     string            `json:"file_path"`
	RelatedFiles []relatedFileItem `json:"related_files"`
}

type relatedFileItem struct {
	Path         string  `json:"path"`
	RelationType string  `json:"relation_type"`
	Weight       float64 `json:"weight"`
}

type impactRequest struct {
	Entity string `json:"entity"`
}

type impactResponse struct {
	Entity          string               `json:"entity"`
	AffectedSymbols []impactSymbolResult `json:"affected_symbols"`
	AffectedFiles   []string             `json:"affected_files"`
}

type impactSymbolResult struct {
	Name         string `json:"name"`
	FilePath     string `json:"file_path"`
	RelationType string `json:"relation_type"`
}

type apiErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func searchProjectContextTool() mcpgo.Tool {
	return mcpgo.NewTool("search_project_context",
		mcpgo.WithDescription("Search relevant code context for a question. Use this tool BEFORE reading files — it returns pre-indexed, semantically ranked code snippets from the project. Much faster and cheaper than scanning files directly. Returns function names, file paths, line numbers, and relevance scores."),
		mcpgo.WithString("project_id", mcpgo.Required(), mcpgo.Description("Project ID or project name")),
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Natural language question about the codebase, e.g. 'how does payment processing work' or 'where is user authentication handled'")),
		mcpgo.WithNumber("max_chunks", mcpgo.Description("Max chunks to return (default 8, max 20)")),
	)
}

func getFileSummaryTool() mcpgo.Tool {
	return mcpgo.NewTool("get_file_summary",
		mcpgo.WithDescription("Get the indexed summary for a file — lists all symbols (functions, classes, methods) with their line ranges. Use this instead of reading a file when you only need to understand its structure."),
		mcpgo.WithString("project_id", mcpgo.Required(), mcpgo.Description("Project ID or project name")),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("File path relative to repo root")),
	)
}

func getRelatedFilesTool() mcpgo.Tool {
	return mcpgo.NewTool("get_related_files",
		mcpgo.WithDescription("Find files related to a given file by dependencies, imports, calls, or shared symbols. Use this to understand how a file connects to the rest of the codebase without reading every file."),
		mcpgo.WithString("project_id", mcpgo.Required(), mcpgo.Description("Project ID or project name")),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("File path relative to repo root")),
		mcpgo.WithNumber("max_depth", mcpgo.Description("Traversal depth (default 1)")),
	)
}

func explainFlowTool() mcpgo.Tool {
	return mcpgo.NewTool("explain_flow",
		mcpgo.WithDescription("Explain a functional flow (like login, payment, order creation) by retrieving all related code symbols and their relationships. Returns a comprehensive view of how a feature works across multiple files. Use this for architecture questions or understanding end-to-end flows."),
		mcpgo.WithString("project_id", mcpgo.Required(), mcpgo.Description("Project ID or project name")),
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Flow to explain, for example 'login flow', 'payment processing', 'order creation'")),
	)
}

func findImpactTool() mcpgo.Tool {
	return mcpgo.NewTool("find_impact",
		mcpgo.WithDescription("Find all files and symbols impacted by modifying a given entity (class, function, table, service). Use this before making changes to understand the blast radius and avoid breaking dependent code."),
		mcpgo.WithString("project_id", mcpgo.Required(), mcpgo.Description("Project ID or project name")),
		mcpgo.WithString("entity", mcpgo.Required(), mcpgo.Description("Entity name, for example AuthService, Users table, PaymentController")),
	)
}

func searchSolutionContextTool() mcpgo.Tool {
	return mcpgo.NewTool("search_solution_context",
		mcpgo.WithDescription("Search across all projects in a solution (multi-repo fan-out). Use when the query spans multiple services or when you need cross-project context."),
		mcpgo.WithString("solution_name", mcpgo.Required(), mcpgo.Description("Name of the solution as registered with brain solution create")),
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Natural language search query")),
		mcpgo.WithNumber("max_chunks", mcpgo.Description("Max chunks to return (default 20)")),
		mcpgo.WithBoolean("include_relationships", mcpgo.Description("Include symbol relationships")),
		mcpgo.WithString("mode", mcpgo.Description("Routing mode: smart (default, two-pass probe+deep) or full (fan-out to all projects unconditionally).")),
	)
}

func (s *Server) handleSearchProjectContext(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	projectID, project, err := s.resolveProject(req)
	if err != nil {
		return toolError(err), nil
	}
	query, err := requiredTrimmedString(req, "query")
	if err != nil {
		return toolError(err), nil
	}
	maxChunks := clampToolNumber(req.GetFloat("max_chunks", 8), 8, 20)

	// OKF-first orchestration: query the local knowledge layer before falling back
	// to the RAG code index. This returns curated, structured knowledge (requirements,
	// decisions, playbooks) with lower token cost when a confident match is found.
	// Falls back transparently when okfIndex is nil (no bundles registered) or when
	// the best FTS5 score is below the project's configured threshold (default 0.7).
	if s.okfIndex != nil {
		if result, ok := s.okfIndex.Query(ctx, projectID, query, project.OKFThreshold, project.LocalOnly); ok {
			return textResult(result), nil
		}
	}

	// local_only projects have no RAG index (no server project) — return empty on OKF miss.
	if project.LocalOnly {
		return textResult("No matching context found in local knowledge bundle."), nil
	}

	// Local query cache — check before calling the API.
	// Advisory only: on any cache error we fall through to the API transparently.
	if s.cacheStore != nil {
		state, cacheResult, hit := s.cacheGet(ctx, projectID, project, "search_project_context",
			query, map[string]any{"max_chunks": maxChunks})
		if hit {
			return textResult(cacheResult), nil
		}
		result, err := s.searchContext(ctx, projectID, project, query, maxChunks, false)
		if err != nil {
			return toolError(err), nil
		}
		s.cachePut(projectID, "search_project_context", query,
			map[string]any{"max_chunks": maxChunks}, state, result)
		return textResult(result), nil
	}

	result, err := s.searchContext(ctx, projectID, project, query, maxChunks, false)
	if err != nil {
		return toolError(err), nil
	}
	return textResult(result), nil
}

func (s *Server) handleExplainFlow(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	projectID, project, err := s.resolveProject(req)
	if err != nil {
		return toolError(err), nil
	}
	query, err := requiredTrimmedString(req, "query")
	if err != nil {
		return toolError(err), nil
	}

	// OKF-first orchestration: query the local knowledge layer before falling back
	// to the RAG code index. This returns curated, structured knowledge (requirements,
	// decisions, playbooks) with lower token cost when a confident match is found.
	// Falls back transparently when okfIndex is nil (no bundles registered) or when
	// the best FTS5 score is below the project's configured threshold (default 0.7).
	if s.okfIndex != nil {
		if result, ok := s.okfIndex.Query(ctx, projectID, query, project.OKFThreshold, project.LocalOnly); ok {
			return textResult(result), nil
		}
	}

	// local_only projects have no RAG index — return empty on OKF miss.
	if project.LocalOnly {
		return textResult("No matching context found in local knowledge bundle."), nil
	}

	// Local query cache — check before calling the API.
	if s.cacheStore != nil {
		state, cacheResult, hit := s.cacheGet(ctx, projectID, project, "explain_flow", query, nil)
		if hit {
			return textResult(cacheResult), nil
		}
		result, err := s.searchContext(ctx, projectID, project, query, 20, true)
		if err != nil {
			return toolError(err), nil
		}
		formatted := strings.Replace(result, "## Relevant Context", "## Flow Explanation", 1)
		s.cachePut(projectID, "explain_flow", query, nil, state, formatted)
		return textResult(formatted), nil
	}

	result, err := s.searchContext(ctx, projectID, project, query, 20, true)
	if err != nil {
		return toolError(err), nil
	}
	return textResult(strings.Replace(result, "## Relevant Context", "## Flow Explanation", 1)), nil
}

func (s *Server) handleGetFileSummary(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	projectID, project, err := s.resolveProject(req)
	if err != nil {
		return toolError(err), nil
	}
	path, err := requiredTrimmedString(req, "path")
	if err != nil {
		return toolError(err), nil
	}

	var response fileSummaryResponse
	endpoint := fmt.Sprintf("%s/api/projects/%s/files/summary?path=%s", s.apiEndpoint, projectID, url.QueryEscape(path))
	if err := s.doJSON(ctx, http.MethodGet, endpoint, project.MCPReadKey, nil, &response); err != nil {
		return toolError(err), nil
	}

	return textResult(formatFileSummary(response)), nil
}

func (s *Server) handleGetRelatedFiles(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	projectID, project, err := s.resolveProject(req)
	if err != nil {
		return toolError(err), nil
	}
	path, err := requiredTrimmedString(req, "path")
	if err != nil {
		return toolError(err), nil
	}
	maxDepth := clampToolNumber(req.GetFloat("max_depth", 1), 1, 3)

	var response relatedFilesResponse
	endpoint := fmt.Sprintf("%s/api/projects/%s/files/related?path=%s&max_depth=%d", s.apiEndpoint, projectID, url.QueryEscape(path), maxDepth)
	if err := s.doJSON(ctx, http.MethodGet, endpoint, project.MCPReadKey, nil, &response); err != nil {
		return toolError(err), nil
	}

	return textResult(formatRelatedFiles(response, maxDepth)), nil
}

func (s *Server) handleFindImpact(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	projectID, project, err := s.resolveProject(req)
	if err != nil {
		return toolError(err), nil
	}
	entity, err := requiredTrimmedString(req, "entity")
	if err != nil {
		return toolError(err), nil
	}

	var response impactResponse
	endpoint := fmt.Sprintf("%s/api/projects/%s/impact", s.apiEndpoint, projectID)
	if err := s.doJSON(ctx, http.MethodPost, endpoint, project.MCPReadKey, impactRequest{Entity: entity}, &response); err != nil {
		return toolError(err), nil
	}

	return textResult(formatImpact(response)), nil
}

func (s *Server) handleSearchSolutionContext(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	solutionName, err := requiredTrimmedString(req, "solution_name")
	if err != nil {
		return toolError(err), nil
	}
	query, err := requiredTrimmedString(req, "query")
	if err != nil {
		return toolError(err), nil
	}
	maxChunks := clampToolNumber(req.GetFloat("max_chunks", 20), 20, 50)
	includeRelationships := req.GetBool("include_relationships", false)
	mode := strings.TrimSpace(req.GetString("mode", "smart"))
	if mode == "" {
		mode = "smart"
	}

	// Resolve solution → list of project configs
	projects, err := s.resolveSolution(solutionName)
	if err != nil {
		return toolError(err), nil
	}

	// Resolve effective threshold (for smart mode)
	customThreshold := 0.0
	if sol, ok := s.solutionsByName[strings.ToLower(solutionName)]; ok && sol.SmartThreshold != 0 {
		customThreshold = sol.SmartThreshold
	}

	params := solutionSearchParams{
		solutionName:         solutionName,
		query:                query,
		maxChunks:            maxChunks,
		includeRelationships: includeRelationships,
		mode:                 mode,
		threshold:            customThreshold,
	}
	result, err := s.solutionFanOut(ctx, params, projects)
	if err != nil {
		return toolError(err), nil
	}
	return textResult(result), nil
}

// solutionSearchParams holds pre-parsed parameters for the fan-out logic.
// Used by tests to bypass the embedder and drive fan-out directly.
type solutionSearchParams struct {
	solutionName         string
	query                string
	queryEmbedding       []float32 // legacy test field; read path now embeds server-side
	maxChunks            int
	includeRelationships bool
	mode                 string
	threshold            float64 // 0 → use defaultSmartThreshold
}

// solutionFanOut executes the full or smart fan-out logic given pre-computed params.
// Exported for testing without a real embedder.
func (s *Server) solutionFanOut(ctx context.Context, params solutionSearchParams, projects []brainconfig.ProjectConfig) (string, error) {
	n := len(projects)
	solutionName := params.solutionName
	query := params.query
	maxChunks := params.maxChunks
	if maxChunks <= 0 {
		maxChunks = 20
	}
	includeRelationships := params.includeRelationships
	mode := params.mode
	if mode == "" {
		mode = "smart"
	}

	// ── FULL MODE ────────────────────────────────────────────────────────────
	if mode == "full" {
		perProjectCap := 50 / n
		if perProjectCap < 10 {
			perProjectCap = 10
		}
		type projectResult struct {
			chunks  []searchChunkResult
			warning string
		}
		results := make([]projectResult, n)
		var mu sync.Mutex
		var wg sync.WaitGroup
		for i, proj := range projects {
			wg.Add(1)
			go func(idx int, project brainconfig.ProjectConfig) {
				defer wg.Done()
				if strings.TrimSpace(project.MCPReadKey) == "" {
					mu.Lock()
					results[idx] = projectResult{warning: fmt.Sprintf("%s: no mcp_read_key configured (skipped)", project.ProjectID)}
					mu.Unlock()
					return
				}
				payload := searchRequest{
					Query:                query,
					MaxChunks:            perProjectCap,
					IncludeRelationships: includeRelationships,
				}
				endpoint := fmt.Sprintf("%s/api/projects/%s/context/search", s.apiEndpoint, strings.TrimSpace(project.ProjectID))
				var resp searchResponse
				if err := s.doJSON(ctx, http.MethodPost, endpoint, project.MCPReadKey, payload, &resp); err != nil {
					mu.Lock()
					results[idx] = projectResult{warning: fmt.Sprintf("%s: %v", project.ProjectID, err)}
					mu.Unlock()
					return
				}
				projectName := s.projectName(project.ProjectID)
				chunks := make([]searchChunkResult, len(resp.Chunks))
				for j, c := range resp.Chunks {
					chunks[j] = c
					chunks[j].ProjectName = projectName
				}
				mu.Lock()
				results[idx] = projectResult{chunks: chunks}
				mu.Unlock()
			}(i, proj)
		}
		wg.Wait()
		var allChunks []searchChunkResult
		var warnings []string
		for _, r := range results {
			allChunks = append(allChunks, r.chunks...)
			if r.warning != "" {
				warnings = append(warnings, r.warning)
			}
		}
		if len(allChunks) == 0 && len(warnings) == n {
			return "", fmt.Errorf("all projects in solution %q failed: %s", solutionName, strings.Join(warnings, "; "))
		}
		sort.Slice(allChunks, func(i, j int) bool { return allChunks[i].Score > allChunks[j].Score })
		if len(allChunks) > maxChunks {
			allChunks = allChunks[:maxChunks]
		}
		return formatSolutionResults(solutionName, query, allChunks, n, warnings), nil
	}

	// ── SMART MODE ───────────────────────────────────────────────────────────
	threshold := params.threshold
	if threshold == 0 {
		threshold = defaultSmartThreshold
	}

	// PASS 1 — probe
	type probeResult struct {
		chunks  []searchChunkResult
		warning string
		failed  bool
	}
	probeResults := make([]probeResult, n)
	var probeMu sync.Mutex
	var probeWg sync.WaitGroup
	for i, proj := range projects {
		probeWg.Add(1)
		go func(idx int, project brainconfig.ProjectConfig) {
			defer probeWg.Done()
			if strings.TrimSpace(project.MCPReadKey) == "" {
				probeMu.Lock()
				probeResults[idx] = probeResult{
					warning: fmt.Sprintf("%s: no mcp_read_key configured (skipped)", project.ProjectID),
					failed:  true,
				}
				probeMu.Unlock()
				return
			}
			payload := searchRequest{
				Query:                query,
				MaxChunks:            probeCap,
				IncludeRelationships: false,
			}
			endpoint := fmt.Sprintf("%s/api/projects/%s/context/search", s.apiEndpoint, strings.TrimSpace(project.ProjectID))
			var resp searchResponse
			if err := s.doJSON(ctx, http.MethodPost, endpoint, project.MCPReadKey, payload, &resp); err != nil {
				probeMu.Lock()
				probeResults[idx] = probeResult{
					warning: fmt.Sprintf("%s (probe): %v", project.ProjectID, err),
					failed:  true,
				}
				probeMu.Unlock()
				return
			}
			projectName := s.projectName(project.ProjectID)
			chunks := make([]searchChunkResult, len(resp.Chunks))
			for j, c := range resp.Chunks {
				chunks[j] = c
				chunks[j].ProjectName = projectName
			}
			probeMu.Lock()
			probeResults[idx] = probeResult{chunks: chunks}
			probeMu.Unlock()
		}(i, proj)
	}
	probeWg.Wait()

	// Filter
	var allWarnings []string
	var relevantProjects []brainconfig.ProjectConfig
	var skipped []skippedProject
	for i, proj := range projects {
		pr := probeResults[i]
		if pr.failed {
			if pr.warning != "" {
				allWarnings = append(allWarnings, pr.warning)
			}
			continue
		}
		score := probeMaxScore(pr.chunks)
		if score >= threshold {
			relevantProjects = append(relevantProjects, proj)
		} else {
			skipped = append(skipped, skippedProject{
				Name:       s.projectName(proj.ProjectID),
				ProbeScore: score,
			})
		}
	}

	// Fallback
	if len(relevantProjects) == 0 {
		allWarnings = append(allWarnings, fmt.Sprintf("No project exceeded threshold %.2f; falling back to full search", threshold))
		for i, proj := range projects {
			if !probeResults[i].failed {
				relevantProjects = append(relevantProjects, proj)
			}
		}
		skipped = nil
	}

	// PASS 2 — deep
	m := len(relevantProjects)
	if m == 0 {
		return formatSmartResults(solutionName, nil, n, skipped, threshold, allWarnings), nil
	}
	deepCap := 50 / m
	if deepCap < 10 {
		deepCap = 10
	}
	type deepResult struct {
		chunks  []searchChunkResult
		warning string
	}
	deepResults := make([]deepResult, m)
	var deepMu sync.Mutex
	var deepWg sync.WaitGroup
	for i, proj := range relevantProjects {
		deepWg.Add(1)
		go func(idx int, project brainconfig.ProjectConfig) {
			defer deepWg.Done()
			payload := searchRequest{
				Query:                query,
				MaxChunks:            deepCap,
				IncludeRelationships: includeRelationships,
			}
			endpoint := fmt.Sprintf("%s/api/projects/%s/context/search", s.apiEndpoint, strings.TrimSpace(project.ProjectID))
			var resp searchResponse
			if err := s.doJSON(ctx, http.MethodPost, endpoint, project.MCPReadKey, payload, &resp); err != nil {
				deepMu.Lock()
				deepResults[idx] = deepResult{warning: fmt.Sprintf("%s: %v", project.ProjectID, err)}
				deepMu.Unlock()
				return
			}
			projectName := s.projectName(project.ProjectID)
			chunks := make([]searchChunkResult, len(resp.Chunks))
			for j, c := range resp.Chunks {
				chunks[j] = c
				chunks[j].ProjectName = projectName
			}
			deepMu.Lock()
			deepResults[idx] = deepResult{chunks: chunks}
			deepMu.Unlock()
		}(i, proj)
	}
	deepWg.Wait()

	var allChunks []searchChunkResult
	for _, r := range deepResults {
		allChunks = append(allChunks, r.chunks...)
		if r.warning != "" {
			allWarnings = append(allWarnings, r.warning)
		}
	}

	sort.Slice(allChunks, func(i, j int) bool { return allChunks[i].Score > allChunks[j].Score })
	if len(allChunks) > maxChunks {
		allChunks = allChunks[:maxChunks]
	}
	return formatSmartResults(solutionName, allChunks, n, skipped, threshold, allWarnings), nil
}

// resolveSolution looks up a solution by name and returns member project configs.
// Projects without MCPReadKey are included (caller handles skip logic).
func (s *Server) resolveSolution(name string) ([]brainconfig.ProjectConfig, error) {
	solution, ok := s.solutionsByName[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("solution %q not found — run 'brain solution create' to create one", name)
	}
	if len(solution.Projects) == 0 {
		return nil, fmt.Errorf("solution %q has no member projects", name)
	}
	projects := make([]brainconfig.ProjectConfig, 0, len(solution.Projects))
	for _, projectName := range solution.Projects {
		if id, ok := s.projectsByName[strings.ToLower(projectName)]; ok {
			if proj, ok := s.projectsByID[id]; ok {
				projects = append(projects, proj)
				continue
			}
		}
		// Project in solution not found in projectsByID — skip with log but don't hard fail here
		projects = append(projects, brainconfig.ProjectConfig{ProjectID: projectName})
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("solution %q: no member projects could be resolved", name)
	}
	return projects, nil
}

// projectName returns the human-readable name for a project ID.
func (s *Server) projectName(projectID string) string {
	for name, id := range s.projectsByName {
		if id == projectID {
			return name
		}
	}
	return projectID
}

func formatSolutionResults(solutionName, query string, chunks []searchChunkResult, totalProjects int, warnings []string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## Solution Context for: %q (solution: %s, %d projects)\n", query, solutionName, totalProjects))
	if len(chunks) == 0 {
		builder.WriteString("\nNo relevant context found.")
	} else {
		for _, chunk := range chunks {
			builder.WriteString("\n")
			projectLabel := ""
			if chunk.ProjectName != "" {
				projectLabel = fmt.Sprintf(" [Project: %s]", chunk.ProjectName)
			}
			builder.WriteString(fmt.Sprintf("\n### %s — %s (%s, lines %d-%d)%s\n",
				chunk.FilePath, displaySymbolName(chunk.SymbolName), displaySymbolType(chunk.SymbolType),
				chunk.StartLine, chunk.EndLine, projectLabel))
			builder.WriteString(fmt.Sprintf("Score: %.2f\n", chunk.Score))
			if len(chunk.Relationships) > 0 {
				builder.WriteString("Relationships: " + formatRelationships(chunk.Relationships) + "\n")
			}
		}
	}
	if len(warnings) > 0 {
		builder.WriteString("\n\n### Warnings\n")
		for _, w := range warnings {
			builder.WriteString("- " + w + "\n")
		}
	}
	return builder.String()
}

func formatSmartResults(solutionName string, chunks []searchChunkResult, totalProjects int, skipped []skippedProject, threshold float64, warnings []string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## Solution Context: %q | mode: smart | threshold: %.2f\n", solutionName, threshold))
	builder.WriteString(fmt.Sprintf("## Projects searched: %d/%d\n", totalProjects-len(skipped), totalProjects))
	if len(skipped) > 0 {
		parts := make([]string, 0, len(skipped))
		for _, sp := range skipped {
			parts = append(parts, fmt.Sprintf("%s (probe: %.2f)", sp.Name, sp.ProbeScore))
		}
		builder.WriteString("## Skipped (below threshold): " + strings.Join(parts, ", ") + "\n")
	}
	if len(chunks) == 0 {
		builder.WriteString("\nNo relevant context found.")
	} else {
		for _, chunk := range chunks {
			builder.WriteString("\n")
			projectLabel := ""
			if chunk.ProjectName != "" {
				projectLabel = fmt.Sprintf(" [Project: %s]", chunk.ProjectName)
			}
			builder.WriteString(fmt.Sprintf("\n### %s — %s (%s, lines %d-%d)%s\n",
				chunk.FilePath, displaySymbolName(chunk.SymbolName), displaySymbolType(chunk.SymbolType),
				chunk.StartLine, chunk.EndLine, projectLabel))
			builder.WriteString(fmt.Sprintf("Score: %.2f\n", chunk.Score))
			if len(chunk.Relationships) > 0 {
				builder.WriteString("Relationships: " + formatRelationships(chunk.Relationships) + "\n")
			}
		}
	}
	if len(warnings) > 0 {
		builder.WriteString("\n\n### Warnings\n")
		for _, w := range warnings {
			builder.WriteString("- " + w + "\n")
		}
	}
	return builder.String()
}

func (s *Server) searchContext(ctx context.Context, projectID string, project brainconfig.ProjectConfig, query string, maxChunks int, includeRelationships bool) (string, error) {
	var response searchResponse
	endpoint := fmt.Sprintf("%s/api/projects/%s/context/search", s.apiEndpoint, projectID)
	payload := searchRequest{
		Query:                query,
		MaxChunks:            maxChunks,
		IncludeRelationships: includeRelationships,
	}
	if err := s.doJSON(ctx, http.MethodPost, endpoint, project.MCPReadKey, payload, &response); err != nil {
		return "", err
	}
	return formatSearchResults(query, response.Chunks, includeRelationships), nil
}

func (s *Server) resolveProject(req mcpgo.CallToolRequest) (string, brainconfig.ProjectConfig, error) {
	input := strings.TrimSpace(req.GetString("project_id", ""))
	if input == "" {
		input = s.defaultProjectID
	}
	if input == "" {
		return "", brainconfig.ProjectConfig{}, fmt.Errorf("project_id is required")
	}

	// Try by UUID first
	projectID := input
	project, ok := s.projectsByID[projectID]

	// If not found by UUID, try by project name (case-insensitive)
	if !ok {
		if resolvedID, found := s.projectsByName[strings.ToLower(input)]; found {
			projectID = resolvedID
			project, ok = s.projectsByID[projectID]
		}
	}

	if !ok {
		return "", brainconfig.ProjectConfig{}, projectNotRegisteredError{}
	}
	// local_only projects have no mcp_read_key — they are OKF-only, no RAG backend.
	if !project.LocalOnly && strings.TrimSpace(project.MCPReadKey) == "" {
		return "", brainconfig.ProjectConfig{}, errUnauthorized
	}
	return projectID, project, nil
}

func requiredTrimmedString(req mcpgo.CallToolRequest, key string) (string, error) {
	value, err := req.RequireString(key)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func clampToolNumber(value float64, fallback int, max int) int {
	if value <= 0 {
		return fallback
	}
	resolved := int(value)
	if resolved > max {
		return max
	}
	return resolved
}

func toolError(err error) *mcpgo.CallToolResult {
	return mcpgo.NewToolResultError(userMessage(err))
}

func userMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	switch {
	case errors.Is(err, errLoginRequired):
		return errLoginRequired.Error()
	case errors.Is(err, errUnauthorized):
		return errUnauthorized.Error()
	case errors.Is(err, errAPIUnreachable):
		return errAPIUnreachable.Error()
	}
	var projectErr projectNotRegisteredError
	if errors.As(err, &projectErr) {
		return projectErr.Error()
	}
	return err.Error()
}

func (s *Server) doJSON(ctx context.Context, method, endpoint, token string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) {
			return errAPIUnreachable
		}
		return errAPIUnreachable
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errUnauthorized
	}
	if resp.StatusCode >= 400 {
		var apiErr apiErrorResponse
		if err := json.Unmarshal(data, &apiErr); err == nil && strings.TrimSpace(apiErr.Error) != "" {
			return errors.New(apiErr.Error)
		}
		return fmt.Errorf("request failed with status %s", resp.Status)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func formatSearchResults(query string, chunks []searchChunkResult, includeRelationships bool) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## Relevant Context for: %q\n", query))
	if len(chunks) == 0 {
		builder.WriteString("\nNo relevant context found.")
		return builder.String()
	}
	for _, chunk := range chunks {
		builder.WriteString("\n")
		builder.WriteString(fmt.Sprintf("\n### %s — %s (%s, lines %d-%d)\n", chunk.FilePath, displaySymbolName(chunk.SymbolName), displaySymbolType(chunk.SymbolType), chunk.StartLine, chunk.EndLine))
		builder.WriteString(fmt.Sprintf("Score: %.2f\n", chunk.Score))
		if includeRelationships {
			relationships := formatRelationships(chunk.Relationships)
			builder.WriteString("Relationships: " + relationships + "\n")
		} else if len(chunk.Relationships) > 0 {
			builder.WriteString("Relationships: " + formatRelationships(chunk.Relationships) + "\n")
		}
	}
	return builder.String()
}

func formatRelationships(relationships []retriever.Relationship) string {
	if len(relationships) == 0 {
		return "None"
	}
	parts := make([]string, 0, len(relationships))
	for _, rel := range relationships {
		target := strings.TrimSpace(rel.DstName)
		if target == "" {
			target = strings.TrimSpace(rel.DstType)
		}
		parts = append(parts, strings.ToUpper(strings.TrimSpace(rel.RelType))+" "+target)
	}
	return strings.Join(parts, ", ")
}

func formatFileSummary(response fileSummaryResponse) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## File Summary\n\nPath: %s\nLanguage: %s\nChunks: %d\n", response.FilePath, displayValue(response.Language), response.ChunkCount))
	if response.IndexedAt != nil {
		builder.WriteString("Indexed At: " + response.IndexedAt.Format(time.RFC3339) + "\n")
	}
	builder.WriteString("\n### Symbols\n")
	if len(response.Symbols) == 0 {
		builder.WriteString("- None\n")
		return builder.String()
	}
	for _, symbol := range response.Symbols {
		builder.WriteString(fmt.Sprintf("- %s (%s, lines %d-%d)\n", displaySymbolName(symbol.Name), displaySymbolType(symbol.Kind), symbol.StartLine, symbol.EndLine))
	}
	return builder.String()
}

func formatRelatedFiles(response relatedFilesResponse, maxDepth int) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## Related Files\n\nPath: %s\nMax Depth: %d\n\n", response.FilePath, maxDepth))
	if len(response.RelatedFiles) == 0 {
		builder.WriteString("No related files found.")
		return builder.String()
	}
	for _, file := range response.RelatedFiles {
		builder.WriteString(fmt.Sprintf("- %s — %s (weight %.2f)\n", file.Path, strings.ToUpper(strings.TrimSpace(file.RelationType)), file.Weight))
	}
	return builder.String()
}

func formatImpact(response impactResponse) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## Impact Report for: %q\n", response.Entity))
	if len(response.AffectedSymbols) == 0 && len(response.AffectedFiles) == 0 {
		builder.WriteString("\nNo impacted symbols or files found.")
		return builder.String()
	}
	if len(response.AffectedSymbols) > 0 {
		builder.WriteString("\n\n### Affected Symbols\n")
		for _, symbol := range response.AffectedSymbols {
			builder.WriteString(fmt.Sprintf("- %s — %s [%s]\n", displaySymbolName(symbol.Name), symbol.FilePath, strings.ToUpper(strings.TrimSpace(symbol.RelationType))))
		}
	}
	if len(response.AffectedFiles) > 0 {
		files := append([]string(nil), response.AffectedFiles...)
		sort.Strings(files)
		builder.WriteString("\n### Affected Files\n")
		for _, path := range files {
			builder.WriteString("- " + path + "\n")
		}
	}
	return builder.String()
}

func displaySymbolName(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(anonymous)"
	}
	return strings.TrimSpace(value)
}

func displaySymbolType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "symbol"
	}
	return strings.TrimSpace(value)
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return strings.TrimSpace(value)
}

// cacheGet attempts to return a cached response for the given tool + query + params.
// It first fetches (or derives) the remote project state to validate freshness.
// Returns (state, text, true) on a hit, or (state, "", false) on a miss or any error.
// All failures are silent — the caller must fall back to the API.
func (s *Server) cacheGet(ctx context.Context, projectID string, project brainconfig.ProjectConfig, toolName, query string, params map[string]any) (cache.ProjectState, string, bool) {
	state, err := cache.FetchProjectState(ctx, s.httpClient, s.apiEndpoint, projectID, project.MCPReadKey)
	if err != nil {
		// Cannot prove freshness — bypass cache entirely.
		return cache.ProjectState{}, "", false
	}

	// Persist the latest state for CLI inspection commands (brain cache status).
	_ = s.cacheStore.UpsertProjectState(state)

	key, err := cache.BuildKey(projectID, toolName, query, params, state)
	if err != nil {
		return state, "", false
	}

	text, hit, err := s.cacheStore.Get(key)
	if err != nil || !hit {
		return state, "", false
	}
	return state, text, true
}

// cachePut stores a response in the local cache. All errors are silently ignored —
// the cache is advisory and a write failure must not affect the caller.
func (s *Server) cachePut(projectID, toolName, query string, params map[string]any, state cache.ProjectState, responseText string) {
	key, err := cache.BuildKey(projectID, toolName, query, params, state)
	if err != nil {
		return
	}
	_ = s.cacheStore.Put(key, responseText, cache.DefaultTTL)
}
