package api

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/labstack/echo/v4"
)

// adminSecretMiddleware protects admin endpoints with a static secret read from
// the BRAIN_ADMIN_SECRET environment variable. If the env var is not set, the
// endpoint is disabled (503) to prevent accidental exposure.
func adminSecretMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			secret := strings.TrimSpace(os.Getenv("BRAIN_ADMIN_SECRET"))
			if secret == "" {
				return writeError(c, http.StatusServiceUnavailable,
					"ADMIN_DISABLED",
					"admin endpoints are disabled — set BRAIN_ADMIN_SECRET on the server to enable")
			}
			provided := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
			if strings.TrimSpace(provided) != secret {
				return writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "invalid admin secret")
			}
			return next(c)
		}
	}
}

// registrationOpen returns true if tenant self-registration is allowed.
// Controlled by BRAIN_REGISTRATION env var: "open" (default) or "closed".
func registrationOpen() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv("BRAIN_REGISTRATION"))) != "closed"
}

type registerTenantRequest struct {
	Name string `json:"name"`
}

type loginRequest struct {
	APIKey string `json:"api_key"`
}

type tokenPayload struct {
	ID        string          `json:"id"`
	Prefix    string          `json:"prefix"`
	Scope     auth.TokenScope `json:"scope"`
	ExpiresAt time.Time       `json:"expires_at"`
	Token     string          `json:"token,omitempty"`
}

func (h *Handler) RegisterTenant(c echo.Context) error {
	if !registrationOpen() {
		return writeError(c, http.StatusForbidden, "REGISTRATION_CLOSED",
			"tenant registration is currently closed — contact your admin")
	}

	var req registerTenantRequest
	if err := c.Bind(&req); err != nil {
		return writeError(c, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "tenant name is required")
	}

	issued, err := auth.IssueToken(auth.ScopeTenant, time.Now().UTC())
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "failed to issue tenant api key")
	}

	tenant, record, err := h.store.CreateTenantWithAPIKey(c.Request().Context(), req.Name, issued)
	if err != nil {
		log.Printf("[AUDIT] tenant register FAILED | ip=%s name=%q err=%v", c.RealIP(), req.Name, err)
		return handleStoreError(c, err)
	}

	log.Printf("[AUDIT] tenant register OK | ip=%s name=%q tenant_id=%s", c.RealIP(), req.Name, tenant.ID)

	return c.JSON(http.StatusCreated, map[string]any{
		"tenant": tenant,
		"api_key": tokenPayload{
			ID:        record.ID.String(),
			Prefix:    record.KeyPrefix,
			Scope:     record.Scope,
			ExpiresAt: record.ExpiresAt,
			Token:     issued.Raw,
		},
	})
}

// RecoverTenantKey issues a fresh tenant API key for the tenant that owns
// the provided expired or revoked key_prefix. Protected by adminSecretMiddleware.
// POST /api/admin/recover-tenant-key
func (h *Handler) RecoverTenantKey(c echo.Context) error {
	var req struct {
		KeyPrefix string `json:"key_prefix"`
	}
	if err := c.Bind(&req); err != nil || strings.TrimSpace(req.KeyPrefix) == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "key_prefix is required")
	}

	issued, err := auth.IssueToken(auth.ScopeTenant, time.Now().UTC())
	if err != nil {
		return writeError(c, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "failed to issue new tenant key")
	}

	record, err := h.store.RecoverTenantKey(c.Request().Context(), strings.TrimSpace(req.KeyPrefix), issued)
	if err != nil {
		log.Printf("[AUDIT] recover-tenant-key FAILED | ip=%s prefix=%q err=%v", c.RealIP(), req.KeyPrefix, err)
		return handleStoreError(c, err)
	}

	log.Printf("[AUDIT] recover-tenant-key OK | ip=%s prefix=%q tenant_id=%s new_prefix=%s",
		c.RealIP(), req.KeyPrefix, record.TenantID, record.KeyPrefix)

	return c.JSON(http.StatusOK, map[string]any{
		"api_key": tokenPayload{
			ID:        record.ID.String(),
			Prefix:    record.KeyPrefix,
			Scope:     record.Scope,
			ExpiresAt: record.ExpiresAt,
			Token:     issued.Raw,
		},
		"message": "New tenant API key issued. Old key has been revoked. Run brain login with the new key.",
	})
}

func (h *Handler) Login(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return writeError(c, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
	}
	req.APIKey = strings.TrimSpace(req.APIKey)
	if req.APIKey == "" {
		return writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", "api_key is required")
	}

	record, err := h.store.AuthenticateAPIKey(c.Request().Context(), req.APIKey)
	if err != nil {
		return handleStoreError(c, err)
	}

	response := map[string]any{
		"tenant_id":  record.TenantID,
		"scope":      record.Scope,
		"key_id":     record.ID,
		"key_prefix": record.KeyPrefix,
		"expires_at": record.ExpiresAt,
	}
	if record.ProjectID != nil {
		response["project_id"] = *record.ProjectID
	}

	return c.JSON(http.StatusOK, response)
}
