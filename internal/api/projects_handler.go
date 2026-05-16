package api

import (
	"net/http"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/crypto"
	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

var allowedDimensions = map[int]struct{}{
	768:  {},
	1024: {},
	3072: {},
}

type createProjectRequest struct {
	Name            string  `json:"name"`
	RepositoryURL   *string `json:"repository_url"`
	DefaultBranch   string  `json:"default_branch"`
	EmbedModel      string  `json:"embed_model"`
	EmbedDimensions int     `json:"embed_dimensions"`
	EmbedAPIKey     string  `json:"embed_api_key"` // optional — encrypted before storing
}

func (h *Handler) CreateProject(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	var req createProjectRequest
	if err := c.Bind(&req); err != nil {
		return writeError(c, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
	}

	req.Name = strings.TrimSpace(req.Name)
	req.DefaultBranch = strings.TrimSpace(req.DefaultBranch)
	req.EmbedModel = strings.TrimSpace(req.EmbedModel)
	if req.RepositoryURL != nil {
		trimmed := strings.TrimSpace(*req.RepositoryURL)
		req.RepositoryURL = &trimmed
		if trimmed == "" {
			req.RepositoryURL = nil
		}
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}

	if req.Name == "" || req.EmbedModel == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "name and embed_model are required")
	}
	if _, ok := allowedDimensions[req.EmbedDimensions]; !ok {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "embed_dimensions must be one of 768, 1024, or 3072")
	}

	// Encrypt embed API key if provided
	var embedKeyEnc string
	if strings.TrimSpace(req.EmbedAPIKey) != "" {
		if !crypto.IsConfigured() {
			// Create project without key, warn the user
			project, err := h.store.CreateProject(c.Request().Context(), tenantID, store.CreateProjectParams{
				Name:            req.Name,
				RepositoryURL:   req.RepositoryURL,
				DefaultBranch:   req.DefaultBranch,
				EmbedModel:      req.EmbedModel,
				EmbedDimensions: req.EmbedDimensions,
			})
			if err != nil {
				return handleStoreError(c, err)
			}
			return c.JSON(http.StatusCreated, map[string]any{
				"id":      project.ID,
				"project": project,
				"warning": "BRAIN_ENCRYPTION_KEY not set on server — embed_api_key was NOT stored",
			})
		}
		enc, err := crypto.Encrypt(strings.TrimSpace(req.EmbedAPIKey))
		if err != nil {
			return writeError(c, http.StatusInternalServerError, "ENCRYPT_FAILED", "failed to encrypt embed api key")
		}
		embedKeyEnc = enc
	}

	project, err := h.store.CreateProject(c.Request().Context(), tenantID, store.CreateProjectParams{
		Name:            req.Name,
		RepositoryURL:   req.RepositoryURL,
		DefaultBranch:   req.DefaultBranch,
		EmbedModel:      req.EmbedModel,
		EmbedDimensions: req.EmbedDimensions,
		EmbedAPIKeyEnc:  embedKeyEnc,
	})
	if err != nil {
		return handleStoreError(c, err)
	}

	return c.JSON(http.StatusCreated, project)
}

// UpdateEmbedKey sets or replaces the encrypted embed API key for a project.
// PATCH /api/projects/:id/embed-key
func (h *Handler) UpdateEmbedKey(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}

	var req struct {
		EmbedAPIKey string `json:"embed_api_key"`
	}
	if err := c.Bind(&req); err != nil || strings.TrimSpace(req.EmbedAPIKey) == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "embed_api_key is required")
	}

	if !crypto.IsConfigured() {
		return writeError(c, http.StatusServiceUnavailable, "ENCRYPTION_NOT_CONFIGURED",
			"BRAIN_ENCRYPTION_KEY not set on server")
	}

	enc, err := crypto.Encrypt(strings.TrimSpace(req.EmbedAPIKey))
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "ENCRYPT_FAILED", "failed to encrypt embed api key")
	}

	if err := h.store.SaveEmbedAPIKey(c.Request().Context(), tenantID, projectID, enc); err != nil {
		return handleStoreError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message": "embed API key stored and encrypted successfully",
	})
}

func (h *Handler) DeleteProject(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}

	if err := h.store.DeleteProject(c.Request().Context(), tenantID, projectID); err != nil {
		return handleStoreError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) GetProject(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}

	project, err := h.store.GetProject(c.Request().Context(), tenantID, projectID)
	if err != nil {
		return handleStoreError(c, err)
	}

	return c.JSON(http.StatusOK, project)
}
