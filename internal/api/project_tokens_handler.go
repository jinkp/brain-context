package api

import (
	"net/http"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

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
