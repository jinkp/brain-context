package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/jobs"
	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type chunkUploadRequest struct {
	ChunkHash  string    `json:"chunk_hash"`
	SymbolName string    `json:"symbol_name"`
	SymbolType string    `json:"symbol_type"`
	StartLine  int       `json:"start_line"`
	EndLine    int       `json:"end_line"`
	FilePath   string    `json:"file_path"`
	Language   string    `json:"language"`
	TokenCount int       `json:"token_count"`
	Embedding  []float32 `json:"embedding"`
}

type chunksUploadResponse struct {
	Accepted int      `json:"accepted"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

func (h *Handler) UploadChunks(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}
	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}
	authProjectID, ok := projectIDFromContext(c)
	if !ok || authProjectID != projectID {
		return writeError(c, http.StatusForbidden, "FORBIDDEN", "token does not match the target project")
	}

	req, err := decodeChunkUploadRequest(c)
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	}

	project, err := h.store.GetProject(c.Request().Context(), tenantID, projectID)
	if err != nil {
		return handleStoreError(c, err)
	}

	payloads := make([]store.ChunkPayload, 0, len(req))
	for _, chunk := range req {
		if err := validateChunkUpload(chunk, project.EmbedDimensions); err != nil {
			return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		}
		payloads = append(payloads, store.ChunkPayload{
			ChunkHash:  strings.TrimSpace(chunk.ChunkHash),
			SymbolName: strings.TrimSpace(chunk.SymbolName),
			SymbolType: strings.TrimSpace(chunk.SymbolType),
			StartLine:  chunk.StartLine,
			EndLine:    chunk.EndLine,
			FilePath:   strings.TrimSpace(chunk.FilePath),
			Language:   strings.TrimSpace(chunk.Language),
			TokenCount: chunk.TokenCount,
			Embedding:  chunk.Embedding,
		})
	}

	job, err := jobs.CreateJob(c.Request().Context(), h.store, jobs.CreateJobParams{
		TenantID:  tenantID,
		ProjectID: projectID,
		Kind:      "incremental",
	})
	if err != nil {
		return handleStoreError(c, err)
	}

	accepted, skipped, err := h.store.UpsertChunks(c.Request().Context(), projectID, tenantID, payloads)
	if err != nil {
		_ = jobs.FailJob(c.Request().Context(), h.store, job.ID, err.Error())
		return handleStoreError(c, err)
	}
	if err := jobs.CompleteJob(c.Request().Context(), h.store, job.ID); err != nil {
		return handleStoreError(c, err)
	}

	return c.JSON(http.StatusOK, chunksUploadResponse{Accepted: accepted, Skipped: skipped, Errors: []string{}})
}

func decodeChunkUploadRequest(c echo.Context) ([]chunkUploadRequest, error) {
	defer c.Request().Body.Close()

	var raw []map[string]json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid request body")
	}
	for _, item := range raw {
		if _, found := item["content"]; found {
			return nil, fmt.Errorf("raw content is not allowed in chunk uploads")
		}
	}

	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid request body")
	}

	var req []chunkUploadRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid request body")
	}
	return req, nil
}

func validateChunkUpload(chunk chunkUploadRequest, expectedDimensions int) error {
	if strings.TrimSpace(chunk.ChunkHash) == "" {
		return fmt.Errorf("chunk_hash is required")
	}
	if strings.TrimSpace(chunk.FilePath) == "" {
		return fmt.Errorf("file_path is required")
	}
	if chunk.StartLine <= 0 || chunk.EndLine < chunk.StartLine {
		return fmt.Errorf("start_line and end_line must define a valid range")
	}
	if len(chunk.Embedding) != expectedDimensions {
		return fmt.Errorf("embedding dimensions must match project setting %d", expectedDimensions)
	}
	return nil
}
