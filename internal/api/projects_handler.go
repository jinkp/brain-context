package api

import (
	"net/http"
	"strings"

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

	return c.JSON(http.StatusCreated, project)
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
