package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/Gentleman-Programming/brain-context/internal/retriever"
	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
)

type searchRequest struct {
	QueryEmbedding       []float32 `json:"query_embedding"`
	QueryTSV             string    `json:"query_tsv,omitempty"`
	MaxChunks            int       `json:"max_chunks"`
	IncludeRelationships bool      `json:"include_relationships"`
}

type searchResponse struct {
	ProjectID       uuid.UUID             `json:"project_id"`
	Chunks          []searchChunkResponse `json:"chunks"`
	TotalCandidates int                   `json:"total_candidates"`
}

type searchChunkResponse struct {
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

func (h *Handler) SearchContext(c echo.Context) error {
	projectID, tenantID, err := h.authorizeProjectRead(c)
	if err != nil {
		return err
	}

	var req searchRequest
	if err := c.Bind(&req); err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
	}
	if len(req.QueryEmbedding) == 0 {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "query_embedding is required")
	}

	project, err := h.store.GetProject(c.Request().Context(), tenantID, projectID)
	if err != nil {
		return handleStoreError(c, err)
	}
	if len(req.QueryEmbedding) != project.EmbedDimensions {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", fmt.Sprintf("query_embedding must contain %d dimensions", project.EmbedDimensions))
	}

	candidates, err := retriever.Search(c.Request().Context(), h.store.Executor(c.Request().Context()), projectID, tenantID, req.QueryEmbedding, project.EmbedDimensions, req.MaxChunks)
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to search project context")
	}

	ranked := retriever.Rerank(candidates, req.QueryTSV, req.MaxChunks)
	chunks := make([]searchChunkResponse, 0, len(ranked))
	for _, candidate := range ranked {
		relationships := candidate.Relationships
		if !req.IncludeRelationships {
			relationships = nil
		}
		chunks = append(chunks, searchChunkResponse{
			SymbolName:    candidate.SymbolName,
			SymbolType:    candidate.SymbolType,
			FilePath:      candidate.FilePath,
			StartLine:     candidate.StartLine,
			EndLine:       candidate.EndLine,
			Language:      candidate.Language,
			Score:         candidate.FinalScore,
			Relationships: relationships,
		})
	}

	// ── Metrics instrumentation (synchronous, after response is built) ────────
	fmt.Printf("[metrics] recording query for project %s chunks=%d\n", projectID, len(ranked))
	actorPrefix := ""
	if key, ok := authKeyFromContext(c); ok {
		actorPrefix = key.KeyPrefix
	}
	var topScore, scoreSum float64
	var tokensReturned int
	for i, chunk := range ranked {
		if i == 0 {
			topScore = chunk.FinalScore
		}
		scoreSum += chunk.FinalScore
		tokensReturned += chunk.TokenCount
	}
	var avgScore float64
	if len(ranked) > 0 {
		avgScore = scoreSum / float64(len(ranked))
	}
	totalChunks, avgTokens, _ := h.store.GetProjectChunkCount(c.Request().Context(), tenantID, projectID)
	h.store.InsertQueryLog(c.Request().Context(), store.QueryLog{
		TenantID:        tenantID,
		ProjectID:       projectID,
		ActorPrefix:     actorPrefix,
		ChunksReturned:  len(ranked),
		TopScore:        float32(topScore),
		AvgScore:        float32(avgScore),
		TotalCandidates: len(candidates),
		TokensReturned:  tokensReturned,
		TokensInRepo:    totalChunks * avgTokens,
	})

	return c.JSON(http.StatusOK, searchResponse{ProjectID: projectID, Chunks: chunks, TotalCandidates: len(candidates)})
}

func (h *Handler) GetFileSummary(c echo.Context) error {
	projectID, _, err := h.authorizeProjectRead(c)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(c.QueryParam("path"))
	if path == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
	}

	var response fileSummaryResponse
	var indexedAt sql.NullTime
	err = h.store.Executor(c.Request().Context()).QueryRow(c.Request().Context(), `
		select pf.path, pf.language, count(c.id), pf.indexed_at
		from project_files pf
		left join chunks c on c.file_id = pf.id
		where pf.project_id = $1 and pf.path = $2
		group by pf.id, pf.path, pf.language, pf.indexed_at
	`, projectID, path).Scan(&response.FilePath, &response.Language, &response.ChunkCount, &indexedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return writeError(c, http.StatusNotFound, "NOT_FOUND", "file not found")
		}
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load file summary")
	}
	if indexedAt.Valid {
		response.IndexedAt = &indexedAt.Time
	}

	rows, err := h.store.Executor(c.Request().Context()).Query(c.Request().Context(), `
		select coalesce(symbol_name, ''), coalesce(symbol_type, ''), min(start_line), max(end_line)
		from chunks c
		join project_files pf on pf.id = c.file_id
		where c.project_id = $1 and pf.path = $2 and coalesce(symbol_name, '') <> ''
		group by symbol_name, symbol_type
		order by min(start_line), symbol_name
	`, projectID, path)
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load file symbols")
	}
	defer rows.Close()

	response.Symbols = make([]fileSummarySymbol, 0)
	for rows.Next() {
		var symbol fileSummarySymbol
		if err := rows.Scan(&symbol.Name, &symbol.Kind, &symbol.StartLine, &symbol.EndLine); err != nil {
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load file symbols")
		}
		response.Symbols = append(response.Symbols, symbol)
	}
	if err := rows.Err(); err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load file symbols")
	}

	return c.JSON(http.StatusOK, response)
}

