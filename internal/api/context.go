package api

import (
	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const (
	contextTenantIDKey  = "tenant_id"
	contextAuthKeyKey   = "auth_key"
	contextScopeKey     = "token_scope"
	contextProjectIDKey = "project_id"
)

func setAuthContext(c echo.Context, record store.APIKeyRecord) {
	c.Set(contextTenantIDKey, record.TenantID)
	c.Set(contextAuthKeyKey, record)
	c.Set(contextScopeKey, record.Scope)
	if record.ProjectID != nil {
		c.Set(contextProjectIDKey, *record.ProjectID)
	}
}

func tenantIDFromContext(c echo.Context) (uuid.UUID, bool) {
	tenantID, ok := c.Get(contextTenantIDKey).(uuid.UUID)
	return tenantID, ok
}

func authKeyFromContext(c echo.Context) (store.APIKeyRecord, bool) {
	record, ok := c.Get(contextAuthKeyKey).(store.APIKeyRecord)
	return record, ok
}

func scopeFromContext(c echo.Context) (auth.TokenScope, bool) {
	scope, ok := c.Get(contextScopeKey).(auth.TokenScope)
	return scope, ok
}

func projectIDFromContext(c echo.Context) (uuid.UUID, bool) {
	projectID, ok := c.Get(contextProjectIDKey).(uuid.UUID)
	return projectID, ok
}
