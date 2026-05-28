package api

import (
	"log"
	"net/http"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/crypto"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const defaultInviteTTL = 24 * time.Hour

// safeCodePrefix returns the first 16 characters of an invite code for audit
// logging without exposing the full secret.
func safeCodePrefix(code string) string {
	if len(code) <= 16 {
		return code[:len(code)/2] + "***"
	}
	return code[:16] + "***"
}

// CreateInvite generates an invite code for a project.
// POST /api/projects/:id/invite
func (h *Handler) CreateInvite(c echo.Context) error {
	tenantID, ok := tenantIDFromContext(c)
	if !ok {
		return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing tenant context")
	}

	projectID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid project id")
	}

	// Make sure the project has an embed API key configured
	if _, err := h.store.GetEmbedAPIKey(c.Request().Context(), tenantID, projectID); err != nil {
		return writeError(c, http.StatusBadRequest, "EMBED_KEY_MISSING",
			"set an embed API key first: brain register --project <name> --embed-api-key <key>")
	}

	invite, err := h.store.CreateInviteCode(c.Request().Context(), tenantID, projectID, defaultInviteTTL)
	if err != nil {
		return handleStoreError(c, err)
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"code":       invite.Code,
		"expires_at": invite.ExpiresAt,
		"message":    "Share this code with your developer. It expires in 24 hours and can only be used once.",
	})
}

// RedeemInvite validates an invite code and returns the project join config.
// The embed API key is returned encrypted — the CLI stores it and decrypts
// it in memory only when needed.
// POST /api/invite/redeem
func (h *Handler) RedeemInvite(c echo.Context) error {
	var req struct {
		Code string `json:"code"`
	}
	if err := c.Bind(&req); err != nil || req.Code == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "code is required")
	}

	// Verify encryption is available before redeeming
	if !crypto.IsConfigured() {
		return writeError(c, http.StatusServiceUnavailable, "ENCRYPTION_NOT_CONFIGURED",
			"server encryption is not configured — contact your admin")
	}

	config, err := h.store.RedeemInviteCode(c.Request().Context(), req.Code)
	if err != nil {
		log.Printf("[AUDIT] invite redeem FAILED | ip=%s code_prefix=%s err=%v",
			c.RealIP(), safeCodePrefix(req.Code), err)
		return handleStoreError(c, err)
	}

	log.Printf("[AUDIT] invite redeem OK | ip=%s project=%s code_prefix=%s",
		c.RealIP(), config.ProjectID, safeCodePrefix(req.Code))

	return c.JSON(http.StatusOK, map[string]any{
		"project_id":        config.ProjectID,
		"project_name":      config.ProjectName,
		"embed_model":       config.EmbedModel,
		"embed_dimensions":  config.EmbedDimensions,
		"embed_api_key_enc": config.EmbedAPIKeyEnc,
		"mcp_read_key":      config.MCPReadKey,
		"message":           "Store your mcp_read_key securely — shown once.",
	})
}