func (h *Handler) GetRelatedFiles(c echo.Context) error {
	projectID, _, err := h.authorizeProjectRead(c)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(c.QueryParam("path"))
	if path == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
	}
	maxDepth := 1
	if value := strings.TrimSpace(c.QueryParam("max_depth")); value != "" {
		if _, err := fmt.Sscanf(value, "%d", &maxDepth); err != nil || maxDepth <= 0 {
			return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "max_depth must be a positive integer")
		}
		if maxDepth > 3 {
			maxDepth = 3
		}
	}

	rows, err := h.store.Executor(c.Request().Context()).Query(c.Request().Context(), `
		with recursive seed as (
			select s.id, s.file_id
			from symbols s
			join project_files pf on pf.id = s.file_id
			where s.project_id = $1 and pf.path = $2
		), graph as (
			select r.dst_id as symbol_id, r.rel_type, r.weight, 1 as depth
			from relationships r
			join seed s on s.id = r.src_id
			where r.project_id = $1 and r.src_type = 'symbol' and r.dst_type = 'symbol'
			union all
			select r.dst_id as symbol_id, r.rel_type, greatest(g.weight, r.weight), g.depth + 1
			from relationships r
			join graph g on g.symbol_id = r.src_id
			where r.project_id = $1 and r.src_type = 'symbol' and r.dst_type = 'symbol' and g.depth < $3
		)
		select pf.path, g.rel_type, max(g.weight) as weight
		from graph g
		join symbols s on s.id = g.symbol_id and s.project_id = $1
		join project_files pf on pf.id = s.file_id
		where pf.path <> $2
		group by pf.path, g.rel_type
		order by max(g.weight) desc, pf.path asc
	`, projectID, path, maxDepth)
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load related files")
	}
	defer rows.Close()

	response := relatedFilesResponse{FilePath: path, RelatedFiles: make([]relatedFileItem, 0)}
	for rows.Next() {
		var item relatedFileItem
		if err := rows.Scan(&item.Path, &item.RelationType, &item.Weight); err != nil {
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load related files")
		}
		response.RelatedFiles = append(response.RelatedFiles, item)
	}
	if err := rows.Err(); err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load related files")
	}

	return c.JSON(http.StatusOK, response)
}

func (h *Handler) FindImpact(c echo.Context) error {
	projectID, _, err := h.authorizeProjectRead(c)
	if err != nil {
		return err
	}

	var req impactRequest
	if err := c.Bind(&req); err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
	}
	req.Entity = strings.TrimSpace(req.Entity)
	if req.Entity == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "entity is required")
	}

	pattern := "%" + req.Entity + "%"
	directRows, err := h.store.Executor(c.Request().Context()).Query(c.Request().Context(), `
		select distinct coalesce(c.symbol_name, ''), pf.path
		from chunks c
		join project_files pf on pf.id = c.file_id
		where c.project_id = $1
			and (coalesce(c.symbol_name, '') ilike $2 or pf.path ilike $2)
		order by pf.path, coalesce(c.symbol_name, '')
	`, projectID, pattern)
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to calculate impact")
	}
	defer directRows.Close()

	response := impactResponse{Entity: req.Entity, AffectedSymbols: make([]impactSymbolResult, 0), AffectedFiles: make([]string, 0)}
	files := make(map[string]struct{})
	seen := make(map[string]struct{})
	appendImpact := func(item impactSymbolResult) {
		key := item.Name + "|" + item.FilePath + "|" + item.RelationType
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		response.AffectedSymbols = append(response.AffectedSymbols, item)
		if item.FilePath != "" {
			files[item.FilePath] = struct{}{}
		}
	}

	for directRows.Next() {
		var item impactSymbolResult
		if err := directRows.Scan(&item.Name, &item.FilePath); err != nil {
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to calculate impact")
		}
		item.RelationType = "DIRECT"
		appendImpact(item)
	}
	if err := directRows.Err(); err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to calculate impact")
	}

	relatedRows, err := h.store.Executor(c.Request().Context()).Query(c.Request().Context(), `
		select distinct coalesce(other.fq_name, ''), other_pf.path, r.rel_type
		from symbols s
		join project_files pf on pf.id = s.file_id
		join relationships r on r.project_id = s.project_id and (r.src_id = s.id or r.dst_id = s.id)
		join symbols other on other.project_id = s.project_id and (other.id = r.dst_id or other.id = r.src_id) and other.id <> s.id
		join project_files other_pf on other_pf.id = other.file_id
		where s.project_id = $1 and (s.fq_name ilike $2 or pf.path ilike $2)
		order by other_pf.path, coalesce(other.fq_name, '')
	`, projectID, pattern)
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to calculate impact")
	}
	defer relatedRows.Close()

	for relatedRows.Next() {
		var item impactSymbolResult
		if err := relatedRows.Scan(&item.Name, &item.FilePath, &item.RelationType); err != nil {
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to calculate impact")
		}
		appendImpact(item)
	}
	if err := relatedRows.Err(); err != nil {
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to calculate impact")
	}
	for filePath := range files {
		response.AffectedFiles = append(response.AffectedFiles, filePath)
	}
	sort.Strings(response.AffectedFiles)

	return c.JSON(http.StatusOK, response)
}

func (h *Handler) authorizeProjectRead(c echo.Context) (uuid.UUID, uuid.UUID, error) {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return uuid.Nil, uuid.Nil, writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}
	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}
	scope, ok := scopeFromContext(c)
	if !ok {
		return uuid.Nil, uuid.Nil, writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing auth context")
	}
	if scope == auth.ScopeProject || scope == auth.ScopeMCPRead {
		authProjectID, ok := projectIDFromContext(c)
		if !ok || authProjectID != projectID {
			return uuid.Nil, uuid.Nil, writeError(c, http.StatusForbidden, "FORBIDDEN", "token does not match the target project")
		}
	}
	return projectID, tenantID, nil
}
