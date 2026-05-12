package api

import (
	"net/http"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// CreateProjectTokens issues a new project token + MCP read key for a project.
// POST /api/projects/:id/tokens
func (h *Handler) CreateProjectTokens(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}

	projectKey, err := auth.IssueToken(auth.ScopeProject, time.Now().UTC())
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "failed to issue project token")
	}
	mcpKey, err := auth.IssueToken(auth.ScopeMCPRead, time.Now().UTC())
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "failed to issue mcp read key")
	}

	projectRecord, mcpRecord, err := h.store.CreateProjectTokens(c.Request().Context(), tenantID, projectID, projectKey, mcpKey)
	if err != nil {
		return handleStoreError(c, err)
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"project_token": tokenPayload{
			ID:        projectRecord.ID.String(),
			Prefix:    projectRecord.KeyPrefix,
			Scope:     projectRecord.Scope,
			ExpiresAt: projectRecord.ExpiresAt,
			Token:     projectKey.Raw,
		},
		"mcp_read_key": tokenPayload{
			ID:        mcpRecord.ID.String(),
			Prefix:    mcpRecord.KeyPrefix,
			Scope:     mcpRecord.Scope,
			ExpiresAt: mcpRecord.ExpiresAt,
			Token:     mcpKey.Raw,
		},
	})
}

// ListProjectTokens returns active (non-revoked, non-expired) tokens for a project.
// Secrets are never returned — only prefix, scope, expiry, and ID.
// GET /api/projects/:id/tokens
func (h *Handler) ListProjectTokens(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}

	tokens, err := h.store.ListProjectTokens(c.Request().Context(), tenantID, projectID)
	if err != nil {
		return handleStoreError(c, err)
	}

	items := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		items = append(items, map[string]any{
			"id":         t.ID.String(),
			"prefix":     t.KeyPrefix,
			"scope":      t.Scope,
			"expires_at": t.ExpiresAt,
			"created_at": t.CreatedAt,
		})
	}

	return c.JSON(http.StatusOK, map[string]any{"tokens": items})
}

// RenewProjectTokens revokes all active project+mcp_read tokens for a project
// and issues a fresh pair.
// POST /api/projects/:id/tokens/renew
func (h *Handler) RenewProjectTokens(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}

	projectKey, err := auth.IssueToken(auth.ScopeProject, time.Now().UTC())
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "failed to issue project token")
	}
	mcpKey, err := auth.IssueToken(auth.ScopeMCPRead, time.Now().UTC())
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "failed to issue mcp read key")
	}

	projectRecord, mcpRecord, err := h.store.RenewProjectTokens(c.Request().Context(), tenantID, projectID, projectKey, mcpKey)
	if err != nil {
		return handleStoreError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message": "previous tokens revoked — store these new tokens securely, they are shown once",
		"project_token": tokenPayload{
			ID:        projectRecord.ID.String(),
			Prefix:    projectRecord.KeyPrefix,
			Scope:     projectRecord.Scope,
			ExpiresAt: projectRecord.ExpiresAt,
			Token:     projectKey.Raw,
		},
		"mcp_read_key": tokenPayload{
			ID:        mcpRecord.ID.String(),
			Prefix:    mcpRecord.KeyPrefix,
			Scope:     mcpRecord.Scope,
			ExpiresAt: mcpRecord.ExpiresAt,
			Token:     mcpKey.Raw,
		},
	})
}
