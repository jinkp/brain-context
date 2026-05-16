package api

import (
	"github.com/Gentleman-Programming/brain-context/internal/auth"
	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Handler struct {
	store *store.Store
}

func NewServer(st *store.Store) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())

	h := &Handler{store: st}
	apiGroup := e.Group("/api")
	apiGroup.POST("/tenants/register", h.RegisterTenant)
	apiGroup.POST("/auth/login", h.Login)

	authed := apiGroup.Group("", AuthMiddleware(st))
	authed.POST("/auth/renew", h.RenewKey)

	tenantOnly := authed.Group("", RequireScopes(auth.ScopeTenant))
	tenantOnly.DELETE("/auth/keys/:id", h.RevokeKey)
	tenantOnly.POST("/projects", h.CreateProject)
	tenantOnly.GET("/projects/:id", h.GetProject)
	tenantOnly.DELETE("/projects/:id", h.DeleteProject)
	tenantOnly.PATCH("/projects/:id/embed-key", h.UpdateEmbedKey)
	tenantOnly.GET("/admin/metrics", h.GetMetrics)
	tenantOnly.POST("/projects/:id/tokens", h.CreateProjectTokens)
	tenantOnly.GET("/projects/:id/tokens", h.ListProjectTokens)
	tenantOnly.POST("/projects/:id/tokens/renew", h.RenewProjectTokens)
	tenantOnly.POST("/projects/:id/invite", h.CreateInvite)

	// Public — no auth needed, the invite code IS the auth
	apiGroup.POST("/invite/redeem", h.RedeemInvite)

	projectOnly := authed.Group("", RequireScopes(auth.ScopeProject))
	projectOnly.POST("/projects/:id/chunks", h.UploadChunks)

	readOnly := authed.Group("", RequireScopes(auth.ScopeTenant, auth.ScopeProject, auth.ScopeMCPRead))
	readOnly.POST("/projects/:id/context/search", h.SearchContext)
	readOnly.GET("/projects/:id/files/summary", h.GetFileSummary)
	readOnly.GET("/projects/:id/files/related", h.GetRelatedFiles)
	readOnly.POST("/projects/:id/impact", h.FindImpact)

	return e
}
