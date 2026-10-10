package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
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

// parseTTL parses a human-friendly TTL string.
// Supported formats: Go durations ("72h", "30m") and day shorthand ("7d", "30d").
func parseTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("ttl cannot be empty")
	}
	// Support "Nd" shorthand for days
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid ttl %q: days must be a positive integer", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid ttl %q: use Go duration (e.g. 72h) or day shorthand (e.g. 7d)", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("ttl must be positive")
	}
	return d, nil
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

	// Parse optional body parameters
	var req struct {
		MaxUses *int   `json:"max_uses"` // nil = not provided (default 1), 0 = unlimited, N = limit
		TTL     string `json:"ttl"`      // Go duration string, e.g. "72h", "7d" — default 24h
	}
	// Bind is best-effort; missing body means defaults apply
	_ = c.Bind(&req)

	maxUses := 1 // default: single-use
	if req.MaxUses != nil {
		maxUses = *req.MaxUses
		if maxUses < 0 {
			maxUses = 0
		}
	}

	ttl := defaultInviteTTL
	if req.TTL != "" {
		parsed, pErr := parseTTL(req.TTL)
		if pErr != nil {
			return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", pErr.Error())
		}
		ttl = parsed
	}

	// Make sure the project has an embed API key configured
	if _, err := h.store.GetEmbedAPIKey(c.Request().Context(), tenantID, projectID); err != nil {
		return writeError(c, http.StatusBadRequest, "EMBED_KEY_MISSING",
			"set an embed API key first: brain register --project <name> --embed-api-key <key>")
	}

	invite, err := h.store.CreateInviteCode(c.Request().Context(), tenantID, projectID, ttl, maxUses)
	if err != nil {
		return handleStoreError(c, err)
	}

	usesLabel := "unlimited"
	if invite.MaxUses > 0 {
		usesLabel = fmt.Sprintf("%d", invite.MaxUses)
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"code":       invite.Code,
		"expires_at": invite.ExpiresAt,
		"max_uses":   invite.MaxUses,
		"use_count":  invite.UseCount,
		"message":    fmt.Sprintf("Share this code with your team. It expires at %s and can be used %s time(s).", invite.ExpiresAt.Format(time.RFC3339), usesLabel),
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
		"mcp_read_key":      config.MCPReadKey,
		"message":           "Store your mcp_read_key securely — shown once.",
	})
}
