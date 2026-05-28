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
