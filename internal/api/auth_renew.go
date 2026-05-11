package api

import (
	"net/http"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func (h *Handler) RenewKey(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}
	currentKey, ok := authKeyFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing auth key context")
	}

	issued, err := auth.IssueToken(currentKey.Scope, time.Now().UTC())
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "failed to renew api key")
	}

	record, err := h.store.RenewAPIKey(c.Request().Context(), currentKey.ID, tenantID, issued)
	if err != nil {
		return handleStoreError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"api_key": tokenPayload{
			ID:        record.ID.String(),
			Prefix:    record.KeyPrefix,
			Scope:     record.Scope,
			ExpiresAt: record.ExpiresAt,
			Token:     issued.Raw,
		},
	})
}

func (h *Handler) RevokeKey(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid api key id")
	}

	if err := h.store.RevokeAPIKey(c.Request().Context(), keyID, tenantID); err != nil {
		return handleStoreError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}
