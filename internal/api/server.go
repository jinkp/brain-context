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
	publicRL := newPublicRateLimiter()
	authRL := newAuthRateLimiter()

	apiGroup := e.Group("/api")

	// Public endpoints — rate-limited to prevent abuse
	apiGroup.POST("/tenants/register", h.RegisterTenant, publicRL)
	apiGroup.POST("/auth/login", h.Login, publicRL)

	authed := apiGroup.Group("", AuthMiddleware(st))
	authed.POST("/auth/renew", h.RenewKey, authRL)

	tenantOnly := authed.Group("", RequireScopes(auth.ScopeTenant))
	tenantOnly.DELETE("/auth/keys/:id", h.RevokeKey)
	tenantOnly.POST("/projects", h.CreateProject)
	tenantOnly.GET("/projects/:id", h.GetProject)
	tenantOnly.DELETE("/projects/:id", h.DeleteProject)
	tenantOnly.PATCH("/projects/:id/rename", h.RenameProject)
	tenantOnly.PATCH("/projects/:id/embed-key", h.UpdateEmbedKey)
	tenantOnly.GET("/metrics", h.GetMetrics)
	tenantOnly.POST("/projects/:id/tokens", h.CreateProjectTokens, authRL)
	tenantOnly.GET("/projects/:id/tokens", h.ListProjectTokens)
	tenantOnly.POST("/projects/:id/tokens/renew", h.RenewProjectTokens, authRL)
	tenantOnly.POST("/projects/:id/invite", h.CreateInvite)

	// Public — no auth needed, the invite code IS the auth — rate-limited
	apiGroup.POST("/invite/redeem", h.RedeemInvite, publicRL)

	projectOnly := authed.Group("", RequireScopes(auth.ScopeProject))
	projectOnly.POST("/projects/:id/chunks", h.UploadChunks)

	// Read-only endpoints require a valid scoped token (ScopeTenant, ScopeProject,
	// or ScopeMCPRead). Token auth acts as the primary rate-control mechanism.
	//
	// Rate limiting evaluation (server-side embedding load):
	// SearchContext now generates the query embedding server-side, adding an
	// external provider call per request. Current mitigation: token auth limits
	// blast radius per client. If embedding latency becomes a bottleneck under
	// load, consider adding a per-project or per-token rate limiter here using
	// newAuthRateLimiter() with a higher burst allowance (e.g. Rate=10, Burst=20).
	// Track via the embed_ms column in query_logs before adjusting.
	readOnly := authed.Group("", RequireScopes(auth.ScopeTenant, auth.ScopeProject, auth.ScopeMCPRead))
	readOnly.POST("/projects/:id/context/search", h.SearchContext)
	readOnly.GET("/projects/:id/files/summary", h.GetFileSummary)
	readOnly.GET("/projects/:id/files/related", h.GetRelatedFiles)
	readOnly.POST("/projects/:id/impact", h.FindImpact)

	return e
}
