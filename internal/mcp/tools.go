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
	"time"

	brainconfig "github.com/Gentleman-Programming/brain-context/internal/config"
	"github.com/Gentleman-Programming/brain-context/internal/embedder"
	"github.com/Gentleman-Programming/brain-context/internal/retriever"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

var (
	errLoginRequired  = errors.New("Run 'brain login' first")
	errUnauthorized   = errors.New("Invalid or expired token. Run 'brain login' to refresh.")
	errAPIUnreachable = errors.New("Cannot connect to brain-context API. Check your connection.")
)

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
	QueryEmbedding       []float32 `json:"query_embedding"`
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

func (s *Server) searchContext(ctx context.Context, projectID string, project brainconfig.ProjectConfig, query string, maxChunks int, includeRelationships bool) (string, error) {
	if strings.TrimSpace(project.EmbedModel) == "" {
		return "", fmt.Errorf("project embedder is not configured")
	}
	if strings.TrimSpace(project.EmbedAPIKey) == "" && !strings.HasPrefix(project.EmbedModel, embedder.ProviderOllama+"/") {
		return "", fmt.Errorf("project embedder api key is not configured")
	}

	emb, err := embedder.New(project.EmbedModel, project.EmbedAPIKey)
	if err != nil {
		return "", fmt.Errorf("create embedder: %w", err)
	}
	vectors, err := emb.Embed(ctx, []string{query})
	if err != nil {
		return "", fmt.Errorf("embed query: %w", err)
	}
	if len(vectors) == 0 {
		return "", fmt.Errorf("embed query: no vector returned")
	}

	var response searchResponse
	endpoint := fmt.Sprintf("%s/api/projects/%s/context/search", s.apiEndpoint, projectID)
	payload := searchRequest{
		QueryEmbedding:       vectors[0],
		QueryTSV:             query,
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
	if strings.TrimSpace(project.MCPReadKey) == "" {
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
